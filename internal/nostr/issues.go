package nostr

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"time"

	"fiatjaf.com/nostr"
)

// BeadsIssueContent is the content body for kind 30319 issue mirror events.
type BeadsIssueContent struct {
	Schema       string            `json:"schema"` // "gt/beads_issue_state@1"
	ID           string            `json:"id"`
	Title        string            `json:"title"`
	Status       string            `json:"status"`
	Priority     string            `json:"priority"`
	Type         string            `json:"type"`
	CreatedAt    string            `json:"created_at"`
	CreatedBy    string            `json:"created_by,omitempty"`
	UpdatedAt    string            `json:"updated_at"`
	Assignee     string            `json:"assignee,omitempty"`
	Labels       []string          `json:"labels"`
	Rig          string            `json:"rig"`
	Dependencies IssueDependencies `json:"dependencies"`
	Branch       string            `json:"branch,omitempty"`
	Molecule     *MoleculeState    `json:"molecule,omitempty"`
	Blobs        []BlobReference   `json:"blobs,omitempty"`
	Source       *IssueSource      `json:"source,omitempty"`
}

// IssueDependencies holds the dependency graph for an issue.
type IssueDependencies struct {
	BlockedBy []string `json:"blocked_by"`
	Blocks    []string `json:"blocks"`
	Children  []string `json:"children"`
	Parent    *string  `json:"parent"`
}

// MoleculeState tracks molecule (sub-task) progress.
type MoleculeState struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	WispCount      int    `json:"wisp_count"`
	WispsCompleted int    `json:"wisps_completed"`
}

// BlobReference points to a file stored on a Blossom server.
type BlobReference struct {
	Type   string `json:"type"`   // patch, diff, screenshot, log
	URL    string `json:"url"`    // Blossom URL
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
}

// IssueSource tracks the origin repo for an issue.
type IssueSource struct {
	Repo       string  `json:"repo"`
	NIP34Event *string `json:"nip34_event,omitempty"`
}

// PublishIssueMirror publishes a kind 30319 issue mirror event.
func PublishIssueMirror(ctx context.Context, publisher *Publisher, rig string, content BeadsIssueContent) error {
	if content.Schema == "" {
		content.Schema = SchemaVersion("beads_issue_state", 1)
	}
	if content.UpdatedAt == "" {
		content.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return err
	}

	tags := nostr.Tags{
		nostr.Tag{"gt", ProtocolVersion},
		ReplaceableTag(content.ID),
		nostr.Tag{"rig", rig},
		nostr.Tag{"status", content.Status},
		nostr.Tag{"type", content.Type},
		nostr.Tag{"priority", content.Priority},
	}

	if content.Assignee != "" {
		tags = append(tags, nostr.Tag{"assignee", content.Assignee})
	}
	if content.Dependencies.Parent != nil {
		tags = append(tags, nostr.Tag{"parent", *content.Dependencies.Parent})
	}
	for _, label := range content.Labels {
		tags = append(tags, nostr.Tag{"label", label})
	}

	event := &nostr.Event{
		Kind:      KindBeadsIssueState,
		Tags:      tags,
		Content:   string(contentJSON),
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
	}

	if err := publisher.PublishReplaceable(ctx, event); err != nil {
		log.Printf("[nostr/issues] Failed to publish issue mirror %s: %v", content.ID, err)
		return err
	}

	return nil
}

// HashIssueContent returns a deterministic hash of issue content for change detection.
// Used by the background poll daemon to detect changes.
func HashIssueContent(content BeadsIssueContent) string {
	// Normalize for deterministic hashing
	normalized := content
	normalized.UpdatedAt = "" // Exclude timestamp from hash

	// Sort labels for deterministic ordering
	if normalized.Labels != nil {
		sorted := make([]string, len(normalized.Labels))
		copy(sorted, normalized.Labels)
		sort.Strings(sorted)
		normalized.Labels = sorted
	}

	data, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}

	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash[:16]) // 32 hex chars is enough for change detection
}

// IssueSnapshot tracks the last-known state of each issue for change detection.
type IssueSnapshot struct {
	Hashes map[string]string // issueID -> content hash
}

// NewIssueSnapshot creates an empty snapshot.
func NewIssueSnapshot() *IssueSnapshot {
	return &IssueSnapshot{
		Hashes: make(map[string]string),
	}
}

// HasChanged checks if an issue's content has changed since the last snapshot.
func (s *IssueSnapshot) HasChanged(issueID string, content BeadsIssueContent) bool {
	newHash := HashIssueContent(content)
	oldHash, exists := s.Hashes[issueID]
	if !exists || oldHash != newHash {
		s.Hashes[issueID] = newHash
		return true
	}
	return false
}
