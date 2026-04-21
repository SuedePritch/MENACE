package tui

import (
	"os"
	"path/filepath"
	"testing"

	"menace/internal/engine"
	"menace/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSaveAndListTasks(t *testing.T) {
	s := testStore(t)

	task := store.TaskData{
		ID:          "t1",
		ProjectID:   "proj1",
		Description: "First task",
		Instruction: "Do the thing",
		Status:      store.StatusPending,
		Subtasks: []store.SubtaskData{
			{ID: "t1-1", Seq: 1, Description: "Step 1", Status: store.StatusPending},
			{ID: "t1-2", Seq: 2, Description: "Step 2", Status: store.StatusPending},
		},
	}

	if err := s.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	tasks, err := s.ListTasks("proj1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Description != "First task" {
		t.Fatalf("expected 'First task', got '%s'", tasks[0].Description)
	}
	if len(tasks[0].Subtasks) != 2 {
		t.Fatalf("expected 2 subtasks, got %d", len(tasks[0].Subtasks))
	}
	if tasks[0].Subtasks[0].Description != "Step 1" {
		t.Fatalf("expected 'Step 1', got '%s'", tasks[0].Subtasks[0].Description)
	}
}

func TestGetTask(t *testing.T) {
	s := testStore(t)
	s.SaveTask(store.TaskData{ID: "t1", ProjectID: "p1", Description: "Task", Status: store.StatusPending})

	got, err := s.GetTask("t1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected non-nil task")
	}
	if got.Description != "Task" {
		t.Fatalf("expected 'Task', got '%s'", got.Description)
	}

	notFound, err := s.GetTask("nonexistent")
	if err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if notFound != nil {
		t.Fatal("expected nil for nonexistent task")
	}
}

func TestUpdateTaskStatus(t *testing.T) {
	s := testStore(t)
	s.SaveTask(store.TaskData{ID: "t1", ProjectID: "p1", Description: "Task", Status: store.StatusPending})

	s.UpdateTaskStatus("t1", store.StatusRunning)
	got, err := s.GetTask("t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusRunning {
		t.Fatalf("expected running, got %s", got.Status)
	}
	if got.StartedAt == nil {
		t.Fatal("expected started_at to be set")
	}

	s.UpdateTaskStatus("t1", store.StatusDone)
	got, err = s.GetTask("t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusDone {
		t.Fatalf("expected done, got %s", got.Status)
	}
	if got.CompletedAt == nil {
		t.Fatal("expected completed_at to be set")
	}
}

func TestUpdateSubtaskStatus(t *testing.T) {
	s := testStore(t)
	s.SaveTask(store.TaskData{
		ID: "t1", ProjectID: "p1", Description: "Task", Status: store.StatusPending,
		Subtasks: []store.SubtaskData{
			{ID: "t1-1", Seq: 1, Description: "Sub", Status: store.StatusPending},
		},
	})

	s.UpdateSubtaskStatus("t1-1", store.StatusDone)
	got, err := s.GetTask("t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Subtasks[0].Status != store.StatusDone {
		t.Fatalf("expected subtask done, got %s", got.Subtasks[0].Status)
	}
}

func TestRemoveTask(t *testing.T) {
	s := testStore(t)
	s.SaveTask(store.TaskData{
		ID: "t1", ProjectID: "p1", Description: "Task", Status: store.StatusPending,
		Subtasks: []store.SubtaskData{{ID: "t1-1", Seq: 1, Description: "Sub", Status: store.StatusPending}},
	})

	s.RemoveTask("t1")
	removed, err := s.GetTask("t1")
	if err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if removed != nil {
		t.Fatal("expected task to be deleted")
	}
	tasks, err := s.ListTasks("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestClearFinishedTasks(t *testing.T) {
	s := testStore(t)
	s.SaveTask(store.TaskData{ID: "t1", ProjectID: "p1", Description: "Done", Status: store.StatusDone})
	s.SaveTask(store.TaskData{ID: "t2", ProjectID: "p1", Description: "Failed", Status: store.StatusFailed})
	s.SaveTask(store.TaskData{ID: "t3", ProjectID: "p1", Description: "Pending", Status: store.StatusPending})
	s.SaveTask(store.TaskData{ID: "t4", ProjectID: "p1", Description: "Running", Status: store.StatusRunning})

	s.ClearFinishedTasks("p1")
	tasks, err := s.ListTasks("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 remaining tasks, got %d", len(tasks))
	}
	for _, task := range tasks {
		if task.Status == store.StatusDone || task.Status == store.StatusFailed {
			t.Fatalf("finished task should have been cleared: %s", task.ID)
		}
	}
}

