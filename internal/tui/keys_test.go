package tui

import (
	"menace/internal/config"
	"testing"
)

func TestResolveNormal(t *testing.T) {
	tests := []struct {
		key  string
		want action
	}{
		{"j", actDown},
		{"down", actDown},
		{"enter", actConfirm},
		{"ctrl+c", actQuit},
		{"unknown_key", actNone},
	}
	for _, tt := range tests {
		if got := resolveNormal(tt.key); got != tt.want {
			t.Errorf("resolveNormal(%q) = %d, want %d", tt.key, got, tt.want)
		}
	}
}

func TestResolveInsert(t *testing.T) {
	tests := []struct {
		key  string
		want action
	}{
		{"enter", actSend},
		{"esc", actEscape},
		{"alt+enter", actNewline},
		{"nope", actNone},
	}
	for _, tt := range tests {
		if got := resolveInsert(tt.key); got != tt.want {
			t.Errorf("resolveInsert(%q) = %d, want %d", tt.key, got, tt.want)
		}
	}
}

func TestResolveModal(t *testing.T) {
	tests := []struct {
		key  string
		want action
	}{
		{"a", actApprove},
		{"esc", actEscape},
		{"q", actEscape},
		{"bogus", actNone},
	}
	for _, tt := range tests {
		if got := resolveModal(tt.key); got != tt.want {
			t.Errorf("resolveModal(%q) = %d, want %d", tt.key, got, tt.want)
		}
	}
}

func TestApplyKeyOverrides(t *testing.T) {
	// Save originals and restore after test to avoid polluting other tests.
	origNormal := make(map[string]action, len(normalKeys))
	for k, v := range normalKeys {
		origNormal[k] = v
	}
	defer func() {
		for k := range normalKeys {
			delete(normalKeys, k)
		}
		for k, v := range origNormal {
			normalKeys[k] = v
		}
	}()

	applyKeyOverrides(config.KeysConfig{
		Normal: map[string]string{
			"z":       "quit",
			"badkey":  "nonexistent_action",
		},
	})

	// New override should work.
	if got := resolveNormal("z"); got != actQuit {
		t.Errorf("after override, resolveNormal(\"z\") = %d, want %d (actQuit)", got, actQuit)
	}

	// Existing keys should still resolve.
	if got := resolveNormal("j"); got != actDown {
		t.Errorf("after override, resolveNormal(\"j\") = %d, want %d (actDown)", got, actDown)
	}

	// Unknown action name should not have been added.
	if got := resolveNormal("badkey"); got != actNone {
		t.Errorf("unknown action override should be ignored, got %d", got)
	}
}

func TestKeyFor(t *testing.T) {
	m := map[string]action{
		"j":    actDown,
		"down": actDown,
	}
	got := keyFor(m, actDown)
	if got != "j" {
		t.Errorf("keyFor(actDown) = %q, want \"j\" (shortest)", got)
	}

	// Missing action returns "?".
	if got := keyFor(m, actQuit); got != "?" {
		t.Errorf("keyFor(missing) = %q, want \"?\"", got)
	}
}

func TestHelpPair(t *testing.T) {
	m := map[string]action{
		"j":    actDown,
		"k":    actUp,
		"down": actDown,
		"up":   actUp,
	}
	got := helpPair(m, actDown, actUp, "scroll")
	if got.Key != "j/k" {
		t.Errorf("helpPair Key = %q, want \"j/k\"", got.Key)
	}
	if got.Label != "scroll" {
		t.Errorf("helpPair Label = %q, want \"scroll\"", got.Label)
	}
}
