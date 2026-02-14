package agentloop

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/llm"
)

const (
	// DefaultShellTimeout is the default timeout for shell_exec commands.
	DefaultShellTimeout = 120 * time.Second
	// MaxFileReadSize is the maximum file size to read (10MB).
	MaxFileReadSize = 10 * 1024 * 1024
	// MaxOutputSize is the maximum tool output size (100KB).
	MaxOutputSize = 100 * 1024
)

// Executor handles tool call execution in a specific working directory.
// All file operations are sandboxed to the worktree directory.
type Executor struct {
	workDir  string // git worktree path
	rigName  string
	rigPath  string
	townRoot string
	actor    string // e.g., "rig/polecats/Toast"
	role     string // e.g., "polecat", "witness", "deacon"
}

// NewExecutor creates a tool executor for a specific working directory.
func NewExecutor(workDir, rigName, rigPath, townRoot, actor, role string) *Executor {
	if role == "" {
		role = "polecat" // default for backward compatibility
	}
	return &Executor{
		workDir:  workDir,
		rigName:  rigName,
		rigPath:  rigPath,
		townRoot: townRoot,
		actor:    actor,
		role:     role,
	}
}

// Execute runs a tool call and returns the result as a string.
// Tool execution happens locally regardless of where the LLM runs.
func (e *Executor) Execute(ctx context.Context, call llm.ToolCall) (string, error) {
	switch call.Name {
	case "gt_prime":
		return e.execGTPrime(ctx)
	case "gt_done":
		return e.execGTDone(ctx, call.Args)
	case "bd_show":
		return e.execBDShow(ctx, call.Args)
	case "bd_list":
		return e.execBDList(ctx, call.Args)
	case "bd_update":
		return e.execBDUpdate(ctx, call.Args)
	case "git_diff":
		return e.execGitDiff(ctx, call.Args)
	case "git_status":
		return e.execGitStatus(ctx)
	case "git_commit":
		return e.execGitCommit(ctx, call.Args)
	case "file_read":
		return e.execFileRead(ctx, call.Args)
	case "file_write":
		return e.execFileWrite(ctx, call.Args)
	case "file_edit":
		return e.execFileEdit(ctx, call.Args)
	case "file_list":
		return e.execFileList(ctx, call.Args)
	case "file_search":
		return e.execFileSearch(ctx, call.Args)
	case "shell_exec":
		return e.execShell(ctx, call.Args)
	case "gt_mail_send":
		return e.execMailSend(ctx, call.Args)
	case "gt_mail_read":
		return e.execMailRead(ctx, call.Args)
	default:
		return "", fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// --- Tool implementations ---

func (e *Executor) execGTPrime(ctx context.Context) (string, error) {
	// Run `gt prime` in the working directory
	return e.runCommand(ctx, "gt", []string{"prime"}, DefaultShellTimeout)
}

func (e *Executor) execGTDone(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("parsing gt_done args: %w", err)
	}
	if params.Message == "" {
		return "", fmt.Errorf("gt_done requires a message")
	}
	return e.runCommand(ctx, "gt", []string{"done", "-m", params.Message}, DefaultShellTimeout)
}

func (e *Executor) execBDShow(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		IssueID string `json:"issue_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("parsing bd_show args: %w", err)
	}
	if params.IssueID == "" {
		return "", fmt.Errorf("bd_show requires issue_id")
	}
	return e.runCommand(ctx, "bd", []string{"show", params.IssueID}, DefaultShellTimeout)
}

func (e *Executor) execBDList(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Status string `json:"status"`
		Label  string `json:"label"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &params)
	}

	cmdArgs := []string{"list"}
	if params.Status != "" {
		cmdArgs = append(cmdArgs, "--status", params.Status)
	}
	if params.Label != "" {
		cmdArgs = append(cmdArgs, "--label", params.Label)
	}
	return e.runCommand(ctx, "bd", cmdArgs, DefaultShellTimeout)
}

