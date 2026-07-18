package config

import (
	"os"
	"testing"

	"github.com/steveyegge/gastown/internal/constants"
)

func TestResolveBeadsBackend(t *testing.T) {
	t.Run("defaults to dolt", func(t *testing.T) {
		t.Setenv(BeadsBackendEnv, "")
		got, err := ResolveBeadsBackend(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if got != BeadsBackendDolt {
			t.Fatalf("backend = %q, want %q", got, BeadsBackendDolt)
		}
	})

	t.Run("town setting selects sqlite", func(t *testing.T) {
		t.Setenv(BeadsBackendEnv, "")
		townRoot := t.TempDir()
		settings := NewTownSettings()
		settings.Beads = &TownBeadsConfig{Backend: "sqlite"}
		if err := SaveTownSettings(TownSettingsPath(townRoot), settings); err != nil {
			t.Fatal(err)
		}
		got, err := ResolveBeadsBackend(townRoot)
		if err != nil {
			t.Fatal(err)
		}
		if got != BeadsBackendSQLite {
			t.Fatalf("backend = %q, want %q", got, BeadsBackendSQLite)
		}
	})

	t.Run("environment overrides setting", func(t *testing.T) {
		townRoot := t.TempDir()
		settings := NewTownSettings()
		settings.Beads = &TownBeadsConfig{Backend: "sqlite"}
		if err := SaveTownSettings(TownSettingsPath(townRoot), settings); err != nil {
			t.Fatal(err)
		}
		t.Setenv(BeadsBackendEnv, "dolt")
		got, err := ResolveBeadsBackend(townRoot)
		if err != nil {
			t.Fatal(err)
		}
		if got != BeadsBackendDolt {
			t.Fatalf("backend = %q, want env override %q", got, BeadsBackendDolt)
		}
	})

	t.Run("invalid selector fails", func(t *testing.T) {
		t.Setenv(BeadsBackendEnv, "postgres")
		if _, err := ResolveBeadsBackend(t.TempDir()); err == nil {
			t.Fatal("expected invalid backend error")
		}
	})
}

func TestAgentEnvSQLiteOmitsDoltControls(t *testing.T) {
	townRoot := t.TempDir()
	settings := NewTownSettings()
	settings.Beads = &TownBeadsConfig{Backend: "sqlite"}
	if err := SaveTownSettings(TownSettingsPath(townRoot), settings); err != nil {
		t.Fatal(err)
	}
	t.Setenv(BeadsBackendEnv, "")
	t.Setenv("GT_DOLT_HOST", "stale-host")
	t.Setenv("GT_DOLT_PORT", "3307")
	t.Setenv("BEADS_DOLT_SERVER_HOST", "stale-host")

	env := AgentEnv(AgentEnvConfig{Role: constants.RolePolecat, Rig: "sample", AgentName: "worker", TownRoot: townRoot})
	if env[BeadsBackendEnv] != "sqlite" {
		t.Fatalf("%s = %q, want sqlite", BeadsBackendEnv, env[BeadsBackendEnv])
	}
	for _, key := range []string{"GT_DOLT_HOST", "GT_DOLT_PORT", "BEADS_DOLT_SERVER_HOST", "BEADS_DOLT_SERVER_PORT", "BEADS_DOLT_PORT", "BEADS_DOLT_AUTO_START"} {
		if value, ok := env[key]; ok {
			t.Fatalf("sqlite agent env contains %s=%q", key, value)
		}
	}
}

func TestClearDoltEnv(t *testing.T) {
	keys := []string{"GT_DOLT_HOST", "GT_DOLT_PORT", "GT_DOLT_DATA", "BEADS_DOLT_SERVER_HOST", "BEADS_DOLT_SERVER_PORT", "BEADS_DOLT_PORT", "BEADS_DOLT_SERVER_DATABASE", "BEADS_DOLT_AUTO_START", "BD_DOLT_AUTO_COMMIT", "BD_DOLT_AUTO_PUSH"}
	for _, key := range keys {
		t.Setenv(key, "stale")
	}
	ClearDoltEnv()
	for _, key := range keys {
		if _, ok := os.LookupEnv(key); ok {
			t.Fatalf("%s was not cleared", key)
		}
	}
}
