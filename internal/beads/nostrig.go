package beads

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/steveyegge/gastown/internal/telemetry"
	"github.com/steveyegge/gastown/internal/util"
)

const (
	nostrigCommandTimeout = 60 * time.Second
	nostrigOutboxFilename = "nostrig-outbox.jsonl"
)

var nostrigOperationLocks sync.Map

type nostrigIntent struct {
	Args     []string  `json:"args"`
	QueuedAt time.Time `json:"queued_at"`
}

func (b *Beads) nostrigEnabled() bool {
	if b == nil || b.isolated {
		return false
	}
	return strings.TrimSpace(os.Getenv("GT_NOSTRIG_ENABLE")) != ""
}

func (b *Beads) syncNostrigLedgerIfEnabled() error {
	if !b.nostrigEnabled() || b.store != nil {
		return nil
	}
	selectorArgs, err := nostrigSyncSelectorArgs()
	if err != nil {
		return err
	}
	beadsDir := b.getResolvedBeadsDir()
	outDir := strings.TrimSpace(os.Getenv("GT_NOSTRIG_OUT"))
	if outDir == "" {
		outDir = filepath.Dir(beadsDir)
	}
	args := append([]string{"sync"}, selectorArgs...)
	args = append(args, "--out", outDir)
	if cache := strings.TrimSpace(os.Getenv("GT_NOSTRIG_CACHE")); cache != "" {
		args = append(args, "--cache", cache)
	}
	if limit := strings.TrimSpace(os.Getenv("GT_NOSTRIG_LIMIT")); limit != "" {
		args = append(args, "--limit", limit)
	}
	if strings.TrimSpace(os.Getenv("GT_NOSTRIG_FAIL_ON_CONFLICT")) != "" {
		args = append(args, "--fail-on-conflict")
	}
	lock := nostrigOperationLock(beadsDir)
	lock.Lock()
	defer lock.Unlock()
	release, err := acquireNostrigFileLock(b.nostrigLockPath())
	if err != nil {
		return fmt.Errorf("locking nostrig ledger: %w", err)
	}
	defer release()
	if err := b.drainNostrigOutboxLocked(); err != nil {
		return fmt.Errorf("draining nostrig outbox before sync: %w", err)
	}
	return b.runNostrig(args...)
}

func nostrigSyncSelectorArgs() ([]string, error) {
	args := nostrigRelayArgs(nil)
	if repoAddr := strings.TrimSpace(os.Getenv("GT_NOSTRIG_REPO_ADDR")); repoAddr != "" {
		args = append(args, "--repo-addr", repoAddr)
	} else {
		repoID := strings.TrimSpace(os.Getenv("GT_NOSTRIG_REPO_ID"))
		owner := strings.TrimSpace(os.Getenv("GT_NOSTRIG_OWNER"))
		if repoID != "" {
			args = append(args, "--repo-id", repoID)
		}
		if owner != "" {
			args = append(args, "--owner", owner)
		}
		if repoID == "" || owner == "" {
			return nil, fmt.Errorf("GT_NOSTRIG_ENABLE requires GT_NOSTRIG_REPO_ADDR or both GT_NOSTRIG_REPO_ID and GT_NOSTRIG_OWNER")
		}
	}
	for _, author := range splitNostrigList(os.Getenv("GT_NOSTRIG_AUTHORS")) {
		args = append(args, "--author", author)
	}
	return args, nil
}

func (b *Beads) publishNostrigCreateIfEnabled(issue *Issue) error {
	if !b.nostrigEnabled() || b.store != nil || issue == nil || issue.Ephemeral || strings.TrimSpace(issue.ID) == "" {
		return nil
	}
	args := b.nostrigMutationArgs("create", issue.ID)
	args = append(args, "--title", issue.Title)
	if repoAddr := strings.TrimSpace(os.Getenv("GT_NOSTRIG_REPO_ADDR")); repoAddr != "" {
		args = append(args, "--repo-addr", repoAddr)
	} else {
		if repoID := strings.TrimSpace(os.Getenv("GT_NOSTRIG_REPO_ID")); repoID != "" {
			args = append(args, "--repo-id", repoID)
		}
		if owner := strings.TrimSpace(os.Getenv("GT_NOSTRIG_OWNER")); owner != "" {
			args = append(args, "--owner", owner)
		}
	}
	if issue.Description != "" {
		args = append(args, "--description", issue.Description)
	}
	if issue.Status != "" {
		args = append(args, "--status", issue.Status)
	}
	if issue.Priority >= 0 && issue.Priority <= 4 {
		args = append(args, "--priority", fmt.Sprintf("P%d", issue.Priority))
	}
	if issue.Parent != "" {
		args = append(args, "--epic", issue.Parent)
	}
	if issue.Assignee != "" {
		args = append(args, "--assignee", issue.Assignee)
	}
	for _, label := range issue.Labels {
		args = append(args, "--label", label)
	}
	for _, dep := range issue.DependsOn {
		args = append(args, "--depends-on", dep)
	}
	return b.publishNostrigIntentLocked(args)
}

