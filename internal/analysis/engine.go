// Package analysis orchestrates local adapters, graph construction, generic
// safety evaluation, and legacy migration readiness.
package analysis

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tadurisaikiran/telemetry-change-guard/adapters/argorollouts"
	"github.com/tadurisaikiran/telemetry-change-guard/adapters/grafana"
	"github.com/tadurisaikiran/telemetry-change-guard/adapters/hpa"
	"github.com/tadurisaikiran/telemetry-change-guard/adapters/keda"
	"github.com/tadurisaikiran/telemetry-change-guard/adapters/persesusage"
	"github.com/tadurisaikiran/telemetry-change-guard/adapters/prometheusrules"
	"github.com/tadurisaikiran/telemetry-change-guard/adapters/pyrra"
	"github.com/tadurisaikiran/telemetry-change-guard/adapters/runtimequeries"
	"github.com/tadurisaikiran/telemetry-change-guard/adapters/sloth"
	"github.com/tadurisaikiran/telemetry-change-guard/adapters/tempo"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/config"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/graph"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/impact"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/ownership"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/readiness"
	remoteurl "github.com/tadurisaikiran/telemetry-change-guard/internal/remote"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/safety"
	filesource "github.com/tadurisaikiran/telemetry-change-guard/internal/source"
	"github.com/tadurisaikiran/telemetry-change-guard/pkg/traceql"
)

// Run executes the complete deterministic local analysis pipeline.
func Run(
	ctx context.Context,
	configuration config.Config,
	migration domain.Migration,
) (readiness.Result, *graph.Graph, domain.Discovery, error) {
	changeSet, err := config.NormalizeMigration(migration)
	if err != nil {
		return readiness.Result{}, nil, domain.Discovery{}, fmt.Errorf("normalize migration: %w", err)
	}
	discovery, dependencyGraph, err := AnalyzeChangeSet(ctx, configuration, changeSet)
	if err != nil {
		return readiness.Result{}, nil, domain.Discovery{}, err
	}
	result, err := readiness.Evaluate(migration, discovery, dependencyGraph, ReadinessPolicy(configuration))
	if err != nil {
		return readiness.Result{}, nil, domain.Discovery{}, fmt.Errorf("evaluate readiness: %w", err)
	}
	return result, dependencyGraph, discovery, nil
}

// RunSafety executes the generic finding and policy pipeline. It is separate
// from Run so generic statuses cannot alter legacy migration readiness.
func RunSafety(
	ctx context.Context,
	configuration config.Config,
	changeSet domain.ChangeSet,
	policy safety.Policy,
) (safety.Result, *graph.Graph, domain.Discovery, error) {
	return RunSafetyWithDiagnostics(ctx, configuration, changeSet, policy, nil)
}

// RunSafetyWithDiagnostics evaluates a ChangeSet while retaining uncertainty
// reported by its deterministic change source. Source diagnostics enter the
// same authoritative status aggregation as consumer-adapter diagnostics.
func RunSafetyWithDiagnostics(
	ctx context.Context,
	configuration config.Config,
	changeSet domain.ChangeSet,
	policy safety.Policy,
	sourceDiagnostics []domain.Diagnostic,
) (safety.Result, *graph.Graph, domain.Discovery, error) {
	discovery, dependencyGraph, err := AnalyzeChangeSet(ctx, configuration, changeSet)
	if err != nil {
		return safety.ErrorResult(changeSet, nil, sourceDiagnostics, err), nil, domain.Discovery{}, err
	}
	discovery.Diagnostics = append(discovery.Diagnostics, sourceDiagnostics...)
	findings, err := impact.Analyze(
		changeSet,
		discovery,
		dependencyGraph,
		configuration.Analysis.IncludeTransitiveDependencies,
	)
	if err != nil {
		err = fmt.Errorf("analyze impact: %w", err)
		return safety.ErrorResult(changeSet, findings, discovery.Diagnostics, err), dependencyGraph, discovery, err
	}
	if limit := configuration.Execution.MaxFindings; configuration.Execution.RepositoryRoot != "" && len(findings) > limit {
		err = fmt.Errorf("finding count %d exceeds the execution limit of %d", len(findings), limit)
		return safety.ErrorResult(changeSet, findings[:limit], discovery.Diagnostics, err), dependencyGraph, discovery, err
	}
	result, err := safety.Evaluate(changeSet, findings, discovery.Diagnostics, policy)
	if err != nil {
		return result, dependencyGraph, discovery, fmt.Errorf("evaluate safety: %w", err)
	}
	return result, dependencyGraph, discovery, err
}

// AnalyzeChangeSet validates the generic change input, discovers downstream
// evidence, and builds the dependency graph shared by generic and legacy
// policy layers. It does not make a safety decision.
func AnalyzeChangeSet(
	ctx context.Context,
	configuration config.Config,
	changeSet domain.ChangeSet,
) (domain.Discovery, *graph.Graph, error) {
	if err := config.ValidateChangeSet(changeSet); err != nil {
		return domain.Discovery{}, nil, fmt.Errorf("validate change set: %w", err)
	}
	discovery, dependencyGraph, err := Discover(ctx, configuration)
	if err != nil {
		return domain.Discovery{}, nil, err
	}
	discovery.Diagnostics = append(
		discovery.Diagnostics,
		traceMappingDiagnostics(configuration, changeSet)...,
	)
	return discovery, dependencyGraph, nil
}

