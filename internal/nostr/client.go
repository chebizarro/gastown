package nostr

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"fiatjaf.com/nostr"

	"github.com/steveyegge/gastown/internal/config"
)

// RelayPool manages connections to read and write relays.
// It handles auto-reconnection and health monitoring.
type RelayPool struct {
	mu          sync.RWMutex
	readURLs    []string
	writeURLs   []string
	readRelays  []*nostr.Relay
	writeRelays []*nostr.Relay
	closed      bool
}

// NewRelayPool creates a relay pool from the Nostr configuration.
// It connects to all configured read and write relays.
func NewRelayPool(ctx context.Context, cfg *config.NostrConfig) (*RelayPool, error) {
	p := &RelayPool{
		readURLs:  cfg.ReadRelays,
		writeURLs: cfg.WriteRelays,
	}

	// Connect to write relays (required)
	for _, url := range cfg.WriteRelays {
		relay, err := nostr.RelayConnect(ctx, url, nostr.RelayOptions{})
		if err != nil {
			log.Printf("[nostr] warning: failed to connect to write relay %s: %v", url, err)
			continue
		}
		p.writeRelays = append(p.writeRelays, relay)
	}

	// Connect to read relays (optional)
	for _, url := range cfg.ReadRelays {
		relay, err := nostr.RelayConnect(ctx, url, nostr.RelayOptions{})
		if err != nil {
			log.Printf("[nostr] warning: failed to connect to read relay %s: %v", url, err)
			continue
		}
		p.readRelays = append(p.readRelays, relay)
	}

	return p, nil
}

// Publish sends an event to all write relays.
// Returns an error only if ALL relays fail.
func (p *RelayPool) Publish(ctx context.Context, event nostr.Event) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return fmt.Errorf("relay pool is closed")
	}

	if len(p.writeRelays) == 0 {
		return fmt.Errorf("no write relays connected")
	}

	var lastErr error
	successes := 0

	for _, relay := range p.writeRelays {
		if err := relay.Publish(ctx, event); err != nil {
			lastErr = err
			log.Printf("[nostr] publish to %s failed: %v", relay.URL, err)
		} else {
			successes++
		}
	}

	if successes == 0 {
		return fmt.Errorf("all write relays failed, last error: %w", lastErr)
	}

	return nil
}

// Subscribe creates a subscription across all read relays.
// The caller is responsible for reading from the returned channel.
func (p *RelayPool) Subscribe(ctx context.Context, filters []nostr.Filter) []*nostr.Subscription {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var subs []*nostr.Subscription
	for _, relay := range p.readRelays {
		// Subscribe to each filter individually since fiatjaf.com/nostr
		// takes a single Filter per subscription call.
		for _, f := range filters {
			sub, err := relay.Subscribe(ctx, f, nostr.SubscriptionOptions{})
			if err != nil {
				log.Printf("[nostr] subscribe on %s failed: %v", relay.URL, err)
				continue
			}
			subs = append(subs, sub)
		}
	}

	return subs
}

// Reconnect attempts to reconnect disconnected relays.
// Call this periodically from a health monitor goroutine.
func (p *RelayPool) Reconnect(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}

	// Reconnect write relays
	for i, relay := range p.writeRelays {
		if !relay.IsConnected() {
			log.Printf("[nostr] reconnecting write relay %s", relay.URL)
			newRelay, err := nostr.RelayConnect(ctx, relay.URL, nostr.RelayOptions{})
			if err != nil {
				log.Printf("[nostr] reconnect failed for %s: %v", relay.URL, err)
				continue
			}
			p.writeRelays[i] = newRelay
		}
	}

	// Reconnect read relays
	for i, relay := range p.readRelays {
		if !relay.IsConnected() {
			log.Printf("[nostr] reconnecting read relay %s", relay.URL)
			newRelay, err := nostr.RelayConnect(ctx, relay.URL, nostr.RelayOptions{})
			if err != nil {
				log.Printf("[nostr] reconnect failed for %s: %v", relay.URL, err)
				continue
			}
			p.readRelays[i] = newRelay
		}
	}
}

// ConnectedWriteRelays returns the number of currently connected write relays.
func (p *RelayPool) ConnectedWriteRelays() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, relay := range p.writeRelays {
		if relay.IsConnected() {
			count++
		}
	}
	return count
}

// WriteRelayURLs returns the configured write relay URLs.
func (p *RelayPool) WriteRelayURLs() []string {
	return p.writeURLs
}

// HealthCheck logs the current connection status of all relays.
func (p *RelayPool) HealthCheck() {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, relay := range p.writeRelays {
		if relay.IsConnected() {
			log.Printf("[nostr] write relay %s: connected", relay.URL)
		} else {
			log.Printf("[nostr] write relay %s: disconnected", relay.URL)
		}
	}
	for _, relay := range p.readRelays {
		if relay.IsConnected() {
			log.Printf("[nostr] read relay %s: connected", relay.URL)
		} else {
			log.Printf("[nostr] read relay %s: disconnected", relay.URL)
		}
	}
}

// Close disconnects from all relays.
func (p *RelayPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true

	for _, relay := range p.writeRelays {
		relay.Close()
	}
	for _, relay := range p.readRelays {
		relay.Close()
	}

	p.writeRelays = nil
	p.readRelays = nil
}

// DefaultPublishTimeout is the default timeout for publishing a single event.
const DefaultPublishTimeout = 10 * time.Second

// DefaultConnectTimeout is the default timeout for connecting to a relay.
const DefaultConnectTimeout = 15 * time.Second