package cloudformation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testTemplate = `{"Resources":{"Alarm":{"Type":"AWS::CloudWatch::Alarm","Properties":{"MetricName":"Requests"}}}}`

func TestLoadPathLoadsStandaloneTemplate(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	templateFile := filepath.Join(directory, "payments.json")
	writeTestFile(t, templateFile, testTemplate)
	bundle, err := (Loader{}).LoadPath(context.Background(), templateFile)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Kind != InputKindTemplate || bundle.Source != templateFile || bundle.ManifestVersion != "" || len(bundle.Stacks) != 1 {
		t.Fatalf("bundle = %#v", bundle)
	}
	stack := bundle.Stacks[0]
	if stack.ID != "payments" || stack.ArtifactID != "payments" || stack.Template.Resources[0].Provenance.ArtifactID != "payments" {
		t.Fatalf("stack = %#v", stack)
	}
}

func TestLoadPathLoadsDeterministicMultiStackAssembly(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeTestFile(t, filepath.Join(directory, "a.json"), `{"Resources":{"Topic":{"Type":"AWS::SNS::Topic"}}}`)
	writeTestFile(t, filepath.Join(directory, "b.json"), testTemplate)
	manifest := `{
  "version": "54.0.0",
  "artifacts": {
    "StackB": {
      "type": "aws:cloudformation:stack",
      "environment": "aws://123456789012/us-east-1",
      "dependencies": ["Assets", "StackA"],
      "displayName": "Payments display",
      "properties": {"templateFile": "b.json", "stackName": "payments-prod"}
    },
    "Assets": {"type": "cdk:asset-manifest", "properties": {"file": "assets.json"}},
    "StackA": {
      "type": "aws:cloudformation:stack",
      "environment": "aws://unknown-account/unknown-region",
      "properties": {"templateFile": "a.json"}
    }
  }
}`
	writeTestFile(t, filepath.Join(directory, "manifest.json"), manifest)

	loader := Loader{}
	bundle, err := loader.LoadPath(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Kind != InputKindCloudAssembly || bundle.ManifestVersion != "54.0.0" ||
		!reflect.DeepEqual(stackIDs(bundle.Stacks), []string{"StackA", "StackB"}) {
		t.Fatalf("bundle = %#v", bundle)
	}
	stack := bundle.Stacks[1]
	if stack.StackName != "payments-prod" || stack.DisplayName != "Payments display" ||
		stack.Environment.Account != "123456789012" || stack.Environment.Region != "us-east-1" ||
		!reflect.DeepEqual(stack.Dependencies, []string{"Assets", "StackA"}) {
		t.Fatalf("StackB = %#v", stack)
	}
	provenance := stack.Template.Resources[0].Provenance
	if provenance.ManifestFile != filepath.Join(directory, "manifest.json") ||
		provenance.TemplateFile != filepath.Join(directory, "b.json") ||
		provenance.ArtifactID != "StackB" || provenance.StackName != "payments-prod" {
		t.Fatalf("provenance = %#v", provenance)
	}

	fromManifest, err := loader.LoadPath(context.Background(), filepath.Join(directory, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bundle, fromManifest) {
		t.Fatalf("directory and manifest loads differ\ndirectory: %#v\nmanifest: %#v", bundle, fromManifest)
	}
	again, err := loader.LoadPath(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bundle, again) {
		t.Fatalf("assembly load was not deterministic")
	}
}

func TestLoadPathLoadsNestedAssemblyWithUnambiguousIDs(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	child := filepath.Join(directory, "nested")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(directory, "manifest.json"), `{
  "version":"54.0.0",
  "artifacts":{"Stage/Prod":{"type":"cdk:cloud-assembly","properties":{"directoryName":"nested"}}}
}`)
	writeTestFile(t, filepath.Join(child, "manifest.json"), `{
  "version":"53.1.0",
  "artifacts":{"Alarm~Stack":{"type":"aws:cloudformation:stack","properties":{"templateFile":"stack.json"}}}
}`)
	writeTestFile(t, filepath.Join(child, "stack.json"), testTemplate)

	bundle, err := (Loader{}).LoadPath(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Stacks) != 1 || bundle.Stacks[0].ID != "Stage~1Prod/Alarm~0Stack" ||
		!reflect.DeepEqual(bundle.Stacks[0].AssemblyPath, []string{"Stage/Prod"}) {
		t.Fatalf("nested stack = %#v", bundle.Stacks)
	}
	if !reflect.DeepEqual(bundle.Stacks[0].Template.Resources[0].Provenance.AssemblyPath, []string{"Stage/Prod"}) {
		t.Fatalf("nested provenance = %#v", bundle.Stacks[0].Template.Resources[0].Provenance)
	}
}

func TestLoadPathRejectsUnsafeOrIncompleteAssemblies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest string
		message  string
	}{
		{
			name:     "new schema",
			manifest: `{"version":"55.0.0","artifacts":{}}`,
			message:  "newer than supported major 54",
		},
		{
			name:     "malformed schema",
			manifest: `{"version":"v54","artifacts":{}}`,
			message:  "not valid semantic versioning",
		},
		{
			name:     "malformed prerelease",
			manifest: `{"version":"54.0.0-01","artifacts":{}}`,
			message:  "not valid semantic versioning",
		},
		{
			name:     "missing context",
			manifest: `{"version":"54.0.0","missing":[{"key":"vpc"}],"artifacts":{}}`,
			message:  "contains 1 missing context entries",
		},
		{
			name:     "unknown artifact",
			manifest: `{"version":"54.0.0","artifacts":{"A":{"type":"custom:exec"}}}`,
			message:  `unsupported type "custom:exec"`,
		},
		{
			name:     "unknown dependency",
			manifest: `{"version":"54.0.0","artifacts":{"A":{"type":"none","dependencies":["B"]}}}`,
			message:  `depends on unknown artifact "B"`,
		},
		{
			name:     "duplicate dependency",
			manifest: `{"version":"54.0.0","artifacts":{"A":{"type":"none"},"B":{"type":"none","dependencies":["A","A"]}}}`,
			message:  `duplicate dependency "A"`,
		},
		{
			name:     "null artifact field",
			manifest: `{"version":"54.0.0","artifacts":{"A":{"type":"none","dependencies":null}}}`,
			message:  "dependencies must not be null",
		},
		{
			name:     "dependency cycle",
			manifest: `{"version":"54.0.0","artifacts":{"A":{"type":"none","dependencies":["B"]},"B":{"type":"none","dependencies":["A"]}}}`,
			message:  "dependency graph contains a cycle",
		},
		{
			name:     "path traversal",
			manifest: `{"version":"54.0.0","artifacts":{"A":{"type":"aws:cloudformation:stack","properties":{"templateFile":"../outside.json"}}}}`,
			message:  "not a safe relative assembly path",
		},
		{
			name:     "absolute path",
			manifest: `{"version":"54.0.0","artifacts":{"A":{"type":"aws:cloudformation:stack","properties":{"templateFile":"/tmp/outside.json"}}}}`,
			message:  "not a safe relative assembly path",
		},
		{
			name:     "recursive nested assembly",
			manifest: `{"version":"54.0.0","artifacts":{"A":{"type":"cdk:cloud-assembly","properties":{"directoryName":"."}}}}`,
			message:  "must identify a child directory",
		},
		{
			name:     "no stacks",
			manifest: `{"version":"54.0.0","artifacts":{"Tree":{"type":"cdk:tree","properties":{"file":"tree.json"}}}}`,
			message:  "contains no CloudFormation stack artifacts",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			writeTestFile(t, filepath.Join(directory, "manifest.json"), test.manifest)
			_, err := (Loader{}).LoadPath(context.Background(), directory)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want containing %q", err, test.message)
			}
		})
	}
}

