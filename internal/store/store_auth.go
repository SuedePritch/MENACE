package store

import (
	"database/sql"
	"fmt"
)

// ─── Auth methods ─────────────────────────────────────────────────────

func (s *Store) SaveAuth(provider, apiKey, architectModel, workerModel string) error {
	if err := keyringSet(provider, apiKey); err != nil {
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO auth (provider, architect_model, worker_model, updated_at)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(provider) DO UPDATE SET
		   architect_model = excluded.architect_model,
		   worker_model = excluded.worker_model,
		   updated_at = CURRENT_TIMESTAMP`,
		provider, architectModel, workerModel,
	)
	return err
}

func (s *Store) SaveAPIKey(provider, apiKey string) error {
	if err := keyringSet(provider, apiKey); err != nil {
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO auth (provider, updated_at) VALUES (?, CURRENT_TIMESTAMP)
		 ON CONFLICT(provider) DO UPDATE SET updated_at = CURRENT_TIMESTAMP`,
		provider,
	)
	return err
}

type AuthConfig struct {
	Provider       string
	APIKey         string
	ArchitectModel string
	WorkerModel    string
}

// GetAuth returns the active auth config (first row).
// Returns (nil, nil) when no auth is configured.
// Returns a non-nil error only on database or decryption failures.
func (s *Store) GetAuth() (*AuthConfig, error) {
	var a AuthConfig
	err := s.db.QueryRow(`SELECT provider, architect_model, worker_model FROM auth LIMIT 1`).
		Scan(&a.Provider, &a.ArchitectModel, &a.WorkerModel)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query auth: %w", err)
	}
	a.APIKey, err = keyringGet(a.Provider)
	if err != nil {
		return nil, fmt.Errorf("retrieve api key for %s: %w", a.Provider, err)
	}
	return &a, nil
}

func (s *Store) GetAPIKey(provider string) string {
	secret, err := keyringGet(provider)
	if err != nil {
		return ""
	}
	return secret
}

func (s *Store) HasAPIKey(provider string) bool {
	return s.GetAPIKey(provider) != ""
}

func (s *Store) ClearAllAuth() error {
	rows, err := s.db.Query(`SELECT provider FROM auth`)
	if err != nil {
		return fmt.Errorf("query auth providers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return fmt.Errorf("scan auth provider: %w", err)
		}
		keyringDelete(p)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate auth providers: %w", err)
	}
	_, err = s.db.Exec(`DELETE FROM auth`)
	return err
}
