package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSaveBase(t *testing.T) {
	// Override gtDir for testing
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cfg := DefaultBase()

	if err := SaveBase(cfg); err != nil {
		t.Fatalf("SaveBase failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(BasePath()); err != nil {
		t.Fatalf("base config file not created: %v", err)
	}

	loaded, err := LoadBase()
	if err != nil {
		t.Fatalf("LoadBase failed: %v", err)
	}

	if len(loaded.SessionStart) != 1 {
		t.Errorf("expected 1 SessionStart hook, got %d", len(loaded.SessionStart))
	}
	if len(loaded.PreCompact) != 1 {
		t.Errorf("expected 1 PreCompact hook, got %d", len(loaded.PreCompact))
	}
	if len(loaded.UserPromptSubmit) != 1 {
		t.Errorf("expected 1 UserPromptSubmit hook, got %d", len(loaded.UserPromptSubmit))
	}
	if len(loaded.Stop) != 1 {
		t.Errorf("expected 1 Stop hook, got %d", len(loaded.Stop))
	}
}

func TestLoadSaveOverride(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := &HooksConfig{
		PreToolUse: []HookEntry{
			{
				Matcher: "Bash(git push*)",
				Hooks:   []Hook{{Type: "command", Command: "echo blocked && exit 2"}},
			},
		},
	}

	if err := SaveOverride("crew", cfg); err != nil {
		t.Fatalf("SaveOverride failed: %v", err)
	}

	loaded, err := LoadOverride("crew")
	if err != nil {
		t.Fatalf("LoadOverride failed: %v", err)
	}

	if len(loaded.PreToolUse) != 1 {
		t.Fatalf("expected 1 PreToolUse hook, got %d", len(loaded.PreToolUse))
	}
	if loaded.PreToolUse[0].Matcher != "Bash(git push*)" {
		t.Errorf("expected matcher 'Bash(git push*)', got %q", loaded.PreToolUse[0].Matcher)
	}
}

func TestLoadSaveOverrideRigRole(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := &HooksConfig{
		SessionStart: []HookEntry{
			{Matcher: "", Hooks: []Hook{{Type: "command", Command: "echo gastown-crew"}}},
		},
	}

	if err := SaveOverride("gastown/crew", cfg); err != nil {
		t.Fatalf("SaveOverride failed: %v", err)
	}

	// Verify the file path uses __ separator
	expectedPath := filepath.Join(tmpDir, ".gt", "hooks-overrides", "gastown__crew.json")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected override file at %s: %v", expectedPath, err)
	}

	loaded, err := LoadOverride("gastown/crew")
	if err != nil {
		t.Fatalf("LoadOverride failed: %v", err)
	}

	if len(loaded.SessionStart) != 1 {
		t.Fatalf("expected 1 SessionStart hook, got %d", len(loaded.SessionStart))
	}
}

func TestLoadMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := LoadBase()
	if err == nil {
		t.Error("expected error loading missing base config")
	}

	_, err = LoadOverride("crew")
	if err == nil {
		t.Error("expected error loading missing override config")
	}
}

func TestValidTarget(t *testing.T) {
	tests := []struct {
		target string
		valid  bool
	}{
		{"crew", true},
		{"witness", true},
		{"refinery", true},
		{"polecats", true},
		{"mayor", true},
		{"deacon", true},
		{"gastown/crew", true},
		{"beads/witness", true},
		{"sky/polecats", true},
		{"wyvern/refinery", true},
		{"", false},
		{"invalid", false},
		{"gastown/invalid", false},
		{"/crew", false},
		{"gastown/", false},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			if got := ValidTarget(tt.target); got != tt.valid {
				t.Errorf("ValidTarget(%q) = %v, want %v", tt.target, got, tt.valid)
			}
		})
	}
}

func TestGetApplicableOverrides(t *testing.T) {
	tests := []struct {
		target   string
		expected []string
	}{
		{"mayor", []string{"mayor"}},
		{"crew", []string{"crew"}},
		{"gastown/crew", []string{"crew", "gastown/crew"}},
		{"beads/witness", []string{"witness", "beads/witness"}},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got := GetApplicableOverrides(tt.target)
			if len(got) != len(tt.expected) {
				t.Fatalf("GetApplicableOverrides(%q) returned %d items, want %d", tt.target, len(got), len(tt.expected))
			}
			for i, v := range got {
				if v != tt.expected[i] {
					t.Errorf("GetApplicableOverrides(%q)[%d] = %q, want %q", tt.target, i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestDefaultBase(t *testing.T) {
	cfg := DefaultBase()

	if len(cfg.SessionStart) == 0 {
		t.Error("DefaultBase should have SessionStart hooks")
	}
	if len(cfg.PreCompact) == 0 {
		t.Error("DefaultBase should have PreCompact hooks")
	}
	if len(cfg.UserPromptSubmit) == 0 {
		t.Error("DefaultBase should have UserPromptSubmit hooks")
	}
	if len(cfg.Stop) == 0 {
		t.Error("DefaultBase should have Stop hooks")
	}

	// Verify gt prime is in SessionStart
	found := false
	for _, entry := range cfg.SessionStart {
		for _, h := range entry.Hooks {
			if h.Command != "" && len(h.Command) > 0 {
				found = true
			}
		}
	}
	if !found {
		t.Error("DefaultBase SessionStart should have a command")
	}
}

func TestMarshalConfig(t *testing.T) {
	cfg := &HooksConfig{
		SessionStart: []HookEntry{
			{Matcher: "", Hooks: []Hook{{Type: "command", Command: "test"}}},
		},
	}

	data, err := MarshalConfig(cfg)
	if err != nil {
		t.Fatalf("MarshalConfig failed: %v", err)
	}

	// Should be pretty-printed
	if len(data) == 0 {
		t.Error("MarshalConfig returned empty data")
	}

	// Should be valid JSON that round-trips
	loaded := &HooksConfig{}
	if err := json.Unmarshal(data, loaded); err != nil {
		t.Fatalf("round-trip failed: %v", err)
	}

	if len(loaded.SessionStart) != 1 {
		t.Errorf("round-trip lost SessionStart hooks")
	}
}