func (e *Executor) execBDUpdate(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		IssueID string `json:"issue_id"`
		Status  string `json:"status"`
		Comment string `json:"comment"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("parsing bd_update args: %w", err)
	}
	if params.IssueID == "" {
		return "", fmt.Errorf("bd_update requires issue_id")
	}

	cmdArgs := []string{"update", params.IssueID}
	if params.Status != "" {
		cmdArgs = append(cmdArgs, "--status", params.Status)
	}
	if params.Comment != "" {
		cmdArgs = append(cmdArgs, "--comment", params.Comment)
	}
	return e.runCommand(ctx, "bd", cmdArgs, DefaultShellTimeout)
}

func (e *Executor) execGitDiff(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Staged bool   `json:"staged"`
		Path   string `json:"path"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &params)
	}

	cmdArgs := []string{"diff"}
	if params.Staged {
		cmdArgs = append(cmdArgs, "--staged")
	}
	if params.Path != "" {
		safePath, err := e.safePath(params.Path)
		if err != nil {
			return "", err
		}
		cmdArgs = append(cmdArgs, "--", safePath)
	}
	return e.runCommand(ctx, "git", cmdArgs, DefaultShellTimeout)
}

func (e *Executor) execGitStatus(ctx context.Context) (string, error) {
	return e.runCommand(ctx, "git", []string{"status", "--short"}, DefaultShellTimeout)
}

func (e *Executor) execGitCommit(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Message string   `json:"message"`
		Paths   []string `json:"paths"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("parsing git_commit args: %w", err)
	}
	if params.Message == "" {
		return "", fmt.Errorf("git_commit requires a message")
	}

	// Stage files
	if len(params.Paths) > 0 {
		addArgs := []string{"add"}
		for _, p := range params.Paths {
			safePath, err := e.safePath(p)
			if err != nil {
				return "", err
			}
			addArgs = append(addArgs, safePath)
		}
		if _, err := e.runCommand(ctx, "git", addArgs, DefaultShellTimeout); err != nil {
			return "", fmt.Errorf("git add failed: %w", err)
		}
	} else {
		if _, err := e.runCommand(ctx, "git", []string{"add", "-A"}, DefaultShellTimeout); err != nil {
			return "", fmt.Errorf("git add -A failed: %w", err)
		}
	}

	// Commit
	return e.runCommand(ctx, "git", []string{"commit", "-m", params.Message}, DefaultShellTimeout)
}

func (e *Executor) execFileRead(_ context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("parsing file_read args: %w", err)
	}
	if params.Path == "" {
		return "", fmt.Errorf("file_read requires path")
	}

	absPath, err := e.safePath(params.Path)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("file not found: %s", params.Path)
	}
	if info.Size() > MaxFileReadSize {
		return "", fmt.Errorf("file too large (%d bytes, max %d)", info.Size(), MaxFileReadSize)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}

	content := string(data)

	// Apply line range filter if specified
	if params.StartLine > 0 || params.EndLine > 0 {
		lines := strings.Split(content, "\n")
		start := params.StartLine
		if start < 1 {
			start = 1
		}
		end := params.EndLine
		if end < 1 || end > len(lines) {
			end = len(lines)
		}
		if start > len(lines) {
			return "", fmt.Errorf("start_line %d exceeds file length %d", start, len(lines))
		}

		var sb strings.Builder
		for i := start - 1; i < end; i++ {
			fmt.Fprintf(&sb, "%d: %s\n", i+1, lines[i])
		}
		return sb.String(), nil
	}

	// Add line numbers to full file
	var sb strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNum := 1
	for scanner.Scan() {
		fmt.Fprintf(&sb, "%d: %s\n", lineNum, scanner.Text())
		lineNum++
	}

	result := sb.String()
	if len(result) > MaxOutputSize {
		return result[:MaxOutputSize] + "\n... (truncated)", nil
	}
	return result, nil
}

func (e *Executor) execFileWrite(_ context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("parsing file_write args: %w", err)
	}
	if params.Path == "" {
		return "", fmt.Errorf("file_write requires path")
	}

	absPath, err := e.safePath(params.Path)
	if err != nil {
		return "", err
	}

	// Create parent directories
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating directories: %w", err)
	}

	if err := os.WriteFile(absPath, []byte(params.Content), 0644); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}

	return fmt.Sprintf("Wrote %d bytes to %s", len(params.Content), params.Path), nil
}

func (e *Executor) execFileEdit(_ context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Path    string `json:"path"`
		Search  string `json:"search"`
		Replace string `json:"replace"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("parsing file_edit args: %w", err)
	}
	if params.Path == "" || params.Search == "" {
		return "", fmt.Errorf("file_edit requires path and search")
	}

	absPath, err := e.safePath(params.Path)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}

	content := string(data)
	if !strings.Contains(content, params.Search) {
		return "", fmt.Errorf("search text not found in %s", params.Path)
	}

	// Replace first occurrence
	newContent := strings.Replace(content, params.Search, params.Replace, 1)
	if err := os.WriteFile(absPath, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}

	return fmt.Sprintf("Applied edit to %s", params.Path), nil
}

