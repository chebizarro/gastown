package nostr

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"fiatjaf.com/nostr"

	"github.com/steveyegge/gastown/internal/config"
)

const (
	// DefaultAgentHeartbeatInterval is the default heartbeat interval for agents.
	DefaultAgentHeartbeatInterval = 60 * time.Second
	// DefaultDeaconHeartbeatInterval is the default heartbeat interval for the deacon.
	DefaultDeaconHeartbeatInterval = 30 * time.Second
	// StaleMultiplier is the number of missed heartbeats before an agent is considered stale.
	StaleMultiplier = 3
)

// LifecycleStatus represents an agent's lifecycle state.
type LifecycleStatus string

const (
	StatusReady    LifecycleStatus = "ready"
	StatusBusy     LifecycleStatus = "busy"
	StatusRetiring LifecycleStatus = "retiring"
	StatusDead     LifecycleStatus = "dead"
)

// LifecycleContent is the content body for kind 30316 lifecycle events.
type LifecycleContent struct {
	Schema        string `json:"schema"`                    // "gt/lifecycle@1"
	Status        string `json:"status"`                    // ready|busy|retiring|dead
	Role          string `json:"role"`
	Rig           string `json:"rig"`
	Instance      string `json:"instance"`
	CWD           string `json:"cwd,omitempty"`
	StartedAt     string `json:"started_at"`
	LastHeartbeat string `json:"last_heartbeat"`
	CurrentIssue  string `json:"current_issue,omitempty"`
	Model         string `json:"model,omitempty"`
}

// HeartbeatPublisher manages periodic lifecycle heartbeat publishing.
type HeartbeatPublisher struct {
	publisher *Publisher
	actor     string
	rig       string
	role      string
	instance  string
	interval  time.Duration
	startedAt time.Time

	mu           sync.Mutex
	status       LifecycleStatus
	currentIssue string
	model        string
	cwd          string

	cancel context.CancelFunc
	done   chan struct{}
}

// NewHeartbeatPublisher creates a heartbeat publisher for an agent.
func NewHeartbeatPublisher(publisher *Publisher, actor, rig, role, instance string) *HeartbeatPublisher {
	interval := DefaultAgentHeartbeatInterval
	if role == "deacon" {
		interval = DefaultDeaconHeartbeatInterval
	}

	// Check environment for custom interval
	if envInterval := os.Getenv("GT_NOSTR_HEARTBEAT_INTERVAL"); envInterval != "" {
		if secs, err := strconv.Atoi(envInterval); err == nil && secs > 0 {
			interval = time.Duration(secs) * time.Second
		}
	}

	cwd, _ := os.Getwd()

	return &HeartbeatPublisher{
		publisher: publisher,
		actor:     actor,
		rig:       rig,
		role:      role,
		instance:  instance,
		interval:  interval,
		startedAt: time.Now(),
		status:    StatusReady,
		cwd:       cwd,
		done:      make(chan struct{}),
	}
}

// Start begins the periodic heartbeat publishing.
func (h *HeartbeatPublisher) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	h.cancel = cancel

	// Publish initial ready event
	h.publishHeartbeat(ctx)

	go func() {
		defer close(h.done)
		ticker := time.NewTicker(h.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.publishHeartbeat(ctx)
			}
		}
	}()
}

// Stop publishes a retiring event and stops the heartbeat.
func (h *HeartbeatPublisher) Stop(ctx context.Context) {
	h.mu.Lock()
	h.status = StatusRetiring
	h.mu.Unlock()

	// Publish retiring status
	h.publishHeartbeat(ctx)

	// Cancel heartbeat goroutine
	if h.cancel != nil {
		h.cancel()
	}

	// Wait for goroutine to finish
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
	}

	// Publish dead status
	h.mu.Lock()
	h.status = StatusDead
	h.mu.Unlock()
	h.publishHeartbeat(ctx)
}

