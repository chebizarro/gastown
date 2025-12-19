// Package refinery provides the merge queue processing agent.
package refinery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/mail"
	"github.com/steveyegge/gastown/internal/rig"
)

// MergeQueueConfig holds configuration for the merge queue processor.
type MergeQueueConfig struct {
	// Enabled controls whether the merge queue is active.
	Enabled bool `json:"enabled"`

	// TargetBranch is the default branch to merge to (e.g., "main").
	TargetBranch string `json:"target_branch"`

	// IntegrationBranches enables per-epic integration branches.
	IntegrationBranches bool `json:"integration_branches"`

	// OnConflict is the strategy for handling conflicts: "assign_back" or "auto_rebase".
	OnConflict string `json:"on_conflict"`

	// RunTests controls whether to run tests before merging.
	RunTests bool `json:"run_tests"`

	// TestCommand is the command to run for testing.
	TestCommand string `json:"test_command"`

	// DeleteMergedBranches controls whether to delete branches after merge.
	DeleteMergedBranches bool `json:"delete_merged_branches"`

	// RetryFlakyTests is the number of times to retry flaky tests.
	RetryFlakyTests int `json:"retry_flaky_tests"`

	// PollInterval is how often to check for new MRs.
	PollInterval time.Duration `json:"poll_interval"`

	// MaxConcurrent is the maximum number of MRs to process concurrently.
	MaxConcurrent int `json:"max_concurrent"`
}

// DefaultMergeQueueConfig returns sensible defaults for merge queue configuration.
func DefaultMergeQueueConfig() *MergeQueueConfig {
	return &MergeQueueConfig{
		Enabled:              true,
		TargetBranch:         "main",
		IntegrationBranches:  true,
		OnConflict:           "assign_back",
		RunTests:             true,
		TestCommand:          "",
		DeleteMergedBranches: true,
		RetryFlakyTests:      1,
		PollInterval:         30 * time.Second,
		MaxConcurrent:        1,
	}
}

// Engineer is the merge queue processor that polls for ready merge-requests
// and processes them according to the merge queue design.
type Engineer struct {
	rig     *rig.Rig
	beads   *beads.Beads
	config  *MergeQueueConfig
	workDir string

	// stopCh is used for graceful shutdown
	stopCh chan struct{}
}

// NewEngineer creates a new Engineer for the given rig.
func NewEngineer(r *rig.Rig) *Engineer {
	return &Engineer{
		rig:     r,
		beads:   beads.New(r.Path),
		config:  DefaultMergeQueueConfig(),
		workDir: r.Path,
		stopCh:  make(chan struct{}),
	}
}

// LoadConfig loads merge queue configuration from the rig's config.json.
func (e *Engineer) LoadConfig() error {
	configPath := filepath.Join(e.rig.Path, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Use defaults if no config file
			return nil
		}
		return fmt.Errorf("reading config: %w", err)
	}

	// Parse config file to extract merge_queue section
	var rawConfig struct {
		MergeQueue json.RawMessage `json:"merge_queue"`
	}
	if err := json.Unmarshal(data, &rawConfig); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	if rawConfig.MergeQueue == nil {
		// No merge_queue section, use defaults
		return nil
	}

	// Parse merge_queue section into our config struct
	// We need special handling for poll_interval (string -> Duration)
	var mqRaw struct {
		Enabled              *bool   `json:"enabled"`
		TargetBranch         *string `json:"target_branch"`
		IntegrationBranches  *bool   `json:"integration_branches"`
		OnConflict           *string `json:"on_conflict"`
		RunTests             *bool   `json:"run_tests"`
		TestCommand          *string `json:"test_command"`
		DeleteMergedBranches *bool   `json:"delete_merged_branches"`
		RetryFlakyTests      *int    `json:"retry_flaky_tests"`
		PollInterval         *string `json:"poll_interval"`
		MaxConcurrent        *int    `json:"max_concurrent"`
	}

	if err := json.Unmarshal(rawConfig.MergeQueue, &mqRaw); err != nil {
		return fmt.Errorf("parsing merge_queue config: %w", err)
	}

	// Apply non-nil values to config (preserving defaults for missing fields)
	if mqRaw.Enabled != nil {
		e.config.Enabled = *mqRaw.Enabled
	}
	if mqRaw.TargetBranch != nil {
		e.config.TargetBranch = *mqRaw.TargetBranch
	}
	if mqRaw.IntegrationBranches != nil {
		e.config.IntegrationBranches = *mqRaw.IntegrationBranches
	}
	if mqRaw.OnConflict != nil {
		e.config.OnConflict = *mqRaw.OnConflict
	}
	if mqRaw.RunTests != nil {
		e.config.RunTests = *mqRaw.RunTests
	}
	if mqRaw.TestCommand != nil {
		e.config.TestCommand = *mqRaw.TestCommand
	}
	if mqRaw.DeleteMergedBranches != nil {
		e.config.DeleteMergedBranches = *mqRaw.DeleteMergedBranches
	}
	if mqRaw.RetryFlakyTests != nil {
		e.config.RetryFlakyTests = *mqRaw.RetryFlakyTests
	}
	if mqRaw.MaxConcurrent != nil {
		e.config.MaxConcurrent = *mqRaw.MaxConcurrent
	}
	if mqRaw.PollInterval != nil {
		dur, err := time.ParseDuration(*mqRaw.PollInterval)
		if err != nil {
			return fmt.Errorf("invalid poll_interval %q: %w", *mqRaw.PollInterval, err)
		}
		e.config.PollInterval = dur
	}

	return nil
}

