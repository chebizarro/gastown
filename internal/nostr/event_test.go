package nostr

import (
	"encoding/json"
	"testing"

	"fiatjaf.com/nostr"
	cascadia "git.sharegap.net/cascadia/cascadia-go"
)

func TestNewLogStatusEventUsesCanonicalNIP38Shape(t *testing.T) {
	event, err := NewLogStatusEvent(
		"myrig", "polecat", "Toast", "hook", "feed",
		map[string]interface{}{"bead": "fp-123"},
	)
	if err != nil {
		t.Fatalf("NewLogStatusEvent: %v", err)
	}

	if event.Kind != nostr.Kind(cascadia.NIP38_USER_STATUS) {
		t.Fatalf("kind = %d, want NIP-38 %d", event.Kind, nostr.Kind(cascadia.NIP38_USER_STATUS))
	}

	want := map[string]string{
		"d":               agentStatusDTag,
		cascadia.TagAgent: "Toast",
		cascadia.TagType:  "hook",
		"visibility":      "feed",
	}
	for key, value := range want {
		if got, ok := tagValue(event.Tags, key); !ok || got != value {
			t.Errorf("tag %q = %q, %v; want %q", key, got, ok, value)
		}
	}
}

func TestNewAgentHeartbeatEventUsesCanonicalShape(t *testing.T) {
	event, err := NewAgentHeartbeatEvent("myrig/polecats/Toast", "myrig", "polecat", "working")
	if err != nil {
		t.Fatalf("NewAgentHeartbeatEvent: %v", err)
	}

	if event.Kind != nostr.Kind(cascadia.CAS_AGENT_HEARTBEAT) {
		t.Fatalf("kind = %d, want %d", event.Kind, cascadia.CAS_AGENT_HEARTBEAT)
	}

	want := map[string]string{
		"d":                 "myrig/polecats/Toast",
		cascadia.TagStatus:  "working",
		cascadia.TagAgent:   "polecat",
		cascadia.TagRuntime: "gastown",
		cascadia.TagSchema:  agentHeartbeatSchema,
	}
	for key, value := range want {
		if got, ok := tagValue(event.Tags, key); !ok || got != value {
			t.Errorf("tag %q = %q, %v; want %q", key, got, ok, value)
		}
	}

	var payload cascadia.CascadiaAgentHeartbeatV1Payload
	if err := json.Unmarshal([]byte(event.Content), &payload); err != nil {
		t.Fatalf("unmarshal heartbeat content: %v", err)
	}
	if payload.ActiveTasks != 1 {
		t.Errorf("active_tasks = %d, want 1", payload.ActiveTasks)
	}
}

func tagValue(tags nostr.Tags, key string) (string, bool) {
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == key {
			return tag[1], true
		}
	}
	return "", false
}