// ReadinessPolicy converts validated configuration into the exact policy used
// by Run. Candidate reanalysis calls this same function to avoid policy drift.
func ReadinessPolicy(configuration config.Config) readiness.Policy {
	return readiness.Policy{
		FailOnCriticalLegacyConsumer: configuration.Policy.FailOnCriticalLegacyConsumer,
		FailOnCriticalUnknown:        configuration.Policy.FailOnCriticalUnknown,
		MinimumBlockingCriticality:   domain.Criticality(configuration.Policy.MinimumBlockingCriticality),
		IncludeTransitive:            configuration.Analysis.IncludeTransitiveDependencies,
	}
}

// Discover runs configured adapters and constructs the dependency graph.
func Discover(ctx context.Context, configuration config.Config) (domain.Discovery, *graph.Graph, error) {
	var discovery domain.Discovery
	fileBudget, err := analysisFileBudget(configuration.Execution)
	if err != nil {
		return discovery, nil, fmt.Errorf("initialize local evidence policy: %w", err)
	}
	if fileBudget != nil {
		defer fileBudget.Close()
	}
	remotePolicy, err := newRemotePolicy(configuration.RemoteEvidence)
	if err != nil {
		return discovery, nil, fmt.Errorf("validate remote-evidence policy: %w", err)
	}
	environment, err := resolveAuthorizedEnvironmentReferences(configuration, remotePolicy)
	if err != nil {
		return discovery, nil, err
	}
	if err := loadPatterns(ctx, fileBudget, "prometheus_rules", configuration.Sources.PrometheusRules, &discovery,
		func(ctx context.Context, path string, required bool, reader io.Reader) (domain.Discovery, error) {
			return (prometheusrules.Loader{Required: required}).Parse(ctx, path, reader)
		}); err != nil {
		return discovery, nil, err
	}
	if err := loadPatterns(ctx, fileBudget, "grafana", configuration.Sources.Grafana, &discovery,
		func(_ context.Context, path string, required bool, reader io.Reader) (domain.Discovery, error) {
			return (grafana.Loader{Required: required}).Parse(path, reader)
		}); err != nil {
		return discovery, nil, err
	}
	if err := loadPatterns(ctx, fileBudget, "sloth", configuration.Sources.Sloth, &discovery,
		func(_ context.Context, path string, required bool, reader io.Reader) (domain.Discovery, error) {
			return (sloth.Loader{Required: required}).Parse(path, reader)
		}); err != nil {
		return discovery, nil, err
	}
	if err := loadPatterns(ctx, fileBudget, "pyrra", configuration.Sources.Pyrra, &discovery,
		func(ctx context.Context, path string, required bool, reader io.Reader) (domain.Discovery, error) {
			return (pyrra.Loader{Required: required}).Parse(ctx, path, reader)
		}); err != nil {
		return discovery, nil, err
	}
	if err := loadPatterns(ctx, fileBudget, "keda", configuration.Sources.KEDA, &discovery,
		func(ctx context.Context, path string, required bool, reader io.Reader) (domain.Discovery, error) {
			return (keda.Loader{Required: required}).Parse(ctx, path, reader)
		}); err != nil {
		return discovery, nil, err
	}
	if err := loadPatterns(ctx, fileBudget, "argo_rollouts", configuration.Sources.ArgoRollouts, &discovery,
		func(ctx context.Context, path string, required bool, reader io.Reader) (domain.Discovery, error) {
			return (argorollouts.Loader{Required: required}).Parse(ctx, path, reader)
		}); err != nil {
		return discovery, nil, err
	}
	if err := loadHorizontalPodAutoscalers(ctx, fileBudget, configuration.Sources.HorizontalPodAutoscalers, &discovery); err != nil {
		return discovery, nil, err
	}
	loadPersesUsage(ctx, configuration.Sources.PersesUsage, environment, remotePolicy, &discovery)
	if err := loadRuntimeQueries(ctx, fileBudget, configuration.Sources.RuntimeQueries, &discovery); err != nil {
		return discovery, nil, err
	}
	if err := loadTempoQueries(ctx, fileBudget, configuration.Sources.TempoQueries, configuration.Mappings.TraceAttributes, environment, remotePolicy, &discovery); err != nil {
		return discovery, nil, err
	}
	if err := validateDiscoveryLimits(configuration.Execution, discovery); err != nil {
		return discovery, nil, err
	}
	if fileBudget != nil && configuration.Ownership.Enabled {
		ownershipRoot, err := fileBudget.ValidateDirectory(configuration.Ownership.RepositoryRoot)
		if err != nil {
			return discovery, nil, fmt.Errorf("validate ownership repository root: %w", err)
		}
		configuration.Ownership.RepositoryRoot = ownershipRoot
	}
	if err := ownership.EnrichWithBudget(ctx, configuration.Ownership, &discovery, fileBudget); err != nil {
		return domain.Discovery{}, nil, fmt.Errorf("enrich consumer ownership: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return domain.Discovery{}, nil, err
	}
	dependencyGraph, err := impact.BuildGraph(discovery)
	if err != nil {
		return domain.Discovery{}, nil, fmt.Errorf("build dependency graph: %w", err)
	}
	if err := validateGraphLimits(configuration.Execution, dependencyGraph); err != nil {
		return domain.Discovery{}, nil, err
	}
	return discovery, dependencyGraph, nil
}

func loadTempoQueries(
	ctx context.Context,
	fileBudget *filesource.Budget,
	sources []config.TempoQuerySource,
	mappings []config.TraceAttributeMapping,
	environment map[string]environmentValue,
	remotePolicy remotePolicy,
	discovery *domain.Discovery,
) error {
	adapterMappings := make([]tempo.AttributeMapping, 0, len(mappings))
	for _, mapping := range mappings {
		adapterMappings = append(adapterMappings, tempo.AttributeMapping{
			Scope:         traceScope(mapping.Scope),
			OpenTelemetry: mapping.OpenTelemetry,
			Tempo:         mapping.Tempo,
		})
	}
	configured := append([]config.TempoQuerySource(nil), sources...)
	sort.Slice(configured, func(i, j int) bool { return tempoSourceKey(configured[i]) < tempoSourceKey(configured[j]) })
	type loadTask struct {
		source   config.TempoQuerySource
		path     string
		required bool
		conflict bool
	}
	tasks := make(map[string]*loadTask)
	for _, source := range configured {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := remotePolicy.authorize(source.URL, source.BearerTokenEnv != ""); err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, tempoDiagnostic(source, source.Pattern, err.Error()))
			continue
		}
		_, err := time.ParseDuration(source.Timeout)
		if err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, tempoDiagnostic(source, source.Pattern, err.Error()))
			continue
		}
		var token string
		if source.BearerTokenEnv != "" {
			resolved := environment[source.BearerTokenEnv]
			token = resolved.value
			if !resolved.exists || token == "" {
				discovery.Diagnostics = append(discovery.Diagnostics, tempoDiagnostic(
					source,
					source.Pattern,
					fmt.Sprintf("bearer token environment variable %q is unset or empty", source.BearerTokenEnv),
				))
				continue
			}
		}
		files, err := expandFiles(fileBudget, source.Pattern)
		if err != nil {
			if fileBudget != nil {
				return fmt.Errorf("enforce Tempo source policy: %w", err)
			}
			discovery.Diagnostics = append(discovery.Diagnostics, tempoDiagnostic(source, source.Pattern, err.Error()))
			continue
		}
		if len(files) == 0 {
			discovery.Diagnostics = append(discovery.Diagnostics, tempoDiagnostic(source, source.Pattern, "source pattern matched no files"))
			continue
		}
		for _, file := range files {
			identity, display, err := canonicalFile(fileBudget, file)
			if err != nil {
				discovery.Diagnostics = append(discovery.Diagnostics, tempoDiagnostic(source, file, err.Error()))
				continue
			}
			loadedKey := normalizedRemoteBase(source.URL) + "\x00" + identity
			task := tasks[loadedKey]
			if task == nil {
				tasks[loadedKey] = &loadTask{source: source, path: display, required: source.Required}
				continue
			}
			task.required = task.required || source.Required
			if tempoSourceSettingsKey(task.source) != tempoSourceSettingsKey(source) {
				task.conflict = true
			}
		}
	}
	taskKeys := make([]string, 0, len(tasks))
	for key := range tasks {
		taskKeys = append(taskKeys, key)
	}
	sort.Strings(taskKeys)
	sourceContexts := make(map[string]context.Context)
	var sourceCancels []context.CancelFunc
	defer func() {
		for _, cancel := range sourceCancels {
			cancel()
		}
	}()
	for _, key := range taskKeys {
		task := tasks[key]
		source := task.source
		source.Required = task.required
		if task.conflict {
			discovery.Diagnostics = append(discovery.Diagnostics, tempoDiagnostic(
				source,
				task.path,
				"Tempo query file is configured repeatedly with conflicting URL, timeout, token environment, or criticality settings",
			))
			continue
		}
		timeout, _ := time.ParseDuration(source.Timeout)
		var token string
		if source.BearerTokenEnv != "" {
			token = environment[source.BearerTokenEnv].value
		}
		validator := tempo.Client{
			BaseURL: source.URL, Timeout: timeout, BearerToken: token,
			AllowInsecureLoopback: remotePolicy.allowInsecureLoopback,
		}
		contextKey := tempoSourceSettingsKey(source)
		sourceContext := sourceContexts[contextKey]
		if sourceContext == nil {
			var cancel context.CancelFunc
			sourceContext, cancel = context.WithTimeout(ctx, timeout)
			sourceContexts[contextKey] = sourceContext
			sourceCancels = append(sourceCancels, cancel)
		}
		reader, err := openFile(fileBudget, task.path)
		if err != nil {
			if fileBudget != nil {
				return fmt.Errorf("open secured Tempo source %q: %w", task.path, err)
			}
			discovery.Diagnostics = append(discovery.Diagnostics, tempoDiagnostic(source, task.path, err.Error()))
			continue
		}
		additional, loadErr := (tempo.Loader{
			Required:           task.required,
			DefaultCriticality: domain.Criticality(source.Criticality),
			Validator:          validator,
			TempoURL:           source.URL,
			Mappings:           adapterMappings,
		}).Parse(sourceContext, task.path, reader)
		closeErr := reader.Close()
		if loadErr != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, tempoDiagnostic(source, task.path, loadErr.Error()))
			continue
		}
		if closeErr != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, tempoDiagnostic(source, task.path, closeErr.Error()))
			continue
		}
		discovery.Append(additional)
	}
	return nil
}

