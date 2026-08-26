package nostr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	fnostr "fiatjaf.com/nostr"
	"github.com/steveyegge/gastown/internal/config"
)

type statusRecorder struct {
	signer *LocalSigner
	events []fnostr.Event
}

func (r *statusRecorder) PublishReplaceable(ctx context.Context, event *fnostr.Event) error {
	if err := r.signer.Sign(ctx, event); err != nil {
		return err
	}
	r.events = append(r.events, *event)
	return nil
}

func TestConfigFabricValidatesPersistsAppliesAndPublishesStatus(t *testing.T) {
	trustedKey := fnostr.Generate()
	trustedAuthor := PubKeyToString(trustedKey.Public())
	statusKey := fnostr.Generate()
	statusSigner, err := NewLocalSigner(statusKey.Hex())
	if err != nil {
		t.Fatal(err)
	}
	recorder := &statusRecorder{signer: statusSigner}
	path := filepath.Join(t.TempDir(), "nostr.json")
	enabled := true
	base := &config.NostrConfig{
		Type: "nostr", Version: 1, Enabled: true, FeedCurator: &enabled,
		ReadRelays: []string{"wss://read.example"}, WriteRelays: []string{"wss://write.example"},
		ConfigFabric: &config.NostrConfigFabric{TrustedAuthors: []string{trustedAuthor}, SubscriptionRelays: []string{"wss://config.example"}, Scope: "prod"},
	}
	if err := config.SaveNostrConfig(path, base); err != nil {
		t.Fatal(err)
	}
	applied := 0
	fabric := newConfigFabricForTest(path, func(candidate *config.NostrConfig) error {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"version": 2`) {
			t.Fatal("apply ran before desired version was persisted")
		}
		if len(candidate.ReadRelays) != 1 || candidate.ReadRelays[0] != "wss://live.example" {
			t.Fatal("wrong applied projection")
		}
		applied++
		return nil
	}, recorder)
	policy := base.RuntimePolicy()
	policy.ReadRelays = []string{"wss://live.example"}
	accepted := gastownDesired(t, trustedKey, 2, policy, ConfigServiceID)
	ok, err := fabric.Handle(context.Background(), accepted)
	if err != nil || !ok {
		t.Fatalf("accepted event: ok=%v err=%v", ok, err)
	}
	if applied != 1 {
		t.Fatal("policy was not hot-applied")
	}
	loaded, err := config.LoadNostrConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ConfigFabric.Accepted == nil || loaded.ConfigFabric.Accepted.Version != 2 {
		t.Fatal("accepted metadata missing")
	}
	assertGastownStatus(t, recorder.events[len(recorder.events)-1], "applied")

	otherKey := fnostr.Generate()
	cases := []struct {
		name  string
		event *fnostr.Event
	}{
		{"bad author", gastownDesired(t, otherKey, 3, policy, ConfigServiceID)},
		{"stale version", gastownDesired(t, trustedKey, 2, policy, ConfigServiceID)},
		{"wrong service", gastownDesired(t, trustedKey, 4, policy, "other-service")},
	}
	badSignature := gastownDesired(t, trustedKey, 3, policy, ConfigServiceID)
	badSignature.Sig = [64]byte{}
	cases = append(cases, struct {
		name  string
		event *fnostr.Event
	}{"bad signature", badSignature})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := fabric.Handle(context.Background(), tc.event)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				t.Fatal("invalid event accepted")
			}
			assertGastownStatus(t, recorder.events[len(recorder.events)-1], "rejected")
		})
	}
	loaded, _ = config.LoadNostrConfig(path)
	if loaded.ReadRelays[0] != "wss://live.example" {
		t.Fatal("invalid event replaced last valid projection")
	}
}

func gastownDesired(t *testing.T, key fnostr.SecretKey, version int64, policy config.NostrRuntimePolicy, service string) *fnostr.Event {
	t.Helper()
	content, err := json.Marshal(map[string]any{"service_id": service, "scope": "prod", "version": version, "schema": ConfigPolicySchema, "policy": policy})
	if err != nil {
		t.Fatal(err)
	}
	event := &fnostr.Event{Kind: fnostr.Kind(30078), CreatedAt: fnostr.Timestamp(1800000000 + version), Tags: fnostr.Tags{
		{"d", "service:" + service + ":" + ConfigPolicyName}, {"service", service}, {"scope", "prod"}, {"version", strconv.FormatInt(version, 10)}, {"schema", ConfigPolicySchema},
	}, Content: string(content)}
	if err := event.Sign(key); err != nil {
		t.Fatal(err)
	}
	return event
}

func assertGastownStatus(t *testing.T, event fnostr.Event, status string) {
	t.Helper()
	if int(event.Kind) != 30900 || !event.CheckID() || !event.VerifySignature() {
		t.Fatal("status event is not valid and signed")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.Content), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != status {
		t.Fatalf("status = %v", payload["status"])
	}
	if status == "applied" && payload["effective_version"].(float64) != 2 {
		t.Fatal("effective version missing")
	}
	if status == "rejected" && payload["reason"] == "" {
		t.Fatal("rejection reason missing")
	}
}
