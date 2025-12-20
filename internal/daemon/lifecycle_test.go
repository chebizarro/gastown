package daemon

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityToStateFile(t *testing.T) {
	d := &Daemon{
		config: &Config{
			TownRoot: "/test/town",
		},
	}

	tests := []struct {
		identity string
		want     string
	}{
		{"mayor", "/test/town/mayor/state.json"},
		{"gastown-witness", "/test/town/gastown/witness/state.json"},
		{"anotherrig-witness", "/test/town/anotherrig/witness/state.json"},
		{"unknown", ""},           // Unknown identity returns empty
		{"polecat", ""},           // Polecats not handled by daemon
		{"gastown-refinery", ""},  // Refinery not handled by daemon
	}

	for _, tt := range tests {
		t.Run(tt.identity, func(t *testing.T) {
			got := d.identityToStateFile(tt.identity)
			if got != tt.want {
				t.Errorf("identityToStateFile(%q) = %q, want %q", tt.identity, got, tt.want)
			}
		})
	}
}

func TestVerifyAgentRequestingState(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "daemon-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	d := &Daemon{
		config: &Config{
			TownRoot: tmpDir,
		},
		logger: log.New(os.Stderr, "[test] ", log.LstdFlags),
	}

	// Create mayor directory
	mayorDir := filepath.Join(tmpDir, "mayor")
	if err := os.MkdirAll(mayorDir, 0755); err != nil {
		t.Fatal(err)
	}

	stateFile := filepath.Join(mayorDir, "state.json")

	t.Run("missing state file", func(t *testing.T) {
		// Remove any existing state file
		os.Remove(stateFile)

		err := d.verifyAgentRequestingState("mayor", ActionCycle)
		if err == nil {
			t.Error("expected error for missing state file")
		}
	})

	t.Run("missing requesting_cycle field", func(t *testing.T) {
		state := map[string]interface{}{
			"some_other_field": true,
		}
		writeStateFile(t, stateFile, state)

		err := d.verifyAgentRequestingState("mayor", ActionCycle)
		if err == nil {
			t.Error("expected error for missing requesting_cycle field")
		}
	})

	t.Run("requesting_cycle is false", func(t *testing.T) {
		state := map[string]interface{}{
			"requesting_cycle": false,
		}
		writeStateFile(t, stateFile, state)

		err := d.verifyAgentRequestingState("mayor", ActionCycle)
		if err == nil {
			t.Error("expected error when requesting_cycle is false")
		}
	})

	t.Run("requesting_cycle is true", func(t *testing.T) {
		state := map[string]interface{}{
			"requesting_cycle": true,
		}
		writeStateFile(t, stateFile, state)

		err := d.verifyAgentRequestingState("mayor", ActionCycle)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("requesting_shutdown is true", func(t *testing.T) {
		state := map[string]interface{}{
			"requesting_shutdown": true,
		}
		writeStateFile(t, stateFile, state)

		err := d.verifyAgentRequestingState("mayor", ActionShutdown)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("requesting_restart is true", func(t *testing.T) {
		state := map[string]interface{}{
			"requesting_restart": true,
		}
		writeStateFile(t, stateFile, state)

		err := d.verifyAgentRequestingState("mayor", ActionRestart)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("unknown identity skips verification", func(t *testing.T) {
		// Unknown identities should not cause error (backwards compatibility)
		err := d.verifyAgentRequestingState("unknown-agent", ActionCycle)
		if err != nil {
			t.Errorf("unexpected error for unknown identity: %v", err)
		}
	})
}

func writeStateFile(t *testing.T, path string, state map[string]interface{}) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}
