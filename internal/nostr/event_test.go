package nostr

import (
	"encoding/json"
	"strings"
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

func TestNewNIP29GroupMessageUsesKind9AndGroupTag(t *testing.T) {
	event, err := NewNIP29GroupMessage("fleet-ops", "convoy_progress", "gastown", "deacon", "deacon", "[progress] convoy hq-cv-1")
	if err != nil {
		t.Fatalf("NewNIP29GroupMessage: %v", err)
	}
	if event.Kind != nostr.KindSimpleGroupChatMessage {
		t.Fatalf("kind = %d, want %d", event.Kind, nostr.KindSimpleGroupChatMessage)
	}
	if got, ok := tagValue(event.Tags, "h"); !ok || got != "fleet-ops" {
		t.Fatalf("h tag = %q, %v", got, ok)
	}
	if _, ok := tagValue(event.Tags, "d"); ok {
		t.Fatal("client group message must not use relay-side d metadata tag")
	}
}

func TestWithCanonicalReferences(t *testing.T) {
	event := &nostr.Event{}
	id := strings.Repeat("a", 64)
	WithCanonicalReferences(event, id, "30900:pubkey:task:fp-104")
	if got, ok := tagValue(event.Tags, "e"); !ok || got != id {
		t.Fatalf("e tag = %q, %v", got, ok)
	}
	if got, ok := tagValue(event.Tags, "a"); !ok || got != "30900:pubkey:task:fp-104" {
		t.Fatalf("a tag = %q, %v", got, ok)
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
