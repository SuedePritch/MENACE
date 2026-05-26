package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"menace/internal/agent"
	mlog "menace/internal/log"
	"menace/internal/store"
)

// execResult is the outcome of a single executeAndReview call.
type execResult int

const (
	execSuccess execResult = iota
	execFailed
	execStalled // agent made changes but they conflict with the current main tree
)

const (
	MaxArchitectIterations = 50
	MaxWorkerIterations    = 30

	// maxLogPreviewLen caps the length of log previews to avoid flooding logs.
	maxLogPreviewLen = 500
)

// TasksChangedMsg tells the UI to re-read task state.
type TasksChangedMsg struct{}

// RateLimitMsg notifies the UI that rate limiting is active or has cleared.
type RateLimitMsg struct {
	Active     bool
	RetryAfter time.Duration
}

// TaskCompletedMsg signals a task finished.
type TaskCompletedMsg struct {
	TaskID      string
	Description string
	Status      store.TaskStatus
	ErrLine     string
}

type Orchestrator struct {
	cwd            string
	menaceDir      string
	projectID      string
	workerProvider string
	workerModel    string
	workerAPIKey   string
	store          TaskStore
	maxConc      int
	maxRetry     int

	mu          sync.Mutex
	wg          sync.WaitGroup
	running     map[string]*workerProc
	stopped     bool
	ctx         context.Context
	cancel      context.CancelFunc
	rateLimiter *RateLimiter

	program *tea.Program
}

type workerProc struct {
	taskID string
	cancel context.CancelFunc
	start  time.Time
}

type OrchestratorConfig struct {
	CWD            string
	MenaceDir      string
	ProjectID      string
	WorkerProvider string
	WorkerModel    string
	WorkerAPIKey   string
	MaxConcurrent  int
	MaxRetry       int
}

func NewOrchestrator(cfg OrchestratorConfig, s TaskStore, p *tea.Program) *Orchestrator {
	ctx, cancel := context.WithCancel(context.Background())
	o := &Orchestrator{
		cwd:          cfg.CWD,
		menaceDir:    cfg.MenaceDir,
		projectID:    cfg.ProjectID,
		workerProvider: cfg.WorkerProvider,
		workerModel:    cfg.WorkerModel,
		workerAPIKey:   cfg.WorkerAPIKey,
		store:        s,
		maxConc:      cfg.MaxConcurrent,
		maxRetry:     cfg.MaxRetry,
		running:      make(map[string]*workerProc),
		ctx:          ctx,
		cancel:       cancel,
		rateLimiter:  &RateLimiter{},
		program:      p,
	}
	go o.cleanupOrphanWorktrees()
	o.scheduleInner()
	return o
}

func (o *Orchestrator) send(msg tea.Msg) {
	if o.program != nil {
		o.program.Send(msg)
	}
}

func (o *Orchestrator) Schedule() {
	o.mu.Lock()
	if o.stopped {
		o.mu.Unlock()
		return
	}
	o.mu.Unlock()
	o.scheduleInner()
}

func (o *Orchestrator) scheduleInner() {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.stopped {
		return
	}

	slots := o.maxConc - len(o.running)
	if slots <= 0 {
		return
	}

	dbTasks, err := o.store.ListTasks(o.projectID)
	if err != nil {
		mlog.Error("scheduleInner ListTasks", slog.String("err", err.Error()))
		return
	}

	runningTouches := make(map[string]bool)
	for _, t := range dbTasks {
		if _, running := o.running[t.ID]; running {
			for _, f := range t.Touches {
				runningTouches[f] = true
			}
		}
	}

	for _, t := range dbTasks {
		if slots <= 0 {
			break
		}
		if t.Status != store.StatusPending {
			continue
		}
		if _, running := o.running[t.ID]; running {
			continue
		}
		if len(t.Touches) > 0 && len(runningTouches) > 0 {
			conflict := false
			for _, f := range t.Touches {
				if runningTouches[f] {
					conflict = true
					break
				}
			}
			if conflict {
				continue
			}
		}
		taskCtx, taskCancel := context.WithCancel(o.ctx)
		o.running[t.ID] = &workerProc{taskID: t.ID, cancel: taskCancel, start: time.Now()}
		o.wg.Add(1)
		go o.runTask(taskCtx, t)
		for _, f := range t.Touches {
			runningTouches[f] = true
		}
		slots--
	}
}

