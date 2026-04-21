package store

import (
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenAndClose(t *testing.T) {
	s := testStore(t)
	if s == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestRegisterProject(t *testing.T) {
	s := testStore(t)

	err := s.RegisterProject("abc123", "/tmp/myproject")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}

	// Re-register should not error
	err = s.RegisterProject("abc123", "/tmp/myproject")
	if err != nil {
		t.Fatalf("re-RegisterProject: %v", err)
	}
}

func TestRegisterProjectPreservesTheme(t *testing.T) {
	s := testStore(t)

	s.RegisterProject("abc123", "/tmp/myproject")
	s.SetProjectTheme("abc123", "midnight")

	// Re-register should NOT clobber theme
	s.RegisterProject("abc123", "/tmp/myproject")

	theme := s.GetProjectTheme("abc123")
	if theme != "midnight" {
		t.Fatalf("expected theme 'midnight', got '%s'", theme)
	}
}

func TestListProjects(t *testing.T) {
	s := testStore(t)

	s.RegisterProject("aaa", "/tmp/aaa")
	time.Sleep(10 * time.Millisecond)
	s.RegisterProject("bbb", "/tmp/bbb")

	projects, err := s.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
	// Most recent first
	if projects[0].ID != "bbb" {
		t.Fatalf("expected bbb first (most recent), got %s", projects[0].ID)
	}
	if projects[0].Name != "bbb" {
		t.Fatalf("expected name 'bbb', got '%s'", projects[0].Name)
	}
}

func TestProjectTheme(t *testing.T) {
	s := testStore(t)
	s.RegisterProject("abc", "/tmp/abc")

	// Default is empty
	if theme := s.GetProjectTheme("abc"); theme != "" {
		t.Fatalf("expected empty theme, got '%s'", theme)
	}

	s.SetProjectTheme("abc", "catppuccin")
	if theme := s.GetProjectTheme("abc"); theme != "catppuccin" {
		t.Fatalf("expected 'catppuccin', got '%s'", theme)
	}

	// Non-existent project returns empty
	if theme := s.GetProjectTheme("nonexistent"); theme != "" {
		t.Fatalf("expected empty for nonexistent, got '%s'", theme)
	}
}

func TestProjectContext(t *testing.T) {
	s := testStore(t)
	s.RegisterProject("abc", "/tmp/abc")

	if ctx := s.GetProjectContext("abc"); ctx != "" {
		t.Fatalf("expected empty context, got '%s'", ctx)
	}

	s.SetProjectContext("abc", "This is a Go project")
	if ctx := s.GetProjectContext("abc"); ctx != "This is a Go project" {
		t.Fatalf("expected context, got '%s'", ctx)
	}
}

func TestSessionSaveAndLoad(t *testing.T) {
	s := testStore(t)
	s.RegisterProject("p1", "/tmp/p1")

	sess := &Session{
		ID:        "sess1",
		StartedAt: time.Now(),
		Chat: []ChatMessage{
			{Role: "user", Content: "Hello architect"},
			{Role: "architect", Content: "How can I help?"},
		},
	}

	if err := s.SaveSession("p1", sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	loaded, err := s.LoadSession("sess1")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil session")
	}
	if len(loaded.Chat) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(loaded.Chat))
	}
	if loaded.Chat[0].Content != "Hello architect" {
		t.Fatalf("expected first message content, got '%s'", loaded.Chat[0].Content)
	}
}

func TestSessionSummaryFromFirstMessage(t *testing.T) {
	s := testStore(t)
	s.RegisterProject("p1", "/tmp/p1")

	sess := &Session{
		ID:        "sess1",
		StartedAt: time.Now(),
		Chat: []ChatMessage{
			{Role: "user", Content: "Refactor the auth module"},
		},
	}
	s.SaveSession("p1", sess)

	summaries, err := s.ListSessions("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 session, got %d", len(summaries))
	}
	if summaries[0].Summary != "Refactor the auth module" {
		t.Fatalf("expected summary from first message, got '%s'", summaries[0].Summary)
	}
}

