package engine

import (
	"strings"
	"testing"
	"time"

	"menace/internal/store"
)

func TestParseProposalBlocks_GarbageInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"binary noise", string([]byte{0x00, 0x01, 0xFF, 0xFE, 0x89, 0x50, 0x4E, 0x47})},
		{"huge single line", strings.Repeat("x", 10*1024*1024)},
		{"proposal tag flood", strings.Repeat("```proposal\n", 10000)},
		{"nested proposal tags", "```proposal\n```proposal\n```proposal\ndescription: nested\n```\n```\n```"},
		{"unclosed with valid yaml", "```proposal\ndescription: valid\ninstruction: but never closed"},
		{"valid then garbage", "```proposal\ndescription: ok\ninstruction: fine\n```\n" + string([]byte{0xFF, 0xFE, 0x00})},
		{"empty proposal block", "```proposal\n```"},
		{"only whitespace in block", "```proposal\n   \n\t\n```"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Must not panic
			got := ParseProposalBlocks(tt.input)
			_ = got
		})
	}
}

func TestCleanResponse_GarbageInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"binary noise", string([]byte{0x00, 0xFF, 0xFE})},
		{"huge input", strings.Repeat("a", 10*1024*1024)},
		{"mismatched backticks", "```proposal\nfoo\n``\n```\nbar\n```"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanResponse(tt.input)
			_ = got
		})
	}
}

func TestSchedulerWithEmptyTouches(t *testing.T) {
	// Tasks with no touches should never conflict
	block := make(chan struct{})
	ms := &mockStore{
		tasks: []store.TaskData{
			{ID: "t1", ProjectID: "proj1", Description: "no touches", Status: store.StatusPending, Touches: nil},
			{ID: "t2", ProjectID: "proj1", Description: "also none", Status: store.StatusPending, Touches: []string{}},
		},
		blockOnRunning: block,
	}
	o := newTestOrchestrator(ms, 4)

	o.Schedule()
	waitFor(t, 2*time.Second, func() bool { return ms.runningCount() == 2 })

	if ac := o.ActiveCount(); ac != 2 {
		t.Fatalf("expected 2 active (no touches = no conflict), got %d", ac)
	}

	close(block)
	o.Stop()
}

func TestStopIdempotent(t *testing.T) {
	ms := &mockStore{}
	o := newTestOrchestrator(ms, 2)

	// Calling Stop multiple times must not panic
	o.Stop()
	o.Stop()
}

func TestCancelNonexistentTask(t *testing.T) {
	ms := &mockStore{}
	o := newTestOrchestrator(ms, 2)

	// Must not panic
	o.CancelTask("does-not-exist")
	o.Stop()
}
