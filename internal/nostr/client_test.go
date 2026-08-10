package nostr

import (
	"context"
	"errors"
	"testing"

	"fiatjaf.com/nostr"

	"github.com/steveyegge/gastown/internal/config"
)

type authTestSigner struct{ calls int }

func (s *authTestSigner) Sign(context.Context, *nostr.Event) error { s.calls++; return nil }
func (*authTestSigner) GetPublicKey() string                       { return "" }
func (*authTestSigner) Close() error                               { return nil }

func TestRelayPoolReconnectRetriesInitiallyFailedURL(t *testing.T) {
	originalConnect := relayConnect
	t.Cleanup(func() { relayConnect = originalConnect })

	calls := 0
	relayConnect = func(context.Context, string, nostr.RelayOptions) (*nostr.Relay, error) {
		calls++
		return nil, errors.New("relay unavailable")
	}

	pool, err := NewRelayPool(context.Background(), &config.NostrConfig{
		WriteRelays: []string{"wss://offline.example"},
	})
	if err != nil {
		t.Fatalf("NewRelayPool: %v", err)
	}
	if calls != 1 {
		t.Fatalf("initial connect calls = %d, want 1", calls)
	}

	pool.Reconnect(context.Background())
	if calls != 2 {
		t.Fatalf("connect calls after reconnect = %d, want 2", calls)
	}
}

func TestRelayPoolUsesPublisherSignerForNIP42(t *testing.T) {
	originalConnect := relayConnect
	t.Cleanup(func() { relayConnect = originalConnect })

	var handler func(context.Context, *nostr.Relay, *nostr.Event) error
	relayConnect = func(_ context.Context, _ string, opts nostr.RelayOptions) (*nostr.Relay, error) {
		handler = opts.AuthHandler
		return nil, errors.New("offline")
	}
	signer := &authTestSigner{}
	if _, err := NewRelayPool(context.Background(), &config.NostrConfig{
		WriteRelays: []string{"wss://groups.example"},
	}, signer); err != nil {
		t.Fatalf("NewRelayPool: %v", err)
	}
	if handler == nil {
		t.Fatal("NIP-42 auth handler was not configured")
	}
	if err := handler(context.Background(), nil, &nostr.Event{}); err != nil {
		t.Fatalf("auth handler: %v", err)
	}
	if signer.calls != 1 {
		t.Fatalf("signer calls = %d, want 1", signer.calls)
	}
}

func TestRelayPoolConnectsNIP29RelaysWithoutChangingDefaultTargets(t *testing.T) {
	originalConnect := relayConnect
	t.Cleanup(func() { relayConnect = originalConnect })

	var urls []string
	relayConnect = func(_ context.Context, url string, _ nostr.RelayOptions) (*nostr.Relay, error) {
		urls = append(urls, url)
		return nil, errors.New("offline")
	}

	pool, err := NewRelayPool(context.Background(), &config.NostrConfig{
		WriteRelays: []string{"wss://status.example"},
		NIP29: &config.NIP29Config{
			Enabled: true,
			Relays:  []string{"wss://groups.example", "wss://status.example"},
		},
	})
	if err != nil {
		t.Fatalf("NewRelayPool: %v", err)
	}
	if len(urls) != 2 || urls[0] != "wss://status.example" || urls[1] != "wss://groups.example" {
		t.Fatalf("connected URLs = %v, want deduplicated status + groups", urls)
	}
	if got := pool.WriteRelayURLs(); len(got) != 1 || got[0] != "wss://status.example" {
		t.Fatalf("default write URLs = %v", got)
	}
}
