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

	"fiatjaf.com/nostr"
	cascadia "git.sharegap.net/cascadia/cascadia-go"
)

// Standard Nostr kinds used by the retained identity subsystem.
const (
	KindProfile   = 0     // NIP-01: Agent profile metadata
	KindRelayList = 10002 // NIP-65: Agent relay list
)

// ProtocolVersion is included as ["gt", "1"] on Gas Town events.
const ProtocolVersion = "1"

// SchemaPrefix is prepended to Gas Town schema identifiers.
const SchemaPrefix = "gt/"

// Correlations holds cross-reference data for Nostr event tags.
type Correlations struct {
	IssueID   string
	ConvoyID  string
	BeadID    string
	SessionID string
	Branch    string
	MergeReq  string
}

// BaseTags identifies the Gas Town protocol version and logical actor.
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
		tags = append(tags, nostr.Tag{cascadia.TagAgent, actor})
	}
	return tags
}

// CorrelationTags returns optional correlation tags for event tracing.
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
		tags = append(tags, nostr.Tag{cascadia.TagSession, sessionID})
	}
	return tags
}

// ReplaceableTag returns a NIP-33 d-tag for an addressable event.
func ReplaceableTag(d string) nostr.Tag {
	return nostr.Tag{"d", d}
}

// TypeTag returns the canonical Cascadia type discriminator.
func TypeTag(eventType string) nostr.Tag {
	return nostr.Tag{cascadia.TagType, eventType}
}

// VisibilityTag preserves Gas Town's local audit/feed visibility metadata.
func VisibilityTag(visibility string) nostr.Tag {
	return nostr.Tag{"visibility", visibility}
}

// SchemaVersion returns a schema identifier such as "gt/log@1".
func SchemaVersion(name string, version int) string {
	return SchemaPrefix + name + "@" + itoa(version)
}

// IDToString converts a nostr.ID to hexadecimal.
func IDToString(id nostr.ID) string {
	return id.Hex()
}

// PubKeyFromHexGT converts a hex string to a nostr.PubKey.
func PubKeyFromHexGT(hexStr string) nostr.PubKey {
	pk, err := nostr.PubKeyFromHex(hexStr)
	if err != nil {
		return nostr.PubKey{}
	}
	return pk
}

// PubKeyToString converts a nostr.PubKey to hexadecimal.
func PubKeyToString(pk nostr.PubKey) string {
	return pk.Hex()
}

// SigFromHex converts a hex string to a 64-byte signature.
func SigFromHex(hexStr string) [64]byte {
	var sig [64]byte
	b, err := hex.DecodeString(hexStr)
	if err != nil || len(b) != 64 {
		return sig
	}
	copy(sig[:], b)
	return sig
}

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
