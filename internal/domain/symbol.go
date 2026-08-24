// Package domain contains the canonical telemetry migration model.
package domain

// Domain identifies the telemetry system in which a symbol exists.
// Domains are intentionally explicit: names from different telemetry systems
// must not be treated as equivalent without a mapping.
type Domain string

const (
	// DomainPrometheus identifies Prometheus metric and label symbols.
	DomainPrometheus Domain = "prometheus"
)

// SymbolKind identifies the kind of telemetry contract element.
type SymbolKind string

const (
	// SymbolKindMetric identifies a Prometheus metric.
	SymbolKindMetric SymbolKind = "metric"
	// SymbolKindLabel identifies a label attached to a Prometheus metric.
	SymbolKindLabel SymbolKind = "label"
)

// Symbol is a telemetry contract element affected by a migration.
// Parent is required for labels and contains the parent metric name. It is
// empty for metrics.
type Symbol struct {
	Domain Domain     `json:"domain"`
	Kind   SymbolKind `json:"kind"`
	Name   string     `json:"name"`
	Parent string     `json:"parent,omitempty"`
}