// Config returns the current merge queue configuration.
func (e *Engineer) Config() *MergeQueueConfig {
	return e.config
}

// Run starts the Engineer main loop. It blocks until the context is cancelled
// or Stop() is called. Returns nil on graceful shutdown.
func (e *Engineer) Run(ctx context.Context) error {
	if err := e.LoadConfig(); err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if !e.config.Enabled {
		return fmt.Errorf("merge queue is disabled in configuration")
	}

	fmt.Printf("[Engineer] Starting for rig %s (poll_interval=%s)\n",
		e.rig.Name, e.config.PollInterval)

	ticker := time.NewTicker(e.config.PollInterval)
	defer ticker.Stop()

	// Run one iteration immediately, then on ticker
	if err := e.processOnce(ctx); err != nil {
		fmt.Printf("[Engineer] Error: %v\n", err)
	}

	for {
		select {
		case <-ctx.Done():
			fmt.Println("[Engineer] Shutting down (context cancelled)")
			return nil
		case <-e.stopCh:
			fmt.Println("[Engineer] Shutting down (stop signal)")
			return nil
		case <-ticker.C:
			if err := e.processOnce(ctx); err != nil {
				fmt.Printf("[Engineer] Error: %v\n", err)
			}
		}
	}
}

// Stop signals the Engineer to stop processing. This is a non-blocking call.
func (e *Engineer) Stop() {
	close(e.stopCh)
}

// processOnce performs one iteration of the Engineer loop:
// 1. Query for ready merge-requests
// 2. If none, return (will try again on next tick)
// 3. Process the highest priority, oldest MR
func (e *Engineer) processOnce(ctx context.Context) error {
	// Check context before starting
	select {
	case <-ctx.Done():
		return nil
	default:
	}

	// 1. Query: bd ready --type=merge-request (filtered client-side)
	readyMRs, err := e.beads.ReadyWithType("merge-request")
	if err != nil {
		return fmt.Errorf("querying ready merge-requests: %w", err)
	}

	// 2. If empty, return
	if len(readyMRs) == 0 {
		return nil
	}

	// 3. Select highest priority, oldest MR
	// bd ready already returns sorted by priority then age, so first is best
	mr := readyMRs[0]

	fmt.Printf("[Engineer] Processing: %s (%s)\n", mr.ID, mr.Title)

	// 4. Claim: bd update <id> --status=in_progress
	inProgress := "in_progress"
	if err := e.beads.Update(mr.ID, beads.UpdateOptions{Status: &inProgress}); err != nil {
		return fmt.Errorf("claiming MR %s: %w", mr.ID, err)
	}

	// 5. Process (delegate to ProcessMR - implementation in separate issue gt-3x1.2)
	result := e.ProcessMR(ctx, mr)

	// 6. Handle result
	if result.Success {
		e.handleSuccess(mr, result)
	} else {
		e.handleFailure(mr, result)
	}

	return nil
}

// ProcessResult contains the result of processing a merge request.
type ProcessResult struct {
	Success     bool
	MergeCommit string
	Error       string
	Conflict    bool
	TestsFailed bool
}