func (o *Orchestrator) runTask(ctx context.Context, t store.TaskData) {
	defer o.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			mlog.Error("runTask panic recovered", slog.String("task", t.ID), slog.Any("panic", r))
			if err := o.store.UpdateTaskStatus(t.ID, store.StatusFailed); err != nil {
				mlog.Error("runTask panic UpdateTaskStatus", slog.String("task", t.ID), slog.String("err", err.Error()))
			}
			o.mu.Lock()
			delete(o.running, t.ID)
			o.mu.Unlock()
			o.send(TasksChangedMsg{})
		}
	}()
	if err := o.store.UpdateTaskStatus(t.ID, store.StatusRunning); err != nil {
		mlog.Error("runTask UpdateTaskStatus", slog.String("task", t.ID), slog.String("err", err.Error()))
	}
	o.send(TasksChangedMsg{})
	o.taskLog(t.ID, "Starting: %s", t.Description)

	// Create a single worktree for the entire task (all subtasks share it).
	wtPath := filepath.Join(o.menaceDir, "worktrees", t.ID)
	if err := GitWorktreeAdd(o.cwd, wtPath); err != nil {
		o.taskLog(t.ID, "Failed to create worktree: %v", err)
		if err2 := o.store.UpdateTaskStatus(t.ID, store.StatusFailed); err2 != nil {
			mlog.Error("runTask worktree fail UpdateTaskStatus", slog.String("task", t.ID), slog.String("err", err2.Error()))
		}
		o.mu.Lock()
		delete(o.running, t.ID)
		o.mu.Unlock()
		o.send(TaskCompletedMsg{TaskID: t.ID, Description: t.Description, Status: store.StatusFailed, ErrLine: err.Error()})
		o.Schedule()
		return
	}
	defer func() {
		if rmErr := GitWorktreeRemove(o.cwd, wtPath); rmErr != nil {
			mlog.Error("runTask worktree remove", slog.String("path", wtPath), slog.String("err", rmErr.Error()))
		}
	}()

	var outcome execResult
	for attempt := 0; attempt <= o.maxRetry; attempt++ {
		if ctx.Err() != nil {
			break
		}
		if attempt > 0 {
			o.taskLog(t.ID, "Retry %d/%d: %s", attempt, o.maxRetry, t.Description)
			// Reset the worktree to HEAD so the retry starts from a clean state.
			if err := GitWorktreeReset(wtPath); err != nil {
				o.taskLog(t.ID, "Failed to reset worktree for retry: %v", err)
				outcome = execFailed
				break
			}
			for _, sub := range t.Subtasks {
				if err := o.store.UpdateSubtaskStatus(sub.ID, store.StatusPending); err != nil {
					mlog.Error("retry UpdateSubtaskStatus", slog.String("subtask", sub.ID), slog.String("err", err.Error()))
				}
			}
			o.send(TasksChangedMsg{})
		}

		if len(t.Subtasks) > 0 {
			outcome = execSuccess
			fresh, err := o.store.GetTask(t.ID)
			if err != nil {
				if !errors.Is(err, store.ErrNotFound) {
					mlog.Error("runTask GetTask", slog.String("task", t.ID), slog.String("err", err.Error()))
				}
				outcome = execFailed
				break
			}
			for _, sub := range fresh.Subtasks {
				if ctx.Err() != nil {
					outcome = execFailed
					break
				}
				if sub.Status == store.StatusDone {
					o.taskLog(t.ID, "Skipping (done): %s", sub.Description)
					continue
				}
				res := o.executeAndReview(ctx, t, &sub, wtPath)
				if res != execSuccess {
					outcome = res
					break
				}
			}
			// All subtasks succeeded — commit worktree changes and cherry-pick into main.
			if outcome == execSuccess {
				outcome = o.commitAndMerge(t, wtPath)
			}
		} else {
			outcome = o.executeAndReview(ctx, t, nil, wtPath)
		}
		// Don't retry a stalled task — it needs user intervention.
		if outcome == execSuccess || outcome == execStalled {
			break
		}
	}

	cur, err := o.store.GetTask(t.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		mlog.Error("runTask GetTask final check", slog.String("task", t.ID), slog.String("err", err.Error()))
	}
	var finalStatus store.TaskStatus
	if cur != nil && cur.Status == store.StatusCancelled {
		finalStatus = store.StatusCancelled
		o.taskLog(t.ID, "Cancelled.")
	} else if ctx.Err() != nil {
		finalStatus = store.StatusCancelled
		if err := o.store.UpdateTaskStatus(t.ID, store.StatusCancelled); err != nil {
			mlog.Error("runTask final UpdateTaskStatus", slog.String("task", t.ID), slog.String("err", err.Error()))
		}
		o.taskLog(t.ID, "Cancelled.")
	} else if outcome == execSuccess {
		finalStatus = store.StatusDone
		if err := o.store.UpdateTaskStatus(t.ID, store.StatusDone); err != nil {
			mlog.Error("runTask final UpdateTaskStatus", slog.String("task", t.ID), slog.String("err", err.Error()))
		}
		o.taskLog(t.ID, "Done.")
	} else if outcome == execStalled {
		finalStatus = store.StatusStalled
		if err := o.store.UpdateTaskStatus(t.ID, store.StatusStalled); err != nil {
			mlog.Error("runTask final UpdateTaskStatus", slog.String("task", t.ID), slog.String("err", err.Error()))
		}
		o.taskLog(t.ID, "Stalled — conflicts with current tree. Review diff and retry.")
	} else {
		finalStatus = store.StatusFailed
		if err := o.store.UpdateTaskStatus(t.ID, store.StatusFailed); err != nil {
			mlog.Error("runTask final UpdateTaskStatus", slog.String("task", t.ID), slog.String("err", err.Error()))
		}
		o.taskLog(t.ID, "Failed.")
	}

	o.mu.Lock()
	delete(o.running, t.ID)
	o.mu.Unlock()

	errLine := ""
	if finalStatus == store.StatusFailed {
		errLine = o.store.GetTaskLastLogLine(t.ID)
	}
	o.send(TaskCompletedMsg{
		TaskID:      t.ID,
		Description: t.Description,
		Status:      finalStatus,
		ErrLine:     errLine,
	})

	o.Schedule()
}