func TestListAllSessions(t *testing.T) {
	s := testStore(t)
	s.RegisterProject("p1", "/tmp/p1")
	s.RegisterProject("p2", "/tmp/p2")

	s.SaveSession("p1", &Session{ID: "s1", StartedAt: time.Now(), Chat: []ChatMessage{{Role: "user", Content: "msg1"}}})
	s.SaveSession("p2", &Session{ID: "s2", StartedAt: time.Now(), Chat: []ChatMessage{{Role: "user", Content: "msg2"}}})

	all, err := s.ListAllSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 sessions across projects, got %d", len(all))
	}
}

func TestLoadSessionPopulatesIDs(t *testing.T) {
	s := testStore(t)
	s.RegisterProject("p1", "/tmp/p1")
	s.SaveSession("p1", &Session{ID: "sess1", StartedAt: time.Now()})

	// Create a proposal and task tied to the session
	s.SaveProposal("p1", "sess1", Proposal{ID: "prop1", Description: "test proposal"})
	s.SaveTask(TaskData{ID: "task1", ProjectID: "p1", SessionID: "sess1", Description: "test task", Status: StatusPending})

	loaded, err := s.LoadSession("sess1")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected session")
	}
	if len(loaded.ProposalIDs) != 1 || loaded.ProposalIDs[0] != "prop1" {
		t.Fatalf("expected proposal ID, got %v", loaded.ProposalIDs)
	}
	if len(loaded.TaskIDs) != 1 || loaded.TaskIDs[0] != "task1" {
		t.Fatalf("expected task ID, got %v", loaded.TaskIDs)
	}
}

func TestLoadSessionNonexistent(t *testing.T) {
	s := testStore(t)
	sess, err := s.LoadSession("nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess != nil {
		t.Fatal("expected nil for nonexistent session")
	}
}

func TestProposalCRUD(t *testing.T) {
	s := testStore(t)
	s.RegisterProject("p1", "/tmp/p1")

	p := Proposal{
		ID:          "prop1",
		Description: "Add caching",
		Instruction: "Use Redis",
		Subtasks: []ProposalSubtask{
			{ID: "sub1", Seq: 1, Description: "Setup Redis client"},
			{ID: "sub2", Seq: 2, Description: "Add cache layer"},
		},
	}

	if err := s.SaveProposal("p1", "sess1", p); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	proposals, err := s.LoadProposals("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals))
	}
	if proposals[0].Description != "Add caching" {
		t.Fatalf("wrong description: %s", proposals[0].Description)
	}
	if len(proposals[0].Subtasks) != 2 {
		t.Fatalf("expected 2 subtasks, got %d", len(proposals[0].Subtasks))
	}

	s.DeleteProposal("prop1")
	remaining, err := s.LoadProposals("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatal("proposal should be deleted")
	}
}

func TestClearProposals(t *testing.T) {
	s := testStore(t)
	s.RegisterProject("p1", "/tmp/p1")

	s.SaveProposal("p1", "", Proposal{ID: "a", Description: "first"})
	s.SaveProposal("p1", "", Proposal{ID: "b", Description: "second"})

	s.ClearProposals("p1")
	cleared, err := s.LoadProposals("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared) != 0 {
		t.Fatal("expected all proposals cleared")
	}
}

func TestTaskLogMethods(t *testing.T) {
	s := testStore(t)
	s.RegisterProject("p1", "/tmp/p1")
	s.SaveTask(TaskData{ID: "t1", ProjectID: "p1", Description: "test", Status: StatusPending})

	s.AppendTaskLog("t1", "line 1")
	s.AppendTaskLog("t1", "line 2")
	s.AppendTaskLog("t1", "line 3")

	full := s.GetTaskLog("t1")
	if full != "line 1\nline 2\nline 3" {
		t.Fatalf("unexpected full log: %s", full)
	}

	tail := s.GetTaskLogTail("t1", 2)
	if tail != "line 2\nline 3" {
		t.Fatalf("unexpected tail: %s", tail)
	}

	last := s.GetTaskLastLogLine("t1")
	if last != "line 3" {
		t.Fatalf("unexpected last line: %s", last)
	}
}