func traceScope(scope string) traceql.Scope {
	if scope == "resource" {
		return traceql.ScopeResource
	}
	return traceql.ScopeSpan
}

func tempoDiagnostic(source config.TempoQuerySource, path, message string) domain.Diagnostic {
	return domain.Diagnostic{
		Adapter:  "tempo",
		Source:   domain.SourceLocation{File: path, URL: source.URL},
		Message:  message,
		Required: source.Required,
	}
}

func traceMappingDiagnostics(configuration config.Config, changeSet domain.ChangeSet) []domain.Diagnostic {
	if len(configuration.Sources.TempoQueries) == 0 {
		return nil
	}
	mappingRequired := false
	for _, source := range configuration.Sources.TempoQueries {
		mappingRequired = mappingRequired || source.Required
	}
	available := make(map[string]struct{}, len(configuration.Mappings.TraceAttributes))
	for _, mapping := range configuration.Mappings.TraceAttributes {
		available[mapping.Scope+"\x00"+mapping.OpenTelemetry] = struct{}{}
	}
	var diagnostics []domain.Diagnostic
	for _, change := range changeSet.Changes {
		if change.Domain != domain.DomainOpenTelemetry || !isTraceAttribute(change.From.Kind) {
			continue
		}
		scope := symbolScope(change.From.Kind)
		for _, symbol := range changeSymbols(change) {
			if _, exists := available[scope+"\x00"+symbol.Name]; exists {
				continue
			}
			diagnostics = append(diagnostics, domain.Diagnostic{
				Adapter: "tempo_mapping",
				Source:  domain.SourceLocation{File: "mappings.traceAttributes"},
				Message: fmt.Sprintf(
					"change %q requires an explicit %s mapping for OpenTelemetry attribute %q",
					change.ID,
					scope,
					symbol.Name,
				),
				Required: mappingRequired,
			})
		}
	}
	return diagnostics
}