// ProcessMR processes a single merge request.
// It performs: fetch, conflict check, merge, test, push.
func (e *Engineer) ProcessMR(ctx context.Context, mr *beads.Issue) ProcessResult {
	// Parse MR fields from description
	mrFields := beads.ParseMRFields(mr)
	if mrFields == nil {
		return ProcessResult{
			Success: false,
			Error:   "no MR fields found in description",
		}
	}

	// Use config target if MR target is empty
	targetBranch := mrFields.Target
	if targetBranch == "" {
		targetBranch = e.config.TargetBranch
	}

	fmt.Printf("[Engineer] Processing MR:\n")
	fmt.Printf("  Branch: %s\n", mrFields.Branch)
	fmt.Printf("  Target: %s\n", targetBranch)
	fmt.Printf("  Worker: %s\n", mrFields.Worker)
	fmt.Printf("  Source Issue: %s\n", mrFields.SourceIssue)

	// 1. Fetch the source branch
	fmt.Printf("[Engineer] Fetching origin/%s...\n", mrFields.Branch)
	if err := e.gitRun("fetch", "origin", mrFields.Branch); err != nil {
		return ProcessResult{
			Success: false,
			Error:   fmt.Sprintf("fetch failed: %v", err),
		}
	}

	// 2. Checkout target branch and pull latest
	fmt.Printf("[Engineer] Checking out %s...\n", targetBranch)
	if err := e.gitRun("checkout", targetBranch); err != nil {
		return ProcessResult{
			Success: false,
			Error:   fmt.Sprintf("checkout target failed: %v", err),
		}
	}

	// Pull latest (ignore errors - might be up to date)
	_ = e.gitRun("pull", "origin", targetBranch)

	// 3. Check for conflicts before merging (dry-run merge)
	fmt.Printf("[Engineer] Checking for conflicts...\n")
	if conflicts := e.checkConflicts(mrFields.Branch, targetBranch); conflicts != "" {
		return ProcessResult{
			Success:  false,
			Error:    fmt.Sprintf("merge conflict: %s", conflicts),
			Conflict: true,
		}
	}

	// 4. Merge the branch
	fmt.Printf("[Engineer] Merging origin/%s...\n", mrFields.Branch)
	mergeMsg := fmt.Sprintf("Merge %s: %s", mrFields.Branch, mr.Title)
	if err := e.gitRun("merge", "--no-ff", "-m", mergeMsg, "origin/"+mrFields.Branch); err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "CONFLICT") || strings.Contains(errStr, "conflict") {
			// Abort the merge
			_ = e.gitRun("merge", "--abort")
			return ProcessResult{
				Success:  false,
				Error:    "merge conflict during merge",
				Conflict: true,
			}
		}
		return ProcessResult{
			Success: false,
			Error:   fmt.Sprintf("merge failed: %v", err),
		}
	}

	// 5. Run tests if configured
	if e.config.RunTests && e.config.TestCommand != "" {
		fmt.Printf("[Engineer] Running tests: %s\n", e.config.TestCommand)
		if err := e.runTests(e.config.TestCommand); err != nil {
			// Reset to before merge
			_ = e.gitRun("reset", "--hard", "HEAD~1")
			return ProcessResult{
				Success:     false,
				Error:       fmt.Sprintf("tests failed: %v", err),
				TestsFailed: true,
			}
		}
		fmt.Println("[Engineer] Tests passed")
	}

	// 6. Push to origin with retry
	fmt.Printf("[Engineer] Pushing to origin/%s...\n", targetBranch)
	if err := e.pushWithRetry(targetBranch); err != nil {
		// Reset to before merge
		_ = e.gitRun("reset", "--hard", "HEAD~1")
		return ProcessResult{
			Success: false,
			Error:   fmt.Sprintf("push failed: %v", err),
		}
	}

	// 7. Get merge commit SHA
	mergeCommit, err := e.gitOutput("rev-parse", "HEAD")
	if err != nil {
		mergeCommit = "unknown" // Non-fatal, continue
	}

	// 8. Delete source branch if configured
	if e.config.DeleteMergedBranches {
		fmt.Printf("[Engineer] Deleting merged branch origin/%s...\n", mrFields.Branch)
		_ = e.gitRun("push", "origin", "--delete", mrFields.Branch)
	}

	fmt.Printf("[Engineer] ✓ Merged successfully at %s\n", mergeCommit)
	return ProcessResult{
		Success:     true,
		MergeCommit: mergeCommit,
	}
}

