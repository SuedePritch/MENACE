package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	mlog "menace/internal/log"
)

// ─── Session methods ────────────────────────────────────────────────────

func (s *Store) SaveSession(projectID string, sess *Session) error {
	summary := "empty"
	for _, m := range sess.Chat {
		if m.Role == "user" {
			summary = m.Content
			if len(summary) > 80 {
				summary = summary[:77] + "..."
			}
			break
		}
	}
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO sessions (id, project_id, started_at, summary, chat) VALUES (?, ?, ?, ?, '[]')`,
			sess.ID, projectID, sess.StartedAt, summary,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM messages WHERE session_id = ?`, sess.ID); err != nil {
			return err
		}
		for i, m := range sess.Chat {
			if _, err := tx.Exec(
				`INSERT INTO messages (session_id, seq, role, content) VALUES (?, ?, ?, ?)`,
				sess.ID, i, m.Role, m.Content,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) LoadSession(id string) (*Session, error) {
	var sess Session
	var chatJSON string
	err := s.db.QueryRow(
		`SELECT id, started_at, chat FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &sess.StartedAt, &chatJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("LoadSession %s: %w", id, err)
	}

	sess.Chat, err = s.loadMessages(id)
	if err != nil {
		mlog.Error("LoadSession loadMessages", slog.String("session", id), slog.String("err", err.Error()))
	}

	if len(sess.Chat) == 0 && chatJSON != "" && chatJSON != "[]" {
		if err := json.Unmarshal([]byte(chatJSON), &sess.Chat); err != nil {
			mlog.Error("LoadSession unmarshal chat fallback", slog.String("session", id), slog.String("err", err.Error()))
		}
	}

	proposalRows, err := s.db.Query(`SELECT id FROM proposals WHERE session_id = ?`, id)
	if err != nil {
		mlog.Error("LoadSession proposals query", slog.String("session", id), slog.String("err", err.Error()))
	} else {
		defer proposalRows.Close()
		for proposalRows.Next() {
			var pid string
			if proposalRows.Scan(&pid) == nil {
				sess.ProposalIDs = append(sess.ProposalIDs, pid)
			}
		}
	}

	taskRows, err := s.db.Query(`SELECT id FROM tasks WHERE session_id = ?`, id)
	if err != nil {
		mlog.Error("LoadSession tasks query", slog.String("session", id), slog.String("err", err.Error()))
	} else {
		defer taskRows.Close()
		for taskRows.Next() {
			var tid string
			if taskRows.Scan(&tid) == nil {
				sess.TaskIDs = append(sess.TaskIDs, tid)
			}
		}
	}

	return &sess, nil
}

func (s *Store) ListSessions(projectID string) ([]SessionSummary, error) {
	rows, err := s.db.Query(`
		SELECT s.id, s.started_at, s.summary,
			(SELECT COUNT(*) FROM tasks WHERE session_id = s.id) as task_count
		FROM sessions s WHERE project_id = ? ORDER BY started_at DESC
	`, projectID)
	if err != nil {
		mlog.Error("ListSessions", slog.String("err", err.Error()))
		return nil, fmt.Errorf("ListSessions: %w", err)
	}
	defer rows.Close()
	var summaries []SessionSummary
	for rows.Next() {
		var ss SessionSummary
		if err := rows.Scan(&ss.ID, &ss.StartedAt, &ss.Summary, &ss.Tasks); err != nil {
			mlog.Error("ListSessions scan", slog.String("err", err.Error()))
			continue
		}
		ss.ProjectID = projectID
		summaries = append(summaries, ss)
	}
	if err := rows.Err(); err != nil {
		mlog.Error("ListSessions rows iteration", slog.String("err", err.Error()))
		return summaries, fmt.Errorf("ListSessions: rows: %w", err)
	}
	return summaries, nil
}

func (s *Store) ListAllSessions() ([]SessionSummary, error) {
	rows, err := s.db.Query(`
		SELECT s.id, s.started_at, s.summary,
			(SELECT COUNT(*) FROM tasks WHERE session_id = s.id) as task_count,
			p.id, p.name, p.path
		FROM sessions s JOIN projects p ON s.project_id = p.id
		ORDER BY s.started_at DESC
	`)
	if err != nil {
		mlog.Error("ListAllSessions", slog.String("err", err.Error()))
		return nil, fmt.Errorf("ListAllSessions: %w", err)
	}
	defer rows.Close()
	var summaries []SessionSummary
	for rows.Next() {
		var ss SessionSummary
		if err := rows.Scan(&ss.ID, &ss.StartedAt, &ss.Summary, &ss.Tasks, &ss.ProjectID, &ss.ProjectName, &ss.ProjectPath); err != nil {
			mlog.Error("ListAllSessions scan", slog.String("err", err.Error()))
			continue
		}
		summaries = append(summaries, ss)
	}
	if err := rows.Err(); err != nil {
		mlog.Error("ListAllSessions rows iteration", slog.String("err", err.Error()))
		return summaries, fmt.Errorf("ListAllSessions: rows: %w", err)
	}
	return summaries, nil
}

func (s *Store) loadMessages(sessionID string) ([]ChatMessage, error) {
	rows, err := s.db.Query(
		`SELECT role, content FROM messages WHERE session_id = ? ORDER BY seq`, sessionID,
	)
	if err != nil {
		mlog.Error("loadMessages", slog.String("err", err.Error()))
		return nil, fmt.Errorf("loadMessages: %w", err)
	}
	defer rows.Close()
	var msgs []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			mlog.Error("loadMessages scan", slog.String("err", err.Error()))
			continue
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		mlog.Error("loadMessages rows iteration", slog.String("err", err.Error()))
		return msgs, fmt.Errorf("loadMessages: rows: %w", err)
	}
	return msgs, nil
}
