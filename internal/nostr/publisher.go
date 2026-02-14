package nostr

import (
	"context"
	"fmt"
	"log"

	"fiatjaf.com/nostr"

	"github.com/steveyegge/gastown/internal/config"
)

// Publisher is the high-level API for publishing Nostr events.
// It handles: sign → broadcast to relays → spool on failure.
//
// All Gas Town Nostr publishing should go through this type.
type Publisher struct {
	signer Signer
	pool   *RelayPool
	spool  *Spool
}

// NewPublisher creates a publisher from the Nostr configuration.
// It initializes the signer, relay pool, and local spool.
func NewPublisher(ctx context.Context, cfg *config.NostrConfig, signer Signer, runtimeDir string) (*Publisher, error) {
	pool, err := NewRelayPool(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating relay pool: %w", err)
	}

	spool := NewSpool(runtimeDir)

	return &Publisher{
		signer: signer,
		pool:   pool,
		spool:  spool,
	}, nil
}

// Publish signs and broadcasts a regular (non-replaceable) event.
// If all relays fail, the event is spooled locally for later drain.
// Returns an error only if both publishing and spooling fail.
func (p *Publisher) Publish(ctx context.Context, event *nostr.Event) error {
	// Sign the event
	if err := p.signer.Sign(ctx, event); err != nil {
		return fmt.Errorf("signing event: %w", err)
	}

	// Attempt to broadcast
	if err := p.pool.Publish(ctx, *event); err != nil {
		log.Printf("[nostr] publish failed, spooling event %s: %v", event.ID, err)
		// Spool for later retry
		if spoolErr := p.spool.Enqueue(event, p.pool.WriteRelayURLs()); spoolErr != nil {
			return fmt.Errorf("publish failed (%v) and spool failed: %w", err, spoolErr)
		}
		// Spooled successfully — not a hard failure
		return nil
	}

	return nil
}

// PublishReplaceable signs and broadcasts a NIP-33 replaceable event.
// Replaceable events require a "d" tag for deduplication.
// Same spool-on-failure behavior as Publish.
func (p *Publisher) PublishReplaceable(ctx context.Context, event *nostr.Event) error {
	// Verify the event has a "d" tag
	hasD := false
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "d" {
			hasD = true
			break
		}
	}
	if !hasD {
		return fmt.Errorf("replaceable event must have a 'd' tag")
	}

	return p.Publish(ctx, event)
}

// DrainSpool attempts to send all spooled events to relays.
// This should be called periodically by the Deacon daemon.
func (p *Publisher) DrainSpool(ctx context.Context) (sent int, failed int, err error) {
	return p.spool.Drain(ctx, p.pool)
}

// SpoolCount returns the number of events waiting in the spool.
func (p *Publisher) SpoolCount() int {
	return p.spool.Count()
}

// Signer returns the publisher's signer (for identity operations).
func (p *Publisher) Signer() Signer {
	return p.signer
}

// Pool returns the publisher's relay pool (for subscription operations).
func (p *Publisher) Pool() *RelayPool {
	return p.pool
}

// Close releases all resources: signer, relay pool.
func (p *Publisher) Close() error {
	var firstErr error

	if err := p.signer.Close(); err != nil {
		firstErr = err
	}

	p.pool.Close()

	return firstErr
}