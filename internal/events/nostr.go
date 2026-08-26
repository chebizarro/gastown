package events

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/steveyegge/gastown/internal/config"
	gtnostr "github.com/steveyegge/gastown/internal/nostr"
)

// correlations is a local alias for the canonical nostr.Correlations type.
// We use the local type for internal extraction then convert for publishing.
type correlations = gtnostr.Correlations

const (
	publisherInitialBackoff = time.Second
	publisherMaxBackoff     = time.Minute
)

type publisherRetryState struct {
	nextAttempt time.Time
	delay       time.Duration
}

func (r *publisherRetryState) ready(now time.Time) bool {
	return r.nextAttempt.IsZero() || !now.Before(r.nextAttempt)
}

func (r *publisherRetryState) fail(now time.Time) {
	if r.delay == 0 {
		r.delay = publisherInitialBackoff
	} else {
		r.delay *= 2
		if r.delay > publisherMaxBackoff {
			r.delay = publisherMaxBackoff
		}
	}
	r.nextAttempt = now.Add(r.delay)
}

func (r *publisherRetryState) reset() {
	r.nextAttempt = time.Time{}
	r.delay = 0
}

type publisherSlot struct {
	publisher *gtnostr.Publisher
	retry     publisherRetryState
}

var (
	publisherMu           sync.Mutex
	publisherConfig       *config.NostrConfig
	publisherConfigLoaded bool
	publisherConfigRetry  publisherRetryState
	publisherBase         *gtnostr.Publisher
	publisherSlots        = make(map[string]*publisherSlot)
	publisherDrainCancels []context.CancelFunc
	publisherConfigDigest [sha256.Size]byte

	publisherNow        = time.Now
	loadPublisherConfig = config.LoadOrCreateNostrConfig
	newPublisherSigner  = func(ctx context.Context, bunker string) (gtnostr.Signer, error) {
		return gtnostr.NewNIP46Signer(ctx, bunker)
	}
	newEventsPublisher = func(ctx context.Context, cfg *config.NostrConfig, signer gtnostr.Signer, runtimeDir string) (*gtnostr.Publisher, error) {
		return gtnostr.NewPublisher(ctx, cfg, signer, runtimeDir)
	}
)

// getPublisher returns the publisher for a role. Initialization failures are
// retried with bounded exponential backoff instead of disabling Nostr for the
// lifetime of the process.
func getPublisher(role string, townRoots ...string) *gtnostr.Publisher {
	publisherMu.Lock()
	defer publisherMu.Unlock()

	now := publisherNow()
	path := nostrConfigPath(townRoots...)
	if publisherConfigLoaded {
		if data, err := os.ReadFile(path); err == nil {
			digest := sha256.Sum256(data)
			if digest != publisherConfigDigest {
				candidate, err := loadPublisherConfig(path)
				if err != nil {
					log.Printf("[events/nostr] Policy reload rejected; keeping last valid projection: %v", err)
				} else {
					resetPublisherLocked()
					publisherConfig = candidate
					publisherConfigLoaded = true
					publisherConfigDigest = digest
					logEffectivePublisherPolicy("reloaded", candidate)
				}
			}
		}
	}
	if !publisherConfigLoaded {
		if !publisherConfigRetry.ready(now) {
			return nil
		}

		cfg, err := loadPublisherConfig(path)
		if err != nil {
			publisherConfigRetry.fail(now)
			log.Printf("[events/nostr] Failed to load nostr config (retry in %s): %v", publisherConfigRetry.delay, err)
			return nil
		}

		publisherConfig = cfg
		publisherConfigLoaded = true
		publisherConfigRetry.reset()
		if data, err := os.ReadFile(path); err == nil {
			publisherConfigDigest = sha256.Sum256(data)
		}
		logEffectivePublisherPolicy("loaded", cfg)
	}

	if publisherConfig == nil || !publisherConfig.Enabled {
		return nil
	}

	identityKey, identity := resolvePublisherIdentity(publisherConfig, role)
	if identity == nil {
		log.Printf("[events/nostr] No identity configured for role %q and no deacon fallback", role)
		return nil
	}

	slot := publisherSlots[identityKey]
	if slot == nil {
		slot = &publisherSlot{}
		publisherSlots[identityKey] = slot
	}
	if slot.publisher != nil {
		return slot.publisher
	}
	if !slot.retry.ready(now) {
		return nil
	}

	signer, err := newPublisherSigner(context.Background(), identity.Signer.Bunker)
	if err != nil {
		slot.retry.fail(now)
		log.Printf("[events/nostr] Failed to create %s signer (retry in %s): %v", identityKey, slot.retry.delay, err)
		return nil
	}

	var publisher *gtnostr.Publisher
	if publisherBase == nil {
		publisher, err = newEventsPublisher(context.Background(), publisherConfig, signer, nostrRuntimeDir(townRoots...))
		if err != nil {
			_ = signer.Close()
			slot.retry.fail(now)
			log.Printf("[events/nostr] Failed to create %s publisher (retry in %s): %v", identityKey, slot.retry.delay, err)
			return nil
		}
		publisherBase = publisher
		publisherDrainCancels = append(publisherDrainCancels, startPublisherMaintenance(publisher, spoolDrainInterval(publisherConfig)))
	} else {
		publisher = publisherBase.WithSigner(signer)
	}

	slot.publisher = publisher
	slot.retry.reset()
	return publisher
}

