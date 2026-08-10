package nostr

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"fiatjaf.com/nostr"
	cascadia "git.sharegap.net/cascadia/cascadia-go"
)

// NewNIP29GroupMessage creates a NIP-29 group-addressed NIP-C7 chat message.
// Relay-generated 39000-series events are intentionally not used by clients.
func NewNIP29GroupMessage(groupID, messageType, rig, role, actor, content string) (*nostr.Event, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, fmt.Errorf("NIP-29 group ID is required")
	}
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("NIP-29 message content is required")
	}
	tags := BaseTags(rig, role, actor)
	tags = append(tags,
		nostr.Tag{"h", groupID},
		TypeTag(messageType),
	)
	return &nostr.Event{
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      nostr.KindSimpleGroupChatMessage,
		Tags:      tags,
		Content:   content,
	}, nil
}

const (
	agentStatusDTag      = "cascadia:agent"
	agentHeartbeatSchema = "cascadia.agent.heartbeat.v1"
)

// NewLogStatusEvent creates a canonical NIP-38 agent status event carrying a
// Gas Town activity update.
func NewLogStatusEvent(rig, role, actor, eventType, visibility string, payload interface{}) (*nostr.Event, error) {
	tags := BaseTags(rig, role, actor)
	tags = append(tags, ReplaceableTag(agentStatusDTag))
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
		Kind:      nostr.Kind(cascadia.NIP38_USER_STATUS),
		Tags:      tags,
		Content:   string(content),
	}, nil
}

// NewAgentHeartbeatEvent creates a canonical Cascadia agent heartbeat. The
// event is addressable at d=<agentID>, and its content uses the generated
// cascadia.agent.heartbeat.v1 payload type.
func NewAgentHeartbeatEvent(agentID, rig, role, status string) (*nostr.Event, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent ID is required")
	}
	if role == "" {
		return nil, fmt.Errorf("agent role is required")
	}
	if status == "" {
		return nil, fmt.Errorf("agent status is required")
	}

	tags := BaseTags(rig, "", "")
	tags = append(tags,
		ReplaceableTag(agentID),
		nostr.Tag{cascadia.TagStatus, status},
		nostr.Tag{cascadia.TagAgent, role},
		nostr.Tag{cascadia.TagRuntime, "gastown"},
		nostr.Tag{cascadia.TagSchema, agentHeartbeatSchema},
	)

	activeTasks := 0
	if status == "working" || status == "busy" {
		activeTasks = 1
	}
	payload := cascadia.CascadiaAgentHeartbeatV1Payload{ActiveTasks: activeTasks}
	if err := payload.Validate(); err != nil {
		return nil, err
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return &nostr.Event{
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      nostr.Kind(cascadia.CAS_AGENT_HEARTBEAT),
		Tags:      tags,
		Content:   string(content),
	}, nil
}

// WithCorrelation appends correlation tags to an event.
func WithCorrelation(event *nostr.Event, issueID, convoyID, beadID, sessionID string) {
	event.Tags = append(event.Tags, CorrelationTags(issueID, convoyID, beadID, sessionID)...)
}

// WithCanonicalReferences adds optional Nostr event/address references supplied
// by the task projection. Logical task IDs remain in t/bead correlation tags.
func WithCanonicalReferences(event *nostr.Event, eventID, eventAddress string) {
	eventID = strings.TrimSpace(eventID)
	eventAddress = strings.TrimSpace(eventAddress)
	if len(eventID) == 64 {
		event.Tags = append(event.Tags, nostr.Tag{"e", eventID})
	}
	if strings.Count(eventAddress, ":") >= 2 {
		event.Tags = append(event.Tags, nostr.Tag{"a", eventAddress})
	}
}