func isTraceAttribute(kind domain.SymbolKind) bool {
	return kind == domain.SymbolKindSpanAttribute || kind == domain.SymbolKindResourceAttribute
}

func symbolScope(kind domain.SymbolKind) string {
	if kind == domain.SymbolKindResourceAttribute {
		return "resource"
	}
	return "span"
}

func changeSymbols(change domain.Change) []domain.Symbol {
	result := []domain.Symbol{change.From}
	if change.To != nil {
		result = append(result, *change.To)
	}
	return result
}

func loadRuntimeQueries(
	ctx context.Context,
	fileBudget *filesource.Budget,
	sources []config.RuntimeQuerySource,
	discovery *domain.Discovery,
) error {
	type loadTask struct {
		source   config.RuntimeQuerySource
		path     string
		window   time.Duration
		required bool
		conflict bool
	}
	tasks := make(map[string]*loadTask)
	sources = append([]config.RuntimeQuerySource(nil), sources...)
	sort.Slice(sources, func(i, j int) bool { return runtimeSourceKey(sources[i]) < runtimeSourceKey(sources[j]) })
	for _, source := range sources {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		window, err := time.ParseDuration(source.Window)
		if err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, runtimeQueryDiagnostic(source, source.Pattern, err.Error()))
			continue
		}
		files, err := expandFiles(fileBudget, source.Pattern)
		if err != nil {
			if fileBudget != nil {
				return fmt.Errorf("enforce runtime-query source policy: %w", err)
			}
			discovery.Diagnostics = append(discovery.Diagnostics, runtimeQueryDiagnostic(source, source.Pattern, err.Error()))
			continue
		}
		if len(files) == 0 {
			discovery.Diagnostics = append(discovery.Diagnostics, runtimeQueryDiagnostic(source, source.Pattern, "source pattern matched no files"))
			continue
		}
		for _, file := range files {
			identity, display, err := canonicalFile(fileBudget, file)
			if err != nil {
				discovery.Diagnostics = append(discovery.Diagnostics, runtimeQueryDiagnostic(source, file, err.Error()))
				continue
			}
			task := tasks[identity]
			if task == nil {
				tasks[identity] = &loadTask{
					source: source, path: display, window: window, required: source.Required,
				}
				continue
			}
			task.required = task.required || source.Required
			task.conflict = task.conflict || runtimeSourceSettingsKey(task.source) != runtimeSourceSettingsKey(source)
		}
	}
	keys := make([]string, 0, len(tasks))
	for key := range tasks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		task := tasks[key]
		source := task.source
		source.Required = task.required
		if task.conflict {
			discovery.Diagnostics = append(discovery.Diagnostics, runtimeQueryDiagnostic(
				source,
				task.path,
				"runtime-query file is configured repeatedly with conflicting format, window, or criticality settings",
			))
			continue
		}
		reader, err := openFile(fileBudget, task.path)
		if err != nil {
			if fileBudget != nil {
				return fmt.Errorf("open secured runtime-query source %q: %w", task.path, err)
			}
			discovery.Diagnostics = append(discovery.Diagnostics, runtimeQueryDiagnostic(source, task.path, err.Error()))
			continue
		}
		additional, loadErr := (runtimequeries.Loader{
			Required:    task.required,
			Format:      source.Format,
			Window:      task.window,
			Criticality: domain.Criticality(source.Criticality),
		}).Parse(ctx, task.path, reader)
		closeErr := reader.Close()
		if loadErr != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, runtimeQueryDiagnostic(source, task.path, loadErr.Error()))
			continue
		}
		if closeErr != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, runtimeQueryDiagnostic(source, task.path, closeErr.Error()))
			continue
		}
		discovery.Append(additional)
	}
	return nil
}

