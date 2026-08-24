package cloudformation

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParseTemplatePreservesUnresolvedDataInDeterministicOrder(t *testing.T) {
	t.Parallel()

	input := `{
  "AWSTemplateFormatVersion": "2010-09-09",
  "Description": "alarm stack",
  "Metadata": {"team": "payments"},
  "Parameters": {"Zed": {"Type": "String"}, "Alpha": {"Type": "String"}},
  "Conditions": {"IsProd": {"Fn::Equals": [{"Ref": "Stage"}, "prod"]}},
  "Transform": ["AWS::LanguageExtensions"],
  "Resources": {
    "SecondAlarm": {"Type": "AWS::CloudWatch::Alarm", "Properties": {"MetricName": {"Ref": "Metric"}}},
    "FirstTopic": {"Type": "AWS::SNS::Topic"}
  },
  "Outputs": {"Topic": {"Value": {"Ref": "FirstTopic"}}}
}`
	loader := Loader{}
	template, err := loader.ParseTemplate(context.Background(), "template.json", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if template.FormatVersion != "2010-09-09" || template.Description != "alarm stack" {
		t.Fatalf("template metadata = %#v", template)
	}
	if got := namedKeys(template.Parameters); !reflect.DeepEqual(got, []string{"Alpha", "Zed"}) {
		t.Fatalf("parameter order = %v", got)
	}
	if got := resourceIDs(template.Resources); !reflect.DeepEqual(got, []string{"FirstTopic", "SecondAlarm"}) {
		t.Fatalf("resource order = %v", got)
	}
	if !strings.Contains(string(template.Resources[1].Properties), `"Ref": "Metric"`) {
		t.Fatalf("intrinsic was not preserved: %s", template.Resources[1].Properties)
	}
	if !strings.Contains(string(template.Transform), "AWS::LanguageExtensions") {
		t.Fatalf("transform was not preserved: %s", template.Transform)
	}
	if template.Resources[1].Provenance.TemplateFile != "template.json" ||
		template.Resources[1].Provenance.LogicalID != "SecondAlarm" {
		t.Fatalf("provenance = %#v", template.Resources[1].Provenance)
	}

	again, err := loader.ParseTemplate(context.Background(), "template.json", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(template, again) {
		t.Fatalf("repeated parse was not deterministic\nfirst: %#v\nsecond: %#v", template, again)
	}
}

func TestParseTemplateRejectsInvalidDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []byte
		message string
	}{
		{name: "yaml", input: []byte("Resources:\n  Topic:\n    Type: AWS::SNS::Topic\n"), message: "decode JSON"},
		{name: "trailing", input: []byte(`{"Resources":{"A":{"Type":"X"}}} {}`), message: "more than one JSON value"},
		{name: "duplicate nested key", input: []byte(`{"Resources":{"A":{"Type":"X","Properties":{"Name":"one","Name":"two"}}}}`), message: `duplicate key "Name"`},
		{name: "invalid UTF-8", input: append([]byte(`{"Resources":{"A":{"Type":"`), 0xff), message: "not valid UTF-8"},
		{name: "array root", input: []byte(`[]`), message: "root must be a JSON object"},
		{name: "unknown section", input: []byte(`{"Resources":{"A":{"Type":"X"}},"Unexpected":{}}`), message: `unknown top-level section "Unexpected"`},
		{name: "unsupported format", input: []byte(`{"AWSTemplateFormatVersion":"2000-01-01","Resources":{"A":{"Type":"X"}}}`), message: "unsupported AWSTemplateFormatVersion"},
		{name: "null description", input: []byte(`{"Description":null,"Resources":{"A":{"Type":"X"}}}`), message: "Description must be a string"},
		{name: "missing resources", input: []byte(`{"Description":"none"}`), message: "Resources is required"},
		{name: "empty resources", input: []byte(`{"Resources":{}}`), message: "at least one resource"},
		{name: "invalid logical ID", input: []byte(`{"Resources":{"Bad-ID":{"Type":"X"}}}`), message: "logical ID"},
		{name: "missing type", input: []byte(`{"Resources":{"A":{"Properties":{}}}}`), message: "Type must be a non-empty string"},
		{name: "null properties", input: []byte(`{"Resources":{"A":{"Type":"X","Properties":null}}}`), message: "Properties must be a JSON object"},
		{name: "null transform", input: []byte(`{"Transform":null,"Resources":{"A":{"Type":"X"}}}`), message: "Transform must not be null"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := (Loader{}).ParseTemplate(context.Background(), test.name, strings.NewReader(string(test.input)))
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want containing %q", err, test.message)
			}
		})
	}
}

