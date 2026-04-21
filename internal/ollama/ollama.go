package ollama

import (
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Manager handles starting/stopping ollama.
type Manager struct {
	cmd     *exec.Cmd
	started bool
}

func NewManager() *Manager {
	return &Manager{}
}

// Ensure starts ollama if not already running, then pulls the model if needed.
func (om *Manager) Ensure(workerModel string) error {
	if om.IsRunning() {
		if !om.hasModel(workerModel) {
			return exec.Command("ollama", "pull", workerModel).Run()
		}
		return nil
	}

	cmd := exec.Command("ollama", "serve")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return err
	}
	om.cmd = cmd
	om.started = true

	ready := false
	for i := 0; i < 30; i++ {
		if om.IsRunning() {
			ready = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ready {
		return os.ErrDeadlineExceeded
	}

	if !om.hasModel(workerModel) {
		return exec.Command("ollama", "pull", workerModel).Run()
	}
	return nil
}

func (om *Manager) hasModel(name string) bool {
	out, err := exec.Command("ollama", "list").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == name {
			return true
		}
	}
	return false
}

func (om *Manager) IsRunning() bool {
	resp, err := http.Get("http://localhost:11434/api/tags")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == 200
}

func (om *Manager) Shutdown() {
	if om.started && om.cmd != nil && om.cmd.Process != nil {
		_ = om.cmd.Process.Kill()
		_ = om.cmd.Wait()
	}
}

