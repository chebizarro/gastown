package beads

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
	want := "--relay wss://one.example --relay wss://two.example --repo-addr 30617:owner:repo --author alice --author bob"
	if strings.Join(got, " ") != want {
		t.Fatalf("args mismatch\n got: %q\nwant: %q", strings.Join(got, " "), want)
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
	want := "--relay wss://relay.example --repo-id repo --owner owner"
	if strings.Join(got, " ") != want {
		t.Fatalf("args mismatch\n got: %q\nwant: %q", strings.Join(got, " "), want)
	}
}

func TestNostrigSyncSelectorArgsRequiresBoundedSelector(t *testing.T) {
	if _, err := nostrigSyncSelectorArgs(); err == nil {
		t.Fatal("expected missing selector error")
	}
}

func TestNostrigUpdateHasField(t *testing.T) {
	for _, flag := range []string{"--status", "--priority", "--set-label", "--add-dep"} {
		if !nostrigUpdateHasField([]string{"update", "--task-id", "gt-1", flag, "value"}) {
			t.Fatalf("expected %s update to be detected", flag)
		}
	}
	if nostrigUpdateHasField([]string{"update", "--task-id", "gt-1", "--recipient", "pubkey"}) {
		t.Fatal("expected no update field")
	}
}

func TestNostrigEnableGating(t *testing.T) {
	h := installNostrigE2EStubs(t)
	t.Setenv("GT_NOSTRIG_ENABLE", "")
	priority := 1
	if err := h.b.Update("gt-1", UpdateOptions{Priority: &priority}); err != nil {
		t.Fatal(err)
	}
	if got := readOptionalFile(t, h.nostrigLog); got != "" {
		t.Fatalf("nostrig ran while disabled:\n%s", got)
	}
}

