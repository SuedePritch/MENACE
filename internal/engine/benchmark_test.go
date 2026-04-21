package engine

import (
	"strings"
	"testing"

	"menace/internal/store"
)

func BenchmarkParseProposalBlocks_Small(b *testing.B) {
	input := "Some text\n```proposal\ndescription: refactor auth\ninstruction: split middleware\nsubtasks:\n  - extract tokens\n  - update handler\n```\nDone."
	b.ResetTimer()
	for b.Loop() {
		ParseProposalBlocks(input)
	}
}

func BenchmarkParseProposalBlocks_Large(b *testing.B) {
	// Simulate a chatty architect response with embedded proposals
	var sb strings.Builder
	sb.WriteString(strings.Repeat("This is a detailed explanation of the codebase. ", 200))
	for i := 0; i < 10; i++ {
		sb.WriteString("\n```proposal\ndescription: task ")
		sb.WriteString(string(rune('A' + i)))
		sb.WriteString("\ninstruction: |\n  Do something complex involving multiple files.\n  Read the existing code first.\nsubtasks:\n  - step one\n  - step two\n  - step three\n```\n")
		sb.WriteString(strings.Repeat("More discussion about the approach. ", 100))
	}
	input := sb.String()

	b.ResetTimer()
	for b.Loop() {
		ParseProposalBlocks(input)
	}
}

func BenchmarkCleanResponse(b *testing.B) {
	var sb strings.Builder
	for i := 0; i < 5; i++ {
		sb.WriteString("Explanation block.\n\n\n\n")
		sb.WriteString("```proposal\ndescription: task\ninstruction: go\n```\n")
	}
	input := sb.String()

	b.ResetTimer()
	for b.Loop() {
		CleanResponse(input)
	}
}

func BenchmarkConflictScheduling(b *testing.B) {
	// Benchmark the scheduling hot path with many tasks and overlapping touches
	ms := &mockStore{}
	for i := 0; i < 50; i++ {
		id := string(rune('a'+i/26)) + string(rune('a'+i%26))
		touches := []string{id + ".go"}
		if i%3 == 0 {
			touches = append(touches, "shared.go") // create conflicts
		}
		ms.tasks = append(ms.tasks, store.TaskData{
			ID: "t" + id, ProjectID: "proj1", Description: "task",
			Status: store.StatusPending, Touches: touches,
		})
	}

	b.ResetTimer()
	for b.Loop() {
		o := newTestOrchestrator(ms, 10)
		o.scheduleInner()
		// Reset for next iteration
		o.mu.Lock()
		o.running = make(map[string]*workerProc)
		o.mu.Unlock()
		for i := range ms.tasks {
			ms.tasks[i].Status = store.StatusPending
		}
	}
}