func runtimeQueryDiagnostic(source config.RuntimeQuerySource, path, message string) domain.Diagnostic {
	return domain.Diagnostic{
		Adapter:  "runtime_queries",
		Source:   domain.SourceLocation{File: path},
		Message:  message,
		Required: source.Required,
	}
}

func runtimeSourceKey(source config.RuntimeQuerySource) string {
	return filepath.Clean(source.Pattern) + "\x00" + runtimeSourceSettingsKey(source) + "\x00" + fmt.Sprint(source.Required)
}

func runtimeSourceSettingsKey(source config.RuntimeQuerySource) string {
	return source.Format + "\x00" + source.Window + "\x00" + source.Criticality
}

func loadPersesUsage(
	ctx context.Context,
	sources []config.PersesUsageSource,
	environment map[string]environmentValue,
	remotePolicy remotePolicy,
	discovery *domain.Discovery,
) {
	configured := append([]config.PersesUsageSource(nil), sources...)
	sort.Slice(configured, func(i, j int) bool { return persesSourceKey(configured[i]) < persesSourceKey(configured[j]) })
	type loadTask struct {
		source   config.PersesUsageSource
		required bool
		conflict bool
	}
	tasks := make(map[string]*loadTask)
	for _, source := range configured {
		key := normalizedRemoteBase(source.URL)
		task := tasks[key]
		if task == nil {
			tasks[key] = &loadTask{source: source, required: source.Required}
			continue
		}
		task.required = task.required || source.Required
		task.conflict = task.conflict || persesSourceSettingsKey(task.source) != persesSourceSettingsKey(source)
	}
	keys := make([]string, 0, len(tasks))
	for key := range tasks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		task := tasks[key]
		source := task.source
		source.Required = task.required
		if err := ctx.Err(); err != nil {
			return
		}
		if task.conflict {
			discovery.Diagnostics = append(discovery.Diagnostics, persesDiagnostic(
				source,
				"Perses metrics-usage origin is configured repeatedly with conflicting timeout or token environment settings",
			))
			continue
		}
		if err := remotePolicy.authorize(source.URL, source.BearerTokenEnv != ""); err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, persesDiagnostic(source, err.Error()))
			continue
		}
		timeout, err := time.ParseDuration(source.Timeout)
		if err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, persesDiagnostic(source, err.Error()))
			continue
		}
		var token string
		if source.BearerTokenEnv != "" {
			resolved := environment[source.BearerTokenEnv]
			token = resolved.value
			if !resolved.exists || token == "" {
				discovery.Diagnostics = append(discovery.Diagnostics, persesDiagnostic(
					source,
					fmt.Sprintf("bearer token environment variable %q is unset or empty", source.BearerTokenEnv),
				))
				continue
			}
		}
		additional, err := (persesusage.Loader{
			BaseURL:               source.URL,
			Required:              source.Required,
			Timeout:               timeout,
			BearerToken:           token,
			AllowInsecureLoopback: remotePolicy.allowInsecureLoopback,
		}).Discover(ctx)
		if err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, persesDiagnostic(source, err.Error()))
			continue
		}
		discovery.Append(additional)
	}
}

type environmentValue struct {
	value  string
	exists bool
}

type remotePolicy struct {
	enabled               bool
	allowedOrigins        map[string]struct{}
	allowInsecureLoopback bool
}

