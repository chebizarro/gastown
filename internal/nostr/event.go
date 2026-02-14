package nostr

import (
	"encoding/json"
	"time"

	"fiatjaf.com/nostr"
)

// --- Event Construction Helpers ---

// NewLogStatusEvent creates a 30315 LOG_STATUS event.
// This replaces .events.jsonl writes with Nostr events.
func NewLogStatusEvent(rig, role, actor, eventType, visibility string, payload interface{}) (*nostr.Event, error) {
	tags := BaseTags(rig, role, actor)
	tags = append(tags, TypeTag(eventType))
	tags = append(tags, VisibilityTag(visibility))

	content, err := json.Marshal(map[string]interface{}{
		"schema":  SchemaVersion("log", 1),
		"type":    eventType,
		"source":  "gt",
		"payload": payload,
	})
	if err != nil {
		return nil, err
	}

	return &nostr.Event{
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      KindLogStatus,
		Tags:      tags,
		Content:   string(content),
	}, nil
}

// NewLifecycleEvent creates a 30316 LIFECYCLE event.
// Used for agent register, heartbeat, retire, and dead signals.
//
// The "d" tag is set to the instance identifier for NIP-33 deduplication,
// meaning each agent's lifecycle state is a single replaceable event.
func NewLifecycleEvent(rig, role, actor, instanceID, action string, payload interface{}) (*nostr.Event, error) {
	tags := BaseTags(rig, role, actor)
	tags = append(tags, ReplaceableTag(instanceID))
	tags = append(tags, TypeTag(action))

	content, err := json.Marshal(map[string]interface{}{
		"schema": SchemaVersion("lifecycle", 1),
		"action": action,
		"data":   payload,
	})
	if err != nil {
		return nil, err
	}

	return &nostr.Event{
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      KindLifecycle,
		Tags:      tags,
		Content:   string(content),
	}, nil
}

// NewConvoyStateEvent creates a 30318 GT_CONVOY_STATE replaceable event.
func NewConvoyStateEvent(rig, role, actor, convoyID string, state interface{}) (*nostr.Event, error) {
	tags := BaseTags(rig, role, actor)
	tags = append(tags, ReplaceableTag(convoyID))

	content, err := json.Marshal(map[string]interface{}{
		"schema":    SchemaVersion("convoy", 1),
		"convoy_id": convoyID,
		"state":     state,
	})
	if err != nil {
		return nil, err
	}

	return &nostr.Event{
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      KindConvoyState,
		Tags:      tags,
		Content:   string(content),
	}, nil
}

// NewBeadsIssueStateEvent creates a 30319 GT_BEADS_ISSUE_STATE replaceable event.
// This mirrors a beads issue to Nostr for UI consumption.
func NewBeadsIssueStateEvent(rig, role, actor, issueID string, issueData interface{}) (*nostr.Event, error) {
	tags := BaseTags(rig, role, actor)
	tags = append(tags, ReplaceableTag(issueID))
	tags = append(tags, nostr.Tag{"t", issueID})

	content, err := json.Marshal(map[string]interface{}{
		"schema":   SchemaVersion("issue", 1),
		"issue_id": issueID,
		"data":     issueData,
	})
	if err != nil {
		return nil, err
	}

	return &nostr.Event{
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      KindBeadsIssueState,
		Tags:      tags,
		Content:   string(content),
	}, nil
}

// NewProtocolEvent creates a 30320 GT_PROTOCOL_EVENT (regular, not replaceable).
// Used for machine-to-machine signals: MERGE_READY, POLECAT_DONE, HANDOFF, etc.
func NewProtocolEvent(rig, role, actor, protocolType string, payload interface{}) (*nostr.Event, error) {
	tags := BaseTags(rig, role, actor)
	tags = append(tags, TypeTag(protocolType))

	content, err := json.Marshal(map[string]interface{}{
		"schema":  SchemaVersion("protocol", 1),
		"type":    protocolType,
		"payload": payload,
	})
	if err != nil {
		return nil, err
	}

	return &nostr.Event{
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      KindProtocolEvent,
		Tags:      tags,
		Content:   string(content),
	}, nil
}

// NewWorkItemEvent creates a 30325 GT_WORK_ITEM event.
// Used for queue work items that can be claimed by workers.
func NewWorkItemEvent(rig, role, actor, queueName string, workItem interface{}) (*nostr.Event, error) {
	tags := BaseTags(rig, role, actor)
	tags = append(tags, nostr.Tag{"queue", queueName})

	content, err := json.Marshal(map[string]interface{}{
		"schema": SchemaVersion("work_item", 1),
		"queue":  queueName,
		"item":   workItem,
	})
	if err != nil {
		return nil, err
	}

	return &nostr.Event{
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      KindWorkItem,
		Tags:      tags,
		Content:   string(content),
	}, nil
}

// WithCorrelation appends correlation tags to an event.
// This is a convenience for adding issue, convoy, bead, and session references.
func WithCorrelation(event *nostr.Event, issueID, convoyID, beadID, sessionID string) {
	event.Tags = append(event.Tags, CorrelationTags(issueID, convoyID, beadID, sessionID)...)
}