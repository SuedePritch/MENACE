package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	mlog "menace/internal/log"
)

// ─── Task methods ────────────────────────────────────────────────────

func (s *Store) SaveTask(t TaskData) error {
	return s.withTx(func(tx *sql.Tx) error {
		touchesJSON, err := json.Marshal(t.Touches)
		if err != nil {
			return fmt.Errorf("marshal touches: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO tasks (id, proposal_id, session_id, project_id, description, instruction, status, touches, started_at, completed_at, elapsed)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			t.ID, t.ProposalID, t.SessionID, t.ProjectID, t.Description, t.Instruction, t.Status, string(touchesJSON), t.StartedAt, t.CompletedAt, t.Elapsed,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM subtasks WHERE task_id = ?`, t.ID); err != nil {
			return err
		}
		for _, sub := range t.Subtasks {
			if _, err := tx.Exec(
				`INSERT INTO subtasks (id, task_id, seq, description, instruction, status) VALUES (?, ?, ?, ?, ?, ?)`,
				sub.ID, t.ID, sub.Seq, sub.Description, sub.Instruction, sub.Status,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) ListTasks(projectID string) ([]TaskData, error) {
	rows, err := s.db.Query(
		`SELECT id, proposal_id, session_id, project_id, description, instruction, status, touches, started_at, completed_at, elapsed
		 FROM tasks WHERE project_id = ? ORDER BY rowid`, projectID,
	)
	if err != nil {
		mlog.Error("ListTasks", slog.String("err", err.Error()))
		return nil, fmt.Errorf("ListTasks: %w", err)
	}
	defer rows.Close()
	var tasks []TaskData
	for rows.Next() {
		var t TaskData
		var touchesJSON string
		if err := rows.Scan(&t.ID, &t.ProposalID, &t.SessionID, &t.ProjectID, &t.Description, &t.Instruction, &t.Status, &touchesJSON, &t.StartedAt, &t.CompletedAt, &t.Elapsed); err != nil {
			mlog.Error("ListTasks scan", slog.String("err", err.Error()))
			continue
		}
		if err := json.Unmarshal([]byte(touchesJSON), &t.Touches); err != nil {
			mlog.Error("ListTasks unmarshal touches", slog.String("task", t.ID), slog.String("err", err.Error()))
		}
		subs, err := s.listSubtasks(t.ID)
		if err != nil {
			return tasks, fmt.Errorf("ListTasks: subtasks for %s: %w", t.ID, err)
		}
		t.Subtasks = subs
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		mlog.Error("ListTasks rows iteration", slog.String("err", err.Error()))
		return tasks, fmt.Errorf("ListTasks: rows: %w", err)
	}
	return tasks, nil
}

func (s *Store) listSubtasks(taskID string) ([]SubtaskData, error) {
	rows, err := s.db.Query(
		`SELECT id, seq, description, instruction, status FROM subtasks WHERE task_id = ? ORDER BY seq`, taskID,
	)
	if err != nil {
		mlog.Error("listSubtasks", slog.String("err", err.Error()))
		return nil, fmt.Errorf("listSubtasks: %w", err)
	}
	defer rows.Close()
	var subs []SubtaskData
	for rows.Next() {
		var sub SubtaskData
		if err := rows.Scan(&sub.ID, &sub.Seq, &sub.Description, &sub.Instruction, &sub.Status); err != nil {
			mlog.Error("listSubtasks scan", slog.String("err", err.Error()))
			continue
		}
		subs = append(subs, sub)
	}
	if err := rows.Err(); err != nil {
		mlog.Error("listSubtasks rows iteration", slog.String("err", err.Error()))
		return subs, fmt.Errorf("listSubtasks: rows: %w", err)
	}
	return subs, nil
}

func (s *Store) GetTask(id string) (*TaskData, error) {
	var t TaskData
	var touchesJSON string
	err := s.db.QueryRow(
		`SELECT id, proposal_id, session_id, project_id, description, instruction, status, touches, started_at, completed_at, elapsed
		 FROM tasks WHERE id = ?`, id,
	).Scan(&t.ID, &t.ProposalID, &t.SessionID, &t.ProjectID, &t.Description, &t.Instruction, &t.Status, &touchesJSON, &t.StartedAt, &t.CompletedAt, &t.Elapsed)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		mlog.Error("GetTask", slog.String("err", err.Error()))
		return nil, fmt.Errorf("GetTask: %w", err)
	}
	if err := json.Unmarshal([]byte(touchesJSON), &t.Touches); err != nil {
		mlog.Error("GetTask unmarshal touches", slog.String("task", t.ID), slog.String("err", err.Error()))
	}
	subs, err := s.listSubtasks(t.ID)
	if err != nil {
		return nil, fmt.Errorf("GetTask: subtasks for %s: %w", t.ID, err)
	}
	t.Subtasks = subs
	return &t, nil
}

func (s *Store) UpdateTaskStatus(id string, status TaskStatus) error {
	now := time.Now().UTC()
	switch status {
	case StatusRunning:
		_, err := s.db.Exec(`UPDATE tasks SET status = ?, started_at = ? WHERE id = ?`, status, now, id)
		return err
	case StatusDone, StatusFailed, StatusCancelled:
		_, err := s.db.Exec(`UPDATE tasks SET status = ?, completed_at = ? WHERE id = ?`, status, now, id)
		return err
	default:
		_, err := s.db.Exec(`UPDATE tasks SET status = ? WHERE id = ?`, status, id)
		return err
	}
}

func (s *Store) UpdateSubtaskStatus(id string, status TaskStatus) error {
	_, err := s.db.Exec(`UPDATE subtasks SET status = ? WHERE id = ?`, status, id)
	return err
}

func (s *Store) CancelTaskSubtasks(taskID string) error {
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`UPDATE subtasks SET status = ? WHERE task_id = ?`,
			"cancelled", taskID,
		)
		return err
	})
}

func (s *Store) UpdateTaskElapsed(id string, elapsed int) error {
	_, err := s.db.Exec(`UPDATE tasks SET elapsed = ? WHERE id = ?`, elapsed, id)
	return err
}

func (s *Store) RemoveTask(id string) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM task_logs WHERE task_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM task_diffs WHERE task_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM subtasks WHERE task_id = ?`, id); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM tasks WHERE id = ?`, id)
		return err
	})
}

func (s *Store) ClearFinishedTasks(projectID string) error {
	return s.withTx(func(tx *sql.Tx) error {
		finishedFilter := `SELECT id FROM tasks WHERE project_id = ? AND status IN ('done','failed','cancelled')`
		if _, err := tx.Exec(`DELETE FROM task_logs WHERE task_id IN (`+finishedFilter+`)`, projectID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM task_diffs WHERE task_id IN (`+finishedFilter+`)`, projectID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM subtasks WHERE task_id IN (`+finishedFilter+`)`, projectID); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM tasks WHERE project_id = ? AND status IN ('done','failed','cancelled')`, projectID)
		return err
	})
}
