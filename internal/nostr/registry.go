package nostr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// IdentityRegistry maps Gas Town actor addresses to Nostr pubkeys.
// It serves as the local source of truth for which pubkey belongs to which agent.
//
// Storage:
//   - Local: ~/gt/settings/identity-registry.json
//   - Nostr: published as kind 30316 with d=identity_registry
//   - Loadable from relay for Flotilla address resolution
type IdentityRegistry struct {
	mu     sync.RWMutex
	agents map[string]*RegisteredAgent // key: actor address
}

// RegisteredAgent represents a registered agent in the identity registry.
type RegisteredAgent struct {
	Pubkey        string    `json:"pubkey"`
	BunkerURI     string    `json:"bunker,omitempty"`
	Status        string    `json:"status"` // "active" or "retired"
	ProvisionedAt time.Time `json:"provisioned_at"`
	Actor         string    `json:"actor"`
	Role          string    `json:"role"`
	Rig           string    `json:"rig"`
}

// RegistryFileName is the filename for the local identity registry.
const RegistryFileName = "identity-registry.json"

// NewIdentityRegistry creates a new empty identity registry.
func NewIdentityRegistry() *IdentityRegistry {
	return &IdentityRegistry{
		agents: make(map[string]*RegisteredAgent),
	}
}

// Register adds or updates an agent in the registry.
func (r *IdentityRegistry) Register(agent *RegisteredAgent) error {
	if agent.Actor == "" {
		return fmt.Errorf("agent actor address cannot be empty")
	}
	if agent.Pubkey == "" {
		return fmt.Errorf("agent pubkey cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.agents[agent.Actor] = agent
	return nil
}

// Lookup finds an agent by their Gas Town actor address.
func (r *IdentityRegistry) Lookup(actor string) (*RegisteredAgent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agent, ok := r.agents[actor]
	if !ok {
		return nil, fmt.Errorf("agent %q not found in registry", actor)
	}
	return agent, nil
}

// LookupByPubkey finds an agent by their Nostr public key.
func (r *IdentityRegistry) LookupByPubkey(pubkey string) (*RegisteredAgent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, agent := range r.agents {
		if agent.Pubkey == pubkey {
			return agent, nil
		}
	}
	return nil, fmt.Errorf("no agent found with pubkey %q", pubkey)
}

// ActiveAgents returns all agents with status "active".
func (r *IdentityRegistry) ActiveAgents() []*RegisteredAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var active []*RegisteredAgent
	for _, agent := range r.agents {
		if agent.Status == "active" {
			active = append(active, agent)
		}
	}
	return active
}

// All returns all agents in the registry.
func (r *IdentityRegistry) All() []*RegisteredAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	all := make([]*RegisteredAgent, 0, len(r.agents))
	for _, agent := range r.agents {
		all = append(all, agent)
	}
	return all
}

// SaveToFile persists the registry to a JSON file.
func (r *IdentityRegistry) SaveToFile(path string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	data, err := json.MarshalIndent(r.agents, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling registry: %w", err)
	}

	// 0600: registry contains bunker URIs
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing registry: %w", err)
	}

	return nil
}

// LoadFromFile loads the registry from a JSON file.
func (r *IdentityRegistry) LoadFromFile(path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is constructed internally
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No registry yet
		}
		return fmt.Errorf("reading registry: %w", err)
	}

	agents := make(map[string]*RegisteredAgent)
	if err := json.Unmarshal(data, &agents); err != nil {
		return fmt.Errorf("parsing registry: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents = agents

	return nil
}

// RegistryPath returns the standard path for the identity registry.
func RegistryPath(townRoot string) string {
	return filepath.Join(townRoot, "settings", RegistryFileName)
}

// ToJSON returns the registry as a JSON byte slice.
// Useful for publishing as Nostr event content.
func (r *IdentityRegistry) ToJSON() ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return json.Marshal(map[string]interface{}{
		"schema": SchemaVersion("identity_registry", 1),
		"agents": r.agents,
	})
}