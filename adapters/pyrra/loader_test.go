package pyrra

import (
	"context"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

func TestParseDiscoversRatioMetrics(t *testing.T) {
	t.Parallel()

	resource := `apiVersion: pyrra.dev/v1alpha1
kind: ServiceLevelObjective
metadata:
  name: checkout-availability
spec:
  indicator:
    ratio:
      errors:
        metric: checkout_requests_total{status=~"5.."}
      total:
        metric: checkout_requests_total
`
	discovery, err := (Loader{Required: true}).Parse(context.Background(), "checkout.yaml", strings.NewReader(resource))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(discovery.Consumers), 1; got != want {
		t.Fatalf("len(Consumers) = %d, want %d", got, want)
	}
	if discovery.Consumers[0].Criticality != domain.CriticalityCritical {
		t.Errorf("Criticality = %q, want critical", discovery.Consumers[0].Criticality)
	}
	if len(discovery.References) == 0 || len(discovery.Diagnostics) != 0 {
		t.Fatalf("unexpected discovery: %+v", discovery)
	}
}