func (o *Orchestrator) executeAndReview(ctx context.Context, t store.TaskData, sub *store.SubtaskData, wtPath string) execResult {
	id := t.ID
	desc := t.Description
	if sub != nil {
		id = sub.ID
		desc = sub.Description
	}

	if sub != nil {
		if err := o.store.UpdateSubtaskStatus(id, store.StatusRunning); err != nil {
			mlog.Error("executeAndReview UpdateSubtaskStatus", slog.String("subtask", id), slog.String("err", err.Error()))
		}
	} else {
		if err := o.store.UpdateTaskStatus(id, store.StatusRunning); err != nil {
			mlog.Error("executeAndReview UpdateTaskStatus", slog.String("task", id), slog.String("err", err.Error()))
		}
	}
	o.send(TasksChangedMsg{})

	instruction := t.Instruction
	if sub != nil && sub.Instruction != "" {
		instruction = sub.Instruction
	}

	o.taskLog(t.ID, "Executing: %s", desc)
	mlog.Info("executing subtask", slog.String("task", t.ID), slog.String("subtask", id), slog.String("desc", desc))

	prompt := o.buildWorkerPrompt(t, sub, instruction)
	mlog.Debug("worker prompt length", slog.String("task", id), slog.Int("len", len(prompt)))

	// Stage all changes (including new untracked files) so git diff HEAD captures everything.
	_ = GitStageAll(wtPath)

	// Snapshot the worktree before the agent runs so we can detect per-subtask changes.
	preDiff := GitDiffHead(wtPath)
	agentOk := o.runAgentInDir(ctx, t.ID, "worker", prompt, wtPath)

	// Stage again after agent runs to capture any new files it created.
	_ = GitStageAll(wtPath)

	postDiff := GitDiffHead(wtPath)
	mlog.Debug("diff capture", slog.String("task", t.ID), slog.Int("pre", len(preDiff)), slog.Int("post", len(postDiff)))

	// Treat no-diff as failure — agent ran but made no changes.
	if agentOk && postDiff == preDiff {
		agentOk = false
	}

	if !agentOk {
		o.taskLog(t.ID, "Agent failed (no changes or error)")
		if sub != nil {
			if err := o.store.UpdateSubtaskStatus(id, store.StatusFailed); err != nil {
				mlog.Error("UpdateSubtaskStatus failed", slog.String("subtask", id), slog.String("err", err.Error()))
			}
		} else {
			if err := o.store.UpdateTaskStatus(id, store.StatusFailed); err != nil {
				mlog.Error("UpdateTaskStatus failed", slog.String("task", id), slog.String("err", err.Error()))
			}
		}
		o.send(TasksChangedMsg{})
		return execFailed
	}

	// Store the per-subtask diff. postDiff is the full accumulated worktree diff;
	// for the first subtask (preDiff=="") this is exactly the subtask's changes.
	// For later subtasks it accumulates, but is still useful for review.
	subtaskDiff := postDiff
	subID := ""
	if sub != nil {
		subID = sub.ID
	}
	if err := o.store.SaveTaskDiff(t.ID, subID, subtaskDiff); err != nil {
		mlog.Error("diff capture save", slog.String("task", t.ID), slog.String("err", err.Error()))
	}

	// This is the last subtask (or a no-subtask task): check if changes apply to main tree.
	// We only apply at the task level (after all subtasks succeed), not per-subtask.
	// For subtasks, just mark done and continue — the apply happens at task completion.
	if sub != nil {
		o.taskLog(t.ID, "Done: %s", desc)
		if err := o.store.UpdateSubtaskStatus(id, store.StatusDone); err != nil {
			mlog.Error("UpdateSubtaskStatus done", slog.String("subtask", id), slog.String("err", err.Error()))
		}
		o.send(TasksChangedMsg{})
		return execSuccess
	}

	// No subtasks — commit worktree changes and cherry-pick into main.
	return o.commitAndMerge(t, wtPath)
}