func TestDiffMethods(t *testing.T) {
	s := testStore(t)
	s.RegisterProject("p1", "/tmp/p1")
	s.SaveTask(TaskData{ID: "t1", ProjectID: "p1", Description: "test", Status: StatusPending})

	s.SaveTaskDiff("t1", "sub1", "diff content for sub1")
	s.SaveTaskDiff("t1", "sub2", "diff content for sub2")

	combined := s.GetTaskDiff("t1")
	if combined == "" {
		t.Fatal("expected combined diff")
	}

	subDiff := s.GetSubtaskDiff("sub1")
	if subDiff != "diff content for sub1" {
		t.Fatalf("unexpected subtask diff: %s", subDiff)
	}
}

func TestCancelTaskSubtasks(t *testing.T) {
	s := testStore(t)
	s.SaveTask(TaskData{
		ID: "t1", ProjectID: "p1", Description: "test", Status: StatusRunning,
		Subtasks: []SubtaskData{
			{ID: "s1", Seq: 1, Description: "a", Status: StatusPending},
			{ID: "s2", Seq: 2, Description: "b", Status: StatusRunning},
			{ID: "s3", Seq: 3, Description: "c", Status: StatusDone},
		},
	})

	if err := s.CancelTaskSubtasks("t1"); err != nil {
		t.Fatalf("CancelTaskSubtasks: %v", err)
	}

	task, err := s.GetTask("t1")
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range task.Subtasks {
		if sub.Status != StatusCancelled {
			t.Fatalf("subtask %s should be cancelled, got %s", sub.ID, sub.Status)
		}
	}
}

func TestAuthRoundTrip(t *testing.T) {
	s := testStore(t)

	// No auth initially
	auth, err := s.GetAuth()
	if err != nil {
		t.Fatalf("GetAuth on empty: %v", err)
	}
	if auth != nil {
		t.Fatal("expected nil auth on empty DB")
	}

	// Save and retrieve — skip if keyring is unavailable (CI, headless Linux)
	if err := s.SaveAuth("anthropic", "sk-test-key-123", "claude-opus", "claude-haiku"); err != nil {
		t.Skipf("keyring unavailable: %v", err)
	}

	auth, err = s.GetAuth()
	if err != nil {
		t.Fatalf("GetAuth: %v", err)
	}
	if auth == nil {
		t.Fatal("expected non-nil auth")
	}
	if auth.Provider != "anthropic" {
		t.Fatalf("wrong provider: %s", auth.Provider)
	}
	if auth.APIKey != "sk-test-key-123" {
		t.Fatalf("API key round-trip failed: got %q", auth.APIKey)
	}
	if auth.ArchitectModel != "claude-opus" {
		t.Fatalf("wrong architect model: %s", auth.ArchitectModel)
	}
	if auth.WorkerModel != "claude-haiku" {
		t.Fatalf("wrong worker model: %s", auth.WorkerModel)
	}
}

func TestAuthClearAll(t *testing.T) {
	s := testStore(t)
	if err := s.SaveAuth("anthropic", "sk-key", "opus", "haiku"); err != nil {
		t.Skipf("keyring unavailable: %v", err)
	}

	if err := s.ClearAllAuth(); err != nil {
		t.Fatalf("ClearAllAuth: %v", err)
	}

	auth, err := s.GetAuth()
	if err != nil {
		t.Fatalf("GetAuth after clear: %v", err)
	}
	if auth != nil {
		t.Fatal("expected nil auth after clear")
	}
}

func TestSchemaMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Open twice to verify ALTER TABLE doesn't fail on second run
	s1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	s1.Close()

	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	s2.Close()
}
