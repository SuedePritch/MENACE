package tui

import (
	"strings"
	"testing"

	"menace/internal/engine"
	"menace/internal/store"
)

func TestWorkflowProposalToCompletion(t *testing.T) {
	s := testStore(t)
	projectID := "test-proj"

	architectResponse := "```proposal\ndescription: Refactor auth middleware\ninstruction: |\n  Replace session token storage with encrypted cookies.\n  Update all middleware references.\nsubtasks:\n  - Extract token logic into helper\n  - Update middleware chain\n  - Add integration tests\n```"

	proposals := engine.ParseProposalBlocks(architectResponse)
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals))
	}
	p := proposals[0]
	if p.Description != "Refactor auth middleware" {
		t.Fatalf("unexpected description: %s", p.Description)
	}
	if len(p.Subtasks) != 3 {
		t.Fatalf("expected 3 subtasks, got %d", len(p.Subtasks))
	}

	sessionID := "sess-001"
	var subtasks []store.ProposalSubtask
	for i, ps := range p.Subtasks {
		subtasks = append(subtasks, store.ProposalSubtask{
			ID:          engine.GenID(),
			Seq:         i + 1,
			Description: ps.Description,
			Instruction: ps.Instruction,
		})
	}
	taskData, err := engine.AddTask(s, projectID, sessionID, p.Description, p.Instruction, subtasks)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	if taskData.ID == "" {
		t.Fatal("task ID should not be empty")
	}
	if taskData.Status != store.StatusPending {
		t.Fatalf("new task should be pending, got %s", taskData.Status)
	}
	if len(taskData.Subtasks) != 3 {
		t.Fatalf("expected 3 subtasks, got %d", len(taskData.Subtasks))
	}

	dbTask, err := s.GetTask(taskData.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dbTask == nil {
		t.Fatal("task should exist in DB")
	}
	if dbTask.SessionID != sessionID {
		t.Fatalf("expected session_id %s, got %s", sessionID, dbTask.SessionID)
	}
	if dbTask.Instruction != p.Instruction {
		t.Fatalf("instruction mismatch in DB")
	}

	pending, err := s.ListTasks(projectID)
	if err != nil {
		t.Fatal(err)
	}
	var found *store.TaskData
	for i := range pending {
		if pending[i].Status == store.StatusPending {
			found = &pending[i]
			break
		}
	}
	if found == nil {
		t.Fatal("scheduler should find a pending task")
	}
	if found.ID != taskData.ID {
		t.Fatalf("wrong task found: %s", found.ID)
	}

	s.UpdateTaskStatus(taskData.ID, store.StatusRunning)
	running, err := s.GetTask(taskData.ID)
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != store.StatusRunning {
		t.Fatalf("expected running, got %s", running.Status)
	}
	if running.StartedAt == nil {
		t.Fatal("started_at should be set when running")
	}

	for _, sub := range taskData.Subtasks {
		s.UpdateSubtaskStatus(sub.ID, store.StatusRunning)
		got, err := s.GetTask(taskData.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, gs := range got.Subtasks {
			if gs.ID == sub.ID {
				if gs.Status != store.StatusRunning {
					t.Fatalf("subtask %s should be running, got %s", sub.ID, gs.Status)
				}
				break
			}
		}
		s.UpdateSubtaskStatus(sub.ID, store.StatusDone)
	}

	s.UpdateTaskStatus(taskData.ID, store.StatusDone)
	s.UpdateTaskElapsed(taskData.ID, 45)

	completed, err := s.GetTask(taskData.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != store.StatusDone {
		t.Fatalf("expected done, got %s", completed.Status)
	}
	if completed.CompletedAt == nil {
		t.Fatal("completed_at should be set")
	}
	if completed.Elapsed != 45 {
		t.Fatalf("expected elapsed 45, got %d", completed.Elapsed)
	}
	for _, sub := range completed.Subtasks {
		if sub.Status != store.StatusDone {
			t.Fatalf("subtask %s should be done, got %s", sub.ID, sub.Status)
		}
	}

	s.AppendTaskLog(taskData.ID, "[10:00:00] Starting: Refactor auth middleware")
	s.AppendTaskLog(taskData.ID, "[10:00:01] Executing: Extract token logic into helper")
	s.AppendTaskLog(taskData.ID, "[10:00:30] Done: Extract token logic into helper")
	s.AppendTaskLog(taskData.ID, "[10:00:31] Executing: Update middleware chain")
	s.AppendTaskLog(taskData.ID, "[10:01:00] Done: Update middleware chain")
	s.AppendTaskLog(taskData.ID, "[10:01:01] Done.")

	fullLog := s.GetTaskLog(taskData.ID)
	if !strings.Contains(fullLog, "Extract token logic") {
		t.Fatal("full log should contain subtask execution lines")
	}

	tail := s.GetTaskLogTail(taskData.ID, 2)
	lines := strings.Split(tail, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 tail lines, got %d", len(lines))
	}

	lastLine := s.GetTaskLastLogLine(taskData.ID)
	if !strings.Contains(lastLine, "Done.") {
		t.Fatalf("last log line should be the final entry, got: %s", lastLine)
	}

	result := store.TaskResult{
		TaskID:      taskData.ID,
		Description: "Refactor auth middleware",
		Status:      store.StatusDone,
	}

	chatHistory := []store.ChatMessage{
		{Role: "user", Content: "Refactor the auth middleware to use encrypted cookies"},
		{Role: "architect", Content: "I'll create a proposal for that."},
		{Role: "architect", Content: "[✓ done] Refactor auth middleware"},
		{Role: "user", Content: "Great, now add rate limiting"},
	}

	prompt := engine.BuildArchitectPrompt(chatHistory, []store.TaskResult{result})

	if !strings.Contains(prompt, "=== Task Results ===") {
		t.Fatal("prompt should contain task results section")
	}
	if !strings.Contains(prompt, "[done] Refactor auth middleware") {
		t.Fatal("prompt should contain compact status line")
	}
	if strings.Contains(prompt, "Extract token logic") {
		t.Fatal("prompt should NOT contain log content")
	}
	if !strings.Contains(prompt, "Great, now add rate limiting") {
		t.Fatal("prompt should contain latest user message")
	}

	uiTasks := toUITasks(engine.SyncTasks(s, projectID))
	if len(uiTasks) != 1 {
		t.Fatalf("expected 1 UI task, got %d", len(uiTasks))
	}
	if uiTasks[0].status != store.StatusDone {
		t.Fatalf("UI task should show done, got %s", uiTasks[0].status)
	}
	if uiTasks[0].elapsed != 45 {
		t.Fatalf("UI task elapsed should be 45, got %d", uiTasks[0].elapsed)
	}
}

func TestWorkflowSubtaskFailure(t *testing.T) {
	s := testStore(t)
	projectID := "test-proj"

	taskData, err := engine.AddTask(s, projectID, "", "Deploy pipeline", "Run deploy steps",
		[]store.ProposalSubtask{
			{ID: engine.GenID(), Seq: 1, Description: "Build image"},
			{ID: engine.GenID(), Seq: 2, Description: "Push to registry"},
			{ID: engine.GenID(), Seq: 3, Description: "Rolling update"},
		})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	s.UpdateTaskStatus(taskData.ID, store.StatusRunning)
	s.UpdateSubtaskStatus(taskData.Subtasks[0].ID, store.StatusRunning)
	s.UpdateSubtaskStatus(taskData.Subtasks[0].ID, store.StatusDone)
	s.UpdateSubtaskStatus(taskData.Subtasks[1].ID, store.StatusRunning)
	s.UpdateSubtaskStatus(taskData.Subtasks[1].ID, store.StatusFailed)
	s.UpdateTaskStatus(taskData.ID, store.StatusFailed)

	task, err := s.GetTask(taskData.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != store.StatusFailed {
		t.Fatalf("expected failed, got %s", task.Status)
	}
	if task.Subtasks[0].Status != store.StatusDone {
		t.Fatalf("first subtask should be done, got %s", task.Subtasks[0].Status)
	}
	if task.Subtasks[1].Status != store.StatusFailed {
		t.Fatalf("second subtask should be failed, got %s", task.Subtasks[1].Status)
	}
	if task.Subtasks[2].Status != store.StatusPending {
		t.Fatalf("third subtask should still be pending, got %s", task.Subtasks[2].Status)
	}

	s.AppendTaskLog(taskData.ID, "[10:00:00] Executing: Push to registry")
	s.AppendTaskLog(taskData.ID, "Agent error: exit status 1: permission denied")
	s.AppendTaskLog(taskData.ID, "Failed.")

	result := store.TaskResult{
		TaskID:      taskData.ID,
		Description: "Deploy pipeline",
		Status:      store.StatusFailed,
		Error:       "Agent error: exit status 1: permission denied",
	}
	prompt := engine.BuildArchitectPrompt(
		[]store.ChatMessage{{Role: "user", Content: "Deploy the pipeline"}},
		[]store.TaskResult{result},
	)
	if !strings.Contains(prompt, "permission denied") {
		t.Fatal("architect prompt should contain short failure reason")
	}
	if !strings.Contains(prompt, "[failed]") {
		t.Fatal("architect prompt should show failed status")
	}
}

func TestWorkflowMultipleTasksConcurrent(t *testing.T) {
	s := testStore(t)
	projectID := "test-proj"

	t1, _ := engine.AddTask(s, projectID, "sess1", "Task A", "Do A", nil)
	t2, _ := engine.AddTask(s, projectID, "sess1", "Task B", "Do B", nil)
	t3, _ := engine.AddTask(s, projectID, "sess1", "Task C", "Do C", nil)

	pending, err := s.ListTasks(projectID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pending {
		if p.Status != store.StatusPending {
			t.Fatalf("all tasks should start pending, got %s", p.Status)
		}
	}

	s.UpdateTaskStatus(t1.ID, store.StatusRunning)
	s.UpdateTaskStatus(t2.ID, store.StatusRunning)

	got3, err := s.GetTask(t3.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got3.Status != store.StatusPending {
		t.Fatalf("t3 should still be pending, got %s", got3.Status)
	}

	s.UpdateTaskStatus(t1.ID, store.StatusDone)

	tasks, err := s.ListTasks(projectID)
	if err != nil {
		t.Fatal(err)
	}
	statusCounts := map[store.TaskStatus]int{}
	for _, task := range tasks {
		statusCounts[task.Status]++
	}
	if statusCounts[store.StatusDone] != 1 {
		t.Fatalf("expected 1 done, got %d", statusCounts[store.StatusDone])
	}
	if statusCounts[store.StatusRunning] != 1 {
		t.Fatalf("expected 1 running, got %d", statusCounts[store.StatusRunning])
	}
	if statusCounts[store.StatusPending] != 1 {
		t.Fatalf("expected 1 pending, got %d", statusCounts[store.StatusPending])
	}

	s.UpdateTaskStatus(t3.ID, store.StatusRunning)
	s.UpdateTaskStatus(t2.ID, store.StatusDone)
	s.UpdateTaskStatus(t3.ID, store.StatusDone)

	tasks, err = s.ListTasks(projectID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Status != store.StatusDone {
			t.Fatalf("all tasks should be done, %s is %s", task.ID, task.Status)
		}
	}
}

func TestWorkflowRetryAfterFailure(t *testing.T) {
	s := testStore(t)
	projectID := "test-proj"

	taskData, _ := engine.AddTask(s, projectID, "", "Flaky task", "Try this", nil)

	s.UpdateTaskStatus(taskData.ID, store.StatusRunning)
	s.UpdateTaskStatus(taskData.ID, store.StatusFailed)

	task, err := s.GetTask(taskData.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != store.StatusFailed {
		t.Fatalf("expected failed, got %s", task.Status)
	}

	s.UpdateTaskStatus(taskData.ID, store.StatusPending)
	task, err = s.GetTask(taskData.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != store.StatusPending {
		t.Fatalf("expected pending after retry, got %s", task.Status)
	}

	s.UpdateTaskStatus(taskData.ID, store.StatusRunning)
	s.UpdateTaskStatus(taskData.ID, store.StatusDone)

	task, err = s.GetTask(taskData.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != store.StatusDone {
		t.Fatalf("expected done, got %s", task.Status)
	}
}

func TestWorkflowClearFinished(t *testing.T) {
	s := testStore(t)
	projectID := "test-proj"

	engine.AddTask(s, projectID, "", "Done task", "", nil) //nolint:errcheck
	firstTasks, _ := s.ListTasks(projectID)
	s.UpdateTaskStatus(firstTasks[0].ID, store.StatusDone)

	engine.AddTask(s, projectID, "", "Failed task", "", nil) //nolint:errcheck
	tasks, _ := s.ListTasks(projectID)
	s.UpdateTaskStatus(tasks[1].ID, store.StatusFailed)

	running, _ := engine.AddTask(s, projectID, "", "Running task", "", nil)
	s.UpdateTaskStatus(running.ID, store.StatusRunning)

	pending, _ := engine.AddTask(s, projectID, "", "Pending task", "", nil)

	s.ClearFinishedTasks(projectID)

	remaining, _ := s.ListTasks(projectID)
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining, got %d", len(remaining))
	}

	ids := map[string]bool{}
	for _, r := range remaining {
		ids[r.ID] = true
	}
	if !ids[running.ID] {
		t.Fatal("running task should survive clear")
	}
	if !ids[pending.ID] {
		t.Fatal("pending task should survive clear")
	}
}

func TestWorkflowSessionIsolation(t *testing.T) {
	s := testStore(t)

	t1, _ := engine.AddTask(s, "proj-a", "sess-a", "Task for A", "", nil)
	t2, _ := engine.AddTask(s, "proj-b", "sess-b", "Task for B", "", nil)

	projATasks, _ := s.ListTasks("proj-a")
	projBTasks, _ := s.ListTasks("proj-b")

	if len(projATasks) != 1 || projATasks[0].ID != t1.ID {
		t.Fatal("proj-a should only see its own task")
	}
	if len(projBTasks) != 1 || projBTasks[0].ID != t2.ID {
		t.Fatal("proj-b should only see its own task")
	}

	uiA := toUITasks(engine.SyncTasks(s, "proj-a"))
	uiB := toUITasks(engine.SyncTasks(s, "proj-b"))
	if len(uiA) != 1 || uiA[0].id != t1.ID {
		t.Fatal("syncTasks for proj-a leaked")
	}
	if len(uiB) != 1 || uiB[0].id != t2.ID {
		t.Fatal("syncTasks for proj-b leaked")
	}
}

func TestWorkflowArchitectPromptEmpty(t *testing.T) {
	history := []store.ChatMessage{
		{Role: "user", Content: "Help me refactor the API"},
	}

	prompt := engine.BuildArchitectPrompt(history, nil)
	if strings.Contains(prompt, "Completed Tasks") {
		t.Fatal("prompt should not have completed tasks section when empty")
	}
	if !strings.Contains(prompt, "Help me refactor the API") {
		t.Fatal("prompt should contain user message")
	}
}
