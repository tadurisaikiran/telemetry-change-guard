package remote

import (
	"net/url"
	"testing"
)

func TestParseAllowedOriginCanonicalizesExactOrigin(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "HTTPS://Example.COM.:443/", want: "https://example.com"},
		{input: "http://127.0.0.1:9090", want: "http://127.0.0.1:9090"},
		{input: "https://[0:0:0:0:0:0:0:1]:443", want: "https://[::1]"},
	} {
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseAllowedOrigin(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("ParseAllowedOrigin() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestParseAllowedOriginRejectsAmbiguousValues(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"https://user@example.com",
		"https://example.com/path",
		"https://example.com?redirect=evil",
		"https://example.com#fragment",
		"ftp://example.com",
		"https://example.com:70000",
		"https://éxample.com",
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseAllowedOrigin(input); err == nil {
				t.Fatalf("ParseAllowedOrigin(%q) unexpectedly succeeded", input)
			}
		})
	}
}

func TestCredentialTransportRequiresHTTPSOrExplicitLoopback(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		raw        string
		credential bool
		allow      bool
		wantError  bool
	}{
		{name: "https", raw: "https://example.com", credential: true},
		{name: "unauthenticated http", raw: "http://example.com", credential: false},
		{name: "http credential", raw: "http://example.com", credential: true, wantError: true},
		{name: "loopback disabled", raw: "http://127.0.0.1:9090", credential: true, wantError: true},
		{name: "loopback enabled", raw: "http://127.0.0.1:9090", credential: true, allow: true},
		{name: "localhost suffix", raw: "http://localhost.example:9090", credential: true, allow: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			parsed, err := url.Parse(test.raw)
			if err != nil {
				t.Fatal(err)
			}
			err = ValidateCredentialTransport(parsed, test.credential, test.allow, "test")
			if (err != nil) != test.wantError {
				t.Fatalf("ValidateCredentialTransport() error = %v, wantError %t", err, test.wantError)
			}
		})
	}
}

func TestSameOriginNormalizesHostnameAndDefaultPort(t *testing.T) {
	t.Parallel()

	first, _ := url.Parse("https://EXAMPLE.com:443/base")
	second, _ := url.Parse("https://example.com/redirect")
	third, _ := url.Parse("https://example.com:8443/redirect")
	if !SameOrigin(first, second) {
		t.Fatal("equivalent origins were not equal")
	}
	if SameOrigin(first, third) {
		t.Fatal("distinct explicit port was treated as the same origin")
	}
}
