// Package analysis orchestrates local adapters, graph construction, generic
// safety evaluation, and legacy migration readiness.
package analysis

import (
	"context"
	"fmt"
	"time"

	"github.com/tadurisaikiran/telemetry-change-guard/adapters/grafana"
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
	environment, err := resolveEnvironmentReferences(configuration)
	if err != nil {
		return discovery, nil, err
	}
	loadPatterns(ctx, "prometheus_rules", configuration.Sources.PrometheusRules, &discovery,
		func(ctx context.Context, path string, required bool) (domain.Discovery, error) {
			return (prometheusrules.Loader{Required: required}).LoadFile(ctx, path)
		})
	loadPatterns(ctx, "grafana", configuration.Sources.Grafana, &discovery,
		func(ctx context.Context, path string, required bool) (domain.Discovery, error) {
			return (grafana.Loader{Required: required}).LoadFile(ctx, path)
		})
	loadPatterns(ctx, "sloth", configuration.Sources.Sloth, &discovery,
		func(ctx context.Context, path string, required bool) (domain.Discovery, error) {
			return (sloth.Loader{Required: required}).LoadFile(ctx, path)
		})
	loadPatterns(ctx, "pyrra", configuration.Sources.Pyrra, &discovery,
		func(ctx context.Context, path string, required bool) (domain.Discovery, error) {
			return (pyrra.Loader{Required: required}).LoadFile(ctx, path)
		})
	loadPersesUsage(ctx, configuration.Sources.PersesUsage, environment, &discovery)
	loadRuntimeQueries(ctx, configuration.Sources.RuntimeQueries, &discovery)
	loadTempoQueries(ctx, configuration.Sources.TempoQueries, configuration.Mappings.TraceAttributes, environment, &discovery)
	if err := ownership.Enrich(ctx, configuration.Ownership, &discovery); err != nil {
		return domain.Discovery{}, nil, fmt.Errorf("enrich consumer ownership: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return domain.Discovery{}, nil, err
	}
	dependencyGraph, err := impact.BuildGraph(discovery)
	if err != nil {
		return domain.Discovery{}, nil, fmt.Errorf("build dependency graph: %w", err)
	}
	return discovery, dependencyGraph, nil
}

func loadTempoQueries(
	ctx context.Context,
	sources []config.TempoQuerySource,
	mappings []config.TraceAttributeMapping,
	environment map[string]environmentValue,
	discovery *domain.Discovery,
) {
	adapterMappings := make([]tempo.AttributeMapping, 0, len(mappings))
	for _, mapping := range mappings {
		adapterMappings = append(adapterMappings, tempo.AttributeMapping{
			Scope:         traceScope(mapping.Scope),
			OpenTelemetry: mapping.OpenTelemetry,
			Tempo:         mapping.Tempo,
		})
	}
	loaded := make(map[string]struct{})
	for _, source := range sources {
		if ctx.Err() != nil {
			return
		}
		timeout, err := time.ParseDuration(source.Timeout)
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
		files, err := filesource.Expand(source.Pattern)
		if err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, tempoDiagnostic(source, source.Pattern, err.Error()))
			continue
		}
		if len(files) == 0 {
			discovery.Diagnostics = append(discovery.Diagnostics, tempoDiagnostic(source, source.Pattern, "source pattern matched no files"))
			continue
		}
		validator := tempo.Client{BaseURL: source.URL, Timeout: timeout, BearerToken: token}
		sourceContext, cancel := context.WithTimeout(ctx, timeout)
		for _, file := range files {
			loadedKey := source.URL + "\x00" + file
			if _, exists := loaded[loadedKey]; exists {
				continue
			}
			loaded[loadedKey] = struct{}{}
			additional, err := (tempo.Loader{
				Required:           source.Required,
				DefaultCriticality: domain.Criticality(source.Criticality),
				Validator:          validator,
				TempoURL:           source.URL,
				Mappings:           adapterMappings,
			}).LoadFile(sourceContext, file)
			if err != nil {
				discovery.Diagnostics = append(discovery.Diagnostics, tempoDiagnostic(source, file, err.Error()))
				continue
			}
			discovery.Append(additional)
		}
		cancel()
	}
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
	sources []config.RuntimeQuerySource,
	discovery *domain.Discovery,
) {
	loaded := make(map[string]struct{})
	for _, source := range sources {
		if ctx.Err() != nil {
			return
		}
		window, err := time.ParseDuration(source.Window)
		if err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, runtimeQueryDiagnostic(source, source.Pattern, err.Error()))
			continue
		}
		files, err := filesource.Expand(source.Pattern)
		if err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, runtimeQueryDiagnostic(source, source.Pattern, err.Error()))
			continue
		}
		if len(files) == 0 {
			discovery.Diagnostics = append(discovery.Diagnostics, runtimeQueryDiagnostic(source, source.Pattern, "source pattern matched no files"))
			continue
		}
		for _, file := range files {
			if _, exists := loaded[file]; exists {
				continue
			}
			loaded[file] = struct{}{}
			additional, err := (runtimequeries.Loader{
				Required:    source.Required,
				Format:      source.Format,
				Window:      window,
				Criticality: domain.Criticality(source.Criticality),
			}).LoadFile(ctx, file)
			if err != nil {
				discovery.Diagnostics = append(discovery.Diagnostics, runtimeQueryDiagnostic(source, file, err.Error()))
				continue
			}
			discovery.Append(additional)
		}
	}
}

func runtimeQueryDiagnostic(source config.RuntimeQuerySource, path, message string) domain.Diagnostic {
	return domain.Diagnostic{
		Adapter:  "runtime_queries",
		Source:   domain.SourceLocation{File: path},
		Message:  message,
		Required: source.Required,
	}
}

func loadPersesUsage(
	ctx context.Context,
	sources []config.PersesUsageSource,
	environment map[string]environmentValue,
	discovery *domain.Discovery,
) {
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return
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
			BaseURL:     source.URL,
			Required:    source.Required,
			Timeout:     timeout,
			BearerToken: token,
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

func persesDiagnostic(source config.PersesUsageSource, message string) domain.Diagnostic {
	return domain.Diagnostic{
		Adapter:  "perses_metrics_usage",
		Source:   domain.SourceLocation{URL: source.URL},
		Message:  message,
		Required: source.Required,
	}
}

type fileLoader func(context.Context, string, bool) (domain.Discovery, error)

func loadPatterns(
	ctx context.Context,
	adapter string,
	patterns []config.SourcePattern,
	discovery *domain.Discovery,
	load fileLoader,
) {
	loaded := make(map[string]struct{})
	for _, pattern := range patterns {
		if err := ctx.Err(); err != nil {
			return
		}
		files, err := filesource.Expand(pattern.Pattern)
		if err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  adapter,
				Source:   domain.SourceLocation{File: pattern.Pattern},
				Message:  err.Error(),
				Required: pattern.Required,
			})
			continue
		}
		if len(files) == 0 {
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  adapter,
				Source:   domain.SourceLocation{File: pattern.Pattern},
				Message:  "source pattern matched no files",
				Required: pattern.Required,
			})
			continue
		}
		for _, file := range files {
			if _, exists := loaded[file]; exists {
				continue
			}
			loaded[file] = struct{}{}
			additional, err := load(ctx, file, pattern.Required)
			if err != nil {
				discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
					Adapter:  adapter,
					Source:   domain.SourceLocation{File: file},
					Message:  err.Error(),
					Required: pattern.Required,
				})
				continue
			}
			discovery.Append(additional)
		}
	}
}
