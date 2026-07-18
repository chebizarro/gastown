package config

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// BeadsBackend identifies the storage backend used by bd for a town.
type BeadsBackend string

const (
	// BeadsBackendDolt is the default for backward compatibility.
	BeadsBackendDolt BeadsBackend = "dolt"
	// BeadsBackendSQLite runs bd against its local SQLite database.
	BeadsBackendSQLite BeadsBackend = "sqlite"

	// BeadsBackendEnv overrides the town setting when non-empty.
	BeadsBackendEnv = "GT_BEADS_BACKEND"
)

// ParseBeadsBackend validates and normalizes a backend selector.
func ParseBeadsBackend(value string) (BeadsBackend, error) {
	backend := BeadsBackend(strings.ToLower(strings.TrimSpace(value)))
	switch backend {
	case BeadsBackendDolt, BeadsBackendSQLite:
		return backend, nil
	default:
		return "", fmt.Errorf("invalid beads backend %q (expected dolt or sqlite)", value)
	}
}

// ResolveBeadsBackend resolves the effective backend. GT_BEADS_BACKEND takes
// precedence over settings/config.json beads.backend; the default is Dolt.
func ResolveBeadsBackend(townRoot string) (BeadsBackend, error) {
	return ResolveBeadsBackendFromEnv(townRoot, os.Environ())
}

// ResolveBeadsBackendFromEnv is ResolveBeadsBackend with an explicit environment.
// It is used when constructing child-process environments so resolution follows
// the environment being passed to the child rather than ambient process state.
func ResolveBeadsBackendFromEnv(townRoot string, env []string) (BeadsBackend, error) {
	if value := envValueForBackend(env, BeadsBackendEnv); strings.TrimSpace(value) != "" {
		return ParseBeadsBackend(value)
	}
	if townRoot == "" {
		return BeadsBackendDolt, nil
	}
	settings, err := LoadOrCreateTownSettings(TownSettingsPath(townRoot))
	if err != nil {
		return "", fmt.Errorf("load town settings: %w", err)
	}
	if settings.Beads == nil || strings.TrimSpace(settings.Beads.Backend) == "" {
		return BeadsBackendDolt, nil
	}
	return ParseBeadsBackend(settings.Beads.Backend)
}

func envValueForBackend(env []string, key string) string {
	value := ""
	for _, entry := range env {
		name, candidate, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		matches := name == key
		if runtime.GOOS == "windows" {
			matches = strings.EqualFold(name, key)
		}
		if matches {
			value = candidate
		}
	}
	return value
}
