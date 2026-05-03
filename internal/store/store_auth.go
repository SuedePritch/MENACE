package store

import (
	"database/sql"
	"fmt"
)

// AuthConfig holds provider, model, and API key for both architect and worker.
type AuthConfig struct {
	ArchitectProvider string
	ArchitectModel    string
	ArchitectAPIKey   string
	WorkerProvider    string
	WorkerModel       string
	WorkerAPIKey      string
}

// SaveAuth persists provider/model selections and stores API keys in the keychain.
// Architect and worker may use different providers.
func (s *Store) SaveAuth(architectProvider, architectAPIKey, architectModel, workerProvider, workerAPIKey, workerModel string) error {
	if err := keyringSet(architectProvider, architectAPIKey); err != nil {
		return err
	}
	if workerProvider != architectProvider && workerProvider != "" {
		if err := keyringSet(workerProvider, workerAPIKey); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(
		`INSERT INTO auth (provider, architect_model, worker_model, worker_provider, updated_at)
		 VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(provider) DO UPDATE SET
		   architect_model  = excluded.architect_model,
		   worker_model     = excluded.worker_model,
		   worker_provider  = excluded.worker_provider,
		   updated_at       = CURRENT_TIMESTAMP`,
		architectProvider, architectModel, workerModel, workerProvider,
	)
	return err
}

// SaveAPIKey stores an API key for a provider without changing model selections.
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

// GetAuth returns the active auth config.
// Returns (nil, nil) when no auth is configured.
func (s *Store) GetAuth() (*AuthConfig, error) {
	var a AuthConfig
	var workerProvider sql.NullString
	err := s.db.QueryRow(
		`SELECT provider, architect_model, worker_model, worker_provider FROM auth LIMIT 1`,
	).Scan(&a.ArchitectProvider, &a.ArchitectModel, &a.WorkerModel, &workerProvider)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query auth: %w", err)
	}

	a.ArchitectAPIKey, err = keyringGet(a.ArchitectProvider)
	if err != nil {
		return nil, fmt.Errorf("retrieve api key for %s: %w", a.ArchitectProvider, err)
	}

	// Worker provider falls back to architect provider if not set separately.
	if workerProvider.Valid && workerProvider.String != "" {
		a.WorkerProvider = workerProvider.String
	} else {
		a.WorkerProvider = a.ArchitectProvider
	}

	if a.WorkerProvider == a.ArchitectProvider {
		a.WorkerAPIKey = a.ArchitectAPIKey
	} else {
		a.WorkerAPIKey, _ = keyringGet(a.WorkerProvider)
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
	rows, err := s.db.Query(`SELECT provider, worker_provider FROM auth`)
	if err != nil {
		return fmt.Errorf("query auth providers: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var p string
		var wp sql.NullString
		if err := rows.Scan(&p, &wp); err != nil {
			return fmt.Errorf("scan auth provider: %w", err)
		}
		if !seen[p] {
			keyringDelete(p)
			seen[p] = true
		}
		if wp.Valid && wp.String != "" && !seen[wp.String] {
			keyringDelete(wp.String)
			seen[wp.String] = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate auth providers: %w", err)
	}
	_, err = s.db.Exec(`DELETE FROM auth`)
	return err
}
