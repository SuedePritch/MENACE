package engine

import (
	"time"

	"menace/internal/store"
)

// NewSession creates a new session with a generated ID.
func NewSession() *store.Session {
	return &store.Session{
		ID:        GenID(),
		StartedAt: time.Now(),
	}
}
