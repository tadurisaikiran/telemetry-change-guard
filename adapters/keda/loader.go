// Package keda discovers Prometheus dependencies in KEDA ScaledObject
// resources. It retains query and workload identity only; connection and
// authentication metadata never enters the normalized evidence model.
package keda

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	tcgpromql "github.com/tadurisaikiran/telemetry-change-guard/pkg/promql"
)

const maxManifestBytes = 8 << 20

// Loader controls whether unresolved scaler evidence is required.
type Loader struct {
	Required bool
}

// LoadFile reads one local Kubernetes manifest containing KEDA resources.
func (loader Loader) LoadFile(ctx context.Context, path string) (domain.Discovery, error) {
	if err := ctx.Err(); err != nil {
		return domain.Discovery{}, fmt.Errorf("load KEDA manifest %q: %w", path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("open KEDA manifest %q: %w", path, err)
	}
	defer file.Close()

	discovery, err := loader.Parse(ctx, filepath.Clean(path), file)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("load KEDA manifest %q: %w", path, err)
	}
	return discovery, nil
}

// Parse discovers every Prometheus trigger in every ScaledObject document.
// Unrelated Kubernetes resources in the same multi-document manifest are
// ignored, which permits common deployment bundles without weakening
// validation of any ScaledObject that is present.
func (loader Loader) Parse(ctx context.Context, source string, reader io.Reader) (domain.Discovery, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maxManifestBytes+1))
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("read KEDA manifest: %w", err)
	}
	if len(contents) > maxManifestBytes {
		return domain.Discovery{}, fmt.Errorf("KEDA manifest exceeds the %d-byte size limit", maxManifestBytes)
	}

	decoder := yaml.NewDecoder(strings.NewReader(string(contents)))
	var discovery domain.Discovery
	documentIndex := 0
	scaledObjectCount := 0
	for {
		if err := ctx.Err(); err != nil {
			return domain.Discovery{}, err
		}
		var document manifest
		if err := decoder.Decode(&document); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return domain.Discovery{}, fmt.Errorf("decode YAML document %d: %w", documentIndex+1, err)
		}
		documentIndex++
		if document.empty() || document.Kind != "ScaledObject" {
			continue
		}
		scaledObjectCount++
		additional, err := loader.discoverScaledObject(source, documentIndex, document)
		if err != nil {
			return domain.Discovery{}, err
		}
		discovery.Append(additional)
	}

	if documentIndex == 0 {
		return domain.Discovery{}, fmt.Errorf("KEDA manifest is empty")
	}
	if scaledObjectCount == 0 {
		return domain.Discovery{}, fmt.Errorf("KEDA manifest contains no ScaledObject resources")
	}
	return discovery, nil
}

