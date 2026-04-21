package store

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/zalando/go-keyring"
)

const keyringService = "menace"

// keyringSet stores a secret in the OS keychain.
func keyringSet(provider, secret string) error {
	if err := keyring.Set(keyringService, provider, secret); err != nil {
		return keyringErr("set", provider, err)
	}
	return nil
}

// keyringGet retrieves a secret from the OS keychain.
func keyringGet(provider string) (string, error) {
	secret, err := keyring.Get(keyringService, provider)
	if err != nil {
		return "", keyringErr("get", provider, err)
	}
	return secret, nil
}

// keyringErr wraps keyring errors with platform-specific guidance.
func keyringErr(op, provider string, err error) error {
	wrapped := fmt.Errorf("keyring %s %s: %w", op, provider, err)
	if runtime.GOOS == "linux" && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("%w (is a keyring daemon like gnome-keyring or kwallet running?)", wrapped)
	}
	return wrapped
}

// keyringDelete removes a secret from the OS keychain.
// Errors are logged but not returned — this is only used during full auth
// clearing, and a failed delete means the key persists in the keychain.
func keyringDelete(provider string) {
	if err := keyring.Delete(keyringService, provider); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to remove %s key from keychain: %v\n", provider, err)
	}
}