func newRemotePolicy(policy config.RemoteEvidencePolicy) (remotePolicy, error) {
	mode := strings.TrimSpace(policy.Mode)
	if mode == "" {
		mode = config.RemoteEvidenceDisabled
	}
	if mode != config.RemoteEvidenceEnabled && mode != config.RemoteEvidenceDisabled {
		return remotePolicy{}, fmt.Errorf("mode must be %q or %q", config.RemoteEvidenceDisabled, config.RemoteEvidenceEnabled)
	}
	result := remotePolicy{
		enabled:               mode == config.RemoteEvidenceEnabled,
		allowedOrigins:        make(map[string]struct{}, len(policy.AllowedOrigins)),
		allowInsecureLoopback: policy.AllowInsecureLoopback,
	}
	for _, configured := range policy.AllowedOrigins {
		origin, err := remoteurl.ParseAllowedOrigin(configured)
		if err != nil {
			return remotePolicy{}, fmt.Errorf("invalid allowed origin %q: %w", configured, err)
		}
		result.allowedOrigins[origin] = struct{}{}
	}
	if !result.enabled && (len(result.allowedOrigins) != 0 || result.allowInsecureLoopback) {
		return remotePolicy{}, fmt.Errorf("allowed origins and insecure loopback are invalid when remote evidence is disabled")
	}
	if result.enabled && len(result.allowedOrigins) == 0 {
		return remotePolicy{}, fmt.Errorf("enabled remote evidence requires at least one exact allowed origin supplied outside repository configuration")
	}
	return result, nil
}

func (policy remotePolicy) authorize(rawURL string, credentialConfigured bool) error {
	if !policy.enabled {
		return fmt.Errorf("remote evidence is disabled by execution policy")
	}
	parsed, err := remoteurl.ParseBaseURL(rawURL, "remote evidence")
	if err != nil {
		return err
	}
	origin, err := remoteurl.Origin(parsed)
	if err != nil {
		return fmt.Errorf("canonicalize remote evidence origin: %w", err)
	}
	if _, allowed := policy.allowedOrigins[origin]; !allowed {
		return fmt.Errorf("remote evidence origin %q is not in the execution policy allowlist", origin)
	}
	if err := remoteurl.ValidateCredentialTransport(
		parsed,
		credentialConfigured,
		policy.allowInsecureLoopback,
		"remote evidence",
	); err != nil {
		return err
	}
	return nil
}

func resolveAuthorizedEnvironmentReferences(
	configuration config.Config,
	policy remotePolicy,
) (map[string]environmentValue, error) {
	names := make([]string, 0, len(configuration.Sources.PersesUsage)+len(configuration.Sources.TempoQueries))
	for _, source := range configuration.Sources.PersesUsage {
		if source.BearerTokenEnv != "" && policy.authorize(source.URL, true) == nil {
			names = append(names, source.BearerTokenEnv)
		}
	}
	for _, source := range configuration.Sources.TempoQueries {
		if source.BearerTokenEnv != "" && policy.authorize(source.URL, true) == nil {
			names = append(names, source.BearerTokenEnv)
		}
	}
	return resolveEnvironmentNames(names)
}

func resolveEnvironmentReferences(configuration config.Config) (map[string]environmentValue, error) {
	names := make([]string, 0, len(configuration.Sources.PersesUsage)+len(configuration.Sources.TempoQueries))
	for _, source := range configuration.Sources.PersesUsage {
		if source.BearerTokenEnv != "" {
			names = append(names, source.BearerTokenEnv)
		}
	}
	for _, source := range configuration.Sources.TempoQueries {
		if source.BearerTokenEnv != "" {
			names = append(names, source.BearerTokenEnv)
		}
	}

	return resolveEnvironmentNames(names)
}

func resolveEnvironmentNames(names []string) (map[string]environmentValue, error) {
	resolved := make(map[string]environmentValue, len(names))
	for _, name := range names {
		if _, exists := resolved[name]; exists {
			continue
		}
		value, exists, err := config.LookupEnvironment(name)
		if err != nil {
			return nil, fmt.Errorf("resolve bearer token environment variable %q: %w", name, err)
		}
		resolved[name] = environmentValue{value: value, exists: exists}
	}
	return resolved, nil
}

