package engine

import (
	"testing"
)

func TestParseProposalBlocks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLen  int
		checkIdx int // which proposal to inspect (-1 = skip)
		wantDesc string
		wantInst string
		wantSubs int
	}{
		{
			name:    "no proposal blocks",
			input:   "Here is some text without any proposals.",
			wantLen: 0,
		},
		{
			name: "single valid YAML proposal with subtasks",
			input: "Some preamble\n```proposal\ndescription: refactor the parser\ninstruction: split into smaller functions\nsubtasks:\n  - description: extract tokenizer\n    instruction: move to tokenizer.go\n  - description: extract evaluator\n    instruction: move to eval.go\n```\nSome epilogue",
			wantLen:  1,
			checkIdx: 0,
			wantDesc: "refactor the parser",
			wantInst: "split into smaller functions",
			wantSubs: 2,
		},
		{
			name:     "single valid JSON proposal",
			input:    "Text\n```proposal\n{\"description\":\"add logging\",\"instruction\":\"use slog\",\"subtasks\":[{\"description\":\"add to engine\",\"instruction\":\"wrap calls\"}]}\n```\nMore text",
			wantLen:  1,
			checkIdx: 0,
			wantDesc: "add logging",
			wantInst: "use slog",
			wantSubs: 1,
		},
		{
			name:     "multiple proposals",
			input:    "```proposal\ndescription: first\ninstruction: do first\n```\nmiddle\n```proposal\ndescription: second\ninstruction: do second\n```",
			wantLen:  2,
			checkIdx: 1,
			wantDesc: "second",
			wantInst: "do second",
			wantSubs: 0,
		},
		{
			name:    "malformed YAML/JSON inside block is skipped",
			input:   "```proposal\n{{{{not valid json or yaml: [[\n```",
			wantLen: 0,
		},
		{
			name:    "missing closing backticks is skipped",
			input:   "```proposal\ndescription: orphan\ninstruction: no closing",
			wantLen: 0,
		},
		{
			name:    "empty description is skipped",
			input:   "```proposal\ndescription: \"\"\ninstruction: something\n```",
			wantLen: 0,
		},
		{
			name: "subtasks as plain strings via UnmarshalYAML",
			input: "```proposal\ndescription: plan\ninstruction: execute\nsubtasks:\n  - just a string task\n  - another string task\n```",
			wantLen:  1,
			checkIdx: 0,
			wantDesc: "plan",
			wantInst: "execute",
			wantSubs: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseProposalBlocks(tt.input)
			if len(got) != tt.wantLen {
				t.Fatalf("got %d proposals, want %d", len(got), tt.wantLen)
			}
			if tt.checkIdx >= 0 && tt.wantLen > 0 {
				p := got[tt.checkIdx]
				if p.Description != tt.wantDesc {
					t.Errorf("description = %q, want %q", p.Description, tt.wantDesc)
				}
				if p.Instruction != tt.wantInst {
					t.Errorf("instruction = %q, want %q", p.Instruction, tt.wantInst)
				}
				if len(p.Subtasks) != tt.wantSubs {
					t.Errorf("got %d subtasks, want %d", len(p.Subtasks), tt.wantSubs)
				}
			}
		})
	}
}

func TestParseProposalBlocks_SubtaskPlainString(t *testing.T) {
	input := "```proposal\ndescription: plan\ninstruction: go\nsubtasks:\n  - plain string subtask\n```"
	got := ParseProposalBlocks(input)
	if len(got) != 1 {
		t.Fatalf("got %d proposals, want 1", len(got))
	}
	if got[0].Subtasks[0].Description != "plain string subtask" {
		t.Errorf("subtask description = %q, want %q", got[0].Subtasks[0].Description, "plain string subtask")
	}
	if got[0].Subtasks[0].Instruction != "" {
		t.Errorf("subtask instruction = %q, want empty", got[0].Subtasks[0].Instruction)
	}
}

func TestStripThinking(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no thinking unchanged",
			input: "Hello world",
			want:  "Hello world",
		},
		{
			name:  "infinity thought block",
			input: "Before ∞thought\nsome internal reasoning\n∞thought After",
			want:  "Before  After",
		},
		{
			name:  "thinking xml tags",
			input: "Before <thinking>internal stuff</thinking> After",
			want:  "Before  After",
		},
		{
			name:  "thought xml tags",
			input: "Before <thought>internal stuff</thought> After",
			want:  "Before  After",
		},
		{
			name:  "critical instruction lines",
			input: "CRITICAL INSTRUCTION 1: do something\nActual content",
			want:  "Actual content",
		},
		{
			name:  "mixed thinking and content",
			input: "∞thought\nreasoning here\n∞thought\nHere is my analysis.\nCRITICAL INSTRUCTION 2: use tools\nThe code looks good.",
			want:  "\nHere is my analysis.\nThe code looks good.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripThinking(tt.input)
			if got != tt.want {
				t.Errorf("StripThinking() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanResponse_TableDriven(t *testing.T) {
	tests := []struct {
		name string
		input string
		want  string
	}{
		{
			name:  "no proposal blocks unchanged",
			input: "Hello world",
			want:  "Hello world",
		},
		{
			name:  "one proposal block removed",
			input: "Before\n```proposal\ndescription: x\n```\nAfter",
			want:  "Before\n\nAfter",
		},
		{
			name:  "multiple proposal blocks removed",
			input: "A```proposal\nfoo\n```B```proposal\nbar\n```C",
			want:  "ABC",
		},
		{
			name:  "triple newlines collapsed",
			input: "A\n\n\nB\n\n\n\nC",
			want:  "A\n\nB\n\nC",
		},
		{
			name:  "only a proposal block gives empty result",
			input: "```proposal\ndescription: x\n```",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanResponse(tt.input)
			if got != tt.want {
				t.Errorf("CleanResponse() = %q, want %q", got, tt.want)
			}
		})
	}
}
