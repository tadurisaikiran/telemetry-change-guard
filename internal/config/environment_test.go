package config

import (
	"strings"
	"testing"
)

func TestLookupEnvironmentCompatibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configured string
		values     map[string]string
		wantValue  string
		wantExists bool
		wantError  bool
	}{
		{name: "canonical", configured: "TCG_TOKEN", values: map[string]string{"TCG_TOKEN": "canonical"}, wantValue: "canonical", wantExists: true},
		{name: "legacy fallback from canonical reference", configured: "TCG_TOKEN", values: map[string]string{"TMR_TOKEN": "legacy"}, wantValue: "legacy", wantExists: true},
		{name: "canonical from legacy reference", configured: "TMR_TOKEN", values: map[string]string{"TCG_TOKEN": "canonical"}, wantValue: "canonical", wantExists: true},
		{name: "matching pair", configured: "TMR_TOKEN", values: map[string]string{"TCG_TOKEN": "same", "TMR_TOKEN": "same"}, wantValue: "same", wantExists: true},
		{name: "conflicting pair", configured: "TCG_TOKEN", values: map[string]string{"TCG_TOKEN": "canonical-secret", "TMR_TOKEN": "legacy-secret"}, wantError: true},
		{name: "canonical empty", configured: "TCG_TOKEN", values: map[string]string{"TCG_TOKEN": ""}, wantExists: true},
		{name: "matching empty pair", configured: "TCG_TOKEN", values: map[string]string{"TCG_TOKEN": "", "TMR_TOKEN": ""}, wantExists: true},
		{name: "unset pair", configured: "TCG_TOKEN"},
		{name: "unowned exact name", configured: "VENDOR_TOKEN", values: map[string]string{"VENDOR_TOKEN": "vendor"}, wantValue: "vendor", wantExists: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			lookup := func(name string) (string, bool) {
				value, exists := test.values[name]
				return value, exists
			}
			value, exists, err := lookupEnvironment(test.configured, lookup)
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError %t", err, test.wantError)
			}
			if value != test.wantValue || exists != test.wantExists {
				t.Fatalf("result = (%q, %t), want (%q, %t)", value, exists, test.wantValue, test.wantExists)
			}
			if err != nil {
				message := err.Error()
				if !strings.Contains(message, "TCG_TOKEN") || !strings.Contains(message, "TMR_TOKEN") {
					t.Fatalf("error does not identify both variable names: %v", err)
				}
				if strings.Contains(message, "canonical-secret") || strings.Contains(message, "legacy-secret") {
					t.Fatalf("error leaked a secret value: %v", err)
				}
			}
		})
	}
}
