package beads

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
)

func TestBuildPinnedBDEnvSQLiteOmitsDoltSelectors(t *testing.T) {
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(`{"backend":"sqlite","database":"beads.db"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	env := BuildPinnedBDEnv([]string{
		"PATH=/usr/bin",
		"GT_BEADS_BACKEND=sqlite",
		"GT_DOLT_HOST=stale-host",
		"GT_DOLT_PORT=3307",
		"BEADS_DOLT_SERVER_HOST=stale-host",
		"BEADS_DOLT_SERVER_PORT=3307",
		"BEADS_DOLT_SERVER_DATABASE=wrong",
		"BEADS_DOLT_AUTO_START=1",
		"BD_DOLT_AUTO_PUSH=true",
	}, beadsDir)
	got := envMap(env)
	if got["BEADS_DIR"] != beadsDir {
		t.Fatalf("BEADS_DIR = %q, want %q", got["BEADS_DIR"], beadsDir)
	}
	if got[config.BeadsBackendEnv] != "sqlite" {
		t.Fatalf("%s = %q, want sqlite", config.BeadsBackendEnv, got[config.BeadsBackendEnv])
	}
	for key := range got {
		if strings.Contains(key, "DOLT") {
			t.Fatalf("sqlite environment contains Dolt selector %s=%q: %v", key, got[key], env)
		}
	}
}

func TestComputeRedirectTargetSQLiteRig(t *testing.T) {
	townRoot := t.TempDir()
	rigRoot := filepath.Join(townRoot, "sqlite-rig")
	rigBeads := filepath.Join(rigRoot, ".beads")
	crewPath := filepath.Join(rigRoot, "crew", "max")
	if err := os.MkdirAll(rigBeads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(crewPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigBeads, "metadata.json"), []byte(`{"backend":"sqlite","database":"beads.db"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigBeads, "beads.db"), []byte("sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ComputeRedirectTarget(townRoot, crewPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != "../../.beads" {
		t.Fatalf("redirect = %q, want ../../.beads", got)
	}
}

func TestEnsureDatabaseInitializedSQLite(t *testing.T) {
	logPath := installMockBDRecorder(t)
	townRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "town.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := config.NewTownSettings()
	settings.Beads = &config.TownBeadsConfig{Backend: "sqlite"}
	if err := config.SaveTownSettings(config.TownSettingsPath(townRoot), settings); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.BeadsBackendEnv, "")

	beadsDir := filepath.Join(townRoot, "sqlite-rig", ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(`{"backend":"sqlite","database":"beads.db"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureDatabaseInitialized(beadsDir); err != nil {
		t.Fatal(err)
	}
	logOutput := readMockBDLog(t, logPath)
	if !strings.Contains(logOutput, "init --prefix gt --backend sqlite") {
		t.Fatalf("sqlite init missing from log: %q", logOutput)
	}
	if strings.Contains(logOutput, "--server") {
		t.Fatalf("sqlite init unexpectedly used --server: %q", logOutput)
	}
}

func TestBuildPinnedBDEnvDefaultDoltRegression(t *testing.T) {
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := []byte(`{"backend":"dolt","dolt_database":"rigdb","dolt_server_host":"127.0.0.1","dolt_server_port":4407}`)
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), metadata, 0o644); err != nil {
		t.Fatal(err)
	}
	got := envMap(BuildPinnedBDEnv([]string{"PATH=/usr/bin"}, beadsDir))
	if got[config.BeadsBackendEnv] != "dolt" || got["BEADS_DOLT_SERVER_DATABASE"] != "rigdb" || got["BEADS_DOLT_SERVER_PORT"] != "4407" || got["BEADS_DOLT_AUTO_START"] != "0" {
		t.Fatalf("default Dolt environment changed: %v", got)
	}
}