func TestParseTemplateEnforcesConfigurableBudgets(t *testing.T) {
	t.Parallel()

	valid := `{"Resources":{"A":{"Type":"X"}}}`
	tests := []struct {
		name    string
		loader  Loader
		input   string
		message string
	}{
		{
			name:    "bytes",
			loader:  Loader{Limits: Limits{MaxTemplateBytes: int64(len(valid) - 1)}},
			input:   valid,
			message: "byte size limit",
		},
		{
			name:    "depth",
			loader:  Loader{Limits: Limits{MaxJSONDepth: 3}},
			input:   valid,
			message: "nesting exceeds",
		},
		{
			name:    "tokens",
			loader:  Loader{Limits: Limits{MaxJSONValues: 4}},
			input:   valid,
			message: "token count exceeds",
		},
		{
			name:    "resources",
			loader:  Loader{Limits: Limits{MaxResources: 1}},
			input:   `{"Resources":{"A":{"Type":"X"},"B":{"Type":"X"}}}`,
			message: "resource count 2 exceeds",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := test.loader.ParseTemplate(context.Background(), test.name, strings.NewReader(test.input))
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want containing %q", err, test.message)
			}
		})
	}

	resources := make(map[string]any, DefaultLimits().MaxResources+1)
	for index := 0; index <= DefaultLimits().MaxResources; index++ {
		resources[fmt.Sprintf("Resource%d", index)] = map[string]string{"Type": "X"}
	}
	contents, err := json.Marshal(map[string]any{"Resources": resources})
	if err != nil {
		t.Fatal(err)
	}
	_, err = (Loader{}).ParseTemplate(context.Background(), "large.json", strings.NewReader(string(contents)))
	if err == nil || !strings.Contains(err.Error(), "resource count 501 exceeds") {
		t.Fatalf("default resource-limit error = %v", err)
	}
}

func TestParseTemplateHonorsCanceledContextAndRejectsInvalidLimits(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (Loader{}).ParseTemplate(ctx, "template.json", strings.NewReader(`{"Resources":{"A":{"Type":"X"}}}`))
	if !strings.Contains(fmt.Sprint(err), "context canceled") {
		t.Fatalf("canceled error = %v", err)
	}
	_, err = (Loader{Limits: Limits{MaxStacks: -1}}).ParseTemplate(context.Background(), "template.json", strings.NewReader(`{}`))
	if err == nil || !strings.Contains(err.Error(), "limits must be positive") {
		t.Fatalf("invalid-limit error = %v", err)
	}
}

func FuzzParseTemplateDoesNotPanic(f *testing.F) {
	f.Add(`{"Resources":{"Alarm":{"Type":"AWS::CloudWatch::Alarm","Properties":{"MetricName":{"Ref":"Metric"}}}}}`)
	f.Add("Resources:\n  Alarm:\n    Type: AWS::CloudWatch::Alarm")
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = (Loader{Limits: Limits{
			MaxTemplateBytes: 64 << 10,
			MaxJSONDepth:     32,
			MaxJSONValues:    10_000,
		}}).ParseTemplate(context.Background(), "fuzz.json", strings.NewReader(input))
	})
}

func namedKeys(values []NamedValue) []string {
	keys := make([]string, len(values))
	for index, value := range values {
		keys[index] = value.Name
	}
	return keys
}

func resourceIDs(resources []Resource) []string {
	ids := make([]string, len(resources))
	for index, resource := range resources {
		ids[index] = resource.LogicalID
	}
	return ids
}
