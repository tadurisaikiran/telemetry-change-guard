// Package prometheussnapshot collects a bounded telemetry contract from the
// Prometheus HTTP API. It stores metric and label names only; sample values and
// label values never enter the generated artifact.
package prometheussnapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	remoteurl "github.com/tadurisaikiran/telemetry-change-guard/internal/remote"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/snapshot"
)

const (
	defaultTimeout      = 60 * time.Second
	defaultMaxMetrics   = 50_000
	defaultMaxSeries    = 100_000
	hardMaxMetrics      = 100_000
	hardMaxSeries       = 1_000_000
	maxAPIResponseBytes = 64 << 20
)

// Client collects a deterministic Prometheus snapshot through read-only API
// requests. Limits are mandatory and cannot exceed defensive hard bounds.
type Client struct {
	BaseURL               string
	Timeout               time.Duration
	MaxMetrics            int
	MaxSeries             int
	BearerToken           string
	AllowInsecureLoopback bool
	HTTPClient            *http.Client
}

type metadataEntry struct {
	Type string `json:"type"`
	Unit string `json:"unit"`
}

type apiEnvelope struct {
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data"`
	ErrorType string          `json:"errorType"`
	Error     string          `json:"error"`
	Warnings  []string        `json:"warnings"`
}

type metricBuilder struct {
	metric snapshot.Metric
	labels map[string]struct{}
}

// Collect fetches metric metadata and bounded series label names, then
// normalizes them into the versioned snapshot format.
func (client Client) Collect(ctx context.Context, name string) (snapshot.Snapshot, error) {
	baseURL, err := parseURL(client.BaseURL)
	if err != nil {
		return snapshot.Snapshot{}, err
	}
	if err := remoteurl.ValidateCredentialTransport(
		baseURL,
		client.BearerToken != "",
		client.AllowInsecureLoopback,
		"Prometheus",
	); err != nil {
		return snapshot.Snapshot{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = "prometheus"
	}
	timeout := client.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	maxMetrics := client.MaxMetrics
	if maxMetrics <= 0 {
		maxMetrics = defaultMaxMetrics
	}
	if maxMetrics > hardMaxMetrics {
		return snapshot.Snapshot{}, fmt.Errorf("max metrics %d exceeds the hard limit %d", maxMetrics, hardMaxMetrics)
	}
	maxSeries := client.MaxSeries
	if maxSeries <= 0 {
		maxSeries = defaultMaxSeries
	}
	if maxSeries > hardMaxSeries {
		return snapshot.Snapshot{}, fmt.Errorf("max series %d exceeds the hard limit %d", maxSeries, hardMaxSeries)
	}

	requestContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpClient := client.httpClient(baseURL, timeout)
	metadata, err := client.loadMetadata(requestContext, httpClient, baseURL, maxMetrics)
	if err != nil {
		return snapshot.Snapshot{}, err
	}
	series, err := client.loadSeries(requestContext, httpClient, baseURL, maxSeries)
	if err != nil {
		return snapshot.Snapshot{}, err
	}
	if err := requestContext.Err(); err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("collect Prometheus telemetry snapshot: %w", err)
	}

	builders := make(map[string]*metricBuilder, len(metadata))
	for metricName, entry := range metadata {
		builders[metricName] = &metricBuilder{
			metric: snapshot.Metric{Name: metricName, Type: normalizeType(entry.Type), Unit: entry.Unit, Labels: []string{}},
			labels: make(map[string]struct{}),
		}
	}
	for _, labels := range series {
		metricName := labels["__name__"]
		if metricName == "" {
			return snapshot.Snapshot{}, fmt.Errorf("Prometheus series response contains a series without __name__")
		}
		familyName := resolveFamily(metricName, metadata)
		builder := builders[familyName]
		if builder == nil {
			builder = &metricBuilder{
				metric: snapshot.Metric{Name: familyName, Type: "unknown", Labels: []string{}},
				labels: make(map[string]struct{}),
			}
			builders[familyName] = builder
		}
		for label := range labels {
			if label != "__name__" {
				builder.labels[label] = struct{}{}
			}
		}
	}

	if len(builders) > maxMetrics {
		return snapshot.Snapshot{}, fmt.Errorf(
			"Prometheus contract contains %d metrics, exceeding the configured limit %d",
			len(builders),
			maxMetrics,
		)
	}
	metrics := make([]snapshot.Metric, 0, len(builders))
	for _, builder := range builders {
		for label := range builder.labels {
			builder.metric.Labels = append(builder.metric.Labels, label)
		}
		sort.Strings(builder.metric.Labels)
		metrics = append(metrics, builder.metric)
	}
	result := snapshot.Snapshot{
		APIVersion: snapshot.APIVersion,
		Kind:       snapshot.Kind,
		Metadata:   snapshot.Metadata{Name: name},
		Spec: snapshot.Spec{
			Domain:  domain.DomainPrometheus,
			Metrics: metrics,
		},
	}
	if err := snapshot.Normalize(&result); err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("normalize Prometheus telemetry snapshot: %w", err)
	}
	if err := requestContext.Err(); err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("collect Prometheus telemetry snapshot: %w", err)
	}
	return result, nil
}