// commitAndMerge commits all staged worktree changes and cherry-picks the commit into the main tree.
func (o *Orchestrator) commitAndMerge(t store.TaskData, wtPath string) execResult {
	// Stage everything in the worktree.
	if err := GitStageAll(wtPath); err != nil {
		o.taskLog(t.ID, "Failed to stage worktree changes: %v", err)
		return execFailed
	}

	// Check if there's actually anything to commit.
	if GitDiffHead(wtPath) == "" {
		o.taskLog(t.ID, "No changes to commit.")
		return execSuccess
	}

	// Commit in the worktree.
	commitMsg := fmt.Sprintf("menace: %s", t.Description)
	sha, err := GitCommitWorktree(wtPath, commitMsg)
	if err != nil {
		o.taskLog(t.ID, "Failed to commit in worktree: %v", err)
		mlog.Error("commitAndMerge commit", slog.String("task", t.ID), slog.String("err", err.Error()))
		return execFailed
	}
	o.taskLog(t.ID, "Committed in worktree: %s", sha)

	// Cherry-pick into the main tree.
	if err := GitCherryPick(o.cwd, sha); err != nil {
		o.taskLog(t.ID, "Conflict cherry-picking into main tree: %v", err)
		mlog.Info("commitAndMerge cherry-pick conflict", slog.String("task", t.ID), slog.String("sha", sha), slog.String("err", err.Error()))
		// Abort the cherry-pick so main tree is left clean.
		_ = GitCherryPickAbort(o.cwd)
		return execStalled
	}

	o.taskLog(t.ID, "Cherry-picked %s into main tree.", sha)
	return execSuccess
}

