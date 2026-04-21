package tui

import (
	"testing"

	"menace/internal/config"
	"menace/internal/engine"
	"menace/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

// ── Helpers ──────────────────────────────────────────────────────────────────

// testModel creates a dashboard-ready model with an ephemeral store.
// No orchestrator, no LLM process — just enough for Update dispatch.
func testModel(t *testing.T) model {
	t.Helper()
	s := testStore(t)
	_ = s.RegisterProject("test-proj", "/tmp/fake")
	m := model{
		screen:      screenDashboard,
		activePanel: panelArchitect,
		store:       s,
		programRef:  &programRef{},
		chat:        newChatPanel(config.MenaceConfig{ChatCharLimit: 4096, ChatMaxHeight: 10}),
		ts: themeState{
			cfg:   config.Default(),
			theme: config.DefaultTheme(),
			dir:   t.TempDir(),
		},
		project: projectState{id: "test-proj", cwd: "/tmp/fake"},
		width:   120,
		height:  40,
	}
	return m
}

// send dispatches a key string through model.Update and returns the new model + cmd.
func send(t *testing.T, m model, key string) (model, tea.Cmd) {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		msg = tea.KeyMsg{Type: tea.KeyShiftTab}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEscape}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "ctrl+c":
		msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+n":
		msg = tea.KeyMsg{Type: tea.KeyCtrlN}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	result, cmd := m.Update(msg)
	return result.(model), cmd
}

// sendDrain sends a key and feeds any resulting message back into Update.
func sendDrain(t *testing.T, m model, key string) model {
	t.Helper()
	m, cmd := send(t, m, key)
	return drain(t, m, cmd)
}

// drain executes a cmd and feeds the resulting message back into the model.
func drain(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	result, nextCmd := m.Update(msg)
	m = result.(model)
	if nextCmd != nil {
		return drain(t, m, nextCmd)
	}
	return m
}

// sendMsg dispatches a tea.Msg directly through Update.
func sendMsg(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()
	result, cmd := m.Update(msg)
	return result.(model), cmd
}

// ── Panel Navigation ─────────────────────────────────────────────────────────

func TestTabCyclesPanels(t *testing.T) {
	m := testModel(t)
	if m.activePanel != panelArchitect {
		t.Fatal("expected start at architect")
	}

	m = sendDrain(t, m, "tab")
	if m.activePanel != panelProposals {
		t.Fatalf("after tab: expected proposals, got %d", m.activePanel)
	}

	m = sendDrain(t, m, "tab")
	if m.activePanel != panelQueue {
		t.Fatalf("after 2nd tab: expected queue, got %d", m.activePanel)
	}

	m = sendDrain(t, m, "tab")
	if m.activePanel != panelArchitect {
		t.Fatalf("after 3rd tab: expected architect, got %d", m.activePanel)
	}

	// Reverse
	m = sendDrain(t, m, "shift+tab")
	if m.activePanel != panelQueue {
		t.Fatalf("shift+tab from architect: expected queue, got %d", m.activePanel)
	}
}

func TestArrowKeysPanelSwitch(t *testing.T) {
	m := testModel(t)
	m.activePanel = panelArchitect

	m = sendDrain(t, m, "l")
	if m.activePanel != panelProposals {
		t.Fatalf("l from architect: expected proposals, got %d", m.activePanel)
	}

	m = sendDrain(t, m, "h")
	if m.activePanel != panelArchitect {
		t.Fatalf("h from proposals: expected architect, got %d", m.activePanel)
	}
}

func TestPanelScrollSelection(t *testing.T) {
	m := testModel(t)
	m.activePanel = panelProposals
	m.propPanel.proposals = []store.Proposal{
		{ID: "a", Description: "first"},
		{ID: "b", Description: "second"},
		{ID: "c", Description: "third"},
	}
	m.propPanel.sel = 0

	m = sendDrain(t, m, "j")
	if m.propPanel.sel != 1 {
		t.Fatalf("j: expected sel=1, got %d", m.propPanel.sel)
	}

	m = sendDrain(t, m, "j")
	if m.propPanel.sel != 2 {
		t.Fatalf("j again: expected sel=2, got %d", m.propPanel.sel)
	}

	m = sendDrain(t, m, "k")
	if m.propPanel.sel != 1 {
		t.Fatalf("k: expected sel=1, got %d", m.propPanel.sel)
	}
}

func TestOverflowNavBetweenPanels(t *testing.T) {
	m := testModel(t)
	m.activePanel = panelProposals
	m.propPanel.proposals = []store.Proposal{{ID: "a", Description: "only"}}
	m.propPanel.sel = 0
	m.queuePanel.tasks = []task{{id: "t1", description: "task"}}

	// j past last proposal → overflow to queue
	m = sendDrain(t, m, "j")
	if m.activePanel != panelQueue {
		t.Fatalf("overflow j: expected queue, got %d", m.activePanel)
	}

	// k at top of queue → overflow to proposals
	m.queuePanel.sel = 0
	m = sendDrain(t, m, "k")
	if m.activePanel != panelProposals {
		t.Fatalf("overflow k: expected proposals, got %d", m.activePanel)
	}
}

// ── Mode Transitions ─────────────────────────────────────────────────────────

