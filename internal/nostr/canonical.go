package nostr

import (
	"encoding/json"
	"fmt"
	"time"

	"fiatjaf.com/nostr"
	cascadia "git.sharegap.net/cascadia/cascadia-go"
)

const (
	agentCapabilitySchema = "cascadia.agent.capability.v1"
	taskStateSchema       = "cascadia.task-state.v1"
	taskQueueSchema       = "cascadia.task-queue.v1"
	contextVMIntentSchema = "contextvm.intent.v1"
)

// Canonical event-kind aliases. Keep these wired to generated cascadia-go
// constants so Gas Town cannot drift back to retired custom kind literals.
const (
	KindStatus         = cascadia.NIP38_USER_STATUS
	KindHeartbeat      = cascadia.CAS_AGENT_HEARTBEAT
	KindCapability     = cascadia.CAS_AGENT_CAPABILITY
	KindTaskCollection = cascadia.NIP51_TASK_COLLECTION
	KindIntent         = cascadia.CAS_INTENT
	KindTaskState      = cascadia.CAS_CP_STATE
)

// NewAgentCapabilityEvent creates a canonical 30317 capability descriptor.
func NewAgentCapabilityEvent(agentID, rig, role, capability string, details interface{}) (*nostr.Event, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent ID is required")
	}
	if role == "" {
		return nil, fmt.Errorf("agent role is required")
	}
	if capability == "" {
		return nil, fmt.Errorf("capability is required")
	}

	content, err := json.Marshal(map[string]interface{}{
		"schema":     agentCapabilitySchema,
		"runtime":    "gastown",
		"capability": capability,
		"details":    details,
	})
	if err != nil {
		return nil, err
	}

	tags := BaseTags(rig, role, "")
	tags = append(tags,
		ReplaceableTag("agent:"+agentID+":cap:"+capability),
		nostr.Tag{cascadia.TagAgent, role},
		nostr.Tag{cascadia.TagCap, capability},
		nostr.Tag{cascadia.TagRuntime, "gastown"},
		nostr.Tag{cascadia.TagSchema, agentCapabilitySchema},
	)

	return &nostr.Event{
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      nostr.Kind(cascadia.CAS_AGENT_CAPABILITY),
		Tags:      tags,
		Content:   string(content),
	}, nil
}

// NewTaskStateEvent creates a canonical 30900 task:* state projection for
// beads/nostrig task state.
func NewTaskStateEvent(taskID string, state interface{}) (*nostr.Event, error) {
	if taskID == "" {
		return nil, fmt.Errorf("task ID is required")
	}
	content, err := json.Marshal(map[string]interface{}{
		"schema": taskStateSchema,
		"task":   taskID,
		"state":  state,
	})
	if err != nil {
		return nil, err
	}

	tags := nostr.Tags{
		ReplaceableTag(TaskDTag(taskID)),
		nostr.Tag{cascadia.TagDomain, "task"},
		nostr.Tag{cascadia.TagSchema, taskStateSchema},
	}

	return &nostr.Event{
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      nostr.Kind(cascadia.CAS_CP_STATE),
		Tags:      tags,
		Content:   string(content),
	}, nil
}

// NewTaskQueueEvent creates a canonical NIP-51 collection for a queue or epic.
// NIP-29 group identity can be linked with the optional h tag.
func NewTaskQueueEvent(queueID string, taskIDs []string, groupID string) (*nostr.Event, error) {
	if queueID == "" {
		return nil, fmt.Errorf("queue ID is required")
	}
	content, err := json.Marshal(map[string]interface{}{
		"schema": taskQueueSchema,
		"queue":  queueID,
	})
	if err != nil {
		return nil, err
	}

	tags := nostr.Tags{ReplaceableTag(QueueDTag(queueID))}
	if groupID != "" {
		tags = append(tags, nostr.Tag{"h", groupID})
	}
	for _, taskID := range taskIDs {
		if taskID == "" {
			continue
		}
		tags = append(tags, nostr.Tag{cascadia.TagA, fmt.Sprintf("%d:%s:%s", cascadia.CAS_CP_STATE, "", TaskDTag(taskID))})
	}

	return &nostr.Event{
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      nostr.Kind(cascadia.NIP51_TASK_COLLECTION),
		Tags:      tags,
		Content:   string(content),
	}, nil
}

// NewContextVMIntentEvent creates a canonical 25910 JSON-RPC intent envelope.
func NewContextVMIntentEvent(recipientPubkey, method string, params interface{}, id interface{}) (*nostr.Event, error) {
	if recipientPubkey == "" {
		return nil, fmt.Errorf("recipient pubkey is required")
	}
	if method == "" {
		return nil, fmt.Errorf("method is required")
	}
	content, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}

	return &nostr.Event{
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      nostr.Kind(cascadia.CAS_INTENT),
		Tags: nostr.Tags{
			nostr.Tag{"p", recipientPubkey},
			nostr.Tag{cascadia.TagMethod, method},
			nostr.Tag{cascadia.TagSchema, contextVMIntentSchema},
		},
		Content: string(content),
	}, nil
}

func TaskDTag(taskID string) string {
	return "task:" + taskID
}

func QueueDTag(queueID string) string {
	return "queue:" + queueID
}