func TestLoadPathRejectsDuplicateManifestKeys(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeTestFile(t, filepath.Join(directory, "manifest.json"), `{
  "version":"54.0.0",
  "artifacts":{"A":{"type":"none"},"A":{"type":"aws:cloudformation:stack","properties":{"templateFile":"stack.json"}}}
}`)
	_, err := (Loader{}).LoadPath(context.Background(), directory)
	if err == nil || !strings.Contains(err.Error(), `duplicate key "A"`) {
		t.Fatalf("duplicate-key error = %v", err)
	}
}

func TestLoadPathRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	directory := filepath.Join(parent, "assembly")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside.json")
	writeTestFile(t, outside, testTemplate)
	if err := os.Symlink(outside, filepath.Join(directory, "escape.json")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(directory, "manifest.json"), `{
  "version":"54.0.0",
  "artifacts":{"A":{"type":"aws:cloudformation:stack","properties":{"templateFile":"escape.json"}}}
}`)
	_, err := (Loader{}).LoadPath(context.Background(), directory)
	if err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestLoadPathEnforcesAggregateAssemblyLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		limits  Limits
		message string
	}{
		{name: "artifacts", limits: Limits{MaxArtifacts: 1}, message: "artifact count exceeds the limit of 1"},
		{name: "stacks", limits: Limits{MaxStacks: 1}, message: "stack count exceeds the limit of 1"},
		{name: "total bytes", limits: Limits{MaxTotalTemplateBytes: int64(len(testTemplate))}, message: "total template bytes exceed"},
		{name: "total manifest bytes", limits: Limits{MaxTotalManifestBytes: 1}, message: "total manifest bytes exceed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := twoStackAssembly(t)
			_, err := (Loader{Limits: test.limits}).LoadPath(context.Background(), directory)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want containing %q", err, test.message)
			}
		})
	}
}