func nostrConfigPath(townRoots ...string) string {
	if path := strings.TrimSpace(os.Getenv("GT_NOSTR_CONFIG")); path != "" {
		return path
	}
	return config.NostrConfigPath(nostrRuntimeDir(townRoots...))
}

func nostrRuntimeDir(townRoots ...string) string {
	if townRoot := strings.TrimSpace(os.Getenv("GT_TOWN_ROOT")); townRoot != "" {
		return townRoot
	}
	if len(townRoots) > 0 && strings.TrimSpace(townRoots[0]) != "" {
		return townRoots[0]
	}
	return "."
}

func resolvePublisherIdentity(cfg *config.NostrConfig, role string) (string, *config.NostrIdentity) {
	if cfg == nil {
		return "", nil
	}
	role = strings.TrimSpace(role)
	if identity := cfg.Identities[role]; role != "" && identity != nil {
		return role, identity
	}
	if identity := cfg.Identities["deacon"]; identity != nil {
		return "deacon", identity
	}
	// ApplyNostrEnvOverrides writes its single-identity settings here. Keep it
	// after the required deacon fallback so file-configured policy wins.
	if identity := cfg.Identities["default"]; identity != nil {
		return "default", identity
	}
	return "", nil
}

func spoolDrainInterval(cfg *config.NostrConfig) time.Duration {
	seconds := config.DefaultNostrDefaults().SpoolDrainIntervalSec
	if cfg != nil && cfg.Defaults != nil && cfg.Defaults.SpoolDrainIntervalSec > 0 {
		seconds = cfg.Defaults.SpoolDrainIntervalSec
	}
	return time.Duration(seconds) * time.Second
}

func startPublisherMaintenance(publisher *gtnostr.Publisher, interval time.Duration) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				reconnectCtx, reconnectCancel := context.WithTimeout(ctx, gtnostr.DefaultConnectTimeout)
				publisher.Pool().Reconnect(reconnectCtx)
				reconnectCancel()

				drainCtx, drainCancel := context.WithTimeout(ctx, gtnostr.DefaultPublishTimeout)
				sent, failed, err := publisher.DrainSpool(drainCtx)
				drainCancel()
				if err != nil {
					log.Printf("[events/nostr] Spool drain failed: %v", err)
				} else if sent > 0 || failed > 0 {
					log.Printf("[events/nostr] Spool drain: sent=%d failed=%d", sent, failed)
				}
			}
		}
	}()
	return cancel
}

// publishToNostr converts an Event to a canonical NIP-38 status event and publishes it.
// This is called asynchronously from write() and should never block.
func publishToNostr(event Event, townRoot string) {
	// Extract correlation data from the payload
	correlations := extractCorrelations(event.Type, event.Payload)

	// Parse actor address to extract rig, role, actor components
	rig, role, actor := parseActor(event.Actor)
	publisher := getPublisher(role, townRoot)
	if publisher == nil {
		return
	}

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

	// Publisher handles spool fallback. Bound relay operations because selected
	// coordination events execute synchronously before a CLI process exits.
	ctx, cancel := context.WithTimeout(context.Background(), gtnostr.DefaultPublishTimeout)
	err = publisher.PublishReplaceable(ctx, nostrEvent)
	cancel()
	if err != nil {
		log.Printf("[events/nostr] Publish failed for %s (spooled): %v", event.Type, err)
	}

	publishToNIP29(publisher, event, rig, role, actor, correlations)
}

type coordinationClass string

const (
	coordinationProgress coordinationClass = "progress"
	coordinationAsk      coordinationClass = "ask"
	coordinationResult   coordinationClass = "result"
)