func (o *Orchestrator) buildWorkerPrompt(t store.TaskData, sub *store.SubtaskData, instruction string) string {
	var parts []string
	if ctx := o.store.GetProjectContext(o.projectID); ctx != "" {
		parts = append(parts, "Project context: "+ctx)
	}
	parts = append(parts, "Task: "+t.Description)
	if instruction != "" {
		parts = append(parts, "Instructions:\n"+instruction)
	} else if t.Instruction != "" {
		parts = append(parts, "Instructions:\n"+t.Instruction)
	}
	if sub != nil {
		parts = append(parts, "\nCurrent subtask: "+sub.Description)
		if sub.Instruction != "" && instruction == "" {
			parts = append(parts, "Subtask instructions:\n"+sub.Instruction)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (o *Orchestrator) runAgentInDir(ctx context.Context, taskID, agentType, prompt, cwd string) bool {
	mlog.Debug("worker prompt", slog.String("task", taskID), slog.Int("prompt_len", len(prompt)))

	systemPrompt := LoadSystemPrompt(o.menaceDir, agentType)

	if o.workerAPIKey == "" {
		o.taskLog(taskID, "No API key for worker provider %q", o.workerProvider)
		return false
	}

	workerTools := agent.WriteTools(o.menaceDir, cwd)

	ag, err := agent.NewAgent(o.workerProvider, o.workerModel, o.workerAPIKey, systemPrompt, workerTools, MaxWorkerIterations)
	if err != nil {
		o.taskLog(taskID, "Failed to create agent: %v", err)
		return false
	}

	ag.OnEvent = func(ev agent.Event) {
		switch ev.Type {
		case "text_delta":
			clean := StripThinking(ev.Delta)
			if strings.TrimSpace(clean) != "" {
				if err := o.store.AppendTaskLog(taskID, clean); err != nil {
					mlog.Error("AppendTaskLog", slog.String("err", err.Error()))
				}
			}
		case "tool_done":
			if err := o.store.AppendTaskLog(taskID, fmt.Sprintf("[tool] %s", ev.ToolName)); err != nil {
				mlog.Error("AppendTaskLog", slog.String("err", err.Error()))
			}
		}
	}

	mlog.Info("worker agent started", slog.String("task", taskID), slog.String("provider", o.workerProvider), slog.String("model", o.workerModel), slog.Int("tools", len(workerTools)))

	// Wait out any active rate limit before starting.
	if err := o.rateLimiter.Wait(ctx); err != nil {
		o.taskLog(taskID, "Cancelled (rate limit wait)")
		return false
	}

	const maxRateLimitRetries = 3
	var fullText string
	var runErr error
	for attempt := 0; attempt <= maxRateLimitRetries; attempt++ {
		if ctx.Err() != nil {
			break
		}
		fullText, runErr = ag.Run(ctx, prompt)
		if isRL, retryAfter := IsRateLimitError(runErr); isRL {
			o.rateLimiter.RecordRateLimit(retryAfter)
			o.taskLog(taskID, "Rate limited — waiting %s before retry %d/%d", retryAfter, attempt+1, maxRateLimitRetries)
			o.send(RateLimitMsg{Active: true, RetryAfter: retryAfter})
			if waitErr := o.rateLimiter.Wait(ctx); waitErr != nil {
				break
			}
			o.send(RateLimitMsg{Active: false})
			// Reset the agent conversation state for a clean retry.
			ag2, newErr := agent.NewAgent(o.workerProvider, o.workerModel, o.workerAPIKey, systemPrompt, workerTools, MaxWorkerIterations)
			if newErr != nil {
				o.taskLog(taskID, "Failed to recreate agent: %v", newErr)
				return false
			}
			ag2.OnEvent = ag.OnEvent
			ag = ag2
			continue
		}
		break
	}
	_ = fullText

	if ctx.Err() != nil {
		o.taskLog(taskID, "Cancelled")
		return false
	}

	if runErr != nil {
		o.taskLog(taskID, "Agent error: %v", runErr)
		mlog.Error("agent error", slog.String("type", agentType), slog.String("err", runErr.Error()))
		return false
	}

	if fullText != "" {
		preview := fullText
		if len(preview) > maxLogPreviewLen {
			preview = preview[:maxLogPreviewLen] + "…(truncated)"
		}
		mlog.Info("worker done", slog.String("task", taskID), slog.String("preview", preview))
	}

	o.taskLog(taskID, "Agent completed")
	return true
}

func (o *Orchestrator) taskLog(taskID string, format string, args ...interface{}) {
	ts := time.Now().Format("15:04:05")
	line := fmt.Sprintf("[%s] %s", ts, fmt.Sprintf(format, args...))
	if err := o.store.AppendTaskLog(taskID, line); err != nil {
		mlog.Error("taskLog AppendTaskLog", slog.String("err", err.Error()))
	}
}

func (o *Orchestrator) Stop() {
	o.mu.Lock()
	o.stopped = true
	o.cancel()
	for _, wp := range o.running {
		wp.cancel()
	}
	o.mu.Unlock()

	done := make(chan struct{})
	go func() {
		o.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		mlog.Error("orchestrator stop: timed out waiting for workers")
	}
}

func (o *Orchestrator) CancelTask(taskID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if wp, ok := o.running[taskID]; ok {
		wp.cancel()
		delete(o.running, taskID)
		if err := o.store.UpdateTaskStatus(taskID, store.StatusCancelled); err != nil {
			mlog.Error("CancelTask UpdateTaskStatus", slog.String("task", taskID), slog.String("err", err.Error()))
		}
		if err := o.store.CancelTaskSubtasks(taskID); err != nil {
			mlog.Error("CancelTask CancelTaskSubtasks", slog.String("task", taskID), slog.String("err", err.Error()))
		}
	}
}

// RateLimiter returns the shared rate limiter for use by other components (e.g. architect).
func (o *Orchestrator) RateLimiter() *RateLimiter {
	return o.rateLimiter
}

func (o *Orchestrator) ActiveCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.running)
}

// GitAvailable returns true if git is installed and cwd is inside a git repo.
func GitAvailable(cwd string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = cwd
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// GitSnapshot creates a ref to the current working tree state.
// Returns empty string if git is not available.
func GitSnapshot(cwd string) string {
	cmd := exec.Command("git", "stash", "create")
	cmd.Dir = cwd
	out, err := cmd.Output()
	ref := strings.TrimSpace(string(out))
	if err != nil || ref == "" {
		cmd2 := exec.Command("git", "rev-parse", "HEAD")
		cmd2.Dir = cwd
		out2, err2 := cmd2.Output()
		if err2 != nil {
			mlog.Error("GitSnapshot rev-parse", slog.String("err", err2.Error()))
		}
		return strings.TrimSpace(string(out2))
	}
	return ref
}

// maxDiffSize caps stored diffs to avoid unbounded memory/DB usage.
const maxDiffSize = 512 * 1024 // 512KB

// GitDiffBetween returns the diff between two refs.
func GitDiffBetween(cwd, from, to string) string {
	cmd := exec.Command("git", "diff", from, to)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		mlog.Error("GitDiffBetween", slog.String("err", err.Error()))
	}
	if len(out) > maxDiffSize {
		return string(out[:maxDiffSize]) + "\n…(diff truncated)"
	}
	return string(out)
}

// GitDiffHead returns "git diff HEAD" — all staged and unstaged changes vs last commit.
func GitDiffHead(cwd string) string {
	cmd := exec.Command("git", "diff", "HEAD")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	if len(out) > maxDiffSize {
		return string(out[:maxDiffSize]) + "\n…(diff truncated)"
	}
	return string(out)
}

// GitStageFile runs "git add <file>" in cwd.
func GitStageFile(cwd, file string) error {
	cmd := exec.Command("git", "add", file)
	cmd.Dir = cwd
	return cmd.Run()
}

// GitUnstageFile runs "git restore --staged <file>" in cwd.
func GitUnstageFile(cwd, file string) error {
	cmd := exec.Command("git", "restore", "--staged", file)
	cmd.Dir = cwd
	return cmd.Run()
}

// GitRevertFile discards working tree changes to a file ("git checkout -- <file>").
func GitRevertFile(cwd, file string) error {
	cmd := exec.Command("git", "checkout", "--", file)
	cmd.Dir = cwd
	return cmd.Run()
}

// GitCommit runs "git commit -m <msg>" in cwd.
func GitCommit(cwd, msg string) error {
	cmd := exec.Command("git", "commit", "-m", msg)
	cmd.Dir = cwd
	return cmd.Run()
}

// GitStagedFiles returns the set of file paths currently in the staging area.
func GitStagedFiles(cwd string) map[string]bool {
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	result := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			result[line] = true
		}
	}
	return result
}