func (e *Executor) execFileList(_ context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
		Pattern   string `json:"pattern"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &params)
	}

	dir := e.workDir
	if params.Path != "" {
		safePath, err := e.safePath(params.Path)
		if err != nil {
			return "", err
		}
		dir = safePath
	}

	var sb strings.Builder
	if params.Recursive {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // skip errors
			}
			relPath, _ := filepath.Rel(e.workDir, path)
			if strings.HasPrefix(relPath, ".git") {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if params.Pattern != "" {
				matched, _ := filepath.Match(params.Pattern, filepath.Base(path))
				if !matched {
					return nil
				}
			}
			prefix := "  "
			if info.IsDir() {
				prefix = "d "
			}
			fmt.Fprintf(&sb, "%s%s\n", prefix, relPath)
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("walking directory: %w", err)
		}
	} else {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return "", fmt.Errorf("reading directory: %w", err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".git") {
				continue
			}
			if params.Pattern != "" {
				matched, _ := filepath.Match(params.Pattern, entry.Name())
				if !matched {
					continue
				}
			}
			prefix := "  "
			if entry.IsDir() {
				prefix = "d "
			}
			relPath := entry.Name()
			if params.Path != "" {
				relPath = filepath.Join(params.Path, entry.Name())
			}
			fmt.Fprintf(&sb, "%s%s\n", prefix, relPath)
		}
	}

	result := sb.String()
	if result == "" {
		return "(empty directory)", nil
	}
	return result, nil
}

func (e *Executor) execFileSearch(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Include string `json:"include"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("parsing file_search args: %w", err)
	}
	if params.Pattern == "" {
		return "", fmt.Errorf("file_search requires pattern")
	}

	// Use grep for content search
	cmdArgs := []string{"-rn", "--color=never"}
	if params.Include != "" {
		cmdArgs = append(cmdArgs, "--include="+params.Include)
	}
	cmdArgs = append(cmdArgs, params.Pattern)

	searchDir := e.workDir
	if params.Path != "" {
		safePath, err := e.safePath(params.Path)
		if err != nil {
			return "", err
		}
		searchDir = safePath
	}
	cmdArgs = append(cmdArgs, searchDir)

	cmd := exec.CommandContext(ctx, "grep", cmdArgs...)
	cmd.Dir = e.workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()

	// grep exits 1 when no matches found — that's not an error
	if err != nil && output == "" {
		return "(no matches found)", nil
	}

	if len(output) > MaxOutputSize {
		output = output[:MaxOutputSize] + "\n... (truncated)"
	}
	return output, nil
}

