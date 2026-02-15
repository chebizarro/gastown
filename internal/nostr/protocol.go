package nostr

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"fiatjaf.com/nostr"
)

// Protocol event message types.
const (
	MsgPolecatDone   = "POLECAT_DONE"
	MsgMergeReady    = "MERGE_READY"
	MsgMerged        = "MERGED"
	MsgMergeFailed   = "MERGE_FAILED"
	MsgReworkRequest = "REWORK_REQUEST"
	MsgHelp          = "HELP"
)

// ProtocolContent is the content body for kind 30320 protocol events.
type ProtocolContent struct {
	Schema  string                 `json:"schema"` // "gt/protocol@1"
	MsgType string                 `json:"msg_type"`
	From    string                 `json:"from"`
	To      string                 `json:"to"`
	Body    map[string]interface{} `json:"body"`
}

// PublishProtocolEvent publishes a kind 30320 protocol event.
// Protocol events are machine-to-machine commands (not chat messages).
func PublishProtocolEvent(ctx context.Context, publisher *Publisher, msgType, from, to, rig string, body map[string]interface{}, correlations *Correlations) error {
	content := ProtocolContent{
		Schema:  SchemaVersion("protocol", 1),
		MsgType: msgType,
		From:    from,
		To:      to,
		Body:    body,
	}

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return err
	}

	tags := nostr.Tags{
		nostr.Tag{"gt", ProtocolVersion},
		nostr.Tag{"msg_type", msgType},
		nostr.Tag{"from", from},
		nostr.Tag{"to", to},
		nostr.Tag{"rig", rig},
	}

	// Add correlation tags
	if correlations != nil {
		if correlations.IssueID != "" {
			tags = append(tags, nostr.Tag{"t", correlations.IssueID})
		}
		if correlations.Branch != "" {
			tags = append(tags, nostr.Tag{"branch", correlations.Branch})
		}
		if correlations.MergeReq != "" {
			tags = append(tags, nostr.Tag{"mr", correlations.MergeReq})
		}
		if correlations.ConvoyID != "" {
			tags = append(tags, nostr.Tag{"convoy", correlations.ConvoyID})
		}
	}

	event := &nostr.Event{
		Kind:      KindProtocolEvent,
		Tags:      tags,
		Content:   string(contentJSON),
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
	}

	return publisher.Publish(ctx, event)
}

// Note: Correlations is defined in types.go as the canonical location
// for cross-reference data used across the nostr package.

// ProtocolEventRouter dispatches incoming protocol events to handlers.
type ProtocolEventRouter struct {
	handlers map[string]ProtocolHandler
}

// ProtocolHandler is a function that processes a protocol event.
type ProtocolHandler func(ctx context.Context, event *nostr.Event, content ProtocolContent) error

// NewProtocolEventRouter creates a new protocol event router.
func NewProtocolEventRouter() *ProtocolEventRouter {
	return &ProtocolEventRouter{
		handlers: make(map[string]ProtocolHandler),
	}
}

// Handle registers a handler for a specific message type.
func (r *ProtocolEventRouter) Handle(msgType string, handler ProtocolHandler) {
	r.handlers[msgType] = handler
}

// Dispatch processes a protocol event, calling the appropriate handler.
func (r *ProtocolEventRouter) Dispatch(ctx context.Context, event *nostr.Event) error {
	var content ProtocolContent
	if err := json.Unmarshal([]byte(event.Content), &content); err != nil {
		return err
	}

	handler, ok := r.handlers[content.MsgType]
	if !ok {
		log.Printf("[nostr/protocol] No handler for message type: %s", content.MsgType)
		return nil
	}

	return handler(ctx, event, content)
}

// SubscribeProtocol creates a Nostr subscription for protocol events addressed to a specific actor.
func SubscribeProtocol(ctx context.Context, pool *RelayPool, actor string) []*nostr.Subscription {
	filter := nostr.Filter{
		Kinds: []int{KindProtocolEvent},
		Tags:  nostr.TagMap{"to": []string{actor}},
	}
	return pool.Subscribe(ctx, []nostr.Filter{filter})
}
