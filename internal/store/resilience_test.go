package store

import (
	"strings"
	"testing"
)

func TestAppendTaskLog_LineTruncation(t *testing.T) {
	s := testStore(t)
	s.SaveTask(TaskData{ID: "t1", ProjectID: "p1", Description: "test", Status: StatusPending})

	huge := strings.Repeat("x", 100_000)
	if err := s.AppendTaskLog("t1", huge); err != nil {
		t.Fatalf("AppendTaskLog: %v", err)
	}

	last := s.GetTaskLastLogLine("t1")
	if len(last) > maxLogLineSize+10 { // +10 for the truncation marker
		t.Fatalf("expected truncated log line, got len=%d", len(last))
	}
}

func TestSaveTaskDiff_Truncation(t *testing.T) {
	s := testStore(t)
	s.SaveTask(TaskData{ID: "t1", ProjectID: "p1", Description: "test", Status: StatusPending})

	huge := strings.Repeat("+added line\n", 100_000)
	if err := s.SaveTaskDiff("t1", "", huge); err != nil {
		t.Fatalf("SaveTaskDiff: %v", err)
	}

	diff := s.GetTaskDiff("t1")
	if len(diff) > maxDiffStoreSize+100 {
		t.Fatalf("expected truncated diff, got len=%d", len(diff))
	}
}

func TestLoadSessionNonexistentReturnsNil(t *testing.T) {
	s := testStore(t)
	sess, err := s.LoadSession("ghost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess != nil {
		t.Fatal("expected nil for nonexistent session")
	}
}

func TestGetTaskNotFound(t *testing.T) {
	s := testStore(t)
	task, err := s.GetTask("nonexistent")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if task != nil {
		t.Fatal("expected nil task")
	}
}

func TestConcurrentLogWrites(t *testing.T) {
	s := testStore(t)
	s.SaveTask(TaskData{ID: "t1", ProjectID: "p1", Description: "test", Status: StatusPending})

	// Simulate concurrent workers logging to the same task
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				s.AppendTaskLog("t1", "log line")
			}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	full := s.GetTaskLog("t1")
	lines := strings.Split(full, "\n")
	if len(lines) != 1000 {
		t.Fatalf("expected 1000 log lines, got %d", len(lines))
	}
}
