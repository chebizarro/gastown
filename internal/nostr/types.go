// Package nostr provides the Nostr publishing layer for Gas Town.
// All Nostr event construction, signing, relay management, and spooling
// flows through this package.
//
// Key abstractions:
//   - Publisher: high-level sign → broadcast → spool-on-failure API
//   - Signer: NIP-46 bunker signing interface
//   - RelayPool: connection management to read/write relays
//   - Spool: local event store for offline resilience
//   - IdentityManager: per-agent keypair provisioning
package nostr

import (
	"encoding/hex"
	"fmt"

	"fiatjaf.com/nostr"
)

// --- Event Kind Constants ---

// AI-Hub shared kinds (reused from compendium).
const (
	KindLogStatus = 30315 // Activity feed (replaces .events.jsonl)
	KindLifecycle = 30316 // Agent register/heartbeat/retire/dead
)

// Gas Town custom kinds.
const (
	KindConvoyState     = 30318 // Convoy definition and progress
	KindBeadsIssueState = 30319 // Beads issue mirror for UI
	KindProtocolEvent   = 30320 // Machine-to-machine protocol events
	KindGroupDef        = 30321 // Group membership definition
	KindQueueDef        = 30322 // Work queue definition and status
	KindChannelDef      = 30323 // Pub/sub channel definition
	KindWorkItem        = 30325 // Queue work items (claimable tasks)
)

// Standard Nostr kinds (reused as-is).
const (
	KindProfile        = 0     // NIP-01: Agent profile metadata
	KindChannelCreate  = 40    // NIP-28: Channel creation
	KindChannelMeta    = 41    // NIP-28: Channel metadata updates
	KindChannelMessage = 42    // NIP-28: Channel messages
	KindDirectMessage  = 14    // NIP-17: Private DMs
	KindGiftWrap       = 1059  // NIP-59: Gift wraps for DMs
	KindRelayList      = 10002 // NIP-65: Agent relay list
	KindDMRelayList    = 10050 // NIP-17: Agent DM relay preferences
)

// --- Protocol Constants ---

// ProtocolVersion is the current Gas Town Nostr protocol version.
// Included as ["gt", "1"] tag on every GT event.
const ProtocolVersion = "1"

// SchemaPrefix is prepended to all schema identifiers in event content.
const SchemaPrefix = "gt/"

// --- Visibility Constants ---

// Event visibility levels (matching existing events package).
const (
	VisibilityAudit = "audit" // Internal audit trail only
	VisibilityFeed  = "feed"  // Visible in activity feed
	VisibilityBoth  = "both"  // Both audit and feed
)

// --- Cross-reference Types ---

// Correlations holds cross-reference data for Nostr event tags.
// Used to link events to issues, convoys, beads, sessions, branches, and MRs.
type Correlations struct {
	IssueID   string
	ConvoyID  string
	BeadID    string
	SessionID string
	Branch    string
	MergeReq  string
}

// --- Tag Builder Functions ---

// BaseTags returns the base tags included on every Gas Town Nostr event.
// These identify the protocol version, source rig, agent role, and actor.
func BaseTags(rig, role, actor string) nostr.Tags {
	tags := nostr.Tags{
		{"gt", ProtocolVersion},
	}
	if rig != "" {
		tags = append(tags, nostr.Tag{"rig", rig})
	}
	if role != "" {
		tags = append(tags, nostr.Tag{"role", role})
	}
	if actor != "" {
		tags = append(tags, nostr.Tag{"actor", actor})
	}
	return tags
}

// CorrelationTags returns optional correlation tags for event tracing.
// Empty values are omitted.
func CorrelationTags(issueID, convoyID, beadID, sessionID string) nostr.Tags {
	var tags nostr.Tags
	if issueID != "" {
		tags = append(tags, nostr.Tag{"t", issueID})
	}
	if convoyID != "" {
		tags = append(tags, nostr.Tag{"convoy", convoyID})
	}
	if beadID != "" {
		tags = append(tags, nostr.Tag{"bead", beadID})
	}
	if sessionID != "" {
		tags = append(tags, nostr.Tag{"session", sessionID})
	}
	return tags
}

// ReplaceableTag returns a NIP-33 "d" tag for parameterized replaceable events.
func ReplaceableTag(d string) nostr.Tag {
	return nostr.Tag{"d", d}
}

// TypeTag returns a type discriminator tag for events within the same kind.
func TypeTag(eventType string) nostr.Tag {
	return nostr.Tag{"type", eventType}
}

// VisibilityTag returns a visibility tag controlling where the event appears.
func VisibilityTag(visibility string) nostr.Tag {
	return nostr.Tag{"visibility", visibility}
}

// SchemaVersion returns a schema identifier string like "gt/log@1".
func SchemaVersion(name string, version int) string {
	return SchemaPrefix + name + "@" + itoa(version)
}

// --- Type Conversion Helpers ---
// The fiatjaf.com/nostr library uses fixed-size byte array types for ID and PubKey
// (not string aliases). These helpers provide safe conversions.

// IDToString converts a nostr.ID (byte array) to its hex string representation.
func IDToString(id nostr.ID) string {
	return fmt.Sprintf("%x", id)
}

// PubKeyFromHex converts a hex string to a nostr.PubKey byte array.
// Returns a zero PubKey if the hex string is invalid or wrong length.
func PubKeyFromHex(hexStr string) nostr.PubKey {
	var pk nostr.PubKey
	b, err := hex.DecodeString(hexStr)
	if err != nil || len(b) != len(pk) {
		return pk // zero value
	}
	copy(pk[:], b)
	return pk
}

// PubKeyToString converts a nostr.PubKey (byte array) to its hex string representation.
func PubKeyToString(pk nostr.PubKey) string {
	return fmt.Sprintf("%x", pk)
}

// SigFromHex converts a hex string to a nostr Sig byte array ([64]byte).
// Returns a zero Sig if the hex string is invalid or wrong length.
func SigFromHex(hexStr string) [64]byte {
	var sig [64]byte
	b, err := hex.DecodeString(hexStr)
	if err != nil || len(b) != 64 {
		return sig // zero value
	}
	copy(sig[:], b)
	return sig
}

// KindSlice converts plain int values to a []nostr.Kind slice.
func KindSlice(kinds ...int) []nostr.Kind {
	result := make([]nostr.Kind, len(kinds))
	for i, k := range kinds {
		result[i] = nostr.Kind(k)
	}
	return result
}

// itoa is a simple int-to-string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}