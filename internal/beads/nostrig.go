package beads

import (
	"bytes"
	"context"
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

const nostrigCommandTimeout = 60 * time.Second

var nostrigSyncLocks sync.Map

// nostrigEnabled reports whether this Beads wrapper should use nostrig as the
// relay-backed task ledger. It is intentionally opt-in so existing local Beads
// stores keep their current semantics until a town/rig exports the nostrig
// configuration.
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

	lockIface, _ := nostrigSyncLocks.LoadOrStore(beadsDir, &sync.Mutex{})
	lock := lockIface.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	err = b.runNostrig(args...)
	return err
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

func (b *Beads) publishNostrigUpdateIfEnabled(id string, opts UpdateOptions) error {
	if !b.nostrigEnabled() || b.store != nil {
		return nil
	}
	if strings.TrimSpace(id) == "" {
		return nil
	}
	args := nostrigRelayArgs([]string{"update", "--task-id", id})
	recipient := strings.TrimSpace(os.Getenv("GT_NOSTRIG_RECIPIENT"))
	if recipient == "" {
		return fmt.Errorf("GT_NOSTRIG_ENABLE requires GT_NOSTRIG_RECIPIENT to publish task/update")
	}
	args = append(args, "--recipient", recipient)
	if opts.Status != nil && strings.TrimSpace(*opts.Status) != "" {
		args = append(args, "--status", strings.TrimSpace(*opts.Status))
	}
	if opts.Assignee != nil {
		args = append(args, "--assignee", strings.TrimSpace(*opts.Assignee))
	}
	if opts.Title != nil && strings.TrimSpace(*opts.Title) != "" {
		args = append(args, "--title", strings.TrimSpace(*opts.Title))
	}
	if opts.Description != nil && strings.TrimSpace(*opts.Description) != "" {
		args = append(args, "--description", strings.TrimSpace(*opts.Description))
	}
	if len(args) == 0 || !nostrigUpdateHasField(args) {
		return nil
	}
	err := b.runNostrig(args...)
	return err
}

func (b *Beads) publishNostrigStatusIfEnabled(id, status string) error {
	status = strings.TrimSpace(status)
	if status == "" {
		return nil
	}
	return b.publishNostrigUpdateIfEnabled(id, UpdateOptions{Status: &status})
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
		case "--status", "--assignee", "--title", "--description":
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