func publishToNIP29(publisher *gtnostr.Publisher, event Event, rig, role, actor string, c *correlations) {
	class, relays, groups := coordinationTargets(event.Type)
	if len(relays) == 0 || len(groups) == 0 {
		return
	}
	content := formatCoordinationMessage(class, event, c)
	for _, groupID := range groups {
		groupEvent, err := gtnostr.NewNIP29GroupMessage(groupID, event.Type, rig, role, actor, content)
		if err != nil {
			log.Printf("[events/nostr] Failed to build NIP-29 message for %s: %v", event.Type, err)
			continue
		}
		if c != nil {
			gtnostr.WithCorrelation(groupEvent, c.IssueID, c.ConvoyID, c.BeadID, c.SessionID)
		}
		gtnostr.WithCanonicalReferences(groupEvent,
			getString(event.Payload, "nostr_event_id"),
			getString(event.Payload, "nostr_event_address"),
		)
		ctx, cancel := context.WithTimeout(context.Background(), gtnostr.DefaultPublishTimeout)
		err = publisher.PublishToRelays(ctx, groupEvent, relays)
		cancel()
		if err != nil {
			log.Printf("[events/nostr] NIP-29 publish failed for %s/%s: %v", event.Type, groupID, err)
		}
	}
}

func coordinationTargets(eventType string) (coordinationClass, []string, []string) {
	publisherMu.Lock()
	defer publisherMu.Unlock()
	if publisherConfig == nil || publisherConfig.NIP29 == nil || !publisherConfig.NIP29.Enabled {
		return "", nil, nil
	}
	var class coordinationClass
	var groups []string
	switch eventType {
	case TypeConvoyProgress:
		class, groups = coordinationProgress, publisherConfig.NIP29.Groups.Progress
	case TypeConvoyAsk, TypeEscalationSent:
		class, groups = coordinationAsk, publisherConfig.NIP29.Groups.Asks
	case TypeConvoyResult:
		class, groups = coordinationResult, publisherConfig.NIP29.Groups.Results
	default:
		return "", nil, nil
	}
	return class,
		append([]string(nil), publisherConfig.NIP29.Relays...),
		append([]string(nil), groups...)
}

func formatCoordinationMessage(class coordinationClass, event Event, c *correlations) string {
	parts := make([]string, 0, 3)
	if c != nil && c.ConvoyID != "" {
		parts = append(parts, "convoy "+c.ConvoyID)
	}
	if c != nil && c.IssueID != "" {
		parts = append(parts, "task "+c.IssueID)
	}
	detail := ""
	for _, key := range []string{"message", "summary", "reason", "error", "description", "status"} {
		if detail = strings.TrimSpace(getString(event.Payload, key)); detail != "" {
			break
		}
	}
	if len(parts) == 0 {
		parts = append(parts, event.Type)
	}
	if detail != "" {
		parts = append(parts, detail)
	}
	return fmt.Sprintf("[%s] %s", class, strings.Join(parts, " — "))
}

// PublishAgentHeartbeat publishes the latest canonical heartbeat for an API
// agent loop. It is best-effort; callers should invoke it asynchronously.
func PublishAgentHeartbeat(agentID, rig, role, status string) {
	publisher := getPublisher(role)
	if publisher == nil {
		return
	}

	event, err := gtnostr.NewAgentHeartbeatEvent(agentID, rig, role, status)
	if err != nil {
		log.Printf("[events/nostr] Failed to build heartbeat for %s: %v", agentID, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), gtnostr.DefaultPublishTimeout)
	defer cancel()
	if err := publisher.PublishReplaceable(ctx, event); err != nil {
		log.Printf("[events/nostr] Heartbeat publish failed for %s: %v", agentID, err)
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
	case TypeConvoyProgress, TypeConvoyAsk, TypeConvoyResult:
		c.ConvoyID = getString(payload, "convoy_id")
		c.BeadID = getString(payload, "bead")
		c.IssueID = c.BeadID

	case TypeEscalationSent:
		c.IssueID = getString(payload, "escalation_id")
		if c.IssueID == "" {
			c.IssueID = getString(payload, "rig")
		}

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

// ResetPublisherForTesting resets publisher state and maintenance loops.
func ResetPublisherForTesting() {
	publisherMu.Lock()
	defer publisherMu.Unlock()
	resetPublisherLocked()
}

func resetPublisherLocked() {
	for _, cancel := range publisherDrainCancels {
		cancel()
	}
	if publisherBase != nil {
		_ = publisherBase.Close()
	}
	publisherConfig = nil
	publisherConfigLoaded = false
	publisherConfigRetry = publisherRetryState{}
	publisherBase = nil
	publisherSlots = make(map[string]*publisherSlot)
	publisherDrainCancels = nil
	publisherConfigDigest = [sha256.Size]byte{}
}

func logEffectivePublisherPolicy(action string, cfg *config.NostrConfig) {
	log.Printf("[events/nostr] Effective policy %s: enabled=%t read_relays=%d write_relays=%d blossom_servers=%d feed_curator=%t",
		action, cfg.Enabled, len(cfg.ReadRelays), len(cfg.WriteRelays), len(cfg.BlossomServers), cfg.IsFeedCuratorEnabled())
}
