package store

import (
	"testing"
)

func TestKeyringRoundTrip(t *testing.T) {
	provider := "test-provider-menace-unit"
	secret := "sk-test-12345"

	// Clean up in case of prior failed run.
	keyringDelete(provider)

	if err := keyringSet(provider, secret); err != nil {
		t.Skipf("keyring unavailable in this environment: %v", err)
	}
	defer keyringDelete(provider)

	got, err := keyringGet(provider)
	if err != nil {
		t.Fatalf("keyringGet: %v", err)
	}
	if got != secret {
		t.Fatalf("got %q, want %q", got, secret)
	}
}

func TestKeyringGetMissing(t *testing.T) {
	_, err := keyringGet("nonexistent-provider-menace-unit")
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
}