func TestLoadPathEnforcesNestedDepthAndContext(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	child := filepath.Join(directory, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(directory, "manifest.json"), `{"version":"54.0.0","artifacts":{"Child":{"type":"cdk:cloud-assembly","properties":{"directoryName":"child"}}}}`)
	writeTestFile(t, filepath.Join(child, "manifest.json"), `{"version":"54.0.0","artifacts":{"A":{"type":"aws:cloudformation:stack","properties":{"templateFile":"stack.json"}}}}`)
	writeTestFile(t, filepath.Join(child, "stack.json"), testTemplate)
	_, err := (Loader{Limits: Limits{MaxAssemblyDepth: 1}}).LoadPath(context.Background(), directory)
	if err == nil || !strings.Contains(err.Error(), "nested assembly depth exceeds") {
		t.Fatalf("depth error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = (Loader{}).LoadPath(ctx, directory)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("context error = %v", err)
	}
}

func TestLoadPathValidatesEnvironments(t *testing.T) {
	t.Parallel()

	for _, environment := range []string{"aws://123/us-east-1", "azure://123456789012/us-east-1", "aws://123456789012/not/a-region"} {
		t.Run(environment, func(t *testing.T) {
			directory := t.TempDir()
			writeTestFile(t, filepath.Join(directory, "stack.json"), testTemplate)
			writeTestFile(t, filepath.Join(directory, "manifest.json"), fmt.Sprintf(`{
  "version":"54.0.0",
  "artifacts":{"A":{"type":"aws:cloudformation:stack","environment":%q,"properties":{"templateFile":"stack.json"}}}
}`, environment))
			_, err := (Loader{}).LoadPath(context.Background(), directory)
			if err == nil || !strings.Contains(err.Error(), "environment") {
				t.Fatalf("environment error = %v", err)
			}
		})
	}
}

func FuzzParseManifestDoesNotPanic(f *testing.F) {
	f.Add(`{"version":"54.0.0","artifacts":{"Stack":{"type":"aws:cloudformation:stack","properties":{"templateFile":"stack.json"}}}}`)
	f.Add(`{"version":"999.0.0","missing":[]}`)
	limits := DefaultLimits()
	limits.MaxManifestBytes = 64 << 10
	limits.MaxJSONDepth = 32
	limits.MaxJSONValues = 10_000
	f.Fuzz(func(t *testing.T, input string) {
		if int64(len(input)) > limits.MaxManifestBytes {
			t.Skip()
		}
		_, _ = parseAssemblyManifest([]byte(input), limits)
	})
}

func twoStackAssembly(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	writeTestFile(t, filepath.Join(directory, "a.json"), testTemplate)
	writeTestFile(t, filepath.Join(directory, "b.json"), testTemplate)
	writeTestFile(t, filepath.Join(directory, "manifest.json"), `{
  "version":"54.0.0",
  "artifacts":{
    "A":{"type":"aws:cloudformation:stack","properties":{"templateFile":"a.json"}},
    "B":{"type":"aws:cloudformation:stack","properties":{"templateFile":"b.json"}}
  }
}`)
	return directory
}

func writeTestFile(t *testing.T, name string, contents string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func stackIDs(stacks []Stack) []string {
	ids := make([]string, len(stacks))
	for index, stack := range stacks {
		ids[index] = stack.ID
	}
	return ids
}
