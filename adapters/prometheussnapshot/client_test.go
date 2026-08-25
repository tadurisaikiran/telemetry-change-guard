package prometheussnapshot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestClientCollectsBoundedNormalizedContract(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer test-secret" {
			t.Errorf("authorization = %q", got)
		}
		if got := request.Header.Get("User-Agent"); got != "telemetry-change-guard/prometheus-snapshot" {
			t.Errorf("user agent = %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/prom/api/v1/metadata":
			if request.URL.Query().Get("limit") != "11" || request.URL.Query().Has("limit_per_metric") {
				t.Errorf("metadata query = %q", request.URL.RawQuery)
			}
			fmt.Fprint(writer, `{"status":"success","data":{"request_duration_seconds":[{"type":"histogram","help":"duration","unit":"seconds"}],"requests_total":[{"type":"counter","help":"requests","unit":"requests"}]}}`)
		case "/prom/api/v1/series":
			if request.URL.Query().Get("limit") != "21" || request.URL.Query().Get("match[]") != `{__name__=~".+"}` {
				t.Errorf("series query = %q", request.URL.RawQuery)
			}
			fmt.Fprint(writer, `{"status":"success","data":[{"__name__":"request_duration_seconds_bucket","le":"0.5","job":"api"},{"__name__":"request_duration_seconds_sum","job":"api"},{"__name__":"requests_total","job":"api","method":"GET"},{"__name__":"recorded_metric","cluster":"prod"}]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	result, err := (Client{
		BaseURL: server.URL + "/prom", MaxMetrics: 10, MaxSeries: 20, BearerToken: "test-secret",
		AllowInsecureLoopback: true,
	}).Collect(context.Background(), "checkout-contract")
	if err != nil {
		t.Fatal(err)
	}
	if result.Metadata.Name != "checkout-contract" || len(result.Spec.Metrics) != 3 {
		t.Fatalf("snapshot = %#v", result)
	}
	if got, want := result.Spec.Metrics[0].Name, "recorded_metric"; got != want {
		t.Fatalf("first metric = %q, want %q", got, want)
	}
	if got, want := result.Spec.Metrics[0].Type, "unknown"; got != want {
		t.Fatalf("recorded metric type = %q, want %q", got, want)
	}
	if got, want := result.Spec.Metrics[1].Labels, []string{"job", "le"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("histogram labels = %v, want %v", got, want)
	}
	if got, want := result.Spec.Metrics[2].Labels, []string{"job", "method"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("counter labels = %v, want %v", got, want)
	}
}

func TestClientFailsClosedOnPartialOrAmbiguousEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata string
		series   string
		client   Client
		want     string
	}{
		{
			name:     "metadata warning",
			metadata: `{"status":"success","warnings":["truncated"],"data":{}}`,
			series:   `{"status":"success","data":[]}`,
			want:     "warning(s)",
		},
		{
			name:     "conflicting metadata",
			metadata: `{"status":"success","data":{"requests":[{"type":"counter","unit":"requests"},{"type":"counter","unit":"requests"},{"type":"gauge","unit":"requests"}]}}`,
			series:   `{"status":"success","data":[]}`,
			want:     "conflicting type or unit",
		},
		{
			name:     "series limit",
			metadata: `{"status":"success","data":{}}`,
			series:   `{"status":"success","data":[{"__name__":"a"},{"__name__":"b"},{"__name__":"c"}]}`,
			client:   Client{MaxSeries: 2},
			want:     "series count exceeds",
		},
		{
			name:     "missing metric name",
			metadata: `{"status":"success","data":{}}`,
			series:   `{"status":"success","data":[{"job":"api"}]}`,
			want:     "without __name__",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(request.URL.Path, "/metadata") {
					fmt.Fprint(writer, test.metadata)
					return
				}
				fmt.Fprint(writer, test.series)
			}))
			defer server.Close()
			client := test.client
			client.BaseURL = server.URL
			_, err := client.Collect(context.Background(), "fixture")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestClientRejectsUnsafeURLRedirectAndLimits(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{"localhost:9090", "ftp://example.test", "https://user@example.test", "https://example.test?token=secret", "https://example.test#fragment"} {
		_, err := (Client{BaseURL: rawURL}).Collect(context.Background(), "fixture")
		if err == nil {
			t.Fatalf("URL %q was accepted", rawURL)
		}
	}
	_, err := (Client{BaseURL: "https://example.test", MaxMetrics: hardMaxMetrics + 1}).Collect(context.Background(), "fixture")
	if err == nil || !strings.Contains(err.Error(), "hard limit") {
		t.Fatalf("metric hard-limit error = %v", err)
	}

	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"status":"success","data":{}}`)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	_, err = (Client{BaseURL: redirect.URL}).Collect(context.Background(), "fixture")
	if err == nil || !strings.Contains(err.Error(), "refuse redirect outside") {
		t.Fatalf("redirect error = %v", err)
	}
}

func TestClientRejectsBearerTokenOverPlaintextHTTP(t *testing.T) {
	t.Parallel()

	_, err := (Client{
		BaseURL: "http://prometheus.example.test", BearerToken: "must-not-leak",
	}).Collect(context.Background(), "fixture")
	if err == nil || !strings.Contains(err.Error(), "requires HTTPS") || strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("error = %v", err)
	}
}

func TestClientHonorsTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case <-request.Context().Done():
		case <-time.After(time.Second):
			fmt.Fprint(writer, `{"status":"success","data":{}}`)
		}
	}))
	defer server.Close()
	_, err := (Client{BaseURL: server.URL, Timeout: 10 * time.Millisecond}).Collect(context.Background(), "fixture")
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestResolveFamilyRequiresProvenMetadata(t *testing.T) {
	t.Parallel()

	metadata := map[string]metadataEntry{
		"duration":  {Type: "histogram"},
		"orders":    {Type: "counter"},
		"exact_sum": {Type: "gauge"},
	}
	for input, want := range map[string]string{
		"duration_bucket": "duration",
		"duration_sum":    "duration",
		"orders_count":    "orders_count",
		"exact_sum":       "exact_sum",
	} {
		if got := resolveFamily(input, metadata); got != want {
			t.Errorf("resolveFamily(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSafeErrorTypeRejectsTerminalAndUnboundedText(t *testing.T) {
	t.Parallel()

	if got := safeErrorType("bad_data"); got != "bad_data" {
		t.Fatalf("safe error type = %q", got)
	}
	for _, value := range []string{"", "bad data", "\x1b[31merror", strings.Repeat("x", 65)} {
		if got := safeErrorType(value); got != "unknown" {
			t.Errorf("safeErrorType(%q) = %q", value, got)
		}
	}
}
