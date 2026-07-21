package events

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	cascadia "git.sharegap.net/cascadia/cascadia-go"
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

	publisherNow        = time.Now
	loadPublisherConfig = config.LoadNostrConfig
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
func getPublisher(role string) *gtnostr.Publisher {
	publisherMu.Lock()
	defer publisherMu.Unlock()

	now := publisherNow()
	if !publisherConfigLoaded {
		if !publisherConfigRetry.ready(now) {
			return nil
		}

		cfg, err := loadPublisherConfig(nostrConfigPath())
		if err != nil {
			publisherConfigRetry.fail(now)
			log.Printf("[events/nostr] Failed to load nostr config (retry in %s): %v", publisherConfigRetry.delay, err)
			return nil
		}

		// Environment is the final configuration layer for the runtime-wired
		// events path (including relay and default bunker overrides).
		config.ApplyNostrEnvOverrides(cfg)
		publisherConfig = cfg
		publisherConfigLoaded = true
		publisherConfigRetry.reset()
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
		publisher, err = newEventsPublisher(context.Background(), publisherConfig, signer, nostrRuntimeDir())
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

func nostrConfigPath() string {
	if path := strings.TrimSpace(os.Getenv("GT_NOSTR_CONFIG")); path != "" {
		return path
	}
	return config.NostrConfigPath(nostrRuntimeDir())
}

func nostrRuntimeDir() string {
	if townRoot := strings.TrimSpace(os.Getenv("GT_TOWN_ROOT")); townRoot != "" {
		return townRoot
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
func publishToNostr(event Event) {
	// Extract correlation data from the payload
	correlations := extractCorrelations(event.Type, event.Payload)

	// Parse actor address to extract rig, role, actor components
	rig, role, actor := parseActor(event.Actor)
	publisher := getPublisher(role)
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

	// Publish (async - publisher handles spool fallback)
	if err := publisher.PublishReplaceable(context.Background(), nostrEvent); err != nil {
		log.Printf("[events/nostr] Publish failed for %s (spooled): %v", event.Type, err)
	}

	// Task lifecycle feed events also drive the canonical 30900 task-state
	// projection. This keeps the active dual-write path, rather than only
	// library callers, on the shared task contract.
	if taskState, ok := taskStateProjection(event, event.Actor); ok {
		taskEvent, err := gtnostr.NewTaskStateEvent(taskState)
		if err != nil {
			log.Printf("[events/nostr] Failed to build task state for %s: %v", taskState.Id, err)
		} else if err := publisher.PublishReplaceable(context.Background(), taskEvent); err != nil {
			log.Printf("[events/nostr] Task state publish failed for %s (spooled): %v", taskState.Id, err)
		}
	}
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

// PublishAgentCapability advertises an API agent loop on the canonical 30317
// capability contract. It is best-effort and uses the role's configured NIP-46
// signer, exactly like heartbeats.
func PublishAgentCapability(agentID, rig, role, capability string, tools []string) {
	publisher := getPublisher(role)
	if publisher == nil {
		return
	}

	event, err := gtnostr.NewAgentCapabilityEvent(rig, role, cascadia.CascadiaAgentCapabilityV1Payload{
		AgentId:    agentID,
		Capability: capability,
		Tools:      tools,
	})
	if err != nil {
		log.Printf("[events/nostr] Failed to build capability for %s: %v", agentID, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), gtnostr.DefaultPublishTimeout)
	defer cancel()
	if err := publisher.PublishReplaceable(ctx, event); err != nil {
		log.Printf("[events/nostr] Capability publish failed for %s: %v", agentID, err)
	}
}

func taskStateProjection(event Event, assignee string) (cascadia.CascadiaTaskStateV1Payload, bool) {
	if event.Payload == nil {
		return cascadia.CascadiaTaskStateV1Payload{}, false
	}
	taskID := getString(event.Payload, "bead")
	if taskID == "" {
		return cascadia.CascadiaTaskStateV1Payload{}, false
	}

	status := ""
	switch event.Type {
	case TypeSling, TypeHook:
		status = "in_progress"
	case TypeUnhook:
		status = "open"
	case TypeDone:
		status = "closed"
	default:
		return cascadia.CascadiaTaskStateV1Payload{}, false
	}

	return cascadia.CascadiaTaskStateV1Payload{
		Id:       taskID,
		Title:    taskID,
		Status:   status,
		Assignee: assignee,
		Metadata: map[string]any{"gastown_event": event.Type},
	}, true
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

// ResetPublisherForTesting resets publisher state and maintenance loops.
func ResetPublisherForTesting() {
	publisherMu.Lock()
	defer publisherMu.Unlock()
	for _, cancel := range publisherDrainCancels {
		cancel()
	}
	publisherConfig = nil
	publisherConfigLoaded = false
	publisherConfigRetry = publisherRetryState{}
	publisherBase = nil
	publisherSlots = make(map[string]*publisherSlot)
	publisherDrainCancels = nil
}