// GitWorktreeReset resets a worktree to HEAD, discarding all staged and unstaged changes.
func GitWorktreeReset(wtPath string) error {
	cmd := exec.Command("git", "reset", "--hard", "HEAD")
	cmd.Dir = wtPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git reset --hard: %w: %s", err, strings.TrimSpace(string(out)))
	}
	cmd2 := exec.Command("git", "clean", "-fd")
	cmd2.Dir = wtPath
	if out, err := cmd2.CombinedOutput(); err != nil {
		return fmt.Errorf("git clean -fd: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// GitStageAll stages all changes including untracked files in cwd.
func GitStageAll(cwd string) error {
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = cwd
	return cmd.Run()
}


// GitWorktreeAdd creates a detached worktree at path based on HEAD.
func GitWorktreeAdd(cwd, path string) error {
	cmd := exec.Command("git", "worktree", "add", "--detach", path, "HEAD")
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// GitWorktreeRemove removes a worktree forcefully.
func GitWorktreeRemove(cwd, path string) error {
	cmd := exec.Command("git", "worktree", "remove", "--force", path)
	cmd.Dir = cwd
	return cmd.Run()
}

// GitCommitWorktree stages all changes and commits in the worktree, returning the commit SHA.
func GitCommitWorktree(wtPath, message string) (string, error) {
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = wtPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git commit: %w: %s", err, strings.TrimSpace(string(out)))
	}
	// Get the SHA of the commit we just made.
	cmd2 := exec.Command("git", "rev-parse", "HEAD")
	cmd2.Dir = wtPath
	sha, err := cmd2.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(sha)), nil
}

// GitCherryPick cherry-picks a commit SHA into cwd.
func GitCherryPick(cwd, sha string) error {
	cmd := exec.Command("git", "cherry-pick", "--no-commit", sha)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git cherry-pick: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// GitCherryPickAbort aborts an in-progress cherry-pick.
func GitCherryPickAbort(cwd string) error {
	cmd := exec.Command("git", "cherry-pick", "--abort")
	cmd.Dir = cwd
	return cmd.Run()
}

// cleanupOrphanWorktrees removes any leftover worktrees from a previous crash.
func (o *Orchestrator) cleanupOrphanWorktrees() {
	wtDir := filepath.Join(o.menaceDir, "worktrees")
	entries, err := os.ReadDir(wtDir)
	if err != nil {
		return // directory doesn't exist yet — fine
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(wtDir, e.Name())
		if err := GitWorktreeRemove(o.cwd, path); err != nil {
			// Worktree may already be gone from git's perspective; remove the dir directly.
			_ = os.RemoveAll(path)
			mlog.Info("cleaned up orphan worktree (dir removed)", slog.String("path", path))
		} else {
			mlog.Info("cleaned up orphan worktree", slog.String("path", path))
		}
	}
}
