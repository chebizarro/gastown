package nostr

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"fiatjaf.com/nostr"
)

// DMSender handles sending NIP-17 encrypted direct messages.
// NIP-17 uses gift-wrapped sealed events for private communication.
type DMSender struct {
	signer    Signer
	publisher *Publisher
	pool      *RelayPool

	// AllowPlaintextFallback enables the kind 4 plaintext DM fallback.
	// This MUST only be set to true in development/testing environments.
	// In production, DMs should use NIP-17 gift wrapping with NIP-44 encryption.
	// When false (default), SendDM returns an error if NIP-17 is not available.
	AllowPlaintextFallback bool
}

// NewDMSender creates a DM sender.
func NewDMSender(signer Signer, publisher *Publisher, pool *RelayPool) *DMSender {
	return &DMSender{
		signer:    signer,
		publisher: publisher,
		pool:      pool,
	}
}

// SendDM sends an encrypted NIP-17 direct message to a recipient.
// The message goes through the gift wrap pipeline:
// 1. Create kind 14 rumor (unsigned message)
// 2. NIP-44 encrypt into kind 13 seal
// 3. Gift wrap into kind 1059 with random key
// 4. Publish to recipient's DM relays (kind 10050)
func (d *DMSender) SendDM(ctx context.Context, recipientPubkey, content string, extraTags nostr.Tags) error {
	// Create the kind 14 rumor (the actual message content)
	rumor := &nostr.Event{
		Kind:      14,
		Content:   content,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: append(nostr.Tags{
			nostr.Tag{"p", recipientPubkey},
		}, extraTags...),
	}

	// Set pubkey from signer
	rumor.PubKey = PubKeyFromHexGT(d.signer.GetPublicKey())

	// TODO: Implement full NIP-17 gift wrap pipeline:
	// 1. NIP-44 encrypt rumor → kind 13 seal
	// 2. Gift wrap seal → kind 1059 with random key
	// 3. Publish to recipient's 10050 relay list
	//
	// NIP-17 encryption requires NIP-44 support in the signer,
	// which will be implemented when fiatjaf.com/nostr/nip17 and
	// nip44 packages are available.

	if !d.AllowPlaintextFallback {
		return fmt.Errorf("NIP-17 gift wrap not yet implemented; set AllowPlaintextFallback=true for development (INSECURE: sends plaintext kind 4 DMs)")
	}

	// INSECURE FALLBACK: publish kind 4 (legacy DM) with plaintext content.
	// This is only for development/testing. Production must use NIP-17.
	log.Printf("[nostr/dm] WARNING: sending plaintext kind 4 DM (NIP-17 not yet available)")
	dm := &nostr.Event{
		Kind:      4,
		Content:   content, // PLAINTEXT - not encrypted
		Tags:      rumor.Tags,
		CreatedAt: rumor.CreatedAt,
	}

	if err := d.signer.Sign(ctx, dm); err != nil {
		return fmt.Errorf("signing DM: %w", err)
	}

	return d.publisher.Publish(ctx, dm)
}

// DMListener handles receiving NIP-17 encrypted direct messages.
type DMListener struct {
	signer  Signer
	pool    *RelayPool
	handler DMHandler
	cancel  context.CancelFunc
	done    chan struct{}
}

// DMHandler processes an incoming direct message.
type DMHandler func(senderPubkey, content string, event *nostr.Event)

// NewDMListener creates a DM listener.
func NewDMListener(signer Signer, pool *RelayPool, handler DMHandler) *DMListener {
	return &DMListener{
		signer:  signer,
		pool:    pool,
		handler: handler,
		done:    make(chan struct{}),
	}
}

