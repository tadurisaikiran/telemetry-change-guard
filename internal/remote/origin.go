// Package remote provides strict origin handling for optional remote evidence.
package remote

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ParseBaseURL parses one remote-evidence base URL. Credentials, queries, and
// fragments are forbidden because they can leak through errors or alter the
// bounded endpoint assembled by an adapter.
func ParseBaseURL(raw, label string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" ||
		(!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return nil, fmt.Errorf("%s URL must be an absolute http or https URL", label)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("%s URL must not contain user information, a query, or a fragment", label)
	}
	if _, err := normalizedHost(parsed); err != nil {
		return nil, fmt.Errorf("%s URL has an invalid origin: %w", label, err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

// ParseAllowedOrigin validates and canonicalizes one exact allowed origin.
// Paths are intentionally forbidden: authorization is origin-scoped, while an
// adapter's configured base path remains repository-visible evidence.
func ParseAllowedOrigin(raw string) (string, error) {
	parsed, err := ParseBaseURL(raw, "allowed remote origin")
	if err != nil {
		return "", err
	}
	if parsed.Path != "" {
		return "", fmt.Errorf("allowed remote origin must not contain a path")
	}
	return Origin(parsed)
}

// Origin returns the canonical RFC 6454-style origin used for exact policy
// comparison. Hostnames are case-normalized, a terminal DNS dot is removed,
// IP literals are normalized, and default ports are omitted.
func Origin(parsed *url.URL) (string, error) {
	host, err := normalizedHost(parsed)
	if err != nil {
		return "", err
	}
	return strings.ToLower(parsed.Scheme) + "://" + host, nil
}

// SameOrigin reports whether two parsed URLs have exactly the same canonical
// origin. It is suitable for redirect policy checks.
func SameOrigin(first, second *url.URL) bool {
	left, leftErr := Origin(first)
	right, rightErr := Origin(second)
	return leftErr == nil && rightErr == nil && left == right
}

// ValidateCredentialTransport prevents bearer credentials from crossing a
// plaintext connection. An explicit exception is available only for a
// loopback development endpoint.
func ValidateCredentialTransport(parsed *url.URL, hasCredential, allowInsecureLoopback bool, label string) error {
	if !hasCredential || strings.EqualFold(parsed.Scheme, "https") {
		return nil
	}
	if allowInsecureLoopback && IsLoopback(parsed) {
		return nil
	}
	return fmt.Errorf("%s bearer authentication requires HTTPS; plaintext HTTP is allowed only for an explicitly enabled loopback development endpoint", label)
}

// IsLoopback reports whether a URL host is exactly localhost or a loopback IP
// literal. Hostname suffixes such as localhost.example are not accepted.
func IsLoopback(parsed *url.URL) bool {
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func normalizedHost(parsed *url.URL) (string, error) {
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "" {
		return "", fmt.Errorf("hostname is empty")
	}
	for _, character := range hostname {
		if character > 127 || character <= 32 {
			return "", fmt.Errorf("hostname must use an ASCII DNS name or IP literal")
		}
	}
	port := parsed.Port()
	if port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", fmt.Errorf("port must be an integer from 1 through 65535")
		}
		if (strings.EqualFold(parsed.Scheme, "http") && value == 80) ||
			(strings.EqualFold(parsed.Scheme, "https") && value == 443) {
			port = ""
		}
	}
	if address := net.ParseIP(hostname); address != nil {
		hostname = address.String()
	}
	if port != "" {
		return net.JoinHostPort(hostname, port), nil
	}
	if strings.Contains(hostname, ":") {
		return "[" + hostname + "]", nil
	}
	return hostname, nil
}
