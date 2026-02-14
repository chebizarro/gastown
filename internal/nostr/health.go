package nostr

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/config"
)

// SunsetFlags tracks which subsystems have been sunset (local path disabled).
type SunsetFlags struct {
	EventsLocal bool // GT_EVENTS_LOCAL (default: true = local writes on)
	FeedCurator bool // GT_FEED_CURATOR (default: true = curator running)
	ConvoyLocal bool // GT_CONVOY_LOCAL (default: true = bd dep list)
	MailLocal   bool // GT_MAIL_LOCAL (default: true = beads mail)
	NudgeLocal  bool // GT_NUDGE_LOCAL (default: true = tmux nudge)
}

// LoadSunsetFlags reads sunset flags from environment.
// All default to true (local paths enabled) for backward compatibility.
func LoadSunsetFlags() SunsetFlags {
	return SunsetFlags{
		EventsLocal: envBool("GT_EVENTS_LOCAL", true),
		FeedCurator: envBool("GT_FEED_CURATOR", true),
		ConvoyLocal: envBool("GT_CONVOY_LOCAL", true),
		MailLocal:   envBool("GT_MAIL_LOCAL", true),
		NudgeLocal:  envBool("GT_NUDGE_LOCAL", true),
	}
}

// HealthStatus contains the full Nostr health check results.
type HealthStatus struct {
	Enabled      bool              `json:"enabled"`
	WriteRelays  []RelayStatus     `json:"write_relays"`
	ReadRelays   []RelayStatus     `json:"read_relays"`
	SignerStatus string            `json:"signer_status"`
	SpoolCount   int               `json:"spool_count"`
	Sunset       SunsetFlags       `json:"sunset"`
	Agents       []AgentHealthInfo `json:"agents,omitempty"`
}

// RelayStatus represents a relay's connection status.
type RelayStatus struct {
	URL       string `json:"url"`
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
}

// AgentHealthInfo represents an agent's health from lifecycle events.
type AgentHealthInfo struct {
	Actor         string `json:"actor"`
	Status        string `json:"status"`
	LastHeartbeat string `json:"last_heartbeat"`
	CurrentIssue  string `json:"current_issue,omitempty"`
}

// CheckHealth performs a comprehensive Nostr health check.
func CheckHealth(ctx context.Context, pool *RelayPool, spool *Spool, cfg *config.NostrConfig) *HealthStatus {
	status := &HealthStatus{
		Enabled: cfg != nil && cfg.Enabled,
		Sunset:  LoadSunsetFlags(),
	}

	if !status.Enabled || cfg == nil {
		return status
	}

	// Check write relays
	for _, url := range cfg.WriteRelays {
		rs := RelayStatus{URL: url, Connected: false}
		if pool != nil {
			for _, rURL := range pool.WriteRelayURLs() {
				if rURL == url {
					rs.Connected = true
					break
				}
			}
		}
		status.WriteRelays = append(status.WriteRelays, rs)
	}

	// Check read relays
	for _, url := range cfg.ReadRelays {
		rs := RelayStatus{URL: url, Connected: false}
		// Read relay connection check is similar
		status.ReadRelays = append(status.ReadRelays, rs)
	}

	// Signer status
	if len(cfg.Identities) > 0 {
		status.SignerStatus = "configured"
	} else {
		status.SignerStatus = "not configured"
	}

	// Spool count
	if spool != nil {
		status.SpoolCount = spool.Count()
	}

	return status
}

// FormatHealthStatus formats health status as human-readable text.
func FormatHealthStatus(h *HealthStatus) string {
	var sb strings.Builder

	sb.WriteString("Nostr Status:\n")
	sb.WriteString(fmt.Sprintf("  Enabled: %v\n", h.Enabled))

	if !h.Enabled {
		sb.WriteString("  (Set GT_NOSTR_ENABLED=1 to enable)\n")
		return sb.String()
	}

	// Write relays
	for _, r := range h.WriteRelays {
		icon := "connected"
		if !r.Connected {
			icon = "disconnected"
		}
		sb.WriteString(fmt.Sprintf("  Write Relay: %s (%s)\n", r.URL, icon))
	}

	// Read relays
	for _, r := range h.ReadRelays {
		icon := "connected"
		if !r.Connected {
			icon = "disconnected"
		}
		sb.WriteString(fmt.Sprintf("  Read Relay: %s (%s)\n", r.URL, icon))
	}

	sb.WriteString(fmt.Sprintf("  Signer: %s\n", h.SignerStatus))
	sb.WriteString(fmt.Sprintf("  Spool: %d events pending\n", h.SpoolCount))

	// Sunset status
	sb.WriteString("\nSunset Status:\n")
	sb.WriteString(fmt.Sprintf("  Events Local:  %s\n", sunsetLabel(h.Sunset.EventsLocal)))
	sb.WriteString(fmt.Sprintf("  Feed Curator:  %s\n", sunsetLabel(h.Sunset.FeedCurator)))
	sb.WriteString(fmt.Sprintf("  Convoy Local:  %s\n", sunsetLabel(h.Sunset.ConvoyLocal)))
	sb.WriteString(fmt.Sprintf("  Mail Local:    %s\n", sunsetLabel(h.Sunset.MailLocal)))
	sb.WriteString(fmt.Sprintf("  Nudge Local:   %s\n", sunsetLabel(h.Sunset.NudgeLocal)))

	// Agents
	if len(h.Agents) > 0 {
		sb.WriteString("\nAgents:\n")
		for _, a := range h.Agents {
			line := fmt.Sprintf("  %-20s %s", a.Actor, a.Status)
			if a.LastHeartbeat != "" {
				if t, err := time.Parse(time.RFC3339, a.LastHeartbeat); err == nil {
					ago := time.Since(t).Truncate(time.Second)
					line += fmt.Sprintf("  (heartbeat %s ago)", ago)
				}
			}
			if a.CurrentIssue != "" {
				line += fmt.Sprintf("  issue: %s", a.CurrentIssue)
			}
			sb.WriteString(line + "\n")
		}
	}

	return sb.String()
}

// --- Sunset convenience functions ---

// IsEventsLocalEnabled returns true if local event file writing is enabled.
func IsEventsLocalEnabled() bool {
	return envBool("GT_EVENTS_LOCAL", true)
}

// IsFeedCuratorEnabled returns true if the feed curator daemon should run.
func IsFeedCuratorEnabled() bool {
	return envBool("GT_FEED_CURATOR", true)
}

// IsConvoyLocalEnabled returns true if convoy checks should use local bd dep list.
func IsConvoyLocalEnabled() bool {
	return envBool("GT_CONVOY_LOCAL", true)
}

// IsMailLocalEnabled returns true if beads-native mail routing is enabled.
func IsMailLocalEnabled() bool {
	return envBool("GT_MAIL_LOCAL", true)
}

// IsNudgeLocalEnabled returns true if tmux nudge (local) is enabled.
func IsNudgeLocalEnabled() bool {
	return envBool("GT_NUDGE_LOCAL", true)
}

// --- Helpers ---

func sunsetLabel(localEnabled bool) string {
	if localEnabled {
		return "ON  (dual-write)"
	}
	return "OFF (Nostr-only)"
}

func envBool(key string, defaultVal bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	default:
		return defaultVal
	}
}