// SetStatus updates the agent's lifecycle status.
func (h *HeartbeatPublisher) SetStatus(status LifecycleStatus) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status = status
}

// SetCurrentIssue updates the current issue the agent is working on.
func (h *HeartbeatPublisher) SetCurrentIssue(issueID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.currentIssue = issueID
	if issueID != "" {
		h.status = StatusBusy
	} else {
		h.status = StatusReady
	}
}

// SetModel sets the LLM model name for reporting.
func (h *HeartbeatPublisher) SetModel(model string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.model = model
}

// publishHeartbeat publishes a single lifecycle event.
func (h *HeartbeatPublisher) publishHeartbeat(ctx context.Context) {
	h.mu.Lock()
	content := LifecycleContent{
		Schema:        SchemaVersion("lifecycle", 1),
		Status:        string(h.status),
		Role:          h.role,
		Rig:           h.rig,
		Instance:      h.instance,
		CWD:           h.cwd,
		StartedAt:     h.startedAt.Format(time.RFC3339),
		LastHeartbeat: time.Now().Format(time.RFC3339),
		CurrentIssue:  h.currentIssue,
		Model:         h.model,
	}
	h.mu.Unlock()

	event, err := buildLifecycleEvent(h.actor, h.rig, h.role, h.instance, content)
	if err != nil {
		log.Printf("[nostr/lifecycle] Failed to build event: %v", err)
		return
	}

	if err := h.publisher.PublishReplaceable(ctx, event); err != nil {
		log.Printf("[nostr/lifecycle] Failed to publish heartbeat: %v", err)
	}
}

// buildLifecycleEvent constructs a kind 30316 lifecycle event.
func buildLifecycleEvent(actor, rig, role, instance string, content LifecycleContent) (*nostr.Event, error) {
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}

	// Replaceable key: rig/role/instance
	dTag := rig + "/" + role + "/" + instance

	tags := BaseTags(rig, role, actor)
	tags = append(tags,
		ReplaceableTag(dTag),
		nostr.Tag{"instance", instance},
		nostr.Tag{"status", content.Status},
	)

	if content.CurrentIssue != "" {
		tags = append(tags, nostr.Tag{"t", content.CurrentIssue})
	}
	if content.Model != "" {
		tags = append(tags, nostr.Tag{"model", content.Model})
	}

	event := &nostr.Event{
		Kind:      KindLifecycle,
		Tags:      tags,
		Content:   string(contentJSON),
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
	}

	return event, nil
}

// PublishDeath publishes an authoritative death event for a crashed agent.
// Called by the Deacon when it detects a stale or crashed agent.
func PublishDeath(publisher *Publisher, actor, rig, role, instance, reason string) error {
	content := LifecycleContent{
		Schema:        SchemaVersion("lifecycle", 1),
		Status:        string(StatusDead),
		Role:          role,
		Rig:           rig,
		Instance:      instance,
		LastHeartbeat: time.Now().Format(time.RFC3339),
	}

	event, err := buildLifecycleEvent(actor, rig, role, instance, content)
	if err != nil {
		return err
	}

	return publisher.PublishReplaceable(nil, event)
}

// StaleThreshold returns how long without a heartbeat before an agent is considered stale.
func StaleThreshold(role string) time.Duration {
	interval := DefaultAgentHeartbeatInterval
	if role == "deacon" {
		interval = DefaultDeaconHeartbeatInterval
	}

	// Check env for custom interval
	if envInterval := os.Getenv("GT_NOSTR_HEARTBEAT_INTERVAL"); envInterval != "" {
		if secs, err := strconv.Atoi(envInterval); err == nil && secs > 0 {
			interval = time.Duration(secs) * time.Second
		}
	}

	return interval * StaleMultiplier
}

// IsNostrEnabled checks if Nostr integration is active.
// Convenience wrapper for the config package function.
func IsNostrEnabled() bool {
	return config.IsNostrEnabled()
}
