package engine

import (
	"strings"
	"testing"

	"menace/internal/store"
)

func TestGenID(t *testing.T) {
	id := GenID()
	if len(id) != 16 {
		t.Fatalf("expected 16-char hex ID, got %d chars: %s", len(id), id)
	}

	// Should be unique
	id2 := GenID()
	if id == id2 {
		t.Fatal("two GenID calls should not produce the same value")
	}
}

func TestParseProposalBlocksYAML(t *testing.T) {
	response := "Here's my plan:\n```proposal\ndescription: Add caching\ninstruction: Use Redis\nsubtasks:\n  - Setup client\n  - Add middleware\n```\nDone."

	proposals := ParseProposalBlocks(response)
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals))
	}
	if proposals[0].Description != "Add caching" {
		t.Fatalf("wrong description: %s", proposals[0].Description)
	}
	if proposals[0].Instruction != "Use Redis" {
		t.Fatalf("wrong instruction: %s", proposals[0].Instruction)
	}
	if len(proposals[0].Subtasks) != 2 {
		t.Fatalf("expected 2 subtasks, got %d", len(proposals[0].Subtasks))
	}
}

func TestParseProposalBlocksJSON(t *testing.T) {
	response := "```proposal\n{\"description\": \"Fix bug\", \"instruction\": \"patch it\", \"subtasks\": []}\n```"

	proposals := ParseProposalBlocks(response)
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals))
	}
	if proposals[0].Description != "Fix bug" {
		t.Fatalf("wrong description: %s", proposals[0].Description)
	}
}

func TestParseProposalBlocksMultiple(t *testing.T) {
	response := "```proposal\ndescription: First\ninstruction: do first\n```\ntext\n```proposal\ndescription: Second\ninstruction: do second\n```"

	proposals := ParseProposalBlocks(response)
	if len(proposals) != 2 {
		t.Fatalf("expected 2 proposals, got %d", len(proposals))
	}
}

func TestParseProposalBlocksEmpty(t *testing.T) {
	proposals := ParseProposalBlocks("just regular text, no proposals here")
	if len(proposals) != 0 {
		t.Fatalf("expected 0 proposals, got %d", len(proposals))
	}
}

func TestParseProposalBlocksMalformed(t *testing.T) {
	// No closing backticks
	proposals := ParseProposalBlocks("```proposal\ndescription: Broken\n")
	if len(proposals) != 0 {
		t.Fatalf("expected 0 proposals from malformed block, got %d", len(proposals))
	}
}

func TestCleanResponse(t *testing.T) {
	input := "Before\n```proposal\ndescription: X\n```\nAfter"
	clean := CleanResponse(input)
	if strings.Contains(clean, "```proposal") {
		t.Fatal("proposal block should be removed")
	}
	if !strings.Contains(clean, "Before") || !strings.Contains(clean, "After") {
		t.Fatal("surrounding text should be preserved")
	}
}

func TestFormatToolCall(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{"read_file", map[string]interface{}{"path": "src/main.go"}, "read_file(main.go)"},
		{"search_code", map[string]interface{}{"pattern": "TODO"}, "search_code(TODO)"},
		{"unknown_tool", map[string]interface{}{}, "unknown_tool"},
		{"list_dir", map[string]interface{}{"path": "/very/deep/nested/dir"}, "list_dir(dir)"},
	}

	for _, tt := range tests {
		got := FormatToolCall(tt.name, tt.args)
		if got != tt.want {
			t.Errorf("FormatToolCall(%s) = %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestFormatTaskResults(t *testing.T) {
	results := []store.TaskResult{
		{TaskID: "t1", Description: "Build", Status: store.StatusDone},
		{TaskID: "t2", Description: "Deploy", Status: store.StatusFailed, Error: "timeout"},
	}

	output := FormatTaskResults(results)
	if !strings.Contains(output, "=== Task Results ===") {
		t.Fatal("expected header")
	}
	if !strings.Contains(output, "[done] Build") {
		t.Fatal("expected done task")
	}
	if !strings.Contains(output, "[failed] Deploy — timeout") {
		t.Fatal("expected failed task with error")
	}
}

func TestBuildArchitectPrompt(t *testing.T) {
	history := []store.ChatMessage{
		{Role: "user", Content: "Fix the bug"},
		{Role: "architect", Content: "I'll look into it"},
	}
	results := []store.TaskResult{
		{TaskID: "t1", Description: "Patch applied", Status: store.StatusDone},
	}

	prompt := BuildArchitectPrompt(history, results)
	if !strings.Contains(prompt, "Fix the bug") {
		t.Fatal("expected user message")
	}
	if !strings.Contains(prompt, "I'll look into it") {
		t.Fatal("expected architect message")
	}
	if !strings.Contains(prompt, "Patch applied") {
		t.Fatal("expected task result")
	}
}

func TestBuildArchitectPromptNoResults(t *testing.T) {
	history := []store.ChatMessage{{Role: "user", Content: "Hello"}}
	prompt := BuildArchitectPrompt(history, nil)
	if strings.Contains(prompt, "Task Results") {
		t.Fatal("should not have results section when nil")
	}
}

func TestResolveWorkerModel(t *testing.T) {
	if got := ResolveWorkerModel(""); got != DefaultWorkerModel {
		t.Fatalf("expected default, got %s", got)
	}
	if got := ResolveWorkerModel("custom-model"); got != "custom-model" {
		t.Fatalf("expected custom-model, got %s", got)
	}
}

func TestPresetByName(t *testing.T) {
	p := PresetByName("Anthropic")
	if p == nil {
		t.Fatal("expected Anthropic preset")
	}
	if p.ArchitectProvider != "anthropic" {
		t.Fatalf("wrong provider: %s", p.ArchitectProvider)
	}

	if PresetByName("nonexistent") != nil {
		t.Fatal("expected nil for unknown preset")
	}
}

func TestNewSession(t *testing.T) {
	s := NewSession()
	if s == nil {
		t.Fatal("expected non-nil session")
	}
	if s.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if s.StartedAt.IsZero() {
		t.Fatal("expected non-zero start time")
	}
}
