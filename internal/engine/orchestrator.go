package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"menace/internal/agent"
	mlog "menace/internal/log"
	"menace/internal/store"
)

const (
	MaxArchitectIterations = 50
	MaxWorkerIterations    = 30

	// maxLogPreviewLen caps the length of log previews to avoid flooding logs.
	maxLogPreviewLen = 500
)

// TasksChangedMsg tells the UI to re-read task state.
type TasksChangedMsg struct{}

// TaskCompletedMsg signals a task finished.
type TaskCompletedMsg struct {
	TaskID      string
	Description string
	Status      store.TaskStatus
	ErrLine     string
}

type Orchestrator struct {
	cwd          string
	menaceDir    string
	projectID    string
	providerName string
	workerModel  string
	apiKey       string
	store        TaskStore
	maxConc      int
	maxRetry     int

	mu      sync.Mutex
	wg      sync.WaitGroup
	running map[string]*workerProc
	stopped bool
	ctx     context.Context
	cancel  context.CancelFunc

	program *tea.Program
}

type workerProc struct {
	taskID string
	cancel context.CancelFunc
	start  time.Time
}

type OrchestratorConfig struct {
	CWD           string
	MenaceDir     string
	ProjectID     string
	ProviderName  string
	WorkerModel   string
	APIKey        string
	MaxConcurrent int
	MaxRetry      int
}

func NewOrchestrator(cfg OrchestratorConfig, s TaskStore, p *tea.Program) *Orchestrator {
	ctx, cancel := context.WithCancel(context.Background())
	o := &Orchestrator{
		cwd:          cfg.CWD,
		menaceDir:    cfg.MenaceDir,
		projectID:    cfg.ProjectID,
		providerName: cfg.ProviderName,
		workerModel:  cfg.WorkerModel,
		apiKey:       cfg.APIKey,
		store:        s,
		maxConc:      cfg.MaxConcurrent,
		maxRetry:     cfg.MaxRetry,
		running:      make(map[string]*workerProc),
		ctx:          ctx,
		cancel:       cancel,
		program:      p,
	}
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

	var success bool
	for attempt := 0; attempt <= o.maxRetry; attempt++ {
		if ctx.Err() != nil {
			break
		}
		if attempt > 0 {
			o.taskLog(t.ID, "Retry %d/%d: %s", attempt, o.maxRetry, t.Description)
			for _, sub := range t.Subtasks {
				if err := o.store.UpdateSubtaskStatus(sub.ID, store.StatusPending); err != nil {
					mlog.Error("retry UpdateSubtaskStatus", slog.String("subtask", sub.ID), slog.String("err", err.Error()))
				}
			}
			o.send(TasksChangedMsg{})
		}

		if len(t.Subtasks) > 0 {
			success = true
			fresh, err := o.store.GetTask(t.ID)
			if err != nil {
				if !errors.Is(err, store.ErrNotFound) {
					mlog.Error("runTask GetTask", slog.String("task", t.ID), slog.String("err", err.Error()))
				}
				success = false
				break
			}
			for _, sub := range fresh.Subtasks {
				if ctx.Err() != nil {
					success = false
					break
				}
				if sub.Status == store.StatusDone {
					o.taskLog(t.ID, "Skipping (done): %s", sub.Description)
					continue
				}
				ok := o.executeAndReview(ctx, t, &sub)
				if !ok {
					success = false
					break
				}
			}
		} else {
			success = o.executeAndReview(ctx, t, nil)
		}
		if success {
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
	} else if success {
		finalStatus = store.StatusDone
		if err := o.store.UpdateTaskStatus(t.ID, store.StatusDone); err != nil {
			mlog.Error("runTask final UpdateTaskStatus", slog.String("task", t.ID), slog.String("err", err.Error()))
		}
		o.taskLog(t.ID, "Done.")
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

func (o *Orchestrator) executeAndReview(ctx context.Context, t store.TaskData, sub *store.SubtaskData) bool {
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

	preRef := GitSnapshot(o.cwd)
	mlog.Debug("diff capture preRef", slog.String("task", t.ID), slog.String("ref", preRef))
	agentOk := o.runAgent(ctx, t.ID, "worker", prompt)

	postRef := GitSnapshot(o.cwd)
	mlog.Debug("diff capture postRef", slog.String("task", t.ID), slog.String("ref", postRef), slog.Bool("same", preRef == postRef))
	if preRef != "" && postRef != "" {
		diff := GitDiffBetween(o.cwd, preRef, postRef)
		mlog.Debug("diff capture diff", slog.String("task", t.ID), slog.Int("len", len(diff)))
		if diff != "" {
			subID := ""
			if sub != nil {
				subID = sub.ID
			}
			if err := o.store.SaveTaskDiff(t.ID, subID, diff); err != nil {
				mlog.Error("diff capture save", slog.String("task", t.ID), slog.String("err", err.Error()))
			} else {
				mlog.Debug("diff capture saved", slog.String("task", t.ID), slog.String("subtask", subID))
			}
		}
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
		return false
	}

	o.taskLog(t.ID, "Done: %s", desc)
	if sub != nil {
		if err := o.store.UpdateSubtaskStatus(id, store.StatusDone); err != nil {
			mlog.Error("UpdateSubtaskStatus done", slog.String("subtask", id), slog.String("err", err.Error()))
		}
	} else {
		if err := o.store.UpdateTaskStatus(id, store.StatusDone); err != nil {
			mlog.Error("UpdateTaskStatus done", slog.String("task", id), slog.String("err", err.Error()))
		}
	}
	o.send(TasksChangedMsg{})
	return true
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

func (o *Orchestrator) runAgent(ctx context.Context, taskID, agentType, prompt string) bool {
	mlog.Debug("worker prompt", slog.String("task", taskID), slog.Int("prompt_len", len(prompt)))

	systemPrompt := LoadSystemPrompt(o.menaceDir, agentType)

	if o.apiKey == "" {
		o.taskLog(taskID, "No API key for provider %q", o.providerName)
		return false
	}

	workerTools := agent.WriteTools(o.cwd)

	ag, err := agent.NewAgent(o.providerName, o.workerModel, o.apiKey, systemPrompt, workerTools, MaxWorkerIterations)
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

	mlog.Info("worker agent started", slog.String("task", taskID), slog.String("provider", o.providerName), slog.String("model", o.workerModel), slog.Int("tools", len(workerTools)))

	fullText, err := ag.Run(ctx, prompt)
	_ = fullText

	if ctx.Err() != nil {
		o.taskLog(taskID, "Cancelled")
		return false
	}

	if err != nil {
		o.taskLog(taskID, "Agent error: %v", err)
		mlog.Error("agent error", slog.String("type", agentType), slog.String("err", err.Error()))
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
