package config

import "testing"

func TestMergeNostrConfigUsesRigNIP29Override(t *testing.T) {
	townGroups := &NIP29Config{
		Enabled: true,
		Relays:  []string{"wss://town-groups.example"},
		Groups:  NIP29Groups{Progress: []string{"fleet-ops"}},
	}
	rigGroups := &NIP29Config{
		Enabled: true,
		Relays:  []string{"wss://rig-groups.example"},
		Groups:  NIP29Groups{Progress: []string{"gastown"}},
	}
	merged := mergeNostrConfig(
		&NostrConfig{NIP29: townGroups},
		&NostrConfig{NIP29: rigGroups},
	)
	if merged.NIP29 != rigGroups {
		t.Fatalf("NIP29 override = %#v, want rig config", merged.NIP29)
	}
}
