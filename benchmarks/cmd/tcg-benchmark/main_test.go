package main

import (
	"reflect"
	"testing"
)

func TestInspectReportReadsGenericAndMigrationContracts(t *testing.T) {
	t.Parallel()

	generic, err := inspectReport([]byte(`{
  "schemaVersion":"tcg-result/v1alpha1",
  "status":"BLOCK",
  "findings":[{"impact":"SCALING_RISK"}],
  "diagnostics":[]
}`), 2)
	if err != nil {
		t.Fatal(err)
	}
	if generic.Status != "BLOCK" || generic.FindingCount != 1 || !reflect.DeepEqual(generic.Impacts, []string{"SCALING_RISK"}) {
		t.Fatalf("generic observation = %#v", generic)
	}

	migration, err := inspectReport([]byte(`{
  "schemaVersion":"tmr-result/v1alpha1",
  "summary":{"status":"READY"},
  "changes":[{"consumers":[{"classification":"MIGRATED"}]}],
  "diagnostics":[]
}`), 0)
	if err != nil {
		t.Fatal(err)
	}
	if migration.Status != "READY" || !reflect.DeepEqual(migration.ConsumerClassifications, []string{"MIGRATED"}) {
		t.Fatalf("migration observation = %#v", migration)
	}
}

func TestValidateCommandRejectsRunnerOwnedOutputs(t *testing.T) {
	t.Parallel()

	if err := validateCommand([]string{"check", "--config", "tcg.yaml"}); err != nil {
		t.Fatal(err)
	}
	if err := validateCommand([]string{"check", "--output", "result.json"}); err == nil {
		t.Fatal("expected runner-owned output rejection")
	}
	if err := validateCommand([]string{"snapshot"}); err == nil {
		t.Fatal("expected unsupported command rejection")
	}
}

func TestCompareTreatsEmptyAndNilCollectionsEqually(t *testing.T) {
	t.Parallel()

	zero := 0
	mismatches := compare(expectation{
		SchemaVersion:   "tcg-result/v1alpha1",
		Status:          "PASS",
		ExitCode:        0,
		FindingCount:    &zero,
		Impacts:         []string{},
		DiagnosticCount: &zero,
	}, observation{
		SchemaVersion: "tcg-result/v1alpha1",
		Status:        "PASS",
	})
	if len(mismatches) != 0 {
		t.Fatalf("mismatches = %v", mismatches)
	}
}
