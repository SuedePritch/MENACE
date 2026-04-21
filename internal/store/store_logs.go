package store

import (
	"database/sql"
	"log/slog"
	"strings"

	mlog "menace/internal/log"
)

// ─── Task log methods ───────────────────────────────────────────────────

// maxLogLineSize caps individual log lines to prevent unbounded disk usage.
const maxLogLineSize = 4096

func (s *Store) AppendTaskLog(taskID, line string) error {
	if len(line) > maxLogLineSize {
		line = line[:maxLogLineSize] + "…"
	}
	_, err := s.db.Exec(`INSERT INTO task_logs (task_id, line) VALUES (?, ?)`, taskID, line)
	return err
}

func (s *Store) GetTaskLogTail(taskID string, n int) string {
	rows, err := s.db.Query(
		`SELECT line FROM (SELECT line, id FROM task_logs WHERE task_id = ? ORDER BY id DESC LIMIT ?) ORDER BY id`,
		taskID, n,
	)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			mlog.Error("GetTaskLogTail scan", slog.String("err", err.Error()))
			continue
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		mlog.Error("GetTaskLogTail rows iteration", slog.String("err", err.Error()))
	}
	return strings.Join(lines, "\n")
}

func (s *Store) GetTaskLastLogLine(taskID string) string {
	var line string
	if err := s.db.QueryRow(`SELECT line FROM task_logs WHERE task_id = ? ORDER BY id DESC LIMIT 1`, taskID).Scan(&line); err != nil && err != sql.ErrNoRows {
		mlog.Error("GetTaskLastLogLine", slog.String("task", taskID), slog.String("err", err.Error()))
	}
	return line
}

// ─── Diff methods ───────────────────────────────────────────────────────

// maxDiffStoreSize caps stored diffs to prevent unbounded disk usage.
const maxDiffStoreSize = 512 * 1024 // 512KB

func (s *Store) SaveTaskDiff(taskID, subtaskID, diff string) error {
	if len(diff) > maxDiffStoreSize {
		diff = diff[:maxDiffStoreSize] + "\n…(diff truncated)"
	}
	_, err := s.db.Exec(`INSERT INTO task_diffs (task_id, subtask_id, diff) VALUES (?, ?, ?)`,
		taskID, subtaskID, diff)
	return err
}

func (s *Store) GetTaskDiff(taskID string) string {
	rows, err := s.db.Query(`SELECT diff FROM task_diffs WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var buf strings.Builder
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			mlog.Error("GetTaskDiff scan", slog.String("err", err.Error()))
			continue
		}
		buf.WriteString(d)
		buf.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		mlog.Error("GetTaskDiff rows iteration", slog.String("err", err.Error()))
	}
	return buf.String()
}

func (s *Store) GetSubtaskDiff(subtaskID string) string {
	var diff string
	if err := s.db.QueryRow(`SELECT diff FROM task_diffs WHERE subtask_id = ? ORDER BY id DESC LIMIT 1`, subtaskID).Scan(&diff); err != nil && err != sql.ErrNoRows {
		mlog.Error("GetSubtaskDiff", slog.String("subtask", subtaskID), slog.String("err", err.Error()))
	}
	return diff
}

func (s *Store) GetTaskLog(taskID string) string {
	rows, err := s.db.Query(`SELECT line FROM task_logs WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			mlog.Error("GetTaskLog scan", slog.String("err", err.Error()))
			continue
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		mlog.Error("GetTaskLog rows iteration", slog.String("err", err.Error()))
	}
	return strings.Join(lines, "\n")
}
