package nostr

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"fiatjaf.com/nostr"

	"github.com/steveyegge/gastown/internal/config"
)

// IdentityManager handles per-agent identity provisioning, profile publishing,
// standard relay-list publication, and the local identity registry.
type IdentityManager struct {
	cfg       *config.NostrConfig
	publisher *Publisher
	registry  *IdentityRegistry
}

// AgentIdentity holds the Nostr identity for a Gas Town agent.
type AgentIdentity struct {
	Actor     string               `json:"actor"`   // Full actor address (e.g., "gastown/polecats/Toast")
	Role      string               `json:"role"`    // Agent role
	Rig       string               `json:"rig"`     // Rig name
	Pubkey    string               `json:"pubkey"`  // Hex public key
	BunkerURI string               `json:"bunker"`  // NIP-46 bunker connection string
	Profile   *config.AgentProfile `json:"profile"` // Profile metadata
	CreatedAt time.Time            `json:"created_at"`
}

// NewIdentityManager creates a new identity manager.
func NewIdentityManager(cfg *config.NostrConfig, publisher *Publisher) *IdentityManager {
	return &IdentityManager{
		cfg:       cfg,
		publisher: publisher,
		registry:  NewIdentityRegistry(),
	}
}

// ProvisionAgent creates and registers a Nostr identity for a new agent.
// The agent's keypair is managed by the NIP-46 bunker configured for the role.
//
// Flow:
//  1. Look up signer config for the role
//  2. Connect to bunker and get public key
//  3. Create agent identity record
//  4. Register in identity registry
func (im *IdentityManager) ProvisionAgent(ctx context.Context, actor, role, rig string) (*AgentIdentity, error) {
	// Find the identity config for this role
	roleIdentity, ok := im.cfg.Identities[role]
	if !ok {
		// Fall back to "default" identity
		roleIdentity, ok = im.cfg.Identities["default"]
		if !ok {
			return nil, fmt.Errorf("no identity configured for role %q and no default identity", role)
		}
	}

	agent := &AgentIdentity{
		Actor:     actor,
		Role:      role,
		Rig:       rig,
		Pubkey:    roleIdentity.Pubkey,
		BunkerURI: roleIdentity.Signer.Bunker,
		Profile:   roleIdentity.Profile,
		CreatedAt: time.Now(),
	}

	// Register in registry
	if err := im.registry.Register(&RegisteredAgent{
		Pubkey:        agent.Pubkey,
		BunkerURI:     agent.BunkerURI,
		Status:        "active",
		ProvisionedAt: agent.CreatedAt,
		Actor:         actor,
		Role:          role,
		Rig:           rig,
	}); err != nil {
		return nil, fmt.Errorf("registering agent identity: %w", err)
	}

	return agent, nil
}

// PublishProfile publishes a kind 0 profile event for an agent.
func (im *IdentityManager) PublishProfile(ctx context.Context, agent *AgentIdentity) error {
	if agent.Profile == nil {
		return nil // No profile to publish
	}

	profileContent, err := json.Marshal(map[string]interface{}{
		"name":         agent.Profile.Name,
		"display_name": agent.Profile.DisplayName,
		"about":        agent.Profile.About,
		"picture":      agent.Profile.Picture,
		"bot":          agent.Profile.Bot,
	})
	if err != nil {
		return fmt.Errorf("marshaling profile: %w", err)
	}

	event := &nostr.Event{
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      KindProfile,
		Tags:      nostr.Tags{},
		Content:   string(profileContent),
	}

	return im.publisher.Publish(ctx, event)
}

// PublishRelayLists publishes the standard kind 10002 relay list.
func (im *IdentityManager) PublishRelayLists(ctx context.Context, _ *AgentIdentity) error {
	// Kind 10002: Relay list
	var relayTags nostr.Tags
	for _, url := range im.cfg.ReadRelays {
		relayTags = append(relayTags, nostr.Tag{"r", url, "read"})
	}
	for _, url := range im.cfg.WriteRelays {
		relayTags = append(relayTags, nostr.Tag{"r", url, "write"})
	}

	relayListEvent := &nostr.Event{
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      KindRelayList,
		Tags:      relayTags,
		Content:   "",
	}

	if err := im.publisher.Publish(ctx, relayListEvent); err != nil {
		return fmt.Errorf("publishing relay list: %w", err)
	}

	return nil
}

// RetireAgent marks an agent as retired in the retained local registry.
// Lifecycle publication is intentionally absent until the canonical heartbeat
// runtime is wired.
func (im *IdentityManager) RetireAgent(_ context.Context, actor string) error {
	agent, err := im.registry.Lookup(actor)
	if err != nil {
		return fmt.Errorf("looking up agent %q: %w", actor, err)
	}
	agent.Status = "retired"
	return nil
}

// Registry returns the identity registry for direct access.
func (im *IdentityManager) Registry() *IdentityRegistry {
	return im.registry
}