func (e *Executor) execShell(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("parsing shell_exec args: %w", err)
	}
	if params.Command == "" {
		return "", fmt.Errorf("shell_exec requires command")
	}

	timeout := DefaultShellTimeout
	if params.TimeoutSeconds > 0 {
		timeout = time.Duration(params.TimeoutSeconds) * time.Second
	}

	return e.runCommand(ctx, "bash", []string{"-c", params.Command}, timeout)
}

func (e *Executor) execMailSend(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("parsing gt_mail_send args: %w", err)
	}
	if params.To == "" || params.Subject == "" {
		return "", fmt.Errorf("gt_mail_send requires 'to' and 'subject'")
	}

	cmdArgs := []string{"mail", "send", "--to", params.To, "--subject", params.Subject}
	if params.Body != "" {
		cmdArgs = append(cmdArgs, "--body", params.Body)
	}
	return e.runCommand(ctx, "gt", cmdArgs, DefaultShellTimeout)
}

func (e *Executor) execMailRead(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Count      int  `json:"count"`
		UnreadOnly bool `json:"unread_only"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &params)
	}

	cmdArgs := []string{"mail", "read"}
	if params.Count > 0 {
		cmdArgs = append(cmdArgs, "--count", fmt.Sprintf("%d", params.Count))
	}
	if params.UnreadOnly {
		cmdArgs = append(cmdArgs, "--unread")
	}
	return e.runCommand(ctx, "gt", cmdArgs, DefaultShellTimeout)
}

// --- Helpers ---

// runCommand executes a command in the working directory with a timeout.
func (e *Executor) runCommand(ctx context.Context, name string, args []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = e.workDir

	// Set GT environment variables
	cmd.Env = append(os.Environ(),
		"GT_ROLE="+e.role,
		"GT_RIG="+e.rigName,
		"GT_TOWN_ROOT="+e.townRoot,
		"GT_ROOT="+e.townRoot, // alias for compatibility
		"GT_ACTOR="+e.actor,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += "STDERR: " + stderr.String()
	}

	if len(output) > MaxOutputSize {
		output = output[:MaxOutputSize] + "\n... (truncated)"
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return output, fmt.Errorf("command timed out after %v", timeout)
		}
		// Include output with the error — partial output is often useful
		return output, fmt.Errorf("command failed: %w", err)
	}

	return output, nil
}

// safePath validates and resolves a path to be within the working directory.
// Prevents path traversal attacks (e.g., ../../etc/passwd).
func (e *Executor) safePath(path string) (string, error) {
	// Resolve to absolute
	var absPath string
	if filepath.IsAbs(path) {
		absPath = filepath.Clean(path)
	} else {
		absPath = filepath.Clean(filepath.Join(e.workDir, path))
	}

	// Evaluate symlinks to prevent escaping via symlink chains
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// If the target doesn't exist yet (e.g., file_write to new file),
		// check the parent directory instead
		parentDir := filepath.Dir(absPath)
		resolvedParent, parentErr := filepath.EvalSymlinks(parentDir)
		if parentErr != nil {
			// Parent doesn't exist either - check without symlink resolution
			resolved = absPath
		} else {
			resolved = filepath.Join(resolvedParent, filepath.Base(absPath))
		}
	}

	// Also resolve workDir symlinks for a fair comparison
	resolvedWorkDir, err := filepath.EvalSymlinks(e.workDir)
	if err != nil {
		resolvedWorkDir = e.workDir
	}

	// Use filepath.Rel to verify containment (more robust than HasPrefix)
	rel, err := filepath.Rel(resolvedWorkDir, resolved)
	if err != nil {
		return "", fmt.Errorf("path %q is outside working directory", path)
	}

	// If the relative path escapes (starts with ".."), reject it
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path %q is outside working directory", path)
	}

	return absPath, nil
}

// WorkDir returns the executor's working directory.
func (e *Executor) WorkDir() string {
	return e.workDir
}