func (b *Beads) publishNostrigUpdateIfEnabled(id string, opts UpdateOptions) error {
	if !b.nostrigEnabled() || b.store != nil || strings.TrimSpace(id) == "" {
		return nil
	}
	args := b.nostrigMutationArgs("update", id)
	if opts.Status != nil && strings.TrimSpace(*opts.Status) != "" {
		args = append(args, "--status", strings.TrimSpace(*opts.Status))
	}
	if opts.Assignee != nil {
		args = append(args, "--assignee", strings.TrimSpace(*opts.Assignee))
	}
	if opts.Title != nil {
		args = append(args, "--title", strings.TrimSpace(*opts.Title))
	}
	if opts.Description != nil {
		args = append(args, "--description", *opts.Description)
	}
	if opts.Priority != nil {
		args = append(args, "--priority", fmt.Sprintf("P%d", *opts.Priority))
	}
	for _, label := range opts.SetLabels {
		args = append(args, "--set-label", label)
	}
	for _, label := range opts.AddLabels {
		args = append(args, "--add-label", label)
	}
	for _, label := range opts.RemoveLabels {
		args = append(args, "--remove-label", label)
	}
	if !nostrigUpdateHasField(args) {
		return nil
	}
	return b.publishNostrigIntentLocked(args)
}

func (b *Beads) publishNostrigDependencyIfEnabled(id, dependency string, add bool) error {
	if !b.nostrigEnabled() || b.store != nil || strings.TrimSpace(id) == "" || strings.TrimSpace(dependency) == "" {
		return nil
	}
	flag := "--add-dep"
	if !add {
		flag = "--remove-dep"
	}
	return b.publishNostrigIntentLocked(append(b.nostrigMutationArgs("update", id), flag, dependency))
}

func (b *Beads) publishNostrigDeleteIfEnabled(id string) error {
	if !b.nostrigEnabled() || b.store != nil || strings.TrimSpace(id) == "" {
		return nil
	}
	return b.publishNostrigIntentLocked(b.nostrigMutationArgs("delete", id))
}

func (b *Beads) publishNostrigStatusIfEnabled(id, status string) error {
	status = strings.TrimSpace(status)
	if status == "" {
		return nil
	}
	return b.publishNostrigUpdateIfEnabled(id, UpdateOptions{Status: &status})
}

func (b *Beads) nostrigMutationArgs(command, id string) []string {
	args := nostrigRelayArgs([]string{command, "--task-id", strings.TrimSpace(id)})
	if recipient := strings.TrimSpace(os.Getenv("GT_NOSTRIG_RECIPIENT")); recipient != "" {
		args = append(args, "--recipient", recipient)
	}
	return args
}

func nostrigRelayArgs(prefix []string) []string {
	args := append([]string{}, prefix...)
	for _, relay := range splitNostrigList(os.Getenv("GT_NOSTRIG_RELAYS")) {
		args = append(args, "--relay", relay)
	}
	if relay := strings.TrimSpace(os.Getenv("GT_NOSTRIG_RELAY")); relay != "" {
		args = append(args, "--relay", relay)
	}
	return args
}

func splitNostrigList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func nostrigUpdateHasField(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "--status", "--assignee", "--title", "--description", "--priority", "--set-label", "--add-label", "--remove-label", "--add-dep", "--remove-dep":
			return true
		}
	}
	return false
}

func nostrigOperationLock(beadsDir string) *sync.Mutex {
	lockIface, _ := nostrigOperationLocks.LoadOrStore(beadsDir, &sync.Mutex{})
	return lockIface.(*sync.Mutex)
}

func (b *Beads) reportNostrigPostMutationError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: local beads mutation succeeded but nostrig intent could not be queued: %v\n", err)
	}
}

