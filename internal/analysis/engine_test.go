package analysis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/config"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/graph"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/impact"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/readiness"
	remoteurl "github.com/tadurisaikiran/telemetry-change-guard/internal/remote"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/safety"
)

func TestAnalyzeChangeSetDoesNotMutateCallerInput(t *testing.T) {
	t.Parallel()

	destination := domain.Symbol{
		Domain: domain.DomainPrometheus,
		Kind:   domain.SymbolKindMetric,
		Name:   "new_metric",
	}
	changeSet := domain.ChangeSet{
		APIVersion: domain.ChangeSetAPIVersion,
		Kind:       domain.ChangeSetKind,
		Metadata:   domain.ChangeSetMetadata{Name: "immutable-input"},
		Changes: []domain.Change{{
			ID:       "metric-rename",
			Kind:     domain.ChangeKindMetricRename,
			Domain:   domain.DomainPrometheus,
			From:     domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "old_metric"},
			To:       &destination,
			Metadata: map[string]string{"source.adapter": "fixture"},
		}},
	}
	before, err := json.Marshal(changeSet)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := AnalyzeChangeSet(context.Background(), config.Config{}, changeSet); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(changeSet)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("AnalyzeChangeSet mutated caller input\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestAnalyzeChangeSetRejectsInvalidInputBeforeDiscovery(t *testing.T) {
	t.Parallel()

	_, _, err := AnalyzeChangeSet(context.Background(), config.Config{}, domain.ChangeSet{})
	if err == nil || !strings.Contains(err.Error(), "validate change set") {
		t.Fatalf("error = %v, want ChangeSet validation error", err)
	}
}

