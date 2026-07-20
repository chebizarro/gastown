package nostr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
	cascadia "git.sharegap.net/cascadia/cascadia-go"
)

func TestCanonicalAliasesUseGeneratedCascadiaKinds(t *testing.T) {
	want := map[string]int{
		"KindStatus":         cascadia.NIP38_USER_STATUS,
		"KindHeartbeat":      cascadia.CAS_AGENT_HEARTBEAT,
		"KindCapability":     cascadia.CAS_AGENT_CAPABILITY,
		"KindTaskCollection": cascadia.NIP51_TASK_COLLECTION,
		"KindIntent":         cascadia.CAS_INTENT,
		"KindTaskState":      cascadia.CAS_CP_STATE,
	}
	got := map[string]int{
		"KindStatus":         KindStatus,
		"KindHeartbeat":      KindHeartbeat,
		"KindCapability":     KindCapability,
		"KindTaskCollection": KindTaskCollection,
		"KindIntent":         KindIntent,
		"KindTaskState":      KindTaskState,
	}
	for name, expected := range want {
		if got[name] != expected {
			t.Fatalf("%s = %d, want generated kind %d", name, got[name], expected)
		}
	}
}

func TestCanonicalMigrationConstructors(t *testing.T) {
	capability, err := NewAgentCapabilityEvent("myrig/polecats/Toast", "myrig", "polecat", "task-runner", nil)
	if err != nil {
		t.Fatalf("NewAgentCapabilityEvent: %v", err)
	}
	requireKindAndTags(t, capability, cascadia.CAS_AGENT_CAPABILITY, map[string]string{
		"d":                 "agent:myrig/polecats/Toast:cap:task-runner",
		cascadia.TagAgent:   "polecat",
		cascadia.TagCap:     "task-runner",
		cascadia.TagRuntime: "gastown",
		cascadia.TagSchema:  agentCapabilitySchema,
	})

	taskState, err := NewTaskStateEvent("fp-106", map[string]string{"status": "open"})
	if err != nil {
		t.Fatalf("NewTaskStateEvent: %v", err)
	}
	requireKindAndTags(t, taskState, cascadia.CAS_CP_STATE, map[string]string{
		"d":                "task:fp-106",
		cascadia.TagDomain: "task",
		cascadia.TagSchema: taskStateSchema,
	})

	queue, err := NewTaskQueueEvent("merge", []string{"fp-106"}, "fleet-dev")
	if err != nil {
		t.Fatalf("NewTaskQueueEvent: %v", err)
	}
	requireKindAndTags(t, queue, cascadia.NIP51_TASK_COLLECTION, map[string]string{
		"d": "queue:merge",
		"h": "fleet-dev",
	})
	if _, ok := tagValue(queue.Tags, cascadia.TagA); !ok {
		t.Fatal("queue event missing NIP-51 a tag")
	}

	intent, err := NewContextVMIntentEvent("abc123", "task/update", map[string]string{"id": "fp-106"}, "req-1")
	if err != nil {
		t.Fatalf("NewContextVMIntentEvent: %v", err)
	}
	requireKindAndTags(t, intent, cascadia.CAS_INTENT, map[string]string{
		"p":                "abc123",
		cascadia.TagMethod: "task/update",
		cascadia.TagSchema: contextVMIntentSchema,
	})
	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(intent.Content), &envelope); err != nil {
		t.Fatalf("intent content: %v", err)
	}
	if envelope["jsonrpc"] != "2.0" || envelope["method"] != "task/update" {
		t.Fatalf("unexpected intent envelope: %#v", envelope)
	}
}

func TestNoRetiredGastownKindDefinitionsInInternalSource(t *testing.T) {
	retired := []string{"30318", "30319", "30320", "30321", "30322", "30323", "30324", "30325"}
	root := filepath.Clean(filepath.Join("..", "..", "internal"))

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if filepath.Base(path) == "canonical_test.go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		for _, kind := range retired {
			if strings.Contains(text, kind) {
				t.Errorf("%s contains retired Gastown custom kind %s", path, kind)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func requireKindAndTags(t *testing.T, event *nostr.Event, kind int, tags map[string]string) {
	t.Helper()
	if event.Kind != nostr.Kind(kind) {
		t.Fatalf("kind = %d, want %d", event.Kind, kind)
	}
	for key, value := range tags {
		if got, ok := tagValue(event.Tags, key); !ok || got != value {
			t.Fatalf("tag %q = %q, %v; want %q", key, got, ok, value)
		}
	}
}
