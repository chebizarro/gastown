package nostr

import (
	"context"
	"errors"
	"testing"

	"fiatjaf.com/nostr"

	"github.com/steveyegge/gastown/internal/config"
)

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
