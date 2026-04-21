package store

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
)

type TaskStatus string

const (
	StatusPending   TaskStatus = "pending"
	StatusQueued    TaskStatus = "queued"
	StatusRunning   TaskStatus = "running"
	StatusDone      TaskStatus = "done"
	StatusFailed    TaskStatus = "failed"
	StatusStalled   TaskStatus = "stalled"
	StatusCancelled TaskStatus = "cancelled"
)

// ChatMessage represents a single chat message.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// TaskData is the DB-facing representation of a task.
type TaskData struct {
	ID          string        `json:"id"`
	ProposalID  string        `json:"proposal_id,omitempty"`
	SessionID   string        `json:"session_id,omitempty"`
	ProjectID   string        `json:"project_id"`
	Description string        `json:"description"`
	Instruction string        `json:"instruction,omitempty"`
	Status      TaskStatus    `json:"status"`
	Touches     []string      `json:"touches,omitempty"`
	StartedAt   *time.Time    `json:"started_at,omitempty"`
	CompletedAt *time.Time    `json:"completed_at,omitempty"`
	Elapsed     int           `json:"elapsed,omitempty"`
	Subtasks    []SubtaskData `json:"subtasks,omitempty"`
}

// SubtaskData is the DB-facing representation of a subtask.
type SubtaskData struct {
	ID          string     `json:"id"`
	Seq         int        `json:"seq"`
	Description string     `json:"description"`
	Instruction string     `json:"instruction,omitempty"`
	Status      TaskStatus `json:"status"`
}

// Proposal represents a stored proposal.
type Proposal struct {
	ID          string
	Description string
	Instruction string
	Subtasks    []ProposalSubtask
}

// ProposalSubtask represents a stored proposal subtask.
type ProposalSubtask struct {
	ID          string
	Seq         int
	Description string
	Instruction string
}

// Session represents a chat session.
type Session struct {
	ID          string        `json:"id"`
	StartedAt   time.Time     `json:"started_at"`
	Chat        []ChatMessage `json:"chat"`
	ProposalIDs []string      `json:"proposal_ids"`
	TaskIDs     []string      `json:"task_ids"`
	Results     []TaskResult  `json:"results"`
	ResultsSent int           `json:"results_sent"`
}

// TaskResult is a compact summary of a completed task, used for architect feedback.
type TaskResult struct {
	TaskID      string     `json:"task_id"`
	Description string     `json:"description"`
	Status      TaskStatus `json:"status"`
	Error       string     `json:"error,omitempty"`
}

// ProjectEntry represents a registered project.
type ProjectEntry struct {
	ID       string
	Path     string
	Name     string
	Theme    string
	LastUsed time.Time
}

// SessionSummary is a lightweight representation for session lists.
type SessionSummary struct {
	ID          string    `json:"id"`
	StartedAt   time.Time `json:"started_at"`
	Summary     string    `json:"summary"`
	Tasks       int       `json:"tasks"`
	ProjectID   string    `json:"project_id"`
	ProjectName string    `json:"project_name"`
	ProjectPath string    `json:"project_path"`
}
