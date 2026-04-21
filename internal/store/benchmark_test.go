package store

import (
	"fmt"
	"path/filepath"
	"testing"
)

func benchStore(b *testing.B) *Store {
	b.Helper()
	dir := b.TempDir()
	s, err := Open(filepath.Join(dir, "bench.db"))
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { s.Close() })
	return s
}

func BenchmarkAppendTaskLog(b *testing.B) {
	s := benchStore(b)
	s.SaveTask(TaskData{ID: "t1", ProjectID: "p1", Description: "bench", Status: StatusPending})
	line := "2025-01-01 12:00:00 [worker] executing step — reading file, analyzing AST, writing patch"

	b.ResetTimer()
	for b.Loop() {
		s.AppendTaskLog("t1", line)
	}
}

func BenchmarkListTasks(b *testing.B) {
	s := benchStore(b)
	s.RegisterProject("p1", "/tmp/p1")
	for i := 0; i < 100; i++ {
		s.SaveTask(TaskData{
			ID: fmt.Sprintf("t%d", i), ProjectID: "p1",
			Description: "task", Status: StatusPending,
			Touches: []string{fmt.Sprintf("file%d.go", i)},
		})
	}

	b.ResetTimer()
	for b.Loop() {
		s.ListTasks("p1")
	}
}

func BenchmarkSaveAndLoadSession(b *testing.B) {
	s := benchStore(b)
	s.RegisterProject("p1", "/tmp/p1")
	chat := make([]ChatMessage, 20)
	for i := range chat {
		role := "user"
		if i%2 == 1 {
			role = "architect"
		}
		chat[i] = ChatMessage{Role: role, Content: fmt.Sprintf("Message %d with some content to simulate real conversation length.", i)}
	}

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		id := fmt.Sprintf("sess%d", i)
		sess := &Session{ID: id, Chat: chat}
		s.SaveSession("p1", sess)
		s.LoadSession(id)
	}
}