func (client Client) loadMetadata(
	ctx context.Context,
	httpClient *http.Client,
	baseURL *url.URL,
	limit int,
) (map[string]metadataEntry, error) {
	endpoint := endpointURL(baseURL, "/api/v1/metadata")
	query := endpoint.Query()
	query.Set("limit", strconv.Itoa(limit+1))
	endpoint.RawQuery = query.Encode()
	data, err := client.get(ctx, httpClient, endpoint)
	if err != nil {
		return nil, fmt.Errorf("collect Prometheus metric metadata: %w", err)
	}
	var raw map[string][]metadataEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode Prometheus metric metadata: %w", err)
	}
	if len(raw) > limit {
		return nil, fmt.Errorf("Prometheus metric metadata exceeds the configured limit %d", limit)
	}
	result := make(map[string]metadataEntry, len(raw))
	for metricName, entries := range raw {
		if len(entries) == 0 {
			return nil, fmt.Errorf("Prometheus metadata for metric %q is empty", metricName)
		}
		selected := entries[0]
		for _, entry := range entries[1:] {
			if normalizeType(entry.Type) != normalizeType(selected.Type) || entry.Unit != selected.Unit {
				return nil, fmt.Errorf("Prometheus metadata for metric %q has conflicting type or unit values", metricName)
			}
		}
		selected.Type = normalizeType(selected.Type)
		result[metricName] = selected
	}
	return result, nil
}

func (client Client) loadSeries(
	ctx context.Context,
	httpClient *http.Client,
	baseURL *url.URL,
	limit int,
) ([]map[string]string, error) {
	endpoint := endpointURL(baseURL, "/api/v1/series")
	query := endpoint.Query()
	query.Set("match[]", `{__name__=~".+"}`)
	query.Set("limit", strconv.Itoa(limit+1))
	endpoint.RawQuery = query.Encode()
	data, err := client.get(ctx, httpClient, endpoint)
	if err != nil {
		return nil, fmt.Errorf("collect Prometheus series labels: %w", err)
	}
	var result []map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode Prometheus series labels: %w", err)
	}
	if len(result) > limit {
		return nil, fmt.Errorf("Prometheus series count exceeds the configured limit %d", limit)
	}
	return result, nil
}

func (client Client) get(
	ctx context.Context,
	httpClient *http.Client,
	endpoint url.URL,
) (json.RawMessage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "telemetry-change-guard/prometheus-snapshot")
	if client.BearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+client.BearerToken)
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", endpoint.Path, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("request %q returned HTTP status %s", endpoint.Path, response.Status)
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, maxAPIResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %q response: %w", endpoint.Path, err)
	}
	if len(contents) > maxAPIResponseBytes {
		return nil, fmt.Errorf("response from %q exceeds the %d-byte size limit", endpoint.Path, maxAPIResponseBytes)
	}
	var envelope apiEnvelope
	if err := json.Unmarshal(contents, &envelope); err != nil {
		return nil, fmt.Errorf("decode %q response envelope: %w", endpoint.Path, err)
	}
	if envelope.Status != "success" {
		return nil, fmt.Errorf("Prometheus API returned a non-success status (%s)", safeErrorType(envelope.ErrorType))
	}
	if len(envelope.Warnings) != 0 {
		return nil, fmt.Errorf("Prometheus API returned %d warning(s); snapshot evidence may be incomplete", len(envelope.Warnings))
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, fmt.Errorf("Prometheus API returned no data for %q", endpoint.Path)
	}
	return envelope.Data, nil
}

func parseURL(raw string) (*url.URL, error) {
	return remoteurl.ParseBaseURL(raw, "Prometheus")
}

func endpointURL(baseURL *url.URL, suffix string) url.URL {
	result := *baseURL
	result.Path = strings.TrimRight(result.Path, "/") + suffix
	return result
}

func (client Client) httpClient(baseURL *url.URL, timeout time.Duration) *http.Client {
	result := http.Client{Timeout: timeout}
	if client.HTTPClient != nil {
		result = *client.HTTPClient
		result.Timeout = timeout
	}
	result.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stop after 10 Prometheus redirects")
		}
		if !remoteurl.SameOrigin(request.URL, baseURL) {
			return fmt.Errorf("refuse redirect outside configured Prometheus origin")
		}
		return nil
	}
	return &result
}

func resolveFamily(metricName string, metadata map[string]metadataEntry) string {
	if _, exists := metadata[metricName]; exists {
		return metricName
	}
	for _, suffix := range []string{"_bucket", "_sum", "_count", "_created"} {
		if !strings.HasSuffix(metricName, suffix) {
			continue
		}
		base := strings.TrimSuffix(metricName, suffix)
		entry, exists := metadata[base]
		if exists && (entry.Type == "histogram" || entry.Type == "gaugehistogram" || entry.Type == "summary") {
			return base
		}
	}
	return metricName
}

func normalizeType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	return value
}

func safeErrorType(value string) string {
	if value == "" || len(value) > 64 {
		return "unknown"
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '-' {
			continue
		}
		return "unknown"
	}
	return value
}
