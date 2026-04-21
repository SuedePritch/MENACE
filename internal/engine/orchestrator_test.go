package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"menace/internal/store"
)

// mockStore implements TaskStore for scheduling tests.
// When blockOnRunning is non-nil, UpdateTaskStatus blocks on it when status is Running,
// keeping tasks alive so we can inspect scheduling state.
type mockStore struct {
	mu             sync.Mutex
	tasks          []store.TaskData
	statusUpdates  []statusUpdate
	blockOnRunning chan struct{} // if set, UpdateTaskStatus(Running) blocks until closed
}

type statusUpdate struct {
	ID     string
	Status store.TaskStatus
}

func (m *mockStore) ListTasks(_ string) ([]store.TaskData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]store.TaskData, len(m.tasks))
	copy(cp, m.tasks)
	return cp, nil
}

func (m *mockStore) GetTask(id string) (*store.TaskData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tasks {
		if t.ID == id {
			cp := t
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *mockStore) UpdateTaskStatus(id string, status store.TaskStatus) error {
	m.mu.Lock()
	m.statusUpdates = append(m.statusUpdates, statusUpdate{id, status})
	for i := range m.tasks {
		if m.tasks[i].ID == id {
			m.tasks[i].Status = status
		}
	}
	ch := m.blockOnRunning
	m.mu.Unlock()

	// Block after recording, so runTask stays alive while we inspect.
	if status == store.StatusRunning && ch != nil {
		<-ch
	}
	return nil
}

func (m *mockStore) UpdateSubtaskStatus(_ string, _ store.TaskStatus) error { return nil }

func (m *mockStore) SaveTask(t store.TaskData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tasks {
		if m.tasks[i].ID == t.ID {
			m.tasks[i] = t
			return nil
		}
	}
	m.tasks = append(m.tasks, t)
	return nil
}

func (m *mockStore) SaveTaskDiff(_, _, _ string) error    { return nil }
func (m *mockStore) CancelTaskSubtasks(_ string) error   { return nil }
func (m *mockStore) AppendTaskLog(_, _ string) error     { return nil }
func (m *mockStore) GetTaskLastLogLine(_ string) string  { return "" }
func (m *mockStore) GetProjectContext(_ string) string   { return "" }

// uniqueRunningIDs returns the unique task IDs that were set to StatusRunning.
func (m *mockStore) uniqueRunningIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	var ids []string
	for _, u := range m.statusUpdates {
		if u.Status == store.StatusRunning && !seen[u.ID] {
			seen[u.ID] = true
			ids = append(ids, u.ID)
		}
	}
	return ids
}

func (m *mockStore) runningCount() int {
	return len(m.uniqueRunningIDs())
}

func (m *mockStore) getStatus(id string) store.TaskStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tasks {
		if t.ID == id {
			return t.Status
		}
	}
	return ""
}

func newTestOrchestrator(s TaskStore, maxConc int) *Orchestrator {
	ctx, cancel := context.WithCancel(context.Background())
	return &Orchestrator{
		cwd:       "/tmp",
		menaceDir: "/tmp",
		projectID: "proj1",
		store:     s,
		maxConc:   maxConc,
		maxRetry:  0,
		running:   make(map[string]*workerProc),
		ctx:       ctx,
		cancel:    cancel,
		program:   nil,
	}
}

// waitFor polls check every 5ms until it returns true or timeout expires.
func waitFor(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitFor: condition not met within %v", timeout)
}

// ---------------------------------------------------------------------------
// Conflict avoidance
// ---------------------------------------------------------------------------

func TestSchedulerConflictAvoidance(t *testing.T) {
	block := make(chan struct{})
	ms := &mockStore{
		tasks: []store.TaskData{
			{ID: "t1", ProjectID: "proj1", Description: "task1", Status: store.StatusPending, Touches: []string{"main.go"}},
			{ID: "t2", ProjectID: "proj1", Description: "task2", Status: store.StatusPending, Touches: []string{"main.go"}},
		},
		blockOnRunning: block,
	}
	o := newTestOrchestrator(ms, 4)

	o.Schedule()
	waitFor(t, 2*time.Second, func() bool { return ms.runningCount() == 1 })

	if ac := o.ActiveCount(); ac != 1 {
		t.Fatalf("expected 1 active task due to file conflict, got %d", ac)
	}

	ids := ms.uniqueRunningIDs()
	if len(ids) != 1 || ids[0] != "t1" {
		t.Fatalf("expected only t1 to start, got %v", ids)
	}

	close(block)
	o.Stop()
}

// ---------------------------------------------------------------------------
// Parallel non-conflicting
// ---------------------------------------------------------------------------