func TestInsertModeRouting(t *testing.T) {
	m := testModel(t)
	if m.insertMode {
		t.Fatal("should start in normal mode")
	}

	// i enters insert
	m = sendDrain(t, m, "i")
	if !m.insertMode {
		t.Fatal("i should enter insert mode")
	}
	if m.activePanel != panelArchitect {
		t.Fatal("insert mode should focus architect panel")
	}

	// tab in insert mode should NOT change panel
	panel := m.activePanel
	m = sendDrain(t, m, "tab")
	if m.activePanel != panel {
		t.Fatal("tab in insert mode should not change panel")
	}

	// esc exits insert
	m = sendDrain(t, m, "esc")
	if m.insertMode {
		t.Fatal("esc should exit insert mode")
	}
}

func TestSlashEntersInsert(t *testing.T) {
	m := testModel(t)
	m = sendDrain(t, m, "/")
	if !m.insertMode {
		t.Fatal("/ should enter insert mode")
	}
}

// ── Modal Routing ────────────────────────────────────────────────────────────

func TestModalCapturesInput(t *testing.T) {
	m := testModel(t)
	m.activePanel = panelArchitect

	// Open settings modal
	m.activeModal = NewSettingsModal(m.store, m.ts.cfg, m.ts.theme, m.ts.names, m.ts.dir, m.project.id)
	if m.activeModal == nil {
		t.Fatal("modal should be set")
	}

	// Tab with modal open should not change panel
	m = sendDrain(t, m, "tab")
	if m.activePanel != panelArchitect {
		t.Fatal("tab with modal open should not change panel")
	}

	// j with modal open should not change panel
	m = sendDrain(t, m, "j")
	if m.activePanel != panelArchitect {
		t.Fatal("j with modal open should not change panel")
	}

	// Close modal
	m, _ = sendMsg(t, m, modalCloseMsg{})
	if m.activeModal != nil {
		t.Fatal("modal should be nil after close")
	}
}

// ── Keybinding Overrides ────────────────────────────────────────────────────

func TestCustomKeybindDispatch(t *testing.T) {
	// Save original keys
	orig := make(map[string]action)
	for k, v := range normalKeys {
		orig[k] = v
	}
	defer func() {
		for k := range normalKeys {
			delete(normalKeys, k)
		}
		for k, v := range orig {
			normalKeys[k] = v
		}
	}()

	// Add custom bind
	normalKeys["z"] = actNextPanel

	m := testModel(t)
	m.activePanel = panelArchitect

	m = sendDrain(t, m, "z")
	if m.activePanel != panelProposals {
		t.Fatalf("custom keybind z→actNextPanel: expected proposals, got %d", m.activePanel)
	}
}

// ── Empty Panel Safety ───────────────────────────────────────────────────────

func TestEmptyPanelNoPanic(t *testing.T) {
	m := testModel(t)

	// Empty proposals panel
	m.activePanel = panelProposals
	m.propPanel.proposals = nil
	m.propPanel.sel = 0

	m = sendDrain(t, m, "j")
	m = sendDrain(t, m, "k")
	m = sendDrain(t, m, "enter")
	// No panic = pass

	// Empty queue panel
	m.activePanel = panelQueue
	m.queuePanel.tasks = nil
	m.queuePanel.sel = 0

	m = sendDrain(t, m, "j")
	m = sendDrain(t, m, "k")
	m = sendDrain(t, m, "enter")
	// No panic = pass
}

// ── ClampSel ─────────────────────────────────────────────────────────────────

func TestClampSelEmptyList(t *testing.T) {
	// Proposal panel
	pp := &proposalPanel{proposals: nil, sel: 5}
	pp.ClampSel()
	if pp.sel != 0 {
		t.Fatalf("proposal ClampSel empty: expected 0, got %d", pp.sel)
	}

	// Queue panel
	qp := &queuePanel{tasks: nil, sel: 3}
	qp.ClampSel()
	if qp.sel != 0 {
		t.Fatalf("queue ClampSel empty: expected 0, got %d", qp.sel)
	}

	// Non-empty clamp
	pp2 := &proposalPanel{
		proposals: []store.Proposal{{ID: "a"}},
		sel:       5,
	}
	pp2.ClampSel()
	if pp2.sel != 0 {
		t.Fatalf("proposal ClampSel overflow: expected 0, got %d", pp2.sel)
	}

	// In-bounds — no change
	pp3 := &proposalPanel{
		proposals: []store.Proposal{{ID: "a"}, {ID: "b"}},
		sel:       1,
	}
	pp3.ClampSel()
	if pp3.sel != 1 {
		t.Fatalf("proposal ClampSel in-bounds: expected 1, got %d", pp3.sel)
	}
}

// ── Architect Done → Proposals ───────────────────────────────────────────────

func TestArchitectDoneAddsProposals(t *testing.T) {
	m := testModel(t)
	if len(m.propPanel.proposals) != 0 {
		t.Fatal("should start with no proposals")
	}

	// Simulate chatProposalsMsg (what HandleDone sends to parent)
	props := []store.Proposal{
		{ID: engine.GenID(), Description: "refactor auth", Instruction: "do it"},
		{ID: engine.GenID(), Description: "add logging", Instruction: "slog everywhere"},
	}
	m, _ = sendMsg(t, m, chatProposalsMsg{proposals: props})

	if len(m.propPanel.proposals) != 2 {
		t.Fatalf("expected 2 proposals, got %d", len(m.propPanel.proposals))
	}
	if m.propPanel.proposals[0].Description != "refactor auth" {
		t.Fatalf("wrong proposal: %s", m.propPanel.proposals[0].Description)
	}
}