func TestAddTask(t *testing.T) {
	s := testStore(t)
	added, err := engine.AddTask(s, "p1", "s1", "New task", "Do stuff", []store.ProposalSubtask{
		{Description: "step 1"}, {Description: "step 2"},
	})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	if added.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if len(added.Subtasks) != 2 {
		t.Fatalf("expected 2 subtasks, got %d", len(added.Subtasks))
	}

	tasks, err := s.ListTasks("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].SessionID != "s1" {
		t.Fatalf("expected session_id 's1', got '%s'", tasks[0].SessionID)
	}
}

func TestSyncTasks(t *testing.T) {
	s := testStore(t)
	s.SaveTask(store.TaskData{
		ID: "t1", ProjectID: "p1", Description: "Task", Status: store.StatusPending, Elapsed: 42,
		Subtasks: []store.SubtaskData{
			{ID: "t1-1", Seq: 1, Description: "Sub", Status: store.StatusDone},
		},
	})

	tasks := toUITasks(engine.SyncTasks(s, "p1"))
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].elapsed != 42 {
		t.Fatalf("expected elapsed 42, got %d", tasks[0].elapsed)
	}
	if len(tasks[0].subtasks) != 1 {
		t.Fatalf("expected 1 subtask, got %d", len(tasks[0].subtasks))
	}
	if tasks[0].subtasks[0].status != store.StatusDone {
		t.Fatalf("expected subtask status done")
	}
}

func TestSpecialCharacters(t *testing.T) {
	s := testStore(t)

	desc := `Description with "quotes" and : colons and newlines` + "\n" + `and more`
	instruction := "Line 1\nLine 2\nLine 3"

	s.SaveTask(store.TaskData{
		ID: "t1", ProjectID: "p1", Description: desc, Instruction: instruction, Status: store.StatusPending,
	})

	got, err := s.GetTask("t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != desc {
		t.Fatalf("description mismatch: got '%s'", got.Description)
	}
	if got.Instruction != instruction {
		t.Fatalf("instruction mismatch: got '%s'", got.Instruction)
	}
}

func TestUpdateTaskElapsed(t *testing.T) {
	s := testStore(t)
	s.SaveTask(store.TaskData{ID: "t1", ProjectID: "p1", Description: "Task", Status: store.StatusRunning})

	s.UpdateTaskElapsed("t1", 120)
	got, err := s.GetTask("t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Elapsed != 120 {
		t.Fatalf("expected elapsed 120, got %d", got.Elapsed)
	}
}

func TestListTasksIsolation(t *testing.T) {
	s := testStore(t)
	s.SaveTask(store.TaskData{ID: "t1", ProjectID: "p1", Description: "Proj 1", Status: store.StatusPending})
	s.SaveTask(store.TaskData{ID: "t2", ProjectID: "p2", Description: "Proj 2", Status: store.StatusPending})

	p1Tasks, err := s.ListTasks("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(p1Tasks) != 1 {
		t.Fatalf("expected 1 task for p1, got %d", len(p1Tasks))
	}
	p2Tasks, err := s.ListTasks("p2")
	if err != nil {
		t.Fatal(err)
	}
	if len(p2Tasks) != 1 {
		t.Fatalf("expected 1 task for p2, got %d", len(p2Tasks))
	}
}

func TestNoFileCreated(t *testing.T) {
	s := testStore(t)
	dir := t.TempDir()

	engine.AddTask(s, "p1", "", "Task", "", nil) //nolint:errcheck
	s.UpdateTaskStatus("t-fake", store.StatusDone)
	engine.SyncTasks(s, "p1")

	_, err := os.Stat(filepath.Join(dir, "tasks.json"))
	if err == nil {
		t.Fatal("tasks.json should not exist — everything is in SQLite")
	}
	_, err = os.Stat(filepath.Join(dir, "tasks.yaml"))
	if err == nil {
		t.Fatal("tasks.yaml should not exist")
	}
}
