package nostr

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"fiatjaf.com/nostr"
)

// ConvoyStateContent is the content body for kind 30318 convoy state events.
type ConvoyStateContent struct {
	Schema        string               `json:"schema"` // "gt/convoy_state@1"
	ID            string               `json:"id"`
	Title         string               `json:"title"`
	Status        string               `json:"status"` // open|landed|cancelled
	CreatedAt     string               `json:"created_at"`
	CreatedBy     string               `json:"created_by"`
	TrackedIssues []ConvoyTrackedIssue `json:"tracked_issues"`
	Summary       ConvoySummary        `json:"summary"`
	ActiveWorkers []string             `json:"active_workers"`
	Landed        bool                 `json:"landed"`
	LandedAt      *string              `json:"landed_at,omitempty"`
	LastUpdated   string               `json:"last_updated"`
}

// ConvoyTrackedIssue represents an issue tracked by a convoy.
type ConvoyTrackedIssue struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Assignee string `json:"assignee,omitempty"`
	Rig      string `json:"rig,omitempty"`
}

// ConvoySummary provides aggregate counts for a convoy.
type ConvoySummary struct {
	Total   int `json:"total"`
	Open    int `json:"open"`
	Closed  int `json:"closed"`
	Blocked int `json:"blocked"`
}

// PublishConvoyState publishes a kind 30318 convoy state event.
func PublishConvoyState(ctx context.Context, publisher *Publisher, convoyID string, content ConvoyStateContent) error {
	if content.Schema == "" {
		content.Schema = SchemaVersion("convoy_state", 1)
	}
	if content.LastUpdated == "" {
		content.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	}

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return err
	}

	tags := nostr.Tags{
		nostr.Tag{"gt", ProtocolVersion},
		ReplaceableTag(convoyID),
		nostr.Tag{"status", content.Status},
	}

	// Add issue tags for filtering
	for _, issue := range content.TrackedIssues {
		tags = append(tags, nostr.Tag{"t", issue.ID})
	}

	// Add worker notification tags
	for _, worker := range content.ActiveWorkers {
		tags = append(tags, nostr.Tag{"notify", worker})
	}

	event := &nostr.Event{
		Kind:      KindConvoyState,
		Tags:      tags,
		Content:   string(contentJSON),
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
	}

	if err := publisher.PublishReplaceable(ctx, event); err != nil {
		log.Printf("[nostr/convoy] Failed to publish convoy state %s: %v", convoyID, err)
		return err
	}

	return nil
}

// ConvoyStateFromIssues builds a ConvoySummary from tracked issues.
func ConvoyStateFromIssues(issues []ConvoyTrackedIssue) ConvoySummary {
	s := ConvoySummary{Total: len(issues)}
	for _, issue := range issues {
		switch issue.Status {
		case "open", "in-progress":
			s.Open++
		case "closed":
			s.Closed++
		case "blocked":
			s.Blocked++
		default:
			s.Open++
		}
	}
	return s
}
