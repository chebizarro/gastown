package nostr

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"fiatjaf.com/nostr"
)

// Channel types for Gas Town NIP-28 channels.
const (
	ChannelTypeTownOps       = "town-ops"
	ChannelTypeActivity      = "activity"
	ChannelTypeAlerts        = "alerts"
	ChannelTypeAnnouncements = "announcements"
	ChannelTypeRigDev        = "rig-dev"
	ChannelTypeRigMerge      = "rig-merge"
	ChannelTypeRigPatrol     = "rig-patrol"
)

// ChannelMetadata is the content of a kind 40 channel creation event.
type ChannelMetadata struct {
	Name    string `json:"name"`
	About   string `json:"about"`
	Picture string `json:"picture,omitempty"`
}

// TownChannels returns the channel definitions for town-level channels.
func TownChannels() []ChannelMetadata {
	return []ChannelMetadata{
		{
			Name:  "town-ops",
			About: "Cross-rig coordination and Mayor commands",
		},
		{
			Name:  "activity",
			About: "Real-time feed of agent activity events (bot-posted)",
		},
		{
			Name:  "alerts",
			About: "Urgent escalations, mass deaths, and stale agent alerts",
		},
		{
			Name:  "announcements",
			About: "Announcements from overseer and mayor",
		},
	}
}

// RigChannels returns the channel definitions for a specific rig.
func RigChannels(rigName string) []ChannelMetadata {
	return []ChannelMetadata{
		{
			Name:  rigName + "-dev",
			About: fmt.Sprintf("Development channel for the %s rig", rigName),
		},
		{
			Name:  rigName + "-merge",
			About: fmt.Sprintf("Merge queue status for the %s rig", rigName),
		},
		{
			Name:  rigName + "-patrol",
			About: fmt.Sprintf("Witness patrol summaries for the %s rig", rigName),
		},
	}
}

// CreateChannel creates a NIP-28 channel (kind 40 event).
// Returns the event ID which is used as the channel identifier.
func CreateChannel(ctx context.Context, publisher *Publisher, meta ChannelMetadata, rig, channelType string) (string, error) {
	contentJSON, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}

	tags := nostr.Tags{
		nostr.Tag{"gt", ProtocolVersion},
		nostr.Tag{"channel_type", channelType},
	}
	if rig != "" {
		tags = append(tags, nostr.Tag{"rig", rig})
	}

	event := &nostr.Event{
		Kind:      40, // NIP-28 Channel Creation
		Tags:      tags,
		Content:   string(contentJSON),
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
	}

	if err := publisher.Publish(ctx, event); err != nil {
		return "", err
	}

	return IDToString(event.ID), nil
}

// PostChannelMessage posts a message to a NIP-28 channel (kind 42 event).
func PostChannelMessage(ctx context.Context, publisher *Publisher, channelCreateEventID, relay, content string, mentions []string) error {
	tags := nostr.Tags{
		nostr.Tag{"e", channelCreateEventID, relay, "root"},
	}

	for _, pubkey := range mentions {
		tags = append(tags, nostr.Tag{"p", pubkey, relay})
	}

	event := &nostr.Event{
		Kind:      42, // NIP-28 Channel Message
		Tags:      tags,
		Content:   content,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
	}

	return publisher.Publish(ctx, event)
}

// ChannelsForRole returns the channel types an agent should auto-subscribe to.
func ChannelsForRole(role string) []string {
	switch role {
	case "mayor":
		return []string{ChannelTypeTownOps, ChannelTypeAlerts, ChannelTypeAnnouncements, ChannelTypeRigDev}
	case "deacon":
		return []string{ChannelTypeTownOps, ChannelTypeAlerts, ChannelTypeActivity, ChannelTypeRigPatrol}
	case "witness":
		return []string{ChannelTypeRigDev, ChannelTypeRigMerge, ChannelTypeRigPatrol, ChannelTypeAlerts}
	case "refinery":
		return []string{ChannelTypeRigDev, ChannelTypeRigMerge}
	case "polecat":
		return []string{ChannelTypeRigDev}
	case "crew":
		return []string{ChannelTypeRigDev, ChannelTypeAnnouncements}
	default:
		return []string{ChannelTypeRigDev}
	}
}

// CreateTownChannels creates all town-level NIP-28 channels.
// Called during `gt init`. Returns a map of channel type → event ID.
func CreateTownChannels(ctx context.Context, publisher *Publisher) (map[string]string, error) {
	channels := TownChannels()
	result := make(map[string]string, len(channels))

	for _, ch := range channels {
		channelType := ch.Name // e.g., "town-ops"
		eventID, err := CreateChannel(ctx, publisher, ch, "", channelType)
		if err != nil {
			return result, fmt.Errorf("creating channel %s: %w", ch.Name, err)
		}
		result[channelType] = eventID
	}

	return result, nil
}

// CreateRigChannels creates all per-rig NIP-28 channels.
// Called during `gt rig add`. Returns a map of channel type → event ID.
func CreateRigChannels(ctx context.Context, publisher *Publisher, rigName string) (map[string]string, error) {
	channels := RigChannels(rigName)
	result := make(map[string]string, len(channels))

	channelTypes := []string{ChannelTypeRigDev, ChannelTypeRigMerge, ChannelTypeRigPatrol}
	for i, ch := range channels {
		eventID, err := CreateChannel(ctx, publisher, ch, rigName, channelTypes[i])
		if err != nil {
			return result, fmt.Errorf("creating channel %s: %w", ch.Name, err)
		}
		result[channelTypes[i]] = eventID
	}

	return result, nil
}