func normalizedRemoteBase(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func persesSourceKey(source config.PersesUsageSource) string {
	return normalizedRemoteBase(source.URL) + "\x00" + persesSourceSettingsKey(source) + "\x00" + fmt.Sprint(source.Required)
}

func persesSourceSettingsKey(source config.PersesUsageSource) string {
	return source.Timeout + "\x00" + source.BearerTokenEnv
}

func tempoSourceKey(source config.TempoQuerySource) string {
	return normalizedRemoteBase(source.URL) + "\x00" + filepath.Clean(source.Pattern) + "\x00" +
		tempoSourceSettingsKey(source) + "\x00" + fmt.Sprint(source.Required)
}

func tempoSourceSettingsKey(source config.TempoQuerySource) string {
	return normalizedRemoteBase(source.URL) + "\x00" + source.Timeout + "\x00" +
		source.BearerTokenEnv + "\x00" + source.Criticality
}

func persesDiagnostic(source config.PersesUsageSource, message string) domain.Diagnostic {
	return domain.Diagnostic{
		Adapter:  "perses_metrics_usage",
		Source:   domain.SourceLocation{URL: source.URL},
		Message:  message,
		Required: source.Required,
	}
}

type fileLoader func(context.Context, string, bool, io.Reader) (domain.Discovery, error)

func loadHorizontalPodAutoscalers(
	ctx context.Context,
	fileBudget *filesource.Budget,
	sources []config.HorizontalPodAutoscalerSource,
	discovery *domain.Discovery,
) error {
	type loadTask struct {
		path         string
		mapping      hpa.Mapping
		mappingPaths []string
		required     bool
	}
	tasks := make(map[string]*loadTask)
	sources = append([]config.HorizontalPodAutoscalerSource(nil), sources...)
	sort.Slice(sources, func(i, j int) bool {
		left := filepath.Clean(sources[i].Pattern) + "\x00" + filepath.Clean(sources[i].MappingPath)
		right := filepath.Clean(sources[j].Pattern) + "\x00" + filepath.Clean(sources[j].MappingPath)
		if left != right {
			return left < right
		}
		return sources[i].Required && !sources[j].Required
	})
	for _, source := range sources {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, mappingPath, err := canonicalFile(fileBudget, source.MappingPath)
		if err != nil {
			if fileBudget != nil {
				return fmt.Errorf("enforce HPA mapping policy: %w", err)
			}
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter: "hpa_mapping", Source: domain.SourceLocation{File: source.MappingPath},
				Message: err.Error(), Required: source.Required,
			})
			continue
		}
		mappingReader, err := openFile(fileBudget, mappingPath)
		if err != nil {
			if fileBudget != nil {
				return fmt.Errorf("open secured HPA mapping %q: %w", mappingPath, err)
			}
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter: "hpa_mapping", Source: domain.SourceLocation{File: mappingPath},
				Message: err.Error(), Required: source.Required,
			})
			continue
		}
		mapping, parseErr := hpa.ParseMapping(mappingReader)
		closeErr := mappingReader.Close()
		if parseErr != nil {
			err = parseErr
		} else {
			err = closeErr
		}
		if err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  "hpa_mapping",
				Source:   domain.SourceLocation{File: mappingPath},
				Message:  err.Error(),
				Required: source.Required,
			})
			continue
		}
		files, err := expandFiles(fileBudget, source.Pattern)
		if err != nil {
			if fileBudget != nil {
				return fmt.Errorf("enforce HPA source policy: %w", err)
			}
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  "hpa",
				Source:   domain.SourceLocation{File: source.Pattern},
				Message:  err.Error(),
				Required: source.Required,
			})
			continue
		}
		if len(files) == 0 {
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  "hpa",
				Source:   domain.SourceLocation{File: source.Pattern},
				Message:  "source pattern matched no files",
				Required: source.Required,
			})
			continue
		}
		for _, file := range files {
			identity, display, canonicalErr := canonicalFile(fileBudget, file)
			if canonicalErr != nil {
				discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
					Adapter: "hpa", Source: domain.SourceLocation{File: file},
					Message: canonicalErr.Error(), Required: source.Required,
				})
				continue
			}
			task, exists := tasks[identity]
			if !exists {
				tasks[identity] = &loadTask{
					path:         display,
					mapping:      mapping,
					mappingPaths: []string{mappingPath},
					required:     source.Required,
				}
				continue
			}
			task.required = task.required || source.Required
			seenMapping := false
			for _, configured := range task.mappingPaths {
				seenMapping = seenMapping || configured == mappingPath
			}
			if !seenMapping {
				task.mappingPaths = append(task.mappingPaths, mappingPath)
				sort.Strings(task.mappingPaths)
			}
		}
	}

	keys := make([]string, 0, len(tasks))
	for key := range tasks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		task := tasks[key]
		file := task.path
		if len(task.mappingPaths) != 1 {
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  "hpa",
				Source:   domain.SourceLocation{File: file},
				Message:  fmt.Sprintf("HPA manifest is configured with multiple backend mappings %q", task.mappingPaths),
				Required: task.required,
			})
			continue
		}
		reader, err := openFile(fileBudget, file)
		if err != nil {
			if fileBudget != nil {
				return fmt.Errorf("open secured HPA source %q: %w", file, err)
			}
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter: "hpa", Source: domain.SourceLocation{File: file}, Message: err.Error(), Required: task.required,
			})
			continue
		}
		additional, loadErr := (hpa.Loader{
			Required:      task.required,
			Mapping:       task.mapping,
			MappingSource: task.mappingPaths[0],
		}).Parse(ctx, file, reader)
		closeErr := reader.Close()
		if loadErr != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  "hpa",
				Source:   domain.SourceLocation{File: file},
				Message:  loadErr.Error(),
				Required: task.required,
			})
			continue
		}
		if closeErr != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter: "hpa", Source: domain.SourceLocation{File: file}, Message: closeErr.Error(), Required: task.required,
			})
			continue
		}
		discovery.Append(additional)
	}
	return nil
}

