package config

import (
	"fmt"
	"os"
	"strings"
)

const (
	canonicalEnvironmentPrefix = "TCG_"
	legacyEnvironmentPrefix    = "TMR_"
)

// LookupEnvironment resolves a configured environment-variable reference.
// Product-owned TCG names take precedence over the matching legacy TMR name,
// but different simultaneously configured values are rejected explicitly.
// Names outside those prefixes retain exact os.LookupEnv semantics.
func LookupEnvironment(name string) (value string, exists bool, err error) {
	return lookupEnvironment(name, os.LookupEnv)
}

func lookupEnvironment(
	name string,
	lookup func(string) (string, bool),
) (value string, exists bool, err error) {
	canonical := canonicalEnvironmentName(name)
	legacy, paired := legacyEnvironmentName(canonical)
	if !paired {
		value, exists = lookup(name)
		return value, exists, nil
	}

	canonicalValue, canonicalExists := lookup(canonical)
	legacyValue, legacyExists := lookup(legacy)
	if canonicalExists && legacyExists && canonicalValue != legacyValue {
		return "", false, fmt.Errorf(
			"environment variables %q and %q are both set with conflicting values",
			canonical,
			legacy,
		)
	}
	if canonicalExists {
		return canonicalValue, true, nil
	}
	if legacyExists {
		return legacyValue, true, nil
	}
	return "", false, nil
}

func canonicalEnvironmentName(name string) string {
	if strings.HasPrefix(name, legacyEnvironmentPrefix) && len(name) > len(legacyEnvironmentPrefix) {
		return canonicalEnvironmentPrefix + strings.TrimPrefix(name, legacyEnvironmentPrefix)
	}
	return name
}

func legacyEnvironmentName(canonical string) (string, bool) {
	if !strings.HasPrefix(canonical, canonicalEnvironmentPrefix) || len(canonical) <= len(canonicalEnvironmentPrefix) {
		return "", false
	}
	return legacyEnvironmentPrefix + strings.TrimPrefix(canonical, canonicalEnvironmentPrefix), true
}