func (loader Loader) discoverScaledObject(source string, documentIndex int, document manifest) (domain.Discovery, error) {
	prefix := fmt.Sprintf("document %d ScaledObject", documentIndex)
	if document.APIVersion != "keda.sh/v1alpha1" {
		return domain.Discovery{}, fmt.Errorf("%s apiVersion must be keda.sh/v1alpha1", prefix)
	}
	name := strings.TrimSpace(document.Metadata.Name)
	if name == "" {
		return domain.Discovery{}, fmt.Errorf("%s metadata.name is required", prefix)
	}
	target := strings.TrimSpace(document.Spec.ScaleTargetRef.Name)
	if target == "" {
		return domain.Discovery{}, fmt.Errorf("%s spec.scaleTargetRef.name is required", prefix)
	}
	if len(document.Spec.Triggers) == 0 {
		return domain.Discovery{}, fmt.Errorf("%s spec.triggers must contain at least one trigger", prefix)
	}
	for triggerIndex, trigger := range document.Spec.Triggers {
		if strings.TrimSpace(trigger.Type) == "" {
			return domain.Discovery{}, fmt.Errorf("%s spec.triggers[%d].type is required", prefix, triggerIndex)
		}
		if trigger.Metadata == nil {
			return domain.Discovery{}, fmt.Errorf("%s spec.triggers[%d].metadata is required", prefix, triggerIndex)
		}
	}

	namespace := strings.TrimSpace(document.Metadata.Namespace)
	if namespace == "" {
		namespace = "default"
	}
	criticality := autoscalerCriticality(document.Metadata.Labels)
	var discovery domain.Discovery
	for triggerIndex, trigger := range document.Spec.Triggers {
		if !strings.EqualFold(strings.TrimSpace(trigger.Type), "prometheus") {
			continue
		}
		query := strings.TrimSpace(trigger.Metadata["query"])
		location := domain.SourceLocation{File: source, Line: trigger.line, Column: trigger.column}
		consumer := domain.Consumer{
			ID: fmt.Sprintf(
				"keda:%s:%d:%s:%s:prometheus:%d",
				source,
				documentIndex,
				namespace,
				name,
				triggerIndex,
			),
			Kind:        domain.ConsumerKindAutoscaler,
			Name:        target,
			Source:      location,
			Criticality: criticality,
			Expression:  query,
			Metadata: map[string]string{
				"api_version":   document.APIVersion,
				"namespace":     namespace,
				"scale_target":  target,
				"scaled_object": name,
				"trigger_index": fmt.Sprint(triggerIndex),
			},
		}
		if triggerName := strings.TrimSpace(trigger.Name); triggerName != "" {
			consumer.Metadata["trigger_name"] = triggerName
		}

		if query == "" {
			consumer.Unresolved = true
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  "keda",
				Source:   location,
				Message:  fmt.Sprintf("%s spec.triggers[%d].metadata.query is required for a Prometheus trigger", prefix, triggerIndex),
				Required: loader.Required,
			})
			discovery.Consumers = append(discovery.Consumers, consumer)
			continue
		}

		analysis, analysisErr := tcgpromql.Analyze(query)
		if analysisErr != nil || len(analysis.Unresolved) != 0 {
			consumer.Unresolved = true
			message := "PromQL expression is unresolved"
			if analysisErr != nil {
				message = analysisErr.Error()
			} else {
				message = analysis.Unresolved[0].Reason
			}
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  "keda",
				Source:   location,
				Message:  message,
				Required: loader.Required,
			})
		} else {
			for _, reference := range analysis.References {
				reference.ConsumerID = consumer.ID
				reference.Evidence.Source = location
				discovery.References = append(discovery.References, reference)
			}
		}
		discovery.Consumers = append(discovery.Consumers, consumer)
	}
	return discovery, nil
}

func autoscalerCriticality(labels map[string]string) domain.Criticality {
	for _, key := range []string{"environment", "env", "app.kubernetes.io/environment"} {
		switch strings.ToLower(strings.TrimSpace(labels[key])) {
		case "prod", "production":
			return domain.CriticalityCritical
		}
	}
	return domain.CriticalityHigh
}

type manifest struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   metadata `yaml:"metadata"`
	Spec       spec     `yaml:"spec"`
}

func (document manifest) empty() bool {
	return document.APIVersion == "" && document.Kind == "" && document.Metadata.Name == "" &&
		document.Spec.ScaleTargetRef.Name == "" && len(document.Spec.Triggers) == 0
}

type metadata struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Labels    map[string]string `yaml:"labels"`
}

type spec struct {
	ScaleTargetRef scaleTargetRef `yaml:"scaleTargetRef"`
	Triggers       []scaleTrigger `yaml:"triggers"`
}

type scaleTargetRef struct {
	Name string `yaml:"name"`
}

type scaleTrigger struct {
	Type     string            `yaml:"type"`
	Name     string            `yaml:"name"`
	Metadata map[string]string `yaml:"metadata"`
	line     int
	column   int
}

func (trigger *scaleTrigger) UnmarshalYAML(node *yaml.Node) error {
	type plainTrigger scaleTrigger
	var decoded plainTrigger
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*trigger = scaleTrigger(decoded)
	trigger.line = node.Line
	trigger.column = node.Column
	return nil
}