func (b *Beads) publishNostrigIntentLocked(args []string) error {
	beadsDir := b.getResolvedBeadsDir()
	lock := nostrigOperationLock(beadsDir)
	lock.Lock()
	defer lock.Unlock()
	release, err := acquireNostrigFileLock(b.nostrigLockPath())
	if err != nil {
		return fmt.Errorf("locking nostrig outbox: %w", err)
	}
	defer release()
	if err := b.drainNostrigOutboxLocked(); err != nil {
		if queueErr := b.appendNostrigIntentLocked(args); queueErr != nil {
			return errors.Join(err, fmt.Errorf("queueing nostrig intent: %w", queueErr))
		}
		fmt.Fprintf(os.Stderr, "warning: nostrig outbox drain failed; queued %s: %v\n", strings.Join(args, " "), err)
		return nil
	}
	if err := b.runNostrigIntent(args); err != nil {
		if queueErr := b.appendNostrigIntentLocked(args); queueErr != nil {
			return errors.Join(err, fmt.Errorf("queueing nostrig intent: %w", queueErr))
		}
		fmt.Fprintf(os.Stderr, "warning: nostrig publish failed; queued %s: %v\n", strings.Join(args, " "), err)
	}
	return nil
}

func (b *Beads) appendNostrigIntentLocked(args []string) error {
	path := b.nostrigOutboxPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	encErr := json.NewEncoder(f).Encode(nostrigIntent{Args: append([]string(nil), args...), QueuedAt: time.Now().UTC()})
	syncErr := f.Sync()
	closeErr := f.Close()
	return errors.Join(encErr, syncErr, closeErr)
}

func (b *Beads) nostrigOutboxPath() string {
	// export-state is already a local-only runtime directory in .beads/.gitignore.
	return filepath.Join(b.getResolvedBeadsDir(), "export-state", nostrigOutboxFilename)
}

func (b *Beads) nostrigLockPath() string {
	return filepath.Join(b.getResolvedBeadsDir(), "export-state", "nostrig.lock")
}

func (b *Beads) drainNostrigOutboxLocked() error {
	path := b.nostrigOutboxPath()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var intents []nostrigIntent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var intent nostrigIntent
		if err := json.Unmarshal(scanner.Bytes(), &intent); err != nil {
			_ = f.Close()
			return fmt.Errorf("decode %s: %w", path, err)
		}
		if len(intent.Args) == 0 {
			_ = f.Close()
			return fmt.Errorf("decode %s: intent has no args", path)
		}
		intents = append(intents, intent)
	}
	scanErr := scanner.Err()
	closeErr := f.Close()
	if err := errors.Join(scanErr, closeErr); err != nil {
		return err
	}
	for i, intent := range intents {
		if err := b.runNostrigIntent(intent.Args); err != nil {
			if rewriteErr := rewriteNostrigOutbox(path, intents[i:]); rewriteErr != nil {
				return errors.Join(err, rewriteErr)
			}
			return err
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func rewriteNostrigOutbox(path string, intents []nostrigIntent) error {
	if len(intents) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".nostrig-outbox-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	enc := json.NewEncoder(tmp)
	for _, intent := range intents {
		if err := enc.Encode(intent); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (b *Beads) runNostrigIntent(args []string) error {
	effective := append([]string(nil), args...)
	if len(effective) > 0 && effective[0] != "sync" && !containsNostrigFlag(effective, "--recipient") {
		if recipient := strings.TrimSpace(os.Getenv("GT_NOSTRIG_RECIPIENT")); recipient != "" {
			effective = append(effective, "--recipient", recipient)
		}
	}
	return b.runNostrig(effective...)
}

func containsNostrigFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

func (b *Beads) runNostrig(args ...string) error {
	bin := strings.TrimSpace(os.Getenv("GT_NOSTRIG_BIN"))
	if bin == "" {
		bin = "nostrig"
	}
	ctx, cancel := context.WithTimeout(context.Background(), nostrigCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // G204: nostrig is a configured internal CLI
	util.SetDetachedProcessGroup(cmd)
	cmd.Dir = b.workDir
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, telemetryEnvForNostrig()...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if strings.TrimSpace(stderr.String()) != "" {
			return fmt.Errorf("nostrig %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
		}
		return fmt.Errorf("nostrig %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func telemetryEnvForNostrig() []string {
	return telemetry.OTELEnvForSubprocess()
}
