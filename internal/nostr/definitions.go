package nostr

import (
	"context"
	"encoding/json"
	"time"

	"fiatjaf.com/nostr"
)

// GroupDefContent is the content body for kind 30321 group definition events.
type GroupDefContent struct {
	Schema    string   `json:"schema"` // "gt/group_def@1"
	Name      string   `json:"name"`
	Status    string   `json:"status"` // active|archived
	Members   []string `json:"members"`
	CreatedBy string   `json:"created_by"`
	CreatedAt string   `json:"created_at"`
}

// QueueDefContent is the content body for kind 30322 queue definition events.
type QueueDefContent struct {
	Schema          string `json:"schema"` // "gt/queue_def@1"
	Name            string `json:"name"`
	Status          string `json:"status"` // active|paused|archived
	Scope           string `json:"scope"`  // rig|town
	MaxConcurrency  int    `json:"max_concurrency"`
	ProcessingOrder string `json:"processing_order"` // fifo|priority
}

// ChannelDefContent is the content body for kind 30323 channel definition events.
type ChannelDefContent struct {
	Schema      string           `json:"schema"` // "gt/channel_def@1"
	Name        string           `json:"name"`
	Status      string           `json:"status"` // active|archived
	Subscribers []string         `json:"subscribers"`
	Retention   *RetentionConfig `json:"retention,omitempty"`
}

// RetentionConfig controls how long channel messages are kept.
type RetentionConfig struct {
	Count int `json:"count,omitempty"` // Max messages to retain
	Days  int `json:"days,omitempty"`  // Max days to retain
}

// PublishGroupDef publishes a kind 30321 group definition event.
func PublishGroupDef(ctx context.Context, publisher *Publisher, name string, members []string, createdBy string) error {
	content := GroupDefContent{
		Schema:    SchemaVersion("group_def", 1),
		Name:      name,
		Status:    "active",
		Members:   members,
		CreatedBy: createdBy,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return err
	}

	tags := nostr.Tags{
		nostr.Tag{"gt", ProtocolVersion},
		ReplaceableTag(name),
		nostr.Tag{"status", "active"},
	}
	for _, member := range members {
		tags = append(tags, nostr.Tag{"member", member})
	}

	event := &nostr.Event{
		Kind:      KindGroupDef,
		Tags:      tags,
		Content:   string(contentJSON),
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
	}

	return publisher.PublishReplaceable(ctx, event)
}

// PublishQueueDef publishes a kind 30322 queue definition event.
func PublishQueueDef(ctx context.Context, publisher *Publisher, name, status, scope, rig string, maxConcurrency int) error {
	content := QueueDefContent{
		Schema:          SchemaVersion("queue_def", 1),
		Name:            name,
		Status:          status,
		Scope:           scope,
		MaxConcurrency:  maxConcurrency,
		ProcessingOrder: "fifo",
	}

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return err
	}

	tags := nostr.Tags{
		nostr.Tag{"gt", ProtocolVersion},
		ReplaceableTag(name),
		nostr.Tag{"status", status},
		nostr.Tag{"scope", scope},
		nostr.Tag{"rig", rig},
	}

	event := &nostr.Event{
		Kind:      KindQueueDef,
		Tags:      tags,
		Content:   string(contentJSON),
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
	}

	return publisher.PublishReplaceable(ctx, event)
}

// PublishChannelDef publishes a kind 30323 channel definition event.
func PublishChannelDef(ctx context.Context, publisher *Publisher, name string, subscribers []string, retention *RetentionConfig) error {
	content := ChannelDefContent{
		Schema:      SchemaVersion("channel_def", 1),
		Name:        name,
		Status:      "active",
		Subscribers: subscribers,
		Retention:   retention,
	}

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return err
	}

	tags := nostr.Tags{
		nostr.Tag{"gt", ProtocolVersion},
		ReplaceableTag(name),
		nostr.Tag{"status", "active"},
	}
	for _, sub := range subscribers {
		tags = append(tags, nostr.Tag{"subscriber", sub})
	}

	event := &nostr.Event{
		Kind:      KindChannelDef,
		Tags:      tags,
		Content:   string(contentJSON),
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
	}

	return publisher.PublishReplaceable(ctx, event)
}
