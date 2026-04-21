package tui

import (
	"sort"
	"testing"

	"menace/internal/engine"

	tea "github.com/charmbracelet/bubbletea"
)

func setupKey(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

func TestSetupModelSelectionMatchesDisplay(t *testing.T) {
	s := testStore(t)
	sm := newSetupModel(s)

	// Set provider to Google (index 1 in ProviderPresets)
	sm.providerSel = 1

	// Simulate API key already saved
	if err := s.SaveAPIKey("google", "fake-key"); err != nil {
		t.Skipf("keyring unavailable: %v", err)
	}

	// Simulate models fetched — unsorted, to verify sorting happens
	sm = sm.HandleModelsFetched(modelsFetchedMsg{
		architect: []engine.ModelOption{
			{ID: "gemini-3.1-pro", Desc: "Pro"},
			{ID: "gemini-2.5-flash", Desc: "Flash"},
			{ID: "gemini-2.0-nano", Desc: "Nano"},
		},
		worker: []engine.ModelOption{
			{ID: "gemini-2.5-flash-lite", Desc: "Flash Lite"},
			{ID: "gemini-2.0-nano", Desc: "Nano"},
		},
	})

	if sm.step != 2 {
		t.Fatalf("expected step 2 (architect select), got %d", sm.step)
	}

	// Models should be sorted alphabetically after HandleModelsFetched
	for i := 1; i < len(sm.architectModels); i++ {
		if sm.architectModels[i].ID < sm.architectModels[i-1].ID {
			t.Fatalf("architect models not sorted: %s before %s", sm.architectModels[i-1].ID, sm.architectModels[i].ID)
		}
	}

	// Navigate down twice to select the 3rd model (index 2)
	sm, _ = sm.Update(setupKey("j"))
	sm, _ = sm.Update(setupKey("j"))
	if sm.architectSel != 2 {
		t.Fatalf("expected architectSel=2, got %d", sm.architectSel)
	}

	// The model at index 2 in sorted order should be what we get
	expectedArchitect := sm.architectModels[2].ID

	// Confirm architect → advance to worker selection
	sm, _ = sm.Update(setupKey("enter"))
	if sm.step != 3 {
		t.Fatalf("expected step 3 (worker select), got %d", sm.step)
	}

	// Select first worker model and confirm
	sm, cmd := sm.Update(setupKey("enter"))

	// The cmd should produce a loginDoneMsg
	if cmd == nil {
		t.Fatal("expected cmd from final confirm")
	}
	msg := cmd()
	if _, ok := msg.(loginDoneMsg); !ok {
		t.Fatalf("expected loginDoneMsg, got %T", msg)
	}

	// Verify the correct models were saved to the store
	auth, err := s.GetAuth()
	if err != nil {
		t.Fatalf("GetAuth: %v", err)
	}
	if auth == nil {
		t.Fatal("expected auth to be saved")
	}
	if auth.ArchitectModel != expectedArchitect {
		t.Fatalf("architect model: expected %q, got %q", expectedArchitect, auth.ArchitectModel)
	}
	expectedWorker := sm.workerModels[0].ID
	if auth.WorkerModel != expectedWorker {
		t.Fatalf("worker model: expected %q, got %q", expectedWorker, auth.WorkerModel)
	}
}

func TestSetupEscGoesBack(t *testing.T) {
	s := testStore(t)
	sm := newSetupModel(s)

	// Step 0 → enter key step
	sm.step = 1
	sm, _ = sm.Update(setupKey("esc"))
	if sm.step != 0 {
		t.Fatalf("esc from step 1: expected step 0, got %d", sm.step)
	}

	// Step 2 → back to provider
	sm.step = 2
	sm, _ = sm.Update(setupKey("esc"))
	if sm.step != 0 {
		t.Fatalf("esc from step 2: expected step 0, got %d", sm.step)
	}

	// Step 3 → back to architect selection
	sm.step = 3
	sm, _ = sm.Update(setupKey("esc"))
	if sm.step != 2 {
		t.Fatalf("esc from step 3: expected step 2, got %d", sm.step)
	}
}

func TestSetupModelsSortedOnFetch(t *testing.T) {
	s := testStore(t)
	sm := newSetupModel(s)

	unsorted := []engine.ModelOption{
		{ID: "z-model", Desc: "last"},
		{ID: "a-model", Desc: "first"},
		{ID: "m-model", Desc: "middle"},
	}

	sm = sm.HandleModelsFetched(modelsFetchedMsg{
		architect: unsorted,
		worker:    unsorted,
	})

	// Verify both lists are sorted
	if !sort.SliceIsSorted(sm.architectModels, func(i, j int) bool {
		return sm.architectModels[i].ID < sm.architectModels[j].ID
	}) {
		t.Fatal("architect models should be sorted after fetch")
	}

	if !sort.SliceIsSorted(sm.workerModels, func(i, j int) bool {
		return sm.workerModels[i].ID < sm.workerModels[j].ID
	}) {
		t.Fatal("worker models should be sorted after fetch")
	}
}
