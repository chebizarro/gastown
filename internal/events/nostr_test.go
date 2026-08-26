package events

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"fiatjaf.com/nostr"

	"github.com/steveyegge/gastown/internal/config"
	gtnostr "github.com/steveyegge/gastown/internal/nostr"
)

type publisherTestSigner struct{}

func (publisherTestSigner) Sign(context.Context, *nostr.Event) error { return nil }
func (publisherTestSigner) GetPublicKey() string                     { return "" }
func (publisherTestSigner) Close() error                             { return nil }

func TestResolvePublisherIdentityUsesRoleThenDeaconFallback(t *testing.T) {
	witness := &config.NostrIdentity{Signer: config.SignerConfig{Bunker: "witness"}}
	deacon := &config.NostrIdentity{Signer: config.SignerConfig{Bunker: "deacon"}}
	cfg := &config.NostrConfig{Identities: map[string]*config.NostrIdentity{
		"witness": witness,
		"deacon":  deacon,
	}}

	key, identity := resolvePublisherIdentity(cfg, "witness")
	if key != "witness" || identity != witness {
		t.Fatalf("witness identity = (%q, %p), want (%q, %p)", key, identity, "witness", witness)
	}

	key, identity = resolvePublisherIdentity(cfg, "polecat")
	if key != "deacon" || identity != deacon {
		t.Fatalf("fallback identity = (%q, %p), want (%q, %p)", key, identity, "deacon", deacon)
	}
}

func TestGetPublisherUsesRoleSignerThenDeaconFallback(t *testing.T) {
	ResetPublisherForTesting()
	originalLoad := loadPublisherConfig
	originalSigner := newPublisherSigner
	originalPublisher := newEventsPublisher
	t.Cleanup(func() {
		ResetPublisherForTesting()
		loadPublisherConfig = originalLoad
		newPublisherSigner = originalSigner
		newEventsPublisher = originalPublisher
	})

	loadPublisherConfig = func(string) (*config.NostrConfig, error) {
		return &config.NostrConfig{
			Enabled:     true,
			WriteRelays: []string{"wss://relay.example"},
			Identities: map[string]*config.NostrIdentity{
				"witness": {Signer: config.SignerConfig{Bunker: "bunker://witness"}},
				"deacon":  {Signer: config.SignerConfig{Bunker: "bunker://deacon"}},
			},
			Defaults: config.DefaultNostrDefaults(),
		}, nil
	}

	var bunkers []string
	newPublisherSigner = func(_ context.Context, bunker string) (gtnostr.Signer, error) {
		bunkers = append(bunkers, bunker)
		return publisherTestSigner{}, nil
	}
	newEventsPublisher = func(context.Context, *config.NostrConfig, gtnostr.Signer, string) (*gtnostr.Publisher, error) {
		return &gtnostr.Publisher{}, nil
	}

	witnessPublisher := getPublisher("witness")
	if witnessPublisher == nil {
		t.Fatal("witness publisher is nil")
	}
	deaconPublisher := getPublisher("polecat")
	if deaconPublisher == nil {
		t.Fatal("fallback publisher is nil")
	}
	if witnessPublisher == deaconPublisher {
		t.Fatal("role and fallback publishers should have distinct signer views")
	}
	if !reflect.DeepEqual(bunkers, []string{"bunker://witness", "bunker://deacon"}) {
		t.Fatalf("signer bunkers = %v", bunkers)
	}
}

func TestGetPublisherRetriesTransientConfigFailureWithBackoff(t *testing.T) {
	ResetPublisherForTesting()
	originalNow := publisherNow
	originalLoad := loadPublisherConfig
	originalSigner := newPublisherSigner
	originalPublisher := newEventsPublisher
	t.Cleanup(func() {
		ResetPublisherForTesting()
		publisherNow = originalNow
		loadPublisherConfig = originalLoad
		newPublisherSigner = originalSigner
		newEventsPublisher = originalPublisher
	})

	now := time.Unix(1_700_000_000, 0)
	publisherNow = func() time.Time { return now }
	loadCalls := 0
	loadPublisherConfig = func(string) (*config.NostrConfig, error) {
		loadCalls++
		if loadCalls == 1 {
			return nil, errors.New("transient config read failure")
		}
		return &config.NostrConfig{
			Enabled:     true,
			WriteRelays: []string{"wss://relay.example"},
			Identities: map[string]*config.NostrIdentity{
				"deacon": {Signer: config.SignerConfig{Bunker: "bunker://deacon"}},
			},
			Defaults: config.DefaultNostrDefaults(),
		}, nil
	}
	newPublisherSigner = func(context.Context, string) (gtnostr.Signer, error) {
		return publisherTestSigner{}, nil
	}
	newEventsPublisher = func(context.Context, *config.NostrConfig, gtnostr.Signer, string) (*gtnostr.Publisher, error) {
		return &gtnostr.Publisher{}, nil
	}

	if got := getPublisher("polecat"); got != nil {
		t.Fatal("first getPublisher unexpectedly succeeded")
	}
	if got := getPublisher("polecat"); got != nil {
		t.Fatal("config retried before backoff elapsed")
	}
	if loadCalls != 1 {
		t.Fatalf("config load calls during backoff = %d, want 1", loadCalls)
	}

	now = now.Add(publisherInitialBackoff)
	if got := getPublisher("polecat"); got == nil {
		t.Fatal("publisher did not recover after config retry")
	}
	if loadCalls != 2 {
		t.Fatalf("config load calls after retry = %d, want 2", loadCalls)
	}
}

