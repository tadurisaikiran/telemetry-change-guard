// Package pyrra discovers PromQL references in Pyrra ServiceLevelObjective
// resources.
package pyrra

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	tmrpromql "github.com/tadurisaikiran/telemetry-migration-readiness/pkg/promql"
)

const maxPyrraBytes = 8 << 20

// Loader controls whether unresolved Pyrra evidence is required.
type Loader struct {
	Required bool
}

// LoadFile reads one local Pyrra file.
func (loader Loader) LoadFile(ctx context.Context, path string) (domain.Discovery, error) {
	if err := ctx.Err(); err != nil {
		return domain.Discovery{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("open Pyrra SLO %q: %w", path, err)
	}
	defer file.Close()
	result, err := loader.Parse(ctx, filepath.Clean(path), file)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("load Pyrra SLO %q: %w", path, err)
	}
	return result, nil
}

// Parse discovers every metric expression under spec.indicator.
func (loader Loader) Parse(ctx context.Context, source string, reader io.Reader) (domain.Discovery, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maxPyrraBytes+1))
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("read Pyrra SLO: %w", err)
	}
	if len(contents) > maxPyrraBytes {
		return domain.Discovery{}, fmt.Errorf("Pyrra SLO exceeds the %d-byte size limit", maxPyrraBytes)
	}

	decoder := yaml.NewDecoder(strings.NewReader(string(contents)))
	var discovery domain.Discovery
	documentIndex := 0
	for {
		if err := ctx.Err(); err != nil {
			return domain.Discovery{}, err
		}
		var document yaml.Node
		if err := decoder.Decode(&document); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return domain.Discovery{}, fmt.Errorf("decode Pyrra document %d: %w", documentIndex+1, err)
		}
		if len(document.Content) == 0 {
			continue
		}
		documentIndex++
		mapping := document.Content[0]
		kind := scalarValue(mappingValue(mapping, "kind"))
		if kind != "ServiceLevelObjective" {
			return domain.Discovery{}, fmt.Errorf("Pyrra document %d kind must be ServiceLevelObjective", documentIndex)
		}
		metadata := mappingValue(mapping, "metadata")
		name := scalarValue(mappingValue(metadata, "name"))
		if strings.TrimSpace(name) == "" {
			return domain.Discovery{}, fmt.Errorf("Pyrra document %d metadata.name is required", documentIndex)
		}
		spec := mappingValue(mapping, "spec")
		indicator := mappingValue(spec, "indicator")
		queries := collectScalarValues(indicator, "metric")
		consumer := domain.Consumer{
			ID:          fmt.Sprintf("pyrra:%s:%s:%d", source, name, documentIndex),
			Kind:        domain.ConsumerKindSLO,
			Name:        name,
			Source:      domain.SourceLocation{File: source, Line: mapping.Line, Column: mapping.Column},
			Criticality: domain.CriticalityCritical,
			Expression:  strings.Join(queries, "\n"),
			Metadata: map[string]string{
				"api_version": scalarValue(mappingValue(mapping, "apiVersion")),
			},
		}
		if len(queries) == 0 {
			consumer.Unresolved = true
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  "pyrra",
				Source:   consumer.Source,
				Message:  "SLO has no metric expressions under spec.indicator",
				Required: loader.Required,
			})
		}
		for _, query := range queries {
			analysis, analysisErr := tmrpromql.Analyze(query)
			if analysisErr != nil || len(analysis.Unresolved) != 0 {
				consumer.Unresolved = true
				message := "PromQL expression is unresolved"
				if analysisErr != nil {
					message = analysisErr.Error()
				} else {
					message = analysis.Unresolved[0].Reason
				}
				discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
					Adapter:  "pyrra",
					Source:   consumer.Source,
					Message:  message,
					Required: loader.Required,
				})
				continue
			}
			for _, reference := range analysis.References {
				reference.ConsumerID = consumer.ID
				reference.Evidence.Source = consumer.Source
				discovery.References = append(discovery.References, reference)
			}
		}
		discovery.Consumers = append(discovery.Consumers, consumer)
	}

	if documentIndex == 0 {
		return domain.Discovery{}, fmt.Errorf("Pyrra SLO file is empty")
	}
	return discovery, nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

func scalarValue(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
}

func collectScalarValues(node *yaml.Node, key string) []string {
	if node == nil {
		return nil
	}
	var values []string
	switch node.Kind {
	case yaml.MappingNode:
		for index := 0; index < len(node.Content); index += 2 {
			childKey := node.Content[index]
			childValue := node.Content[index+1]
			if childKey.Value == key && childValue.Kind == yaml.ScalarNode && strings.TrimSpace(childValue.Value) != "" {
				values = append(values, childValue.Value)
			}
			values = append(values, collectScalarValues(childValue, key)...)
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			values = append(values, collectScalarValues(child, key)...)
		}
	}
	return values
}
