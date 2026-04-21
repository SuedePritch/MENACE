package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
)

// ProjectHash returns the first 8 characters of the hex-encoded SHA256 hash
// of the given working directory path.
func ProjectHash(cwd string) string {
	hash := sha256.Sum256([]byte(cwd))
	hexHash := hex.EncodeToString(hash[:])
	return hexHash[:8]
}

// PickerCmd creates a temp file and returns a command to launch zoxide
// query -i (or fzf as fallback) with output redirected to the temp file.
func PickerCmd() (*exec.Cmd, string, error) {
	tmpFile, err := os.CreateTemp("", "menace-pick-*.txt")
	if err != nil {
		return nil, "", err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	if _, err = exec.LookPath("zoxide"); err == nil {
		cmd := exec.Command("sh", "-c", fmt.Sprintf("zoxide query -i > %q", tmpPath))
		return cmd, tmpPath, nil
	}

	if _, err = exec.LookPath("fzf"); err == nil {
		cmd := exec.Command("sh", "-c", fmt.Sprintf("fzf --walker=dir --walker-root=$HOME > %q", tmpPath))
		return cmd, tmpPath, nil
	}

	return nil, "", fmt.Errorf("neither zoxide nor fzf found in PATH")
}
