package events

import (
	"context"
	"errors"
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

func TestTaskStateProjectionFollowsActiveLifecycle(t *testing.T) {
	tests := []struct {
		eventType string
		status    string
	}{
		{TypeSling, "in_progress"},
		{TypeHook, "in_progress"},
		{TypeUnhook, "open"},
		{TypeDone, "closed"},
	}
	for _, test := range tests {
		t.Run(test.eventType, func(t *testing.T) {
			payload, ok := taskStateProjection(Event{
				Type:    test.eventType,
				Payload: map[string]interface{}{"bead": "fp-106"},
			}, "rig/polecats/Gus")
			if !ok {
				t.Fatal("task state projection was not produced")
			}
			if payload.Id != "fp-106" || payload.Status != test.status || payload.Assignee != "rig/polecats/Gus" {
				t.Fatalf("projection = %#v", payload)
			}
			if err := payload.Validate(); err != nil {
				t.Fatalf("projection does not satisfy canonical schema: %v", err)
			}
		})
	}

	if _, ok := taskStateProjection(Event{Type: TypeHandoff, Payload: map[string]interface{}{"bead": "fp-106"}}, "Gus"); ok {
		t.Fatal("non-task lifecycle event produced a task projection")
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

func TestGetPublisherAppliesNostrEnvOverrides(t *testing.T) {
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

	t.Setenv("GT_NOSTR_ENABLED", "1")
	t.Setenv("GT_NOSTR_READ_RELAYS", "wss://read-env.example")
	t.Setenv("GT_NOSTR_WRITE_RELAYS", "wss://write-env.example")
	loadPublisherConfig = func(string) (*config.NostrConfig, error) {
		return &config.NostrConfig{
			Enabled:     false,
			ReadRelays:  []string{"wss://read-file.example"},
			WriteRelays: []string{"wss://write-file.example"},
			Identities: map[string]*config.NostrIdentity{
				"deacon": {Signer: config.SignerConfig{Bunker: "bunker://deacon"}},
			},
			Defaults: config.DefaultNostrDefaults(),
		}, nil
	}
	newPublisherSigner = func(context.Context, string) (gtnostr.Signer, error) {
		return publisherTestSigner{}, nil
	}

	var captured *config.NostrConfig
	newEventsPublisher = func(_ context.Context, cfg *config.NostrConfig, _ gtnostr.Signer, _ string) (*gtnostr.Publisher, error) {
		captured = cfg
		return &gtnostr.Publisher{}, nil
	}

	if got := getPublisher("polecat"); got == nil {
		t.Fatal("getPublisher returned nil")
	}
	if captured == nil || !captured.Enabled {
		t.Fatal("GT_NOSTR_ENABLED override was not applied")
	}
	if !reflect.DeepEqual(captured.ReadRelays, []string{"wss://read-env.example"}) {
		t.Fatalf("read relays = %v", captured.ReadRelays)
	}
	if !reflect.DeepEqual(captured.WriteRelays, []string{"wss://write-env.example"}) {
		t.Fatalf("write relays = %v", captured.WriteRelays)
	}
}
