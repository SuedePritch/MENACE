package workspace

import (
	"os"
	"testing"
)

func TestProjectHash(t *testing.T) {
	hash := ProjectHash("/tmp/myproject")
	if len(hash) != 8 {
		t.Fatalf("expected 8-char hash, got %d chars: %s", len(hash), hash)
	}

	// Same input = same hash
	if ProjectHash("/tmp/myproject") != hash {
		t.Fatal("expected deterministic hash")
	}

	// Different input = different hash
	if ProjectHash("/tmp/other") == hash {
		t.Fatal("expected different hash for different path")
	}
}

func TestPickerCmdRequiresTool(t *testing.T) {
	// This test just verifies the function doesn't panic.
	// In CI without zoxide/fzf it should return an error.
	cmd, tmpPath, err := PickerCmd()
	if err != nil {
		// Neither tool found — expected in some environments
		return
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	if tmpPath == "" {
		t.Fatal("expected non-empty temp path")
	}
	os.Remove(tmpPath)
}
