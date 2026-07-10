package beads

import (
	"reflect"
	"testing"
)

func TestNostrigSyncSelectorArgsRepoAddr(t *testing.T) {
	t.Setenv("GT_NOSTRIG_REPO_ADDR", "30617:owner:repo")
	t.Setenv("GT_NOSTRIG_RELAYS", "wss://one.example,wss://two.example")
	t.Setenv("GT_NOSTRIG_AUTHORS", "alice bob")

	got, err := nostrigSyncSelectorArgs()
	if err != nil {
		t.Fatalf("nostrigSyncSelectorArgs returned error: %v", err)
	}
	want := []string{"--relay", "wss://one.example", "--relay", "wss://two.example", "--repo-addr", "30617:owner:repo", "--author", "alice", "--author", "bob"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestNostrigSyncSelectorArgsRepoIDOwner(t *testing.T) {
	t.Setenv("GT_NOSTRIG_REPO_ID", "repo")
	t.Setenv("GT_NOSTRIG_OWNER", "owner")
	t.Setenv("GT_NOSTRIG_RELAY", "wss://relay.example")

	got, err := nostrigSyncSelectorArgs()
	if err != nil {
		t.Fatalf("nostrigSyncSelectorArgs returned error: %v", err)
	}
	want := []string{"--relay", "wss://relay.example", "--repo-id", "repo", "--owner", "owner"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestNostrigSyncSelectorArgsRequiresBoundedSelector(t *testing.T) {
	if _, err := nostrigSyncSelectorArgs(); err == nil {
		t.Fatal("expected missing selector error")
	}
}

func TestNostrigUpdateHasField(t *testing.T) {
	if !nostrigUpdateHasField([]string{"update", "--task-id", "gt-1", "--status", "in_progress"}) {
		t.Fatal("expected status update to be detected")
	}
	if nostrigUpdateHasField([]string{"update", "--task-id", "gt-1", "--recipient", "pubkey"}) {
		t.Fatal("expected no update field")
	}
}
