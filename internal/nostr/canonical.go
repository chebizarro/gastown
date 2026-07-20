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
func NewAgentCapabilityEvent(rig, role string, payload cascadia.CascadiaAgentCapabilityV1Payload) (*nostr.Event, error) {
	if payload.AgentId == "" {
		return nil, fmt.Errorf("agent ID is required")
	}
	if role == "" {
		return nil, fmt.Errorf("agent role is required")
	}
	if payload.Capability == "" {
		return nil, fmt.Errorf("capability is required")
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}

	content, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	tags := BaseTags(rig, role, "")
	tags = append(tags,
		ReplaceableTag("agent:"+payload.AgentId+":cap:"+payload.Capability),
		nostr.Tag{cascadia.TagAgent, payload.AgentId},
		nostr.Tag{cascadia.TagCap, payload.Capability},
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
func NewTaskStateEvent(payload cascadia.CascadiaTaskStateV1Payload) (*nostr.Event, error) {
	if payload.Id == "" {
		return nil, fmt.Errorf("task ID is required")
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	tags := nostr.Tags{
		ReplaceableTag(TaskDTag(payload.Id)),
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
// NIP-29 group identity can be linked with the optional h tag. taskAuthor is
// required because NIP-33 a-tags are kind:pubkey:d-tag coordinates.
func NewTaskQueueEvent(queueID, taskAuthor string, taskIDs []string, groupID string) (*nostr.Event, error) {
	if queueID == "" {
		return nil, fmt.Errorf("queue ID is required")
	}
	if taskAuthor == "" {
		return nil, fmt.Errorf("task author pubkey is required")
	}
	payload := cascadia.CascadiaTaskCollectionV1Payload{Id: queueID}
	if err := payload.Validate(); err != nil {
		return nil, err
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	tags := nostr.Tags{
		ReplaceableTag(QueueDTag(queueID)),
		nostr.Tag{cascadia.TagSchema, taskQueueSchema},
	}
	if groupID != "" {
		tags = append(tags, nostr.Tag{"h", groupID})
	}
	for _, taskID := range taskIDs {
		if taskID == "" {
			continue
		}
		tags = append(tags, nostr.Tag{cascadia.TagA, fmt.Sprintf("%d:%s:%s", cascadia.CAS_CP_STATE, taskAuthor, TaskDTag(taskID))})
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
