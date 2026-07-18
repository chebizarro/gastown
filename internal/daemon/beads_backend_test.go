package daemon

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentconfig "github.com/steveyegge/gastown/internal/config"
)

func writeBackendMetadata(t *testing.T, beadsDir, backend string) {
	t.Helper()
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"backend":"` + backend + `","database":"beads.db"}`)
	if backend == "dolt" {
		data = []byte(`{"backend":"dolt","dolt_database":"test"}`)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonSQLiteBackendWithoutDolt(t *testing.T) {
	townRoot := t.TempDir()
	writeBackendMetadata(t, filepath.Join(townRoot, ".beads"), "sqlite")
	writeBackendMetadata(t, filepath.Join(townRoot, "sample", "mayor", "rig", ".beads"), "sqlite")
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "town.json"), []byte(`{"type":"town","version":2,"name":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rigs := `{"version":1,"rigs":{"sample":{"beads":{"prefix":"sp-"}}}}`
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "rigs.json"), []byte(rigs), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := agentconfig.NewTownSettings()
	settings.Beads = &agentconfig.TownBeadsConfig{Backend: "sqlite"}
	if err := agentconfig.SaveTownSettings(agentconfig.TownSettingsPath(townRoot), settings); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	for _, name := range []string{"gt", "bd", "tmux"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Deliberately no dolt binary in PATH.
	t.Setenv("PATH", binDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(agentconfig.BeadsBackendEnv, "")
	t.Setenv("GT_DOLT_PORT", "3307")
	t.Setenv("BEADS_DOLT_AUTO_START", "1")

	d, err := New(DefaultConfig(townRoot))
	if err != nil {
		t.Fatal(err)
	}
	defer d.cancel()
	if d.beadsBackend != agentconfig.BeadsBackendSQLite {
		t.Fatalf("daemon backend = %q, want sqlite", d.beadsBackend)
	}
	if d.doltServer != nil {
		t.Fatal("sqlite daemon created a Dolt server manager")
	}
	if err := d.checkAllRigsBackend(); err != nil {
		t.Fatalf("sqlite preflight: %v", err)
	}
	if stores, err := d.openBeadsStores(); err != nil || len(stores) != 0 {
		t.Fatalf("sqlite SDK stores = %v, err = %v; want none", stores, err)
	}
	for _, patrol := range doltOnlyPatrols {
		if d.isPatrolActive(patrol) {
			t.Fatalf("Dolt-only patrol %q active in sqlite mode", patrol)
		}
	}
	for _, key := range []string{"GT_DOLT_PORT", "BEADS_DOLT_AUTO_START"} {
		if _, ok := os.LookupEnv(key); ok {
			t.Fatalf("sqlite daemon retained %s", key)
		}
	}
	data, err := os.ReadFile(DefaultConfig(townRoot).LogFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Dolt-only patrols disabled for beads backend sqlite") {
		t.Fatalf("missing auto-disable log line: %s", data)
	}
}

func TestDaemonBackendPreflightRejectsMissingMetadata(t *testing.T) {
	townRoot := t.TempDir()
	d := &Daemon{
		config:       DefaultConfig(townRoot),
		beadsBackend: agentconfig.BeadsBackendDolt,
		logger:       log.New(&bytes.Buffer{}, "", 0),
	}
	err := d.checkAllRigsBackend()
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %v, want missing metadata failure", err)
	}
}

func TestDaemonDoltBackendRegression(t *testing.T) {
	townRoot := t.TempDir()
	writeBackendMetadata(t, filepath.Join(townRoot, ".beads"), "dolt")
	d := &Daemon{
		config:          DefaultConfig(townRoot),
		beadsBackend:    agentconfig.BeadsBackendDolt,
		patrolConfig:    &DaemonPatrolConfig{Patrols: &PatrolsConfig{CompactorDog: &CompactorDogConfig{Enabled: true}}},
		disabledPatrols: map[string]bool{},
		logger:          log.New(&bytes.Buffer{}, "", 0),
	}
	if err := d.checkAllRigsBackend(); err != nil {
		t.Fatalf("Dolt preflight changed: %v", err)
	}
	if !d.isPatrolActive("compactor_dog") {
		t.Fatal("Dolt compactor patrol unexpectedly disabled")
	}
}
