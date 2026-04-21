package engine

import "menace/internal/store"

// TaskReader provides read-only access to task state.
type TaskReader interface {
	ListTasks(projectID string) ([]store.TaskData, error)
	GetTask(id string) (*store.TaskData, error)
	GetTaskLastLogLine(taskID string) string
	GetProjectContext(projectID string) string
}

// TaskWriter provides mutation operations for tasks.
type TaskWriter interface {
	UpdateTaskStatus(id string, status store.TaskStatus) error
	UpdateSubtaskStatus(id string, status store.TaskStatus) error
	SaveTask(t store.TaskData) error
	SaveTaskDiff(taskID, subtaskID, diff string) error
	CancelTaskSubtasks(taskID string) error
}

// TaskLogger provides task logging operations.
type TaskLogger interface {
	AppendTaskLog(taskID, line string) error
}

// TaskStore combines all task persistence operations used by the orchestrator.
type TaskStore interface {
	TaskReader
	TaskWriter
	TaskLogger
}
