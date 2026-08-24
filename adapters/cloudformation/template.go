package cloudformation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

var logicalIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,254}$`)

var templateSections = map[string]struct{}{
	"AWSTemplateFormatVersion": {},
	"Conditions":               {},
	"Description":              {},
	"Mappings":                 {},
	"Metadata":                 {},
	"Outputs":                  {},
	"Parameters":               {},
	"Resources":                {},
	"Rules":                    {},
	"Transform":                {},
}

// ParseTemplate strictly parses one synthesized CloudFormation JSON template.
// Intrinsics and transforms are retained but never evaluated by this phase.
func (loader Loader) ParseTemplate(ctx context.Context, source string, reader io.Reader) (Template, error) {
	limits, err := loader.normalizedLimits()
	if err != nil {
		return Template{}, err
	}
	if err := ctx.Err(); err != nil {
		return Template{}, fmt.Errorf("parse CloudFormation template %q: %w", source, err)
	}
	contents, err := readBounded(reader, limits.MaxTemplateBytes)
	if err != nil {
		return Template{}, fmt.Errorf("read CloudFormation template %q: %w", source, err)
	}
	if err := ctx.Err(); err != nil {
		return Template{}, fmt.Errorf("parse CloudFormation template %q: %w", source, err)
	}
	template, err := parseTemplateBytes(source, contents, limits)
	if err != nil {
		return Template{}, fmt.Errorf("parse CloudFormation template %q: %w", source, err)
	}
	return template, nil
}

func parseTemplateBytes(source string, contents []byte, limits Limits) (Template, error) {
	if len(contents) == 0 {
		return Template{}, errors.New("template is empty")
	}
	if err := validateStrictJSON(contents, limits); err != nil {
		return Template{}, err
	}
	if trimmed := bytes.TrimSpace(contents); len(trimmed) == 0 || trimmed[0] != '{' {
		return Template{}, errors.New("template root must be a JSON object")
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(contents, &document); err != nil {
		return Template{}, fmt.Errorf("decode template object: %w", err)
	}
	if document == nil {
		return Template{}, errors.New("template root must be a JSON object")
	}
	for section := range document {
		if _, allowed := templateSections[section]; !allowed {
			return Template{}, fmt.Errorf("template contains unknown top-level section %q", section)
		}
	}

	template := Template{Source: source}
	if raw, ok := document["AWSTemplateFormatVersion"]; ok {
		if isJSONNull(raw) {
			return Template{}, errors.New("AWSTemplateFormatVersion must be a string")
		}
		if err := json.Unmarshal(raw, &template.FormatVersion); err != nil {
			return Template{}, errors.New("AWSTemplateFormatVersion must be a string")
		}
		if template.FormatVersion != "2010-09-09" {
			return Template{}, fmt.Errorf("unsupported AWSTemplateFormatVersion %q", template.FormatVersion)
		}
	}
	if raw, ok := document["Description"]; ok {
		if isJSONNull(raw) {
			return Template{}, errors.New("Description must be a string")
		}
		if err := json.Unmarshal(raw, &template.Description); err != nil {
			return Template{}, errors.New("Description must be a string")
		}
	}
	if raw, ok := document["Metadata"]; ok {
		if err := requireJSONObject("Metadata", raw); err != nil {
			return Template{}, err
		}
		template.Metadata = cloneRaw(raw)
	}

	var err error
	if template.Parameters, err = parseNamedSection(document, "Parameters"); err != nil {
		return Template{}, err
	}
	if template.Rules, err = parseNamedSection(document, "Rules"); err != nil {
		return Template{}, err
	}
	if template.Mappings, err = parseNamedSection(document, "Mappings"); err != nil {
		return Template{}, err
	}
	if template.Conditions, err = parseNamedSection(document, "Conditions"); err != nil {
		return Template{}, err
	}
	if template.Outputs, err = parseNamedSection(document, "Outputs"); err != nil {
		return Template{}, err
	}
	if raw, ok := document["Transform"]; ok {
		if isJSONNull(raw) {
			return Template{}, errors.New("Transform must not be null")
		}
		template.Transform = cloneRaw(raw)
	}

	resourcesRaw, ok := document["Resources"]
	if !ok {
		return Template{}, errors.New("Resources is required")
	}
	var resources map[string]json.RawMessage
	if err := json.Unmarshal(resourcesRaw, &resources); err != nil || resources == nil {
		return Template{}, errors.New("Resources must be a JSON object")
	}
	if len(resources) == 0 {
		return Template{}, errors.New("Resources must contain at least one resource")
	}
	if len(resources) > limits.MaxResources {
		return Template{}, fmt.Errorf("resource count %d exceeds the limit of %d", len(resources), limits.MaxResources)
	}

	logicalIDs := make([]string, 0, len(resources))
	for logicalID := range resources {
		logicalIDs = append(logicalIDs, logicalID)
	}
	sort.Strings(logicalIDs)
	template.Resources = make([]Resource, 0, len(logicalIDs))
	for _, logicalID := range logicalIDs {
		if !logicalIDPattern.MatchString(logicalID) {
			return Template{}, fmt.Errorf("resource logical ID %q must start with a letter and contain only alphanumeric characters (maximum 255 characters)", logicalID)
		}
		definitionRaw := resources[logicalID]
		var definition map[string]json.RawMessage
		if err := json.Unmarshal(definitionRaw, &definition); err != nil || definition == nil {
			return Template{}, fmt.Errorf("resource %q must be a JSON object", logicalID)
		}
		var resourceType string
		if err := json.Unmarshal(definition["Type"], &resourceType); err != nil || strings.TrimSpace(resourceType) == "" {
			return Template{}, fmt.Errorf("resource %q Type must be a non-empty string", logicalID)
		}
		if resourceType != strings.TrimSpace(resourceType) {
			return Template{}, fmt.Errorf("resource %q Type must not have surrounding whitespace", logicalID)
		}
		resource := Resource{
			LogicalID:  logicalID,
			Type:       resourceType,
			Definition: cloneRaw(definitionRaw),
			Provenance: Provenance{TemplateFile: source, LogicalID: logicalID},
		}
		if properties, exists := definition["Properties"]; exists {
			if err := requireJSONObject("resource "+logicalID+" Properties", properties); err != nil {
				return Template{}, err
			}
			resource.Properties = cloneRaw(properties)
		}
		template.Resources = append(template.Resources, resource)
	}
	return template, nil
}

func parseNamedSection(document map[string]json.RawMessage, name string) ([]NamedValue, error) {
	raw, ok := document[name]
	if !ok {
		return nil, nil
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return nil, fmt.Errorf("%s must be a JSON object", name)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]NamedValue, 0, len(keys))
	for _, key := range keys {
		result = append(result, NamedValue{Name: key, Value: cloneRaw(values[key])})
	}
	return result, nil
}

func requireJSONObject(name string, raw json.RawMessage) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return fmt.Errorf("%s must be a JSON object", name)
	}
	return nil
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}

func readBounded(reader io.Reader, maximum int64) ([]byte, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > maximum {
		return nil, fmt.Errorf("input exceeds the %d-byte size limit", maximum)
	}
	return contents, nil
}
