package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentconfig "github.com/steveyegge/gastown/internal/config"
)

func TestConvoyCLIPollSQLiteNoDolt(t *testing.T) {
	townRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "town.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := agentconfig.NewTownSettings()
	settings.Beads = &agentconfig.TownBeadsConfig{Backend: "sqlite"}
	if err := agentconfig.SaveTownSettings(agentconfig.TownSettingsPath(townRoot), settings); err != nil {
		t.Fatal(err)
	}
	t.Setenv(agentconfig.BeadsBackendEnv, "")

	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "gt.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$GT_TEST_LOG\"\n/usr/bin/env | /usr/bin/sort >> \"$GT_TEST_LOG\"\n"
	gtPath := filepath.Join(binDir, "gt")
	if err := os.WriteFile(gtPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GT_TEST_LOG", logPath)
	t.Setenv("PATH", binDir) // no dolt binary
	t.Setenv("GT_DOLT_PORT", "3307")
	t.Setenv("BEADS_DOLT_AUTO_START", "1")

	m := NewConvoyManager(townRoot, t.Logf, gtPath, 0, nil, nil, nil)
	m.EnableCLIPolling()
	if err := m.pollConvoysViaCLI(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	output := string(data)
	if !strings.Contains(output, "convoy check") || !strings.Contains(output, "GT_BEADS_BACKEND=sqlite") {
		t.Fatalf("unexpected CLI poll invocation:\n%s", output)
	}
	if strings.Contains(output, "BEADS_DOLT_") {
		t.Fatalf("CLI poll leaked Dolt environment:\n%s", output)
	}
}