func TestSchedulerParallelNonConflicting(t *testing.T) {
	block := make(chan struct{})
	ms := &mockStore{
		tasks: []store.TaskData{
			{ID: "t1", ProjectID: "proj1", Description: "task1", Status: store.StatusPending, Touches: []string{"a.go"}},
			{ID: "t2", ProjectID: "proj1", Description: "task2", Status: store.StatusPending, Touches: []string{"b.go"}},
		},
		blockOnRunning: block,
	}
	o := newTestOrchestrator(ms, 4)

	o.Schedule()
	waitFor(t, 2*time.Second, func() bool { return ms.runningCount() == 2 })

	if ac := o.ActiveCount(); ac != 2 {
		t.Fatalf("expected 2 active tasks (no conflict), got %d", ac)
	}

	close(block)
	o.Stop()
}

// ---------------------------------------------------------------------------
// Max concurrency
// ---------------------------------------------------------------------------

func TestMaxConcurrency(t *testing.T) {
	block := make(chan struct{})
	tasks := make([]store.TaskData, 5)
	for i := range tasks {
		id := string(rune('a' + i))
		tasks[i] = store.TaskData{
			ID: "t" + id, ProjectID: "proj1", Description: "task",
			Status: store.StatusPending, Touches: []string{id + ".go"},
		}
	}
	ms := &mockStore{tasks: tasks, blockOnRunning: block}
	o := newTestOrchestrator(ms, 2)

	o.Schedule()
	waitFor(t, 2*time.Second, func() bool { return ms.runningCount() == 2 })

	if ac := o.ActiveCount(); ac != 2 {
		t.Fatalf("expected ActiveCount=2 (maxConc=2), got %d", ac)
	}

	close(block)
	o.Stop()
}

// ---------------------------------------------------------------------------
// Cancel task
// ---------------------------------------------------------------------------

func TestCancelTask(t *testing.T) {
	block := make(chan struct{})
	ms := &mockStore{
		tasks: []store.TaskData{
			{ID: "t1", ProjectID: "proj1", Description: "task1", Status: store.StatusPending, Touches: []string{"x.go"}},
		},
		blockOnRunning: block,
	}
	o := newTestOrchestrator(ms, 4)

	o.Schedule()
	waitFor(t, 2*time.Second, func() bool { return ms.runningCount() == 1 })

	o.CancelTask("t1")

	if ac := o.ActiveCount(); ac != 0 {
		t.Fatalf("expected 0 active tasks after cancel, got %d", ac)
	}

	s := ms.getStatus("t1")
	if s != store.StatusCancelled {
		t.Fatalf("expected cancelled status, got %s", s)
	}

	close(block)
	o.Stop()
}

// ---------------------------------------------------------------------------
// Skips non-pending tasks
// ---------------------------------------------------------------------------

func TestSchedulerSkipsNonPending(t *testing.T) {
	block := make(chan struct{})
	ms := &mockStore{
		tasks: []store.TaskData{
			{ID: "t1", ProjectID: "proj1", Description: "done", Status: store.StatusDone, Touches: []string{"a.go"}},
			{ID: "t2", ProjectID: "proj1", Description: "failed", Status: store.StatusFailed, Touches: []string{"b.go"}},
			{ID: "t3", ProjectID: "proj1", Description: "pending", Status: store.StatusPending, Touches: []string{"c.go"}},
		},
		blockOnRunning: block,
	}
	o := newTestOrchestrator(ms, 4)

	o.Schedule()
	waitFor(t, 2*time.Second, func() bool { return ms.runningCount() == 1 })

	ids := ms.uniqueRunningIDs()
	if len(ids) != 1 || ids[0] != "t3" {
		t.Fatalf("expected only t3 to start, got %v", ids)
	}

	close(block)
	o.Stop()
}

// ---------------------------------------------------------------------------
// Slot opening (sequential scheduling after completion)
// ---------------------------------------------------------------------------

func TestSlotOpening(t *testing.T) {
	// Don't block — let tasks complete so Schedule cascades.
	ms := &mockStore{
		tasks: []store.TaskData{
			{ID: "t1", ProjectID: "proj1", Description: "task1", Status: store.StatusPending, Touches: []string{"a.go"}},
			{ID: "t2", ProjectID: "proj1", Description: "task2", Status: store.StatusPending, Touches: []string{"b.go"}},
		},
	}
	o := newTestOrchestrator(ms, 1)

	o.Schedule()
	waitFor(t, 2*time.Second, func() bool {
		ids := ms.uniqueRunningIDs()
		foundT1, foundT2 := false, false
		for _, id := range ids {
			if id == "t1" {
				foundT1 = true
			}
			if id == "t2" {
				foundT2 = true
			}
		}
		return foundT1 && foundT2
	})

	o.Stop()
}