// checkConflicts checks if merging branch into target would cause conflicts.
// Returns empty string if no conflicts, or a description of conflicts.
func (e *Engineer) checkConflicts(branch, target string) string {
	// Use git merge-tree to check for conflicts without actually merging
	// First get the merge base
	mergeBase, err := e.gitOutput("merge-base", target, "origin/"+branch)
	if err != nil {
		return fmt.Sprintf("failed to find merge base: %v", err)
	}

	// Check for conflicts using merge-tree
	cmd := exec.Command("git", "merge-tree", mergeBase, target, "origin/"+branch)
	cmd.Dir = e.workDir
	output, _ := cmd.Output()

	// merge-tree outputs conflict markers if there are conflicts
	if strings.Contains(string(output), "<<<<<<") ||
		strings.Contains(string(output), "changed in both") {
		return "files modified in both branches"
	}

	return ""
}

// gitRun executes a git command.
func (e *Engineer) gitRun(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = e.workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return fmt.Errorf("%s", errMsg)
		}
		return err
	}

	return nil
}

// gitOutput executes a git command and returns stdout.
func (e *Engineer) gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = e.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return "", fmt.Errorf("%s", errMsg)
		}
		return "", err
	}

	return strings.TrimSpace(stdout.String()), nil
}

// runTests executes the test command.
func (e *Engineer) runTests(testCmd string) error {
	parts := strings.Fields(testCmd)
	if len(parts) == 0 {
		return nil
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = e.workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(stderr.String()))
	}

	return nil
}

// pushWithRetry pushes to the target branch with exponential backoff retry.
func (e *Engineer) pushWithRetry(targetBranch string) error {
	const maxRetries = 3
	const baseDelay = 1 * time.Second

	var lastErr error
	delay := baseDelay

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("[Engineer] Push retry %d/%d after %v\n", attempt, maxRetries, delay)
			time.Sleep(delay)
			delay *= 2 // Exponential backoff
		}

		err := e.gitRun("push", "origin", targetBranch)
		if err == nil {
			return nil // Success
		}
		lastErr = err
	}

	return fmt.Errorf("push failed after %d retries: %v", maxRetries, lastErr)
}

// handleFailure handles a failed merge request.
// It reopens the MR, assigns back to worker, and sends notification.
func (e *Engineer) handleFailure(mr *beads.Issue, result ProcessResult) {
	mrFields := beads.ParseMRFields(mr)

	// Determine failure type and appropriate label
	var failureLabel string
	var failureSubject string
	var failureBody string

	if result.Conflict {
		failureLabel = "needs-rebase"
		failureSubject = fmt.Sprintf("Rebase needed: %s", mr.ID)
		target := e.config.TargetBranch
		if mrFields != nil && mrFields.Target != "" {
			target = mrFields.Target
		}
		failureBody = fmt.Sprintf(`Your merge request has conflicts with %s.

Please rebase your changes:
  git fetch origin
  git rebase origin/%s
  git push -f

Then resubmit with: gt mq submit

MR: %s
Error: %s`, target, target, mr.ID, result.Error)
	} else if result.TestsFailed {
		failureLabel = "needs-fix"
		failureSubject = fmt.Sprintf("Tests failed: %s", mr.ID)
		failureBody = fmt.Sprintf(`Your merge request failed tests.

Please fix the failing tests and resubmit.

MR: %s
Error: %s`, mr.ID, result.Error)
	} else {
		failureLabel = "needs-fix"
		failureSubject = fmt.Sprintf("Merge failed: %s", mr.ID)
		failureBody = fmt.Sprintf(`Your merge request failed to merge.

MR: %s
Error: %s

Please investigate and resubmit.`, mr.ID, result.Error)
	}

	// 1. Reopen the MR (back to open status for rework)
	open := "open"
	if err := e.beads.Update(mr.ID, beads.UpdateOptions{Status: &open}); err != nil {
		fmt.Printf("[Engineer] Warning: failed to reopen MR %s: %v\n", mr.ID, err)
	}

	// 2. Assign back to worker if we know who they are
	if mrFields != nil && mrFields.Worker != "" {
		// Format worker as full address (e.g., "gastown/Nux")
		workerAddr := mrFields.Worker
		if mrFields.Rig != "" && !strings.Contains(workerAddr, "/") {
			workerAddr = mrFields.Rig + "/" + mrFields.Worker
		}
		if err := e.beads.Update(mr.ID, beads.UpdateOptions{Assignee: &workerAddr}); err != nil {
			fmt.Printf("[Engineer] Warning: failed to assign MR %s to %s: %v\n", mr.ID, workerAddr, err)
		}
	}

	// 3. Add failure label (note: beads doesn't support labels yet, log for now)
	fmt.Printf("[Engineer] Would add label: %s\n", failureLabel)
	// TODO: When beads supports labels: e.beads.AddLabel(mr.ID, failureLabel)

	// 4. Send notification to worker
	if mrFields != nil && mrFields.Worker != "" {
		e.notifyWorkerFailure(mrFields, failureSubject, failureBody)
	}

	// Log the failure
	fmt.Printf("[Engineer] ✗ Failed: %s - %s\n", mr.ID, result.Error)
}