func TestNostrigMutationHooksArgvAndEnv(t *testing.T) {
	h := installNostrigE2EStubs(t)
	if _, err := h.b.Create(CreateOptions{Title: "ignored by stub", Priority: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.b.CreateWithID("gt-custom", CreateOptions{Title: "custom"}); err != nil {
		t.Fatal(err)
	}
	priority := 0
	assignee := "agent-a"
	if err := h.b.Update("gt-created", UpdateOptions{Priority: &priority, Assignee: &assignee, SetLabels: []string{"urgent"}, AddLabels: []string{"fleet"}, RemoveLabels: []string{"bug"}}); err != nil {
		t.Fatal(err)
	}
	if err := h.b.Close("gt-created"); err != nil {
		t.Fatal(err)
	}
	if err := h.b.CloseWithReason("done", "gt-created"); err != nil {
		t.Fatal(err)
	}
	if err := h.b.ForceCloseWithReason("forced", "gt-created"); err != nil {
		t.Fatal(err)
	}
	if err := h.b.ReleaseWithReason("gt-created", "retry"); err != nil {
		t.Fatal(err)
	}
	if err := h.b.AddDependency("gt-created", "gt-dep-2"); err != nil {
		t.Fatal(err)
	}
	if err := h.b.RemoveDependency("gt-created", "gt-dep-2"); err != nil {
		t.Fatal(err)
	}
	if err := h.b.DeleteQueueBead("gt-created"); err != nil {
		t.Fatal(err)
	}

	log := readOptionalFile(t, h.nostrigLog)
	for _, want := range []string{
		"ENV bunker=bunker://test",
		"ARGV [create] [--task-id] [gt-created]",
		"[--repo-addr] [30617:owner:repo]", "[--priority] [P1]", "[--epic] [epic-1]", "[--label] [bug]", "[--depends-on] [gt-dep]",
		"ARGV [create] [--task-id] [gt-custom]",
		"ARGV [update] [--task-id] [gt-created]", "[--priority] [P0]", "[--set-label] [urgent]", "[--add-label] [fleet]", "[--remove-label] [bug]",
		"[--status] [closed]", "[--status] [open] [--assignee] []",
		"[--add-dep] [gt-dep-2]", "[--remove-dep] [gt-dep-2]",
		"ARGV [delete] [--task-id] [gt-created]", "[--recipient] [recipient-pubkey]", "[--relay] [wss://relay.example]",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("nostrig log missing %q\nlog:\n%s", want, log)
		}
	}
}

func TestNostrigOutboxQueuesAndDrainsAfterRecipientConfigured(t *testing.T) {
	h := installNostrigE2EStubs(t)
	t.Setenv("GT_NOSTRIG_RECIPIENT", "")
	status := "in_progress"
	if err := h.b.Update("gt-1", UpdateOptions{Status: &status}); err != nil {
		t.Fatalf("local mutation returned publish failure: %v", err)
	}
	outbox := filepath.Join(h.workDir, ".beads", "export-state", nostrigOutboxFilename)
	if _, err := os.Stat(outbox); err != nil {
		t.Fatalf("failed publish was not spooled: %v", err)
	}

	t.Setenv("GT_NOSTRIG_RECIPIENT", "recipient-pubkey")
	if err := h.b.Close("gt-1"); err != nil {
		t.Fatalf("subsequent mutation failed while draining: %v", err)
	}
	if _, err := os.Stat(outbox); !os.IsNotExist(err) {
		t.Fatalf("outbox was not drained, stat err=%v", err)
	}
	log := readOptionalFile(t, h.nostrigLog)
	if strings.Count(log, "ARGV [update] [--task-id] [gt-1]") < 3 {
		t.Fatalf("expected failed publish, retry, and current publish:\n%s", log)
	}
	if !strings.Contains(log, "[--recipient] [recipient-pubkey]") {
		t.Fatalf("drained intent did not adopt configured recipient:\n%s", log)
	}
}

func TestSearchSyncsBeforeRead(t *testing.T) {
	h := installNostrigE2EStubs(t)
	if _, err := h.b.Search(SearchOptions{Query: "needle"}); err != nil {
		t.Fatal(err)
	}
	trace := strings.Split(strings.TrimSpace(readOptionalFile(t, h.traceLog)), "\n")
	var syncAt, searchAt = -1, -1
	for i, line := range trace {
		if strings.Contains(line, "nostrig:sync") {
			syncAt = i
		}
		if strings.Contains(line, "bd:search") {
			searchAt = i
		}
	}
	if syncAt < 0 || searchAt < 0 || syncAt >= searchAt {
		t.Fatalf("Search did not sync before bd read: %#v", trace)
	}
}

func TestConcurrentNostrigSyncUsesPerLedgerLock(t *testing.T) {
	h := installNostrigE2EStubs(t)
	activeDir := filepath.Join(t.TempDir(), "active")
	overlap := filepath.Join(t.TempDir(), "overlap")
	t.Setenv("NOSTRIG_ACTIVE_DIR", activeDir)
	t.Setenv("NOSTRIG_OVERLAP_FILE", overlap)

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- New(h.workDir).syncNostrigLedgerIfEnabled()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(overlap); !os.IsNotExist(err) {
		t.Fatalf("concurrent sync subprocesses overlapped")
	}
	if got := strings.Count(readOptionalFile(t, h.nostrigLog), "ARGV [sync]"); got != workers {
		t.Fatalf("sync count=%d, want %d", got, workers)
	}

	// Separate test-binary processes prove the file lock, not just the in-process mutex.
	t.Setenv("NOSTRIG_SYNC_HELPER", "1")
	t.Setenv("NOSTRIG_HELPER_WORKDIR", h.workDir)
	processErrs := make(chan error, workers)
	var processWG sync.WaitGroup
	for i := 0; i < workers; i++ {
		processWG.Add(1)
		go func() {
			defer processWG.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestNostrigSyncProcessHelper$")
			cmd.Env = os.Environ()
			if output, err := cmd.CombinedOutput(); err != nil {
				processErrs <- fmt.Errorf("sync helper failed: %w: %s", err, output)
				return
			}
			processErrs <- nil
		}()
	}
	processWG.Wait()
	close(processErrs)
	for err := range processErrs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(overlap); !os.IsNotExist(err) {
		t.Fatalf("cross-process sync subprocesses overlapped")
	}
	if got := strings.Count(readOptionalFile(t, h.nostrigLog), "ARGV [sync]"); got != workers*2 {
		t.Fatalf("combined sync count=%d, want %d", got, workers*2)
	}
}

func TestNostrigSyncProcessHelper(t *testing.T) {
	if os.Getenv("NOSTRIG_SYNC_HELPER") != "1" {
		return
	}
	if err := New(os.Getenv("NOSTRIG_HELPER_WORKDIR")).syncNostrigLedgerIfEnabled(); err != nil {
		t.Fatal(err)
	}
}

type nostrigE2EHarness struct {
	b          *Beads
	workDir    string
	nostrigLog string
	traceLog   string
}

func installNostrigE2EStubs(t *testing.T) nostrigE2EHarness {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PATH shim uses POSIX shell")
	}
	ResetBdAllowStaleCacheForTest()
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, ".beads"), 0755); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	bdLog := filepath.Join(t.TempDir(), "bd.log")
	nostrigLog := filepath.Join(t.TempDir(), "nostrig.log")
	traceLog := filepath.Join(t.TempDir(), "trace.log")

	bdScript := `#!/bin/sh
printf '%s\n' "$*" >> "$BD_LOG"
cmd=""
for arg in "$@"; do
  case "$arg" in --*) ;; *) cmd="$arg"; break ;; esac
done
printf 'bd:%s\n' "$cmd" >> "$TRACE_LOG"
case "$cmd" in
  create)
    id="gt-created"
    for arg in "$@"; do case "$arg" in --id=*) id="${arg#--id=}" ;; esac; done
    printf '{"id":"%s","title":"Created title","description":"Created body","status":"open","priority":1,"parent":"epic-1","labels":["bug","backend"],"depends_on":["gt-dep"]}\n' "$id"
    ;;
  search|list|ready) printf '[]\n' ;;
  show) printf '[{"id":"gt-created","title":"Created title","status":"open","priority":1}]\n' ;;
  --help) printf 'bd test shim\n' ;;
esac
exit 0
`
	nostrigScript := `#!/bin/sh
printf 'ENV bunker=%s\n' "$NOSTRIG_SIGNER_BUNKER_URL" >> "$NOSTRIG_LOG"
printf 'ARGV' >> "$NOSTRIG_LOG"
for arg in "$@"; do printf ' [%s]' "$arg" >> "$NOSTRIG_LOG"; done
printf '\n' >> "$NOSTRIG_LOG"
printf 'nostrig:%s\n' "$1" >> "$TRACE_LOG"
if [ "$1" = sync ] && [ -n "$NOSTRIG_ACTIVE_DIR" ]; then
  if ! mkdir "$NOSTRIG_ACTIVE_DIR" 2>/dev/null; then : > "$NOSTRIG_OVERLAP_FILE"; fi
  sleep 0.05
  rmdir "$NOSTRIG_ACTIVE_DIR" 2>/dev/null || true
fi
if [ "$1" != sync ]; then
  found=0
  prev=""
  for arg in "$@"; do
    if [ "$prev" = --recipient ] && [ -n "$arg" ]; then found=1; fi
    prev="$arg"
  done
  if [ "$found" -ne 1 ]; then echo '--recipient is required' >&2; exit 2; fi
fi
exit 0
`
	for name, script := range map[string]string{"bd": bdScript, "nostrig": nostrigScript} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0755); err != nil {
			t.Fatalf("write %s shim: %v", name, err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BD_LOG", bdLog)
	t.Setenv("NOSTRIG_LOG", nostrigLog)
	t.Setenv("TRACE_LOG", traceLog)
	t.Setenv("GT_NOSTRIG_ENABLE", "1")
	t.Setenv("GT_NOSTRIG_REPO_ADDR", "30617:owner:repo")
	t.Setenv("GT_NOSTRIG_RELAY", "wss://relay.example")
	t.Setenv("GT_NOSTRIG_RECIPIENT", "recipient-pubkey")
	t.Setenv("NOSTRIG_SIGNER_BUNKER_URL", "bunker://test")
	return nostrigE2EHarness{b: New(workDir), workDir: workDir, nostrigLog: nostrigLog, traceLog: traceLog}
}

func readOptionalFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(fmt.Errorf("read %s: %w", path, err))
	}
	return string(data)
}
