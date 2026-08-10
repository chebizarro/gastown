package nostr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"fiatjaf.com/nostr"
)

type spoolTestSigner struct{}

func (spoolTestSigner) Sign(context.Context, *nostr.Event) error { return nil }
func (spoolTestSigner) GetPublicKey() string                     { return "" }
func (spoolTestSigner) Close() error                             { return nil }

func TestPublishToRelaysSpoolsExactGroupTargets(t *testing.T) {
	dir := t.TempDir()
	publisher := &Publisher{
		signer: spoolTestSigner{},
		pool: &RelayPool{
			writeURLs:        []string{"wss://status.example", "wss://groups.example"},
			defaultWriteURLs: []string{"wss://status.example"},
		},
		spool: NewSpool(dir),
	}
	targets := []string{"wss://groups.example"}
	if err := publisher.PublishToRelays(context.Background(), &nostr.Event{Kind: nostr.KindSimpleGroupChatMessage}, targets); err != nil {
		t.Fatalf("PublishToRelays: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, SpoolFileName))
	if err != nil {
		t.Fatalf("read spool: %v", err)
	}
	var entry SpoolEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("unmarshal spool: %v", err)
	}
	if !reflect.DeepEqual(entry.SpoolMeta.TargetRelays, targets) {
		t.Fatalf("spool targets = %v, want %v", entry.SpoolMeta.TargetRelays, targets)
	}
}
