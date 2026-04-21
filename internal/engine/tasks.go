package engine

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"

	mlog "menace/internal/log"
	"menace/internal/store"
)

// GenID generates a random hex ID.
func GenID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// AddTask creates a task with optional subtasks in the store and returns it.
func AddTask(s TaskStore, projectID, sessionID, description, instruction string, subtasks []store.ProposalSubtask) (store.TaskData, error) {
	id := GenID()
	t := store.TaskData{
		ID:          id,
		ProjectID:   projectID,
		SessionID:   sessionID,
		Description: description,
		Instruction: instruction,
		Status:      store.StatusPending,
	}
	for i, sd := range subtasks {
		t.Subtasks = append(t.Subtasks, store.SubtaskData{
			ID:          fmt.Sprintf("%s-%d", id, i+1),
			Seq:         i + 1,
			Description: sd.Description,
			Instruction: sd.Instruction,
			Status:      store.StatusPending,
		})
	}
	if err := s.SaveTask(t); err != nil {
		return t, fmt.Errorf("AddTask: %w", err)
	}
	return t, nil
}

// SyncTasks loads tasks from the store and returns them.
func SyncTasks(s TaskStore, projectID string) []store.TaskData {
	tasks, err := s.ListTasks(projectID)
	if err != nil {
		mlog.Error("SyncTasks", slog.String("err", err.Error()))
		return nil
	}
	return tasks
}