func TestCheckoutFixtureIsBlockedAndTransitive(t *testing.T) {
	t.Parallel()

	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	configuration, err := config.LoadConfig(context.Background(), filepath.Join(repositoryRoot, "examples", "checkout-migration", "tmr.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	absolutizePatterns(&configuration, repositoryRoot)
	migration, err := config.LoadMigration(context.Background(), filepath.Join(repositoryRoot, "examples", "checkout-migration", "migration.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	result, _, _, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Status != readiness.StatusBlocked {
		t.Fatalf("status = %s, want BLOCKED", result.Summary.Status)
	}
	if result.Summary.LegacyOnly == 0 || result.Summary.Uncertain == 0 {
		t.Fatalf("summary = %+v, want legacy and uncertain consumers", result.Summary)
	}

	var transitivePath bool
	for _, change := range result.Changes {
		for _, consumer := range change.Consumers {
			if consumer.Consumer.Name != "CheckoutLatencyHigh" {
				continue
			}
			for _, path := range consumer.Paths {
				joined := strings.Join(path.Nodes, " ")
				if strings.Contains(joined, "checkout:p95_latency") && len(path.Edges) > 1 {
					transitivePath = true
				}
			}
		}
	}
	if !transitivePath {
		t.Fatal("missing raw metric -> recording rule -> alert transitive path")
	}
}

func TestGenericSafetyAndLegacyReadinessRemainIndependent(t *testing.T) {
	t.Parallel()

	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	configuration, err := config.LoadConfig(context.Background(), filepath.Join(repositoryRoot, "examples", "checkout-migration", "tmr.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	absolutizePatterns(&configuration, repositoryRoot)
	migration, err := config.LoadMigration(context.Background(), filepath.Join(repositoryRoot, "examples", "checkout-migration", "migration.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	changeSet, err := config.NormalizeMigration(migration)
	if err != nil {
		t.Fatal(err)
	}

	generic, _, _, err := RunSafety(context.Background(), configuration, changeSet, safety.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	legacy, _, _, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}

	if generic.SchemaVersion != safety.ResultSchemaVersion || generic.Status != safety.StatusIncomplete {
		t.Fatalf("generic result = %#v, want versioned INCOMPLETE", generic)
	}
	if len(generic.Findings) == 0 || len(generic.Decisions) != len(generic.Findings) {
		t.Fatalf("generic findings or decisions missing: %#v", generic)
	}
	if legacy.SchemaVersion != readiness.ResultSchemaVersion || legacy.Summary.Status != readiness.StatusBlocked {
		t.Fatalf("legacy result = %#v, want unchanged versioned BLOCKED", legacy)
	}
}

func TestGenericSafetyDistinguishesEmptyIncompleteAndError(t *testing.T) {
	t.Parallel()

	migration := mustParseRemovalMigration(t)
	changeSet, err := config.NormalizeMigration(migration)
	if err != nil {
		t.Fatal(err)
	}

	complete, _, discovery, err := RunSafety(
		context.Background(),
		testConfiguration(config.Sources{}),
		changeSet,
		safety.DefaultPolicy(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if complete.Status != safety.StatusPass || len(complete.Findings) != 0 || len(discovery.Diagnostics) != 0 {
		t.Fatalf("empty valid discovery = %#v, discovery = %#v", complete, discovery)
	}

	requiredMissing := testConfiguration(config.Sources{PrometheusRules: []config.SourcePattern{{
		Pattern:  filepath.Join(t.TempDir(), "missing", "*.yaml"),
		Required: true,
	}}})
	incomplete, _, incompleteDiscovery, err := RunSafety(
		context.Background(),
		requiredMissing,
		changeSet,
		safety.DefaultPolicy(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if incomplete.Status != safety.StatusIncomplete || len(incompleteDiscovery.Diagnostics) != 1 ||
		!incompleteDiscovery.Diagnostics[0].Required {
		t.Fatalf("required missing result = %#v, discovery = %#v", incomplete, incompleteDiscovery)
	}

	failed, dependencyGraph, failedDiscovery, err := RunSafety(
		context.Background(),
		testConfiguration(config.Sources{}),
		domain.ChangeSet{},
		safety.DefaultPolicy(),
	)
	if err == nil || failed.Status != safety.StatusError || dependencyGraph != nil || len(failedDiscovery.Consumers) != 0 {
		t.Fatalf("invalid input result = %#v, graph = %#v, discovery = %#v, error = %v", failed, dependencyGraph, failedDiscovery, err)
	}
}

func TestGenericSafetyRuntimeFailureIsError(t *testing.T) {
	t.Parallel()

	changeSet, err := config.NormalizeMigration(mustParseRemovalMigration(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, dependencyGraph, discovery, err := RunSafety(
		ctx,
		testConfiguration(config.Sources{}),
		changeSet,
		safety.DefaultPolicy(),
	)
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if result.Status != safety.StatusError || safety.ExitCode(result.Status) != 1 || dependencyGraph != nil ||
		len(discovery.Consumers) != 0 {
		t.Fatalf("runtime failure result = %#v, graph = %#v, discovery = %#v", result, dependencyGraph, discovery)
	}
}

func TestOverlappingLocalPatternsCannotDowngradeMalformedRequiredEvidence(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	critical := filepath.Join(root, "monitoring", "critical")
	if err := os.MkdirAll(critical, 0o755); err != nil {
		t.Fatal(err)
	}
	malformed := filepath.Join(critical, "rules.yaml")
	if err := os.WriteFile(malformed, []byte("groups: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	broad := config.SourcePattern{Pattern: filepath.Join(root, "monitoring", "**", "*.yaml"), Required: false}
	narrow := config.SourcePattern{Pattern: filepath.Join(critical, "*.yaml"), Required: true}

	var previousMessage string
	for _, patterns := range [][]config.SourcePattern{{broad, narrow}, {narrow, broad}} {
		discovery, _, err := Discover(context.Background(), testConfiguration(config.Sources{PrometheusRules: patterns}))
		if err != nil {
			t.Fatal(err)
		}
		if len(discovery.Diagnostics) != 1 || !discovery.Diagnostics[0].Required {
			t.Fatalf("diagnostics = %#v", discovery.Diagnostics)
		}
		if previousMessage != "" && discovery.Diagnostics[0].Message != previousMessage {
			t.Fatalf("configuration order changed diagnostic: %q != %q", discovery.Diagnostics[0].Message, previousMessage)
		}
		previousMessage = discovery.Diagnostics[0].Message
	}
}

func TestEnvironmentReferenceLegacyFallbackAndConflict(t *testing.T) {
	t.Setenv("TMR_ANALYSIS_TEST_TOKEN", "legacy-token")
	configuration := testConfiguration(config.Sources{PersesUsage: []config.PersesUsageSource{{
		URL:            "https://usage.example.test",
		Required:       true,
		Timeout:        "1s",
		BearerTokenEnv: "TCG_ANALYSIS_TEST_TOKEN",
	}}})
	configuration.RemoteEvidence.AllowedOrigins = []string{"https://usage.example.test"}

	resolved, err := resolveEnvironmentReferences(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved["TCG_ANALYSIS_TEST_TOKEN"]; !got.exists || got.value != "legacy-token" {
		t.Fatalf("legacy fallback = %#v", got)
	}

	t.Setenv("TCG_ANALYSIS_TEST_TOKEN", "canonical-secret")
	changeSet, err := config.NormalizeMigration(mustParseRemovalMigration(t))
	if err != nil {
		t.Fatal(err)
	}
	result, dependencyGraph, discovery, err := RunSafety(
		context.Background(),
		configuration,
		changeSet,
		safety.DefaultPolicy(),
	)
	if err == nil || result.Status != safety.StatusError || dependencyGraph != nil || len(discovery.Consumers) != 0 {
		t.Fatalf("result = %#v, graph = %#v, discovery = %#v, error = %v", result, dependencyGraph, discovery, err)
	}
	message := err.Error()
	for _, name := range []string{"TCG_ANALYSIS_TEST_TOKEN", "TMR_ANALYSIS_TEST_TOKEN"} {
		if !strings.Contains(message, name) {
			t.Errorf("error = %q, want variable %q", message, name)
		}
	}
	for _, secret := range []string{"canonical-secret", "legacy-token"} {
		if strings.Contains(message, secret) {
			t.Errorf("error leaked secret %q: %s", secret, message)
		}
	}
}

func TestRequiredMissingSourceIsIncomplete(t *testing.T) {
	t.Parallel()

	configuration := config.Config{
		APIVersion: config.ConfigAPIVersion,
		Kind:       config.ConfigKind,
		Sources: config.Sources{PrometheusRules: []config.SourcePattern{{
			Pattern:  filepath.Join(t.TempDir(), "missing", "*.yaml"),
			Required: true,
		}}},
		Analysis: config.AnalysisConfig{IncludeTransitiveDependencies: true, UnresolvedReferencePolicy: "error"},
		Policy:   config.PolicyConfig{FailOnCriticalLegacyConsumer: true, FailOnCriticalUnknown: true, MinimumBlockingCriticality: "high"},
		Output:   config.OutputConfig{Formats: []string{"json"}},
	}
	migration, err := config.ParseMigration(strings.NewReader(`
apiVersion: telemetry-migration/v1alpha1
kind: Migration
metadata: {name: missing-source}
spec:
  changes:
    - id: remove
      kind: metric_remove
      domain: prometheus
      from: {metric: old_metric}
`))
	if err != nil {
		t.Fatal(err)
	}
	result, _, _, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Status != readiness.StatusIncomplete {
		t.Fatalf("status = %s, want INCOMPLETE", result.Summary.Status)
	}
}

func TestPersesUsageFailureHonorsRequiredPolicy(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	migration := mustParseRemovalMigration(t)

	for _, test := range []struct {
		name     string
		required bool
		status   readiness.Status
	}{
		{name: "required", required: true, status: readiness.StatusIncomplete},
		{name: "optional", required: false, status: readiness.StatusReady},
	} {
		t.Run(test.name, func(t *testing.T) {
			configuration := testConfiguration(config.Sources{PersesUsage: []config.PersesUsageSource{{
				URL:      server.URL,
				Required: test.required,
				Timeout:  "1s",
			}}})
			result, _, _, err := Run(context.Background(), configuration, migration)
			if err != nil {
				t.Fatal(err)
			}
			if result.Summary.Status != test.status {
				t.Fatalf("status = %s, want %s", result.Summary.Status, test.status)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Required != test.required {
				t.Fatalf("diagnostics = %#v", result.Diagnostics)
			}
		})
	}
}

func TestRepeatedPersesSourceUsesStrictestRequiredPolicy(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	optional := config.PersesUsageSource{URL: server.URL, Required: false, Timeout: "1s"}
	required := optional
	required.Required = true
	for _, sources := range [][]config.PersesUsageSource{{optional, required}, {required, optional}} {
		discovery, _, err := Discover(context.Background(), testConfiguration(config.Sources{PersesUsage: sources}))
		if err != nil {
			t.Fatal(err)
		}
		if len(discovery.Diagnostics) != 1 || !discovery.Diagnostics[0].Required {
			t.Fatalf("diagnostics = %#v", discovery.Diagnostics)
		}
	}
}

func TestPersesUsageMissingBearerTokenIsIncomplete(t *testing.T) {
	t.Setenv("TMR_TEST_DEFINITELY_UNSET_TOKEN", "")
	configuration := testConfiguration(config.Sources{PersesUsage: []config.PersesUsageSource{{
		URL:            "https://usage.example.test",
		Required:       true,
		Timeout:        "1s",
		BearerTokenEnv: "TMR_TEST_DEFINITELY_UNSET_TOKEN",
	}}})
	configuration.RemoteEvidence.AllowedOrigins = []string{"https://usage.example.test"}
	result, _, _, err := Run(context.Background(), configuration, mustParseRemovalMigration(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Status != readiness.StatusIncomplete {
		t.Fatalf("status = %s, want INCOMPLETE", result.Summary.Status)
	}
	if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "unset or empty") {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
}

func TestRemoteEvidencePolicyFailsClosedWithoutErasingLocalFindings(t *testing.T) {
	t.Parallel()

	rulesPath := filepath.Join(t.TempDir(), "rules.yaml")
	if err := os.WriteFile(rulesPath, []byte(`groups:
  - name: checkout
    rules:
      - alert: LegacyMetricStillUsed
        expr: old_metric > 0
        labels: {severity: critical}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		required   bool
		wantStatus readiness.Status
	}{
		{name: "required denied with confirmed block", required: true, wantStatus: readiness.StatusBlocked},
		{name: "optional denied", required: false, wantStatus: readiness.StatusBlocked},
	} {
		t.Run(test.name, func(t *testing.T) {
			configuration := testConfiguration(config.Sources{
				PrometheusRules: []config.SourcePattern{{Pattern: rulesPath, Required: true}},
				PersesUsage: []config.PersesUsageSource{{
					URL: "https://usage.example.test", Required: test.required, Timeout: "1s",
				}},
			})
			configuration.RemoteEvidence = config.RemoteEvidencePolicy{Mode: config.RemoteEvidenceDisabled}
			result, _, discovery, err := Run(context.Background(), configuration, mustParseRemovalMigration(t))
			if err != nil {
				t.Fatal(err)
			}
			if result.Summary.Status != test.wantStatus || len(result.Changes) != 1 || len(result.Changes[0].Consumers) != 1 {
				t.Fatalf("result = %#v", result)
			}
			if len(discovery.Diagnostics) != 1 || discovery.Diagnostics[0].Required != test.required ||
				!strings.Contains(discovery.Diagnostics[0].Message, "disabled") {
				t.Fatalf("diagnostics = %#v", discovery.Diagnostics)
			}
		})
	}
}

func TestCredentialedRemoteOriginCannotBeRedirectedByRepositoryConfiguration(t *testing.T) {
	t.Setenv("TCG_REMOTE_POLICY_TEST_TOKEN", "must-not-leak")
	configuration := testConfiguration(config.Sources{PersesUsage: []config.PersesUsageSource{{
		URL:            "https://attacker.example.test",
		Required:       true,
		Timeout:        "1s",
		BearerTokenEnv: "TCG_REMOTE_POLICY_TEST_TOKEN",
	}}})
	configuration.RemoteEvidence = config.RemoteEvidencePolicy{
		Mode:           config.RemoteEvidenceEnabled,
		AllowedOrigins: []string{"https://usage.example.test"},
	}
	result, _, discovery, err := Run(context.Background(), configuration, mustParseRemovalMigration(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Status != readiness.StatusIncomplete || len(discovery.Diagnostics) != 1 ||
		!strings.Contains(discovery.Diagnostics[0].Message, "not in the execution policy allowlist") {
		t.Fatalf("result = %#v, diagnostics = %#v", result, discovery.Diagnostics)
	}
	if strings.Contains(discovery.Diagnostics[0].Message, "must-not-leak") {
		t.Fatalf("diagnostic leaked bearer token: %s", discovery.Diagnostics[0].Message)
	}
}

func TestDisabledRequiredRemoteEvidenceCannotProduceReady(t *testing.T) {
	t.Parallel()

	configuration := testConfiguration(config.Sources{PersesUsage: []config.PersesUsageSource{{
		URL: "https://usage.example.test", Required: true, Timeout: "1s",
	}}})
	configuration.RemoteEvidence = config.RemoteEvidencePolicy{Mode: config.RemoteEvidenceDisabled}
	result, _, _, err := Run(context.Background(), configuration, mustParseRemovalMigration(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Status != readiness.StatusIncomplete {
		t.Fatalf("status = %s, want INCOMPLETE", result.Summary.Status)
	}
}

func TestRemoteEvidenceDefaultsDisabledAndAlwaysRequiresAllowlist(t *testing.T) {
	t.Parallel()

	policy, err := newRemotePolicy(config.RemoteEvidencePolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if policy.enabled {
		t.Fatal("zero-value remote policy is enabled")
	}
	if err := policy.authorize("https://usage.example.test", false); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("authorize() error = %v", err)
	}
	if _, err := newRemotePolicy(config.RemoteEvidencePolicy{Mode: config.RemoteEvidenceEnabled}); err == nil ||
		!strings.Contains(err.Error(), "allowed origin") {
		t.Fatalf("enabled policy error = %v", err)
	}
}

func TestAggregateDiscoveryAndGraphLimitsFailClosed(t *testing.T) {
	t.Parallel()

	policy := config.DefaultExecutionPolicy(".")
	policy.MaxConsumers = 1
	if err := validateDiscoveryLimits(policy, domain.Discovery{Consumers: make([]domain.Consumer, 2)}); err == nil ||
		!strings.Contains(err.Error(), "consumer count") {
		t.Fatalf("consumer limit error = %v", err)
	}
	target := graph.New()
	if err := target.AddNode(graph.Node{ID: "one", Kind: graph.NodeKindConsumer, Name: "one"}); err != nil {
		t.Fatal(err)
	}
	policy.MaxGraphNodes = 0
	if err := validateGraphLimits(policy, target); err == nil || !strings.Contains(err.Error(), "graph node count") {
		t.Fatalf("graph limit error = %v", err)
	}
}

func TestOwnershipEnrichmentDoesNotChangeReadiness(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	rulesPath := filepath.Join(root, "monitoring", "rules.yaml")
	if err := os.MkdirAll(filepath.Dir(rulesPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rulesPath, []byte(`groups:
  - name: checkout
    rules:
      - alert: LegacyMetricStillUsed
        expr: old_metric > 0
        labels: {severity: critical}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	codeownersPath := filepath.Join(root, ".github", "CODEOWNERS")
	if err := os.MkdirAll(filepath.Dir(codeownersPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codeownersPath, []byte("* @telemetry-platform\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	configuration := testConfiguration(config.Sources{PrometheusRules: []config.SourcePattern{{
		Pattern:  rulesPath,
		Required: true,
	}}})
	migration := mustParseRemovalMigration(t)
	withoutOwnership, _, _, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	configuration.Ownership = config.OwnershipConfig{
		Enabled:        true,
		RepositoryRoot: root,
		Codeowners:     config.CodeownersConfig{Enabled: true},
	}
	withOwnership, _, discovery, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	if withOwnership.Summary != withoutOwnership.Summary {
		t.Fatalf("summary changed with ownership: before=%+v after=%+v", withoutOwnership.Summary, withOwnership.Summary)
	}
	if len(discovery.Consumers) != 1 || discovery.Consumers[0].Owner == nil || discovery.Consumers[0].Owner.Name != "@telemetry-platform" {
		t.Fatalf("consumers = %#v", discovery.Consumers)
	}
	if err := os.WriteFile(codeownersPath, []byte("!invalid/** @owner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withInvalidOwnership, _, invalidDiscovery, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	if withInvalidOwnership.Summary != withoutOwnership.Summary {
		t.Fatalf("invalid ownership changed readiness: before=%+v after=%+v", withoutOwnership.Summary, withInvalidOwnership.Summary)
	}
	if len(invalidDiscovery.Diagnostics) != 1 || invalidDiscovery.Diagnostics[0].Required {
		t.Fatalf("ownership diagnostics = %#v", invalidDiscovery.Diagnostics)
	}
}

func TestRuntimeQueryEvidenceIsAdditiveAndFailClosed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	queryLog := filepath.Join(root, "query.log")
	writeLog := func(contents string) {
		t.Helper()
		if err := os.WriteFile(queryLog, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := config.RuntimeQuerySource{
		Pattern:     queryLog,
		Required:    true,
		Format:      config.RuntimeQueryFormatPrometheusLog,
		Window:      "24h",
		Criticality: "high",
	}
	configuration := testConfiguration(config.Sources{RuntimeQueries: []config.RuntimeQuerySource{source}})
	migration := mustParseRemovalMigration(t)

	writeLog(`{"params":{"query":"old_metric"},"ts":"2026-08-24T12:00:00Z"}` + "\n")
	blocked, _, discovery, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Summary.Status != readiness.StatusBlocked || len(discovery.Consumers) != 1 || discovery.Consumers[0].Runtime == nil {
		t.Fatalf("blocked result = %+v consumers = %#v", blocked.Summary, discovery.Consumers)
	}
	if len(discovery.References) != 1 || discovery.References[0].Evidence.Method != domain.EvidenceMethodRuntimeQuery {
		t.Fatalf("references = %#v", discovery.References)
	}

	writeLog(`{"params":{"query":"new_metric"},"ts":"2026-08-24T12:00:00Z"}` + "\n")
	ready, _, _, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Summary.Status != readiness.StatusReady {
		t.Fatalf("new-only runtime status = %s, want READY", ready.Summary.Status)
	}

	writeLog("not-json\n")
	incomplete, _, malformed, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	if incomplete.Summary.Status != readiness.StatusIncomplete || len(malformed.Diagnostics) != 1 || !malformed.Diagnostics[0].Required {
		t.Fatalf("malformed result = %+v diagnostics = %#v", incomplete.Summary, malformed.Diagnostics)
	}

	configuration.Sources.RuntimeQueries[0].Required = false
	optional, _, _, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	if optional.Summary.Status != readiness.StatusReady {
		t.Fatalf("optional malformed runtime status = %s, want READY", optional.Summary.Status)
	}

	rulesPath := filepath.Join(root, "rules.yaml")
	if err := os.WriteFile(rulesPath, []byte(`groups:
  - name: runtime-absence
    rules:
      - alert: ConfiguredLegacyQuery
        expr: old_metric > 0
        labels: {severity: critical}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLog("\n")
	configuredAndEmptyRuntime := testConfiguration(config.Sources{
		PrometheusRules: []config.SourcePattern{{Pattern: rulesPath, Required: true}},
		RuntimeQueries: []config.RuntimeQuerySource{{
			Pattern: queryLog, Required: true, Format: config.RuntimeQueryFormatPrometheusLog,
			Window: "24h", Criticality: "high",
		}},
	})
	stillBlocked, _, _, err := Run(context.Background(), configuredAndEmptyRuntime, migration)
	if err != nil {
		t.Fatal(err)
	}
	if stillBlocked.Summary.Status != readiness.StatusBlocked {
		t.Fatalf("empty runtime evidence weakened configured dependency: status = %s", stillBlocked.Summary.Status)
	}
}

func TestRuntimeQueryOverlapUsesStrictestRequiredPolicy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	critical := filepath.Join(root, "critical")
	if err := os.MkdirAll(critical, 0o755); err != nil {
		t.Fatal(err)
	}
	queryLog := filepath.Join(critical, "query.log")
	if err := os.WriteFile(queryLog, []byte("not-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	broad := config.RuntimeQuerySource{
		Pattern: filepath.Join(root, "**", "*.log"), Required: false,
		Format: config.RuntimeQueryFormatPrometheusLog, Window: "24h", Criticality: "high",
	}
	narrow := broad
	narrow.Pattern = filepath.Join(critical, "*.log")
	narrow.Required = true

	for _, sources := range [][]config.RuntimeQuerySource{{broad, narrow}, {narrow, broad}} {
		discovery, _, err := Discover(context.Background(), testConfiguration(config.Sources{RuntimeQueries: sources}))
		if err != nil {
			t.Fatal(err)
		}
		if len(discovery.Diagnostics) != 1 || !discovery.Diagnostics[0].Required {
			t.Fatalf("diagnostics = %#v", discovery.Diagnostics)
		}
	}
}

func TestRepeatedTempoSourceUsesStrictestRequiredPolicy(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Write([]byte(`{"traces":[]}`))
	}))
	defer server.Close()
	manifest := filepath.Join(t.TempDir(), "queries.yaml")
	if err := os.WriteFile(manifest, []byte("queries: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	optional := config.TempoQuerySource{
		URL: server.URL, Pattern: manifest, Required: false, Timeout: "1s", Criticality: "high",
	}
	required := optional
	required.Required = true

	for _, sources := range [][]config.TempoQuerySource{{optional, required}, {required, optional}} {
		discovery, _, err := Discover(context.Background(), testConfiguration(config.Sources{TempoQueries: sources}))
		if err != nil {
			t.Fatal(err)
		}
		if len(discovery.Diagnostics) != 1 || !discovery.Diagnostics[0].Required {
			t.Fatalf("diagnostics = %#v", discovery.Diagnostics)
		}
	}
}

func TestTempoTraceQLUsesExplicitOTelMappingsAndFailsClosed(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/search" || request.URL.Query().Get("q") == "" {
			http.Error(writer, "bad validation request", http.StatusBadRequest)
			return
		}
		writer.Write([]byte(`{"traces":[]}`))
	}))
	defer server.Close()

	root := t.TempDir()
	queryPath := filepath.Join(root, "queries.yaml")
	writeQuery := func(attribute string) {
		t.Helper()
		contents := `apiVersion: tmr.tempo/v1alpha1
kind: TraceQueries
queries:
  - id: checkout
    name: Checkout trace query
    criticality: critical
    expression: '{ span.` + attribute + ` = "GET" }'
`
		if err := os.WriteFile(queryPath, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sources := config.Sources{TempoQueries: []config.TempoQuerySource{{
		URL: server.URL, Pattern: queryPath, Required: true, Timeout: "1s", Criticality: "critical",
	}}}
	configuration := testConfiguration(sources)
	configuration.Mappings.TraceAttributes = []config.TraceAttributeMapping{
		{Scope: "span", OpenTelemetry: "http.method", Tempo: "http.method"},
		{Scope: "span", OpenTelemetry: "http.request.method", Tempo: "http.request.method"},
	}
	destination := domain.Symbol{Domain: domain.DomainOpenTelemetry, Kind: domain.SymbolKindSpanAttribute, Name: "http.request.method"}
	migration := domain.Migration{
		APIVersion: domain.MigrationAPIVersion,
		Kind:       domain.MigrationKind,
		Metadata:   domain.MigrationMetadata{Name: "trace-method"},
		Changes: []domain.Change{{
			ID: "span-method", Kind: domain.ChangeKindSpanAttributeRename, Domain: domain.DomainOpenTelemetry,
			From: domain.Symbol{Domain: domain.DomainOpenTelemetry, Kind: domain.SymbolKindSpanAttribute, Name: "http.method"},
			To:   &destination,
		}},
	}

	writeQuery("http.method")
	blocked, _, discovery, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Summary.Status != readiness.StatusBlocked || len(discovery.References) != 2 {
		t.Fatalf("blocked = %+v discovery = %#v", blocked.Summary, discovery)
	}

	writeQuery("http.request.method")
	ready, _, _, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Summary.Status != readiness.StatusReady || ready.Summary.Migrated != 1 {
		t.Fatalf("ready = %+v", ready.Summary)
	}

	configuration.Mappings.TraceAttributes = configuration.Mappings.TraceAttributes[:1]
	incomplete, _, missingMapping, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	if incomplete.Summary.Status != readiness.StatusIncomplete || len(missingMapping.Diagnostics) != 1 ||
		missingMapping.Diagnostics[0].Adapter != "tempo_mapping" || !missingMapping.Diagnostics[0].Required {
		t.Fatalf("incomplete = %+v diagnostics = %#v", incomplete.Summary, missingMapping.Diagnostics)
	}
}

func TestOptionalTempoMappingDiagnosticIsAdvisory(t *testing.T) {
	t.Parallel()

	destination := domain.Symbol{Domain: domain.DomainOpenTelemetry, Kind: domain.SymbolKindResourceAttribute, Name: "cloud.region"}
	changeSet := domain.ChangeSet{Changes: []domain.Change{{
		ID: "resource-region", Kind: domain.ChangeKindResourceAttributeRename, Domain: domain.DomainOpenTelemetry,
		From: domain.Symbol{Domain: domain.DomainOpenTelemetry, Kind: domain.SymbolKindResourceAttribute, Name: "cloud.zone"},
		To:   &destination,
	}}}
	configuration := config.Config{Sources: config.Sources{TempoQueries: []config.TempoQuerySource{{Required: false}}}}
	diagnostics := traceMappingDiagnostics(configuration, changeSet)
	if len(diagnostics) != 2 || diagnostics[0].Required || diagnostics[1].Required {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestKEDAProductionScalerProducesBlockingScalingRisk(t *testing.T) {
	t.Parallel()

	manifestPath := filepath.Join(t.TempDir(), "scaledobject.yaml")
	writeManifest := func(query string) {
		t.Helper()
		contents := `apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: orders-worker-scaler
  namespace: commerce
  labels:
    environment: production
spec:
  scaleTargetRef:
    name: orders-worker
  triggers:
    - type: prometheus
      metadata:
        serverAddress: https://prometheus.example.test
        threshold: "50"
        query: ` + query + "\n"
		if err := os.WriteFile(manifestPath, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	configuration := testConfiguration(config.Sources{KEDA: []config.SourcePattern{{
		Pattern: manifestPath, Required: true,
	}}})
	changeSet, err := config.NormalizeMigration(mustParseRemovalMigration(t))
	if err != nil {
		t.Fatal(err)
	}

	writeManifest("sum(rate(old_metric[2m]))")
	result, _, discovery, err := RunSafety(context.Background(), configuration, changeSet, safety.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != safety.StatusBlock || len(result.Findings) != 1 {
		t.Fatalf("result = %#v, want one BLOCK finding", result)
	}
	finding := result.Findings[0]
	if finding.Impact != impact.TypeScalingRisk || finding.Consumer.Kind != domain.ConsumerKindAutoscaler ||
		finding.Consumer.Name != "orders-worker" || finding.Criticality != domain.CriticalityCritical || finding.Uncertain {
		t.Fatalf("finding = %#v", finding)
	}
	if len(discovery.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", discovery.Diagnostics)
	}

	writeManifest("rate(old_metric[)")
	incomplete, _, unresolved, err := RunSafety(context.Background(), configuration, changeSet, safety.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if incomplete.Status != safety.StatusIncomplete || len(incomplete.Findings) != 1 ||
		!incomplete.Findings[0].Uncertain || len(unresolved.Diagnostics) != 1 || !unresolved.Diagnostics[0].Required {
		t.Fatalf("incomplete = %#v, discovery = %#v", incomplete, unresolved)
	}
}

func TestHPAExplicitMappingProducesBlockingScalingRisk(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifestPath := filepath.Join(root, "hpa.yaml")
	mappingPath := filepath.Join(root, "hpa-mapping.yaml")
	manifest := `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: orders-worker-scaler
  namespace: commerce
  labels:
    environment: production
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: orders-worker
  metrics:
    - type: External
      external:
        metric:
          name: orders_queue_depth
        target: {type: AverageValue, averageValue: "5"}
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMapping := func(kubernetesMetric string) {
		t.Helper()
		mapping := `apiVersion: tcg.hpa/v1alpha1
kind: HPAMetricMappings
spec:
  mappings:
    - kubernetes:
        type: External
        metric: ` + kubernetesMetric + `
      prometheus:
        metric: old_metric
`
		if err := os.WriteFile(mappingPath, []byte(mapping), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	configuration := testConfiguration(config.Sources{HorizontalPodAutoscalers: []config.HorizontalPodAutoscalerSource{{
		Pattern: manifestPath, MappingPath: mappingPath, Required: true,
	}}})
	changeSet, err := config.NormalizeMigration(mustParseRemovalMigration(t))
	if err != nil {
		t.Fatal(err)
	}

	writeMapping("orders_queue_depth")
	result, _, discovery, err := RunSafety(context.Background(), configuration, changeSet, safety.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != safety.StatusBlock || len(result.Findings) != 1 {
		t.Fatalf("result = %#v, want one BLOCK finding", result)
	}
	finding := result.Findings[0]
	if finding.Impact != impact.TypeScalingRisk || finding.Consumer.Kind != domain.ConsumerKindAutoscaler ||
		finding.Consumer.Name != "orders-worker" || finding.Criticality != domain.CriticalityCritical || finding.Uncertain {
		t.Fatalf("finding = %#v", finding)
	}
	if len(discovery.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", discovery.Diagnostics)
	}

	writeMapping("old_metric")
	incomplete, _, unresolved, err := RunSafety(context.Background(), configuration, changeSet, safety.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if incomplete.Status != safety.StatusIncomplete || len(incomplete.Findings) != 1 ||
		!incomplete.Findings[0].Uncertain || len(unresolved.Diagnostics) != 1 ||
		unresolved.Diagnostics[0].Adapter != "hpa" || !unresolved.Diagnostics[0].Required {
		t.Fatalf("incomplete = %#v, discovery = %#v", incomplete, unresolved)
	}
}

func TestHPASourceRejectsConflictingMappingsForOneManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifestPath := filepath.Join(root, "hpa.yaml")
	if err := os.WriteFile(manifestPath, []byte(`apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: {name: worker}
spec:
  scaleTargetRef: {name: worker}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMapping := func(name string) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(`apiVersion: tcg.hpa/v1alpha1
kind: HPAMetricMappings
spec:
  mappings:
    - kubernetes: {type: External, metric: queue}
      ignore: non-Prometheus
`), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	configuration := testConfiguration(config.Sources{HorizontalPodAutoscalers: []config.HorizontalPodAutoscalerSource{
		{Pattern: manifestPath, MappingPath: writeMapping("one.yaml"), Required: false},
		{Pattern: manifestPath, MappingPath: writeMapping("two.yaml"), Required: true},
	}})
	discovery, _, err := Discover(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Diagnostics) != 1 || discovery.Diagnostics[0].Adapter != "hpa" ||
		!discovery.Diagnostics[0].Required || !strings.Contains(discovery.Diagnostics[0].Message, "multiple backend mappings") {
		t.Fatalf("diagnostics = %#v", discovery.Diagnostics)
	}
}

func TestHPADuplicateSourceUsesStrictestRequiredPolicy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifestPath := filepath.Join(root, "hpa.yaml")
	mappingPath := filepath.Join(root, "mapping.yaml")
	if err := os.WriteFile(manifestPath, []byte(`apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: {name: worker}
spec:
  scaleTargetRef: {name: worker}
  metrics:
    - type: External
      external:
        metric: {name: unmapped_queue}
        target: {type: Value, value: "1"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mappingPath, []byte(`apiVersion: tcg.hpa/v1alpha1
kind: HPAMetricMappings
spec:
  mappings:
    - kubernetes: {type: External, metric: other_queue}
      ignore: non-Prometheus
`), 0o644); err != nil {
		t.Fatal(err)
	}
	configuration := testConfiguration(config.Sources{HorizontalPodAutoscalers: []config.HorizontalPodAutoscalerSource{
		{Pattern: manifestPath, MappingPath: mappingPath, Required: false},
		{Pattern: manifestPath, MappingPath: mappingPath, Required: true},
	}})
	discovery, _, err := Discover(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Consumers) != 1 || len(discovery.Diagnostics) != 1 || !discovery.Diagnostics[0].Required {
		t.Fatalf("discovery = %#v", discovery)
	}
}

func TestArgoProductionAnalysisProducesBlockingDeploymentGateRisk(t *testing.T) {
	t.Parallel()

	manifestPath := filepath.Join(t.TempDir(), "analysis-template.yaml")
	writeManifest := func(query string) {
		t.Helper()
		contents := `apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: orders-rollout-gate
  namespace: commerce
  labels:
    environment: production
spec:
  args:
    - name: service-name
  metrics:
    - name: error-rate
      successCondition: result[0] < 0.01
      provider:
        prometheus:
          address: https://prometheus.example.test
          query: |-
            ` + strings.ReplaceAll(query, "\n", "\n            ") + "\n"
		if err := os.WriteFile(manifestPath, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	configuration := testConfiguration(config.Sources{ArgoRollouts: []config.SourcePattern{{
		Pattern: manifestPath, Required: true,
	}}})
	changeSet, err := config.NormalizeMigration(mustParseRemovalMigration(t))
	if err != nil {
		t.Fatal(err)
	}

	writeManifest(`sum(rate(old_metric{service="{{args.service-name}}"}[2m]))`)
	result, _, discovery, err := RunSafety(context.Background(), configuration, changeSet, safety.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != safety.StatusBlock || len(result.Findings) != 1 {
		t.Fatalf("result = %#v, want one BLOCK finding", result)
	}
	finding := result.Findings[0]
	if finding.Impact != impact.TypeDeploymentGateRisk || finding.Consumer.Kind != domain.ConsumerKindDeploymentGate ||
		finding.Consumer.Name != "orders-rollout-gate / error-rate" || finding.Criticality != domain.CriticalityCritical || finding.Uncertain {
		t.Fatalf("finding = %#v", finding)
	}
	if len(discovery.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", discovery.Diagnostics)
	}

	writeManifest(`{{args.metric-name}}`)
	incomplete, _, unresolved, err := RunSafety(context.Background(), configuration, changeSet, safety.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if incomplete.Status != safety.StatusIncomplete || len(incomplete.Findings) != 1 ||
		!incomplete.Findings[0].Uncertain || len(unresolved.Diagnostics) != 1 || !unresolved.Diagnostics[0].Required {
		t.Fatalf("incomplete = %#v, discovery = %#v", incomplete, unresolved)
	}
}

func testConfiguration(sources config.Sources) config.Config {
	configuration := config.Config{
		APIVersion: config.ConfigAPIVersion,
		Kind:       config.ConfigKind,
		Sources:    sources,
		Analysis: config.AnalysisConfig{
			IncludeTransitiveDependencies: true,
			UnresolvedReferencePolicy:     "error",
		},
		Policy: config.PolicyConfig{
			FailOnCriticalLegacyConsumer: true,
			FailOnCriticalUnknown:        true,
			MinimumBlockingCriticality:   "high",
		},
		Output: config.OutputConfig{Formats: []string{"json"}},
	}
	seenOrigins := make(map[string]struct{})
	for _, rawURL := range appendRemoteSourceURLs(sources) {
		parsed, err := remoteurl.ParseBaseURL(rawURL, "test remote")
		if err != nil {
			continue
		}
		origin, err := remoteurl.Origin(parsed)
		if err == nil {
			seenOrigins[origin] = struct{}{}
		}
	}
	if len(seenOrigins) != 0 {
		configuration.RemoteEvidence.Mode = config.RemoteEvidenceEnabled
		configuration.RemoteEvidence.AllowInsecureLoopback = true
		for origin := range seenOrigins {
			configuration.RemoteEvidence.AllowedOrigins = append(configuration.RemoteEvidence.AllowedOrigins, origin)
		}
		sort.Strings(configuration.RemoteEvidence.AllowedOrigins)
	}
	return configuration
}

func appendRemoteSourceURLs(sources config.Sources) []string {
	result := make([]string, 0, len(sources.PersesUsage)+len(sources.TempoQueries))
	for _, source := range sources.PersesUsage {
		result = append(result, source.URL)
	}
	for _, source := range sources.TempoQueries {
		result = append(result, source.URL)
	}
	return result
}

func mustParseRemovalMigration(t *testing.T) domain.Migration {
	t.Helper()
	migration, err := config.ParseMigration(strings.NewReader(`
apiVersion: telemetry-migration/v1alpha1
kind: Migration
metadata: {name: remote-source}
spec:
  changes:
    - id: remove
      kind: metric_remove
      domain: prometheus
      from: {metric: old_metric}
`))
	if err != nil {
		t.Fatal(err)
	}
	return migration
}

func absolutizePatterns(configuration *config.Config, root string) {
	groups := [][]config.SourcePattern{
		configuration.Sources.PrometheusRules,
		configuration.Sources.Grafana,
		configuration.Sources.Sloth,
		configuration.Sources.Pyrra,
		configuration.Sources.KEDA,
		configuration.Sources.ArgoRollouts,
	}
	for _, group := range groups {
		for index := range group {
			group[index].Pattern = filepath.Join(root, strings.TrimPrefix(group[index].Pattern, "./"))
		}
	}
	for index := range configuration.Sources.HorizontalPodAutoscalers {
		source := &configuration.Sources.HorizontalPodAutoscalers[index]
		source.Pattern = filepath.Join(root, strings.TrimPrefix(source.Pattern, "./"))
		source.MappingPath = filepath.Join(root, strings.TrimPrefix(source.MappingPath, "./"))
	}
}