// notifyWorkerFailure sends a failure notification to the worker.
func (e *Engineer) notifyWorkerFailure(mrFields *beads.MRFields, subject, body string) {
	if mrFields == nil || mrFields.Worker == "" {
		return
	}

	// Determine worker address
	workerAddr := mrFields.Worker
	if mrFields.Rig != "" && !strings.Contains(workerAddr, "/") {
		workerAddr = mrFields.Rig + "/" + mrFields.Worker
	}

	router := mail.NewRouter(e.workDir)
	msg := &mail.Message{
		From:     e.rig.Name + "/refinery",
		To:       workerAddr,
		Subject:  subject,
		Body:     body,
		Priority: mail.PriorityHigh,
	}

	if err := router.Send(msg); err != nil {
		fmt.Printf("[Engineer] Warning: failed to notify worker %s: %v\n", workerAddr, err)
	}
}

// handleSuccess handles a successful merge.
// It closes the MR, closes the source issue, and notifies the worker.
func (e *Engineer) handleSuccess(mr *beads.Issue, result ProcessResult) {
	mrFields := beads.ParseMRFields(mr)

	// 1. Update MR description with merge commit SHA
	if mrFields != nil {
		mrFields.MergeCommit = result.MergeCommit
		mrFields.CloseReason = "merged"
		newDesc := beads.SetMRFields(mr, mrFields)
		if err := e.beads.Update(mr.ID, beads.UpdateOptions{Description: &newDesc}); err != nil {
			fmt.Printf("[Engineer] Warning: failed to update MR %s with merge commit: %v\n", mr.ID, err)
		}
	}

	// 2. Close the MR with merged reason
	reason := fmt.Sprintf("merged: %s", result.MergeCommit)
	if err := e.beads.CloseWithReason(reason, mr.ID); err != nil {
		fmt.Printf("[Engineer] Warning: failed to close MR %s: %v\n", mr.ID, err)
	}

	// 3. Close the source issue (the work item that was merged)
	if mrFields != nil && mrFields.SourceIssue != "" {
		sourceReason := fmt.Sprintf("Merged in %s at %s", mr.ID, result.MergeCommit)
		if err := e.beads.CloseWithReason(sourceReason, mrFields.SourceIssue); err != nil {
			fmt.Printf("[Engineer] Warning: failed to close source issue %s: %v\n", mrFields.SourceIssue, err)
		} else {
			fmt.Printf("[Engineer] Closed source issue: %s\n", mrFields.SourceIssue)
		}
	}

	// 4. Notify worker of success
	if mrFields != nil && mrFields.Worker != "" {
		e.notifyWorkerSuccess(mrFields, mr, result)
	}

	fmt.Printf("[Engineer] ✓ Merged: %s\n", mr.ID)
}

// notifyWorkerSuccess sends a success notification to the worker.
func (e *Engineer) notifyWorkerSuccess(mrFields *beads.MRFields, mr *beads.Issue, result ProcessResult) {
	if mrFields == nil || mrFields.Worker == "" {
		return
	}

	// Determine worker address
	workerAddr := mrFields.Worker
	if mrFields.Rig != "" && !strings.Contains(workerAddr, "/") {
		workerAddr = mrFields.Rig + "/" + mrFields.Worker
	}

	// Determine target branch
	target := e.config.TargetBranch
	if mrFields.Target != "" {
		target = mrFields.Target
	}

	subject := fmt.Sprintf("Work merged: %s", mr.ID)
	body := fmt.Sprintf(`Your work has been merged successfully!

Branch: %s
Target: %s
Merge commit: %s

Issue: %s
Thank you for your contribution!`, mrFields.Branch, target, result.MergeCommit, mrFields.SourceIssue)

	router := mail.NewRouter(e.workDir)
	msg := &mail.Message{
		From:     e.rig.Name + "/refinery",
		To:       workerAddr,
		Subject:  subject,
		Body:     body,
		Priority: mail.PriorityNormal,
	}

	if err := router.Send(msg); err != nil {
		fmt.Printf("[Engineer] Warning: failed to notify worker %s: %v\n", workerAddr, err)
	}
}