// Start begins listening for incoming DMs.
func (l *DMListener) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	l.cancel = cancel

	pubkey := l.signer.GetPublicKey()

	// Subscribe to gift wraps addressed to this agent (NIP-17)
	// Fall back to kind 4 (legacy DMs) for now
	now := nostr.Timestamp(time.Now().Unix())
	filters := []nostr.Filter{
		{
			Kinds: KindSlice(1059, 4), // gift wraps + legacy DMs
			Tags:  nostr.TagMap{"p": []string{pubkey}},
			Since: now,
		},
	}

	subs := l.pool.Subscribe(ctx, filters)

	go func() {
		defer close(l.done)

		for _, sub := range subs {
			go func(s *nostr.Subscription) {
				for event := range s.Events {
					l.processEvent(ctx, &event)
				}
			}(sub)
		}

		<-ctx.Done()
	}()

	pubkeyPrefix := pubkey
	if len(pubkeyPrefix) > 8 {
		pubkeyPrefix = pubkeyPrefix[:8]
	}
	log.Printf("[nostr/dm] Listening for DMs addressed to %s...", pubkeyPrefix)
}

// Stop stops the DM listener.
func (l *DMListener) Stop() {
	if l.cancel != nil {
		l.cancel()
	}
	select {
	case <-l.done:
	case <-time.After(5 * time.Second):
	}
}

// processEvent handles an incoming DM event.
func (l *DMListener) processEvent(ctx context.Context, event *nostr.Event) {
	switch event.Kind {
	case 1059:
		// NIP-17 gift wrap — need to unwrap
		// TODO: Implement full NIP-17 unwrap:
		// 1. Decrypt kind 1059 → kind 13 seal
		// 2. Decrypt kind 13 → kind 14 rumor
		// 3. Extract sender pubkey and content
		log.Printf("[nostr/dm] Received gift wrap from %s (NIP-17 unwrap TODO)", PubKeyToString(event.PubKey)[:8])

	case 4:
		// Legacy DM (NIP-04) — direct content
		// TODO: NIP-04 decrypt
		l.handler(PubKeyToString(event.PubKey), event.Content, event)

	default:
		log.Printf("[nostr/dm] Unexpected event kind %d", event.Kind)
	}
}

// SendInterrupt sends a high-priority DM interrupt to an agent.
func SendInterrupt(ctx context.Context, sender *DMSender, recipientPubkey, message string) error {
	return sender.SendDM(ctx, recipientPubkey, "INTERRUPT: "+message, nostr.Tags{
		nostr.Tag{"subject", "INTERRUPT: " + message},
		nostr.Tag{"priority", "urgent"},
	})
}

// SendHandoff sends a handoff DM (to self or successor).
func SendHandoff(ctx context.Context, sender *DMSender, recipientPubkey, topic, contextInfo, status, next string) error {
	content := fmt.Sprintf("HANDOFF: %s\n\n## Context\n%s\n\n## Status\n%s\n\n## Next\n%s",
		topic, contextInfo, status, next)

	return sender.SendDM(ctx, recipientPubkey, content, nostr.Tags{
		nostr.Tag{"subject", "HANDOFF: " + topic},
	})
}

// ParseDMPriority extracts the priority from a DM event's tags.
func ParseDMPriority(event *nostr.Event) string {
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "priority" {
			return tag[1]
		}
	}
	return "normal"
}

// DMEvent represents a parsed incoming DM.
type DMEvent struct {
	SenderPubkey string
	Content      string
	Priority     string
	Subject      string
	ReplyTo      string // event ID this is replying to
	Raw          *nostr.Event
}

// ParseDMEvent extracts structured data from a DM event.
func ParseDMEvent(senderPubkey, content string, event *nostr.Event) *DMEvent {
	dm := &DMEvent{
		SenderPubkey: senderPubkey,
		Content:      content,
		Priority:     ParseDMPriority(event),
		Raw:          event,
	}

	for _, tag := range event.Tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "subject":
			dm.Subject = tag[1]
		case "e":
			dm.ReplyTo = tag[1]
		}
	}

	return dm
}

// marshalJSON is a helper to convert to JSON string for event content.
func marshalJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