func TestGetPublisherRetriesTransientSignerFailureWithBackoff(t *testing.T) {
	ResetPublisherForTesting()
	originalNow := publisherNow
	originalLoad := loadPublisherConfig
	originalSigner := newPublisherSigner
	originalPublisher := newEventsPublisher
	t.Cleanup(func() {
		ResetPublisherForTesting()
		publisherNow = originalNow
		loadPublisherConfig = originalLoad
		newPublisherSigner = originalSigner
		newEventsPublisher = originalPublisher
	})

	now := time.Unix(1_700_000_000, 0)
	publisherNow = func() time.Time { return now }
	loadPublisherConfig = func(string) (*config.NostrConfig, error) {
		return &config.NostrConfig{
			Enabled:     true,
			WriteRelays: []string{"wss://relay.example"},
			Identities: map[string]*config.NostrIdentity{
				"witness": {Signer: config.SignerConfig{Bunker: "bunker://witness"}},
			},
			Defaults: config.DefaultNostrDefaults(),
		}, nil
	}

	signerCalls := 0
	newPublisherSigner = func(context.Context, string) (gtnostr.Signer, error) {
		signerCalls++
		if signerCalls == 1 {
			return nil, errors.New("transient bunker failure")
		}
		return publisherTestSigner{}, nil
	}
	newEventsPublisher = func(context.Context, *config.NostrConfig, gtnostr.Signer, string) (*gtnostr.Publisher, error) {
		return &gtnostr.Publisher{}, nil
	}

	if got := getPublisher("witness"); got != nil {
		t.Fatal("first getPublisher unexpectedly succeeded")
	}
	if got := getPublisher("witness"); got != nil {
		t.Fatal("publisher retried before backoff elapsed")
	}
	if signerCalls != 1 {
		t.Fatalf("signer calls during backoff = %d, want 1", signerCalls)
	}

	now = now.Add(publisherInitialBackoff)
	if got := getPublisher("witness"); got == nil {
		t.Fatal("publisher did not recover after backoff")
	}
	if signerCalls != 2 {
		t.Fatalf("signer calls after retry = %d, want 2", signerCalls)
	}
}

func TestGetPublisherReloadsPersistedPolicyWithoutRestart(t *testing.T) {
	ResetPublisherForTesting()
	t.Cleanup(ResetPublisherForTesting)
	path := filepath.Join(t.TempDir(), "nostr.json")
	t.Setenv("GT_NOSTR_CONFIG", path)
	initial := config.NewNostrConfig()
	if err := config.SaveNostrConfig(path, initial); err != nil {
		t.Fatal(err)
	}
	_ = getPublisher("polecat")
	if publisherConfig == nil || publisherConfig.Enabled {
		t.Fatal("initial persisted policy was not loaded")
	}

	updated := config.NewNostrConfig()
	updated.Enabled = true
	updated.ReadRelays = []string{"wss://read-file.example"}
	updated.WriteRelays = []string{"wss://write-file.example"}
	if err := config.SaveNostrConfig(path, updated); err != nil {
		t.Fatal(err)
	}
	_ = getPublisher("polecat")
	if publisherConfig == nil || !publisherConfig.Enabled {
		t.Fatal("changed persisted policy was not hot-reloaded")
	}
	if !reflect.DeepEqual(publisherConfig.ReadRelays, updated.ReadRelays) {
		t.Fatalf("read relays = %v", publisherConfig.ReadRelays)
	}
	if err := os.WriteFile(path, []byte(`{"type":"nostr","version":1,"enabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = getPublisher("polecat")
	if !reflect.DeepEqual(publisherConfig.ReadRelays, updated.ReadRelays) {
		t.Fatalf("invalid candidate replaced last valid relays: %v", publisherConfig.ReadRelays)
	}
}

func TestCoordinationTargetsRoutesConfiguredClasses(t *testing.T) {
	ResetPublisherForTesting()
	t.Cleanup(ResetPublisherForTesting)
	publisherConfig = &config.NostrConfig{NIP29: &config.NIP29Config{
		Enabled: true,
		Relays:  []string{"wss://groups.example"},
		Groups: config.NIP29Groups{
			Progress: []string{"fleet-ops"},
			Asks:     []string{"incidents"},
			Results:  []string{"fleet-ops", "fleet-dev"},
		},
	}}

	class, relays, groups := coordinationTargets(TypeConvoyResult)
	if class != coordinationResult {
		t.Fatalf("class = %q", class)
	}
	if !reflect.DeepEqual(relays, []string{"wss://groups.example"}) {
		t.Fatalf("relays = %v", relays)
	}
	if !reflect.DeepEqual(groups, []string{"fleet-ops", "fleet-dev"}) {
		t.Fatalf("groups = %v", groups)
	}

	class, _, groups = coordinationTargets(TypeEscalationSent)
	if class != coordinationAsk || !reflect.DeepEqual(groups, []string{"incidents"}) {
		t.Fatalf("escalation route = %q %v", class, groups)
	}
}

func TestFormatCoordinationMessageIncludesConvoyTaskAndDetail(t *testing.T) {
	got := formatCoordinationMessage(coordinationProgress, Event{
		Type:    TypeConvoyProgress,
		Payload: map[string]interface{}{"message": "next ready task dispatched"},
	}, &correlations{ConvoyID: "hq-cv-1", IssueID: "fp-104"})
	want := "[progress] convoy hq-cv-1 — task fp-104 — next ready task dispatched"
	if got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}