func loadPatterns(
	ctx context.Context,
	fileBudget *filesource.Budget,
	adapter string,
	patterns []config.SourcePattern,
	discovery *domain.Discovery,
	load fileLoader,
) error {
	configured := make([]filesource.Pattern, 0, len(patterns))
	for _, pattern := range patterns {
		configured = append(configured, filesource.Pattern{Path: pattern.Pattern, Required: pattern.Required})
	}
	var matches []filesource.Match
	var failures []filesource.Failure
	if fileBudget == nil {
		matches, failures = filesource.ExpandPatterns(configured)
	} else {
		var err error
		matches, failures, err = fileBudget.ExpandPatterns(configured)
		if err != nil {
			return fmt.Errorf("enforce %s source policy: %w", adapter, err)
		}
	}
	for _, failure := range failures {
		discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
			Adapter:  adapter,
			Source:   domain.SourceLocation{File: failure.Pattern},
			Message:  failure.Err.Error(),
			Required: failure.Required,
		})
	}
	for _, match := range matches {
		if err := ctx.Err(); err != nil {
			return err
		}
		reader, err := openFile(fileBudget, match.Path)
		if err != nil {
			if fileBudget != nil {
				return fmt.Errorf("open secured %s source %q: %w", adapter, match.Path, err)
			}
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter: adapter, Source: domain.SourceLocation{File: match.Path}, Message: err.Error(), Required: match.Required,
			})
			continue
		}
		additional, loadErr := load(ctx, match.Path, match.Required, reader)
		closeErr := reader.Close()
		if loadErr != nil {
			message := loadErr.Error()
			if len(match.Patterns) > 1 {
				message = fmt.Sprintf("%s (matched by patterns %q)", message, match.Patterns)
			}
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  adapter,
				Source:   domain.SourceLocation{File: match.Path},
				Message:  message,
				Required: match.Required,
			})
			continue
		}
		if closeErr != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter: adapter, Source: domain.SourceLocation{File: match.Path}, Message: closeErr.Error(), Required: match.Required,
			})
			continue
		}
		discovery.Append(additional)
	}
	return nil
}

func analysisFileBudget(policy config.ExecutionPolicy) (*filesource.Budget, error) {
	if strings.TrimSpace(policy.RepositoryRoot) == "" {
		return nil, nil
	}
	for name, value := range map[string]int{
		"max consumers":   policy.MaxConsumers,
		"max references":  policy.MaxReferences,
		"max productions": policy.MaxProductions,
		"max graph nodes": policy.MaxGraphNodes,
		"max graph edges": policy.MaxGraphEdges,
		"max findings":    policy.MaxFindings,
	} {
		if value <= 0 {
			return nil, fmt.Errorf("%s must be positive", name)
		}
	}
	return filesource.NewBudget(policy.RepositoryRoot, policy.MaxSourceFiles, policy.MaxSourceBytes)
}

func expandFiles(budget *filesource.Budget, pattern string) ([]string, error) {
	if budget == nil {
		return filesource.Expand(pattern)
	}
	return budget.Expand(pattern)
}

func canonicalFile(budget *filesource.Budget, path string) (string, string, error) {
	if budget == nil {
		return filesource.CanonicalFile(path)
	}
	return budget.Register(path)
}

func openFile(budget *filesource.Budget, path string) (*os.File, error) {
	if budget == nil {
		return os.Open(path)
	}
	return budget.Open(path)
}

func validateDiscoveryLimits(policy config.ExecutionPolicy, discovery domain.Discovery) error {
	if policy.RepositoryRoot == "" {
		return nil
	}
	for _, limit := range []struct {
		label string
		count int
		max   int
	}{
		{label: "consumer", count: len(discovery.Consumers), max: policy.MaxConsumers},
		{label: "reference", count: len(discovery.References), max: policy.MaxReferences},
		{label: "production", count: len(discovery.Productions), max: policy.MaxProductions},
	} {
		if limit.count > limit.max {
			return fmt.Errorf("%s count %d exceeds the execution limit of %d", limit.label, limit.count, limit.max)
		}
	}
	return nil
}

func validateGraphLimits(policy config.ExecutionPolicy, target *graph.Graph) error {
	if policy.RepositoryRoot == "" {
		return nil
	}
	if count := len(target.Nodes()); count > policy.MaxGraphNodes {
		return fmt.Errorf("graph node count %d exceeds the execution limit of %d", count, policy.MaxGraphNodes)
	}
	if count := len(target.Edges()); count > policy.MaxGraphEdges {
		return fmt.Errorf("graph edge count %d exceeds the execution limit of %d", count, policy.MaxGraphEdges)
	}
	return nil
}
