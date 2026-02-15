package nostr

import (
	"context"
	"encoding/json"
	"time"

	"fiatjaf.com/nostr"
)

// Work item status constants.
const (
	WorkStatusAvailable = "available"
	WorkStatusClaimed   = "claimed"
	WorkStatusCompleted = "completed"
	WorkStatusFailed    = "failed"
)

// WorkItemContent is the content body for kind 30325 work item events.
type WorkItemContent struct {
	Schema   string                 `json:"schema"` // "gt/work_item@1"
	Queue    string                 `json:"queue"`
	Subject  string                 `json:"subject"`
	Status   string                 `json:"status"`
	Body     map[string]interface{} `json:"body,omitempty"`
	Priority string                 `json:"priority,omitempty"`
}

// PublishWorkItem publishes a kind 30325 work item event.
func PublishWorkItem(ctx context.Context, publisher *Publisher, queue, from, rig, subject string, body map[string]interface{}) error {
	content := WorkItemContent{
		Schema:  SchemaVersion("work_item", 1),
		Queue:   queue,
		Subject: subject,
		Status:  WorkStatusAvailable,
		Body:    body,
	}

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return err
	}

	tags := nostr.Tags{
		nostr.Tag{"gt", ProtocolVersion},
		nostr.Tag{"queue", queue},
		nostr.Tag{"from", from},
		nostr.Tag{"status", WorkStatusAvailable},
		nostr.Tag{"rig", rig},
	}

	event := &nostr.Event{
		Kind:      KindWorkItem,
		Tags:      tags,
		Content:   string(contentJSON),
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
	}

	return publisher.Publish(ctx, event)
}

// ClaimWorkItem creates a new event claiming ownership of a work item.
func ClaimWorkItem(ctx context.Context, publisher *Publisher, original *nostr.Event, claimedBy string) error {
	var content WorkItemContent
	if err := json.Unmarshal([]byte(original.Content), &content); err != nil {
		return err
	}
	content.Status = WorkStatusClaimed

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return err
	}

	tags := cloneTags(original.Tags)
	tags = setTagValue(tags, "status", WorkStatusClaimed)
	tags = append(tags,
		nostr.Tag{"claimed_by", claimedBy},
		nostr.Tag{"e", string(original.ID)}, // Reference original
	)

	event := &nostr.Event{
		Kind:      KindWorkItem,
		Tags:      tags,
		Content:   string(contentJSON),
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
	}

	return publisher.Publish(ctx, event)
}

// CompleteWorkItem marks a work item as completed.
func CompleteWorkItem(ctx context.Context, publisher *Publisher, original *nostr.Event, completedBy string) error {
	var content WorkItemContent
	if err := json.Unmarshal([]byte(original.Content), &content); err != nil {
		return err
	}
	content.Status = WorkStatusCompleted

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return err
	}

	tags := cloneTags(original.Tags)
	tags = setTagValue(tags, "status", WorkStatusCompleted)
	tags = append(tags,
		nostr.Tag{"completed_by", completedBy},
		nostr.Tag{"e", string(original.ID)},
	)

	event := &nostr.Event{
		Kind:      KindWorkItem,
		Tags:      tags,
		Content:   string(contentJSON),
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
	}

	return publisher.Publish(ctx, event)
}

// FailWorkItem marks a work item as failed.
func FailWorkItem(ctx context.Context, publisher *Publisher, original *nostr.Event, failedBy, reason string) error {
	var content WorkItemContent
	if err := json.Unmarshal([]byte(original.Content), &content); err != nil {
		return err
	}
	content.Status = WorkStatusFailed

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return err
	}

	tags := cloneTags(original.Tags)
	tags = setTagValue(tags, "status", WorkStatusFailed)
	tags = append(tags,
		nostr.Tag{"failed_by", failedBy},
		nostr.Tag{"failure_reason", reason},
		nostr.Tag{"e", string(original.ID)},
	)

	event := &nostr.Event{
		Kind:      KindWorkItem,
		Tags:      tags,
		Content:   string(contentJSON),
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
	}

	return publisher.Publish(ctx, event)
}

// SubscribeWorkQueue creates a subscription for work items on a queue.
func SubscribeWorkQueue(pool *RelayPool, queueName string) []*nostr.Subscription {
	filter := nostr.Filter{
		Kinds: []int{KindWorkItem},
		Tags:  nostr.TagMap{"queue": []string{queueName}, "status": []string{WorkStatusAvailable}},
	}
	return pool.Subscribe(context.Background(), []nostr.Filter{filter})
}

// --- Tag helpers ---

func cloneTags(tags nostr.Tags) nostr.Tags {
	result := make(nostr.Tags, len(tags))
	for i, tag := range tags {
		cloned := make(nostr.Tag, len(tag))
		copy(cloned, tag)
		result[i] = cloned
	}
	return result
}

func setTagValue(tags nostr.Tags, key, value string) nostr.Tags {
	for i, tag := range tags {
		if len(tag) >= 2 && tag[0] == key {
			tags[i] = nostr.Tag{key, value}
			return tags
		}
	}
	return append(tags, nostr.Tag{key, value})
}
