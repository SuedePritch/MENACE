package store

import (
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	mlog "menace/internal/log"

	_ "github.com/mattn/go-sqlite3"
)

// Store wraps *sql.DB and provides typed CRUD methods.
type Store struct {
	db *sql.DB
}

// Open opens or creates a SQLite database at the given path.
func Open(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) initSchema() error {
	// Baseline schema (version 0): all CREATE TABLE IF NOT EXISTS statements.
	baseline := []string{
		`CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			path TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			context TEXT DEFAULT '',
			theme TEXT DEFAULT '',
			last_used DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL REFERENCES projects(id),
			started_at DATETIME NOT NULL,
			summary TEXT DEFAULT 'empty',
			chat TEXT DEFAULT '[]'
		)`,
		`CREATE TABLE IF NOT EXISTS proposals (
			id TEXT PRIMARY KEY,
			session_id TEXT DEFAULT '',
			project_id TEXT NOT NULL,
			description TEXT NOT NULL,
			instruction TEXT DEFAULT '',
			subtasks TEXT DEFAULT '[]'
		)`,
		`CREATE TABLE IF NOT EXISTS proposal_subtasks (
			id TEXT PRIMARY KEY,
			proposal_id TEXT NOT NULL REFERENCES proposals(id) ON DELETE CASCADE,
			seq INTEGER NOT NULL,
			description TEXT NOT NULL,
			instruction TEXT DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			proposal_id TEXT DEFAULT '',
			session_id TEXT DEFAULT '',
			project_id TEXT NOT NULL,
			description TEXT NOT NULL,
			instruction TEXT DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			touches TEXT DEFAULT '[]',
			started_at DATETIME,
			completed_at DATETIME,
			elapsed INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS task_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL,
			line TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS subtasks (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			seq INTEGER NOT NULL,
			description TEXT NOT NULL,
			instruction TEXT DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending'
		)`,
		`CREATE TABLE IF NOT EXISTS task_diffs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL,
			subtask_id TEXT DEFAULT '',
			diff TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS auth (
			provider TEXT PRIMARY KEY,
			architect_model TEXT DEFAULT '',
			worker_model TEXT DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, stmt := range baseline {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_task_logs_task_id ON task_logs(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_task_diffs_task_id ON task_diffs(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_subtasks_task_id ON subtasks(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_project_id ON tasks(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_project_id ON sessions(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_proposals_project_id ON proposals(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_proposal_subtasks_proposal_id ON proposal_subtasks(proposal_id)`,
	}
	for _, stmt := range indexes {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}

	return nil
}


// withTx runs fn inside a transaction.
func (s *Store) withTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ─── Project methods ───────────────────────────────────────────────────

func (s *Store) RegisterProject(id, path string) error {
	name := filepath.Base(path)
	now := time.Now()
	_, err := s.db.Exec(
		`INSERT INTO projects (id, path, name, last_used) VALUES (?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET last_used = excluded.last_used`,
		id, path, name, now,
	)
	return err
}

func (s *Store) ListProjects() ([]ProjectEntry, error) {
	rows, err := s.db.Query(
		`SELECT id, path, name, COALESCE(theme, ''), last_used FROM projects ORDER BY last_used DESC`,
	)
	if err != nil {
		mlog.Error("ListProjects", slog.String("err", err.Error()))
		return nil, fmt.Errorf("ListProjects: %w", err)
	}
	defer rows.Close()
	var projects []ProjectEntry
	for rows.Next() {
		var p ProjectEntry
		if err := rows.Scan(&p.ID, &p.Path, &p.Name, &p.Theme, &p.LastUsed); err != nil {
			mlog.Error("ListProjects scan", slog.String("err", err.Error()))
			continue
		}
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		mlog.Error("ListProjects rows iteration", slog.String("err", err.Error()))
		return projects, fmt.Errorf("ListProjects: rows: %w", err)
	}
	return projects, nil
}

func (s *Store) SetProjectTheme(projectID, theme string) error {
	_, err := s.db.Exec(`UPDATE projects SET theme = ? WHERE id = ?`, theme, projectID)
	return err
}

func (s *Store) GetProjectTheme(projectID string) string {
	var theme string
	if err := s.db.QueryRow(`SELECT COALESCE(theme, '') FROM projects WHERE id = ?`, projectID).Scan(&theme); err != nil && err != sql.ErrNoRows {
		mlog.Error("GetProjectTheme", slog.String("project", projectID), slog.String("err", err.Error()))
	}
	return theme
}

func (s *Store) SetProjectContext(projectID, context string) error {
	_, err := s.db.Exec(`UPDATE projects SET context = ? WHERE id = ?`, context, projectID)
	return err
}

func (s *Store) GetProjectContext(projectID string) string {
	var context string
	if err := s.db.QueryRow(`SELECT context FROM projects WHERE id = ?`, projectID).Scan(&context); err != nil {
		return ""
	}
	return context
}
