package events

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sync"

	"github.com/steveyegge/gastown/internal/config"
	gtnostr "github.com/steveyegge/gastown/internal/nostr"
)

// correlations is a local alias for the canonical nostr.Correlations type.
// We use the local type for internal extraction then convert for publishing.
type correlations = gtnostr.Correlations

// Global publisher singleton for Nostr event publishing.
var (
	globalPublisher *gtnostr.Publisher
	publisherOnce   sync.Once
	publisherErr    error
)

// getPublisher returns the global Nostr publisher, initializing it on first call.
// Returns nil if Nostr is not enabled or initialization fails.
func getPublisher() *gtnostr.Publisher {
	publisherOnce.Do(func() {
		if !config.IsNostrEnabled() {
			return
		}

		// Load config from GT_NOSTR_CONFIG env or default path
		cfgPath := os.Getenv("GT_NOSTR_CONFIG")
		if cfgPath == "" {
			townRoot := os.Getenv("GT_TOWN_ROOT")
			if townRoot == "" {
				townRoot = "."
			}
			cfgPath = config.NostrConfigPath(townRoot)
		}
		cfg, err := config.LoadNostrConfig(cfgPath)
		if err != nil {
			log.Printf("[events/nostr] Failed to load nostr config: %v", err)
			publisherErr = err
			return
		}

		if !cfg.Enabled {
			return
		}

		// For the deacon identity, look up in config
		deaconID, ok := cfg.Identities["deacon"]
		if !ok {
			log.Printf("[events/nostr] No deacon identity configured, Nostr publishing disabled")
			return
		}

		// Create signer from deacon identity
		signer, err := gtnostr.NewNIP46Signer(context.Background(), deaconID.Signer.Bunker)
		if err != nil {
			log.Printf("[events/nostr] Failed to create signer: %v", err)
			publisherErr = err
			return
		}

		// Determine runtime dir for spool
		runtimeDir := os.Getenv("GT_TOWN_ROOT")
		if runtimeDir == "" {
			runtimeDir = "."
		}

		publisher, err := gtnostr.NewPublisher(context.Background(), cfg, signer, runtimeDir)
		if err != nil {
			log.Printf("[events/nostr] Failed to create publisher: %v", err)
			publisherErr = err
			return
		}

		globalPublisher = publisher
	})

	return globalPublisher
}

// publishToNostr converts an Event to a kind 30315 Nostr event and publishes it.
// This is called asynchronously from write() and should never block.
func publishToNostr(event Event) {
	publisher := getPublisher()
	if publisher == nil {
		return
	}

	// Extract correlation data from the payload
	correlations := extractCorrelations(event.Type, event.Payload)

	// Parse actor address to extract rig, role, actor components
	rig, role, actor := parseActor(event.Actor)

	// Build the Nostr event
	nostrEvent, err := gtnostr.NewLogStatusEvent(
		rig, role, actor, event.Type, event.Visibility, event.Payload,
	)
	if err != nil {
		log.Printf("[events/nostr] Failed to build nostr event for %s: %v", event.Type, err)
		return
	}

	// Add correlation tags
	if correlations != nil {
		gtnostr.WithCorrelation(nostrEvent, correlations.IssueID, correlations.ConvoyID, correlations.BeadID, correlations.SessionID)

		// Add type-specific extra tags
		addExtraTags(nostrEvent, event.Type, correlations)
	}

	// Publish (async - publisher handles spool fallback)
	if err := publisher.Publish(context.Background(), nostrEvent); err != nil {
		log.Printf("[events/nostr] Publish failed for %s (spooled): %v", event.Type, err)
	}
}

// extractCorrelations extracts cross-reference data from event payloads.
// Each event type stores different fields in its payload map.
func extractCorrelations(eventType string, payload map[string]interface{}) *correlations {
	if payload == nil {
		return nil
	}

	c := &correlations{}

	switch eventType {
	case TypeSling:
		c.BeadID = getString(payload, "bead")
		c.IssueID = c.BeadID

	case TypeHook:
		c.BeadID = getString(payload, "bead")
		c.IssueID = c.BeadID

	case TypeUnhook:
		c.BeadID = getString(payload, "bead")
		c.IssueID = c.BeadID

	case TypeHandoff:
		c.SessionID = getString(payload, "session")

	case TypeDone:
		c.BeadID = getString(payload, "bead")
		c.IssueID = c.BeadID
		c.Branch = getString(payload, "branch")

	case TypeSessionStart, TypeSessionEnd:
		c.SessionID = getString(payload, "session_id")

	case TypeSessionDeath:
		c.SessionID = getString(payload, "session")

	case TypeMergeStarted, TypeMerged, TypeMergeFailed, TypeMergeSkipped:
		c.MergeReq = getString(payload, "mr_id")
		c.Branch = getString(payload, "branch")
	}

	return c
}

// addExtraTags adds event-type-specific tags to the Nostr event.
func addExtraTags(event interface{}, eventType string, c *correlations) {
	// The nostr.Event type uses Tags field - we need to work with the concrete type
	// Since we're using fiatjaf.com/nostr, we add tags via the event construction
	// The event.go helpers already handle base tags; extra tags are added via
	// WithCorrelation which covers issue, convoy, bead, session
	//
	// Type-specific tags (branch, mr, target) are added here.
	// Note: We rely on the tag format from nostr-protocol.md spec.
	//
	// TODO: Add branch and MR tags once the nostr event type allows tag mutation.
	// For now, the correlation tags cover the critical cross-references.
	_ = eventType
	_ = c
}

// parseActor splits an actor address like "rig/polecats/Name" or "rig/witness"
// into rig, role, and actor components.
func parseActor(actor string) (rig, role, name string) {
	// Common patterns:
	// "MyRig/polecats/Toast" -> rig=MyRig, role=polecat, actor=Toast
	// "MyRig/witness" -> rig=MyRig, role=witness, actor=witness
	// "gt" -> rig="", role="system", actor="gt"
	// "deacon" -> rig="", role="deacon", actor="deacon"

	parts := splitActor(actor)
	switch len(parts) {
	case 3:
		return parts[0], singularRole(parts[1]), parts[2]
	case 2:
		return parts[0], parts[1], parts[1]
	case 1:
		return "", parts[0], parts[0]
	default:
		return "", "unknown", actor
	}
}

// splitActor splits on "/" without importing strings.
func splitActor(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// singularRole converts plural role directories to singular role names.
func singularRole(plural string) string {
	switch plural {
	case "polecats":
		return "polecat"
	case "crews", "crew":
		return "crew"
	default:
		return plural
	}
}

// getString safely extracts a string from a map.
func getString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		// Try JSON marshaling for non-string values
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
	return s
}

// ResetPublisherForTesting resets the publisher singleton (test use only).
func ResetPublisherForTesting() {
	publisherOnce = sync.Once{}
	globalPublisher = nil
	publisherErr = nil
}
