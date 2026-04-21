package tui

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"menace/internal/config"
	"menace/internal/engine"
	"menace/internal/indexer"
	mlog "menace/internal/log"
	"menace/internal/store"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// settingsEditMode tracks the current interaction mode in the settings modal.
type settingsEditMode int

const (
	settingsNav      settingsEditMode = iota
	settingsEditing
	settingsPickList
)

type settingsItemKind int

const (
	siDisplay settingsItemKind = iota
	siNumeric
	siPickList
	siAction
)

type settingsItem struct {
	key     string
	label   string
	section string
	kind    settingsItemKind
}

func settingsItems() []settingsItem {
	return []settingsItem{
		{"provider", "provider", "auth", siDisplay},
		{"architect_model", "architect", "auth", siPickList},
		{"worker_model", "worker", "auth", siPickList},
		{"concurrency", "concurrency", "performance", siNumeric},
		{"max_retry", "max retry", "performance", siNumeric},
		{"theme", "theme", "theme", siPickList},
		{"customize", "customize theme", "theme", siAction},
		{"logout", "logout", "", siAction},
	}
}

// selectableItems returns only the items that can be navigated to.
func selectableItems() []settingsItem {
	var items []settingsItem
	for _, item := range settingsItems() {
		if item.kind != siDisplay {
			items = append(items, item)
		}
	}
	return items
}

// SettingsModal encapsulates the settings modal state.
type SettingsModal struct {
	store      *store.Store
	cfg        config.MenaceConfig
	theme      config.Theme
	themeNames []string
	menaceDir  string
	projectID  string
	sel        int
	mode       settingsEditMode
	editBuf    string
	pickItems  []string
	pickSel    int
	prevTheme  string
}

// NewSettingsModal creates a settings modal.
func NewSettingsModal(s *store.Store, cfg config.MenaceConfig, theme config.Theme, themeNames []string, menaceDir, projectID string) *SettingsModal {
	return &SettingsModal{
		store:      s,
		cfg:        cfg,
		theme:      theme,
		themeNames: themeNames,
		menaceDir:  menaceDir,
		projectID:  projectID,
	}
}

// IsEditing returns true when the modal is in numeric editing mode.
func (sm *SettingsModal) IsEditing() bool {
	return sm.mode == settingsEditing
}

func (sm *SettingsModal) WantsRawKeys() bool { return sm.mode == settingsEditing }

// Resize is a no-op; SettingsModal computes its own layout in View.
func (sm *SettingsModal) Resize(w, h int) {}

// Update processes an action and returns a command for the parent.
func (sm *SettingsModal) Update(act action) tea.Cmd {
	selItems := selectableItems()
	itemCount := len(selItems)

	switch sm.mode {
	case settingsPickList:
		return sm.handlePickList(act)
	case settingsEditing:
		return nil
	}

	// Nav mode
	switch act {
	case actEscape:
		return func() tea.Msg { return modalCloseMsg{} }
	case actDown:
		if sm.sel < itemCount-1 {
			sm.sel++
		}
	case actUp:
		if sm.sel > 0 {
			sm.sel--
		}
	case actConfirm:
		if sm.sel >= itemCount {
			return nil
		}
		item := selItems[sm.sel]
		switch item.kind {
		case siNumeric:
			sm.mode = settingsEditing
			sm.editBuf = sm.settingsValue(item.key)
		case siPickList:
			sm.mode = settingsPickList
			sm.pickSel = 0
			switch item.key {
			case "theme":
				sm.pickItems = sm.themeNames
				sm.prevTheme = sm.theme.Meta.Name
				for i, name := range sm.themeNames {
					if name == sm.theme.Meta.Name {
						sm.pickSel = i
						break
					}
				}
			case "architect_model":
				sm.pickItems = sm.modelIDs("architect")
				current := sm.settingsValue("architect_model")
				for i, id := range sm.pickItems {
					if id == current {
						sm.pickSel = i
						break
					}
				}
			case "worker_model":
				sm.pickItems = sm.modelIDs("worker")
				current := sm.settingsValue("worker_model")
				for i, id := range sm.pickItems {
					if id == current {
						sm.pickSel = i
						break
					}
				}
			}
		case siAction:
			switch item.key {
			case "logout":
				return func() tea.Msg { return settingsLogoutMsg{} }
			case "customize":
				return sm.customize()
			}
		}
	}
	return nil
}

func (sm *SettingsModal) handlePickList(act action) tea.Cmd {
	switch act {
	case actEscape:
		item := selectableItems()[sm.sel]
		if item.key == "theme" && sm.prevTheme != "" {
			sm.theme = config.LoadTheme(sm.prevTheme, sm.menaceDir)
			applyThemeColors(sm.theme.Colors)
		}
		sm.mode = settingsNav
	case actDown:
		if sm.pickSel < len(sm.pickItems)-1 {
			sm.pickSel++
			sm.previewPick()
		}
	case actUp:
		if sm.pickSel > 0 {
			sm.pickSel--
			sm.previewPick()
		}
	case actConfirm:
		if sm.pickSel >= len(sm.pickItems) {
			sm.mode = settingsNav
			return nil
		}
		item := selectableItems()[sm.sel]
		picked := sm.pickItems[sm.pickSel]
		switch item.key {
		case "theme":
			sm.theme = config.LoadTheme(picked, sm.menaceDir)
			applyThemeColors(sm.theme.Colors)
			sm.cfg.Theme = picked
			config.Save(sm.menaceDir, sm.cfg)
			_ = sm.store.SetProjectTheme(sm.projectID, picked)
			sm.mode = settingsNav
			return func() tea.Msg {
				return settingsThemeChangedMsg{ThemeName: picked, Theme: sm.theme}
			}
		case "architect_model":
			auth, _ := sm.store.GetAuth()
			if auth != nil {
				_ = sm.store.SaveAuth(auth.Provider, auth.APIKey, picked, auth.WorkerModel)
			}
			sm.mode = settingsNav
			return func() tea.Msg { return settingsModelChangedMsg{} }
		case "worker_model":
			auth, _ := sm.store.GetAuth()
			if auth != nil {
				_ = sm.store.SaveAuth(auth.Provider, auth.APIKey, auth.ArchitectModel, picked)
			}
			sm.mode = settingsNav
			return func() tea.Msg { return settingsModelChangedMsg{} }
		}
		sm.mode = settingsNav
	}
	return nil
}

func (sm *SettingsModal) previewPick() {
	item := selectableItems()[sm.sel]
	if item.key == "theme" && sm.pickSel < len(sm.pickItems) {
		name := sm.pickItems[sm.pickSel]
		preview := config.LoadTheme(name, sm.menaceDir)
		applyThemeColors(preview.Colors)
	}
}

func (sm *SettingsModal) saveEdit() tea.Cmd {
	selItems := selectableItems()
	if sm.sel >= len(selItems) {
		return nil
	}
	item := selItems[sm.sel]
	val := 0
	fmt.Sscanf(sm.editBuf, "%d", &val)
	if val < 1 {
		val = 1
	}
	switch item.key {
	case "concurrency":
		if val > 20 {
			val = 20
		}
		sm.cfg.Concurrency = val
	case "max_retry":
		if val > 10 {
			val = 10
		}
		sm.cfg.MaxRetry = val
	}
	config.Save(sm.menaceDir, sm.cfg)
	return func() tea.Msg { return settingsCfgChangedMsg{Cfg: sm.cfg} }
}

// HandleRawKey handles raw key input for numeric editing mode.
func (sm *SettingsModal) HandleRawKey(key string) tea.Cmd {
	switch key {
	case "enter":
		cmd := sm.saveEdit()
		sm.mode = settingsNav
		return cmd
	case "esc", "escape":
		sm.mode = settingsNav
	case "backspace":
		if len(sm.editBuf) > 0 {
			sm.editBuf = sm.editBuf[:len(sm.editBuf)-1]
		}
	default:
		if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
			sm.editBuf += key
		}
	}
	return nil
}

func (sm *SettingsModal) customize() tea.Cmd {
	path, err := config.ExportCustomTheme(sm.menaceDir, sm.theme)
	if err != nil {
		mlog.Error("ExportCustomTheme", slog.String("err", err.Error()))
		return func() tea.Msg { return modalCloseMsg{} }
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, path)
	return tea.Batch(
		func() tea.Msg { return settingsCustomizeMsg{} },
		tea.ExecProcess(cmd, func(err error) tea.Msg {
			return customizeEditDoneMsg{err: err}
		}),
	)
}

// modelIDs returns model ID strings for the current provider, fetching live from the API.
// Falls back to hardcoded lists on failure. tier is "architect" or "worker".
func (sm *SettingsModal) modelIDs(tier string) []string {
	auth, _ := sm.store.GetAuth()
	if auth == nil {
		return nil
	}
	architect, worker := engine.FetchModels(auth.Provider, auth.APIKey)
	var models []engine.ModelOption
	if tier == "architect" {
		models = architect
	} else {
		models = worker
	}
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	return ids
}

func (sm *SettingsModal) settingsValue(key string) string {
	auth, _ := sm.store.GetAuth()
	switch key {
	case "provider":
		if auth != nil && auth.Provider != "" {
			return auth.Provider
		}
		return "—"
	case "architect_model":
		if auth != nil && auth.ArchitectModel != "" {
			return auth.ArchitectModel
		}
		return "—"
	case "worker_model":
		if auth != nil && auth.WorkerModel != "" {
			return auth.WorkerModel
		}
		return "—"
	case "concurrency":
		return fmt.Sprintf("%d", sm.cfg.Concurrency)
	case "max_retry":
		return fmt.Sprintf("%d", sm.cfg.MaxRetry)
	case "theme":
		return sm.theme.Meta.Name
	}
	return ""
}

// View renders the settings modal.
func (sm *SettingsModal) View(w, h int) string {
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	valStyle := lipgloss.NewStyle().Foreground(ColorInfo)
	selLabel := lipgloss.NewStyle().Foreground(ColorText).Bold(true)
	cursor := lipgloss.NewStyle().Foreground(ColorActive).Render("▸ ")
	indent := "  "
	logoutStyle := lipgloss.NewStyle().Foreground(ColorFail).Bold(true)

	allItems := settingsItems()
	selItems := selectableItems()
	selIdx := sm.sel

	var lines []string
	lines = append(lines, "")

	lastSection := ""
	selectableIdx := 0

	for _, item := range allItems {
		if item.section != lastSection && item.section != "" {
			if lastSection != "" {
				lines = append(lines, "")
			}
			lines = append(lines, indent+sectionStyle.Render(item.section))
			lastSection = item.section
		}

		if item.kind == siDisplay {
			val := sm.settingsValue(item.key)
			lines = append(lines, indent+"  "+labelStyle.Render(item.label)+"  "+valStyle.Render(val))
			continue
		}

		isSel := selectableIdx == selIdx
		selectableIdx++

		switch item.key {
		case "logout":
			if lastSection != "" {
				lines = append(lines, "")
			}
			if isSel {
				lines = append(lines, indent+cursor+logoutStyle.Render(item.label))
			} else {
				lines = append(lines, indent+"  "+lipgloss.NewStyle().Foreground(ColorMuted).Render(item.label))
			}

		case "customize":
			if isSel {
				lines = append(lines, indent+cursor+selLabel.Render(item.label))
			} else {
				lines = append(lines, indent+"  "+labelStyle.Render(item.label))
			}

		default:
			val := sm.settingsValue(item.key)

			if isSel && sm.mode == settingsEditing {
				editCursor := lipgloss.NewStyle().Foreground(ColorActive).Render("█")
				val = lipgloss.NewStyle().Foreground(ColorText).Render(sm.editBuf) + editCursor
			}

			if isSel {
				lines = append(lines, indent+cursor+selLabel.Render(item.label)+"  "+val)
			} else {
				lines = append(lines, indent+"  "+labelStyle.Render(item.label)+"  "+valStyle.Render(val))
			}
		}

		if isSel && sm.mode == settingsPickList {
			for j, pick := range sm.pickItems {
				if j == sm.pickSel {
					lines = append(lines, indent+"    "+cursor+lipgloss.NewStyle().Foreground(ColorText).Bold(true).Render(pick))
				} else {
					lines = append(lines, indent+"      "+lipgloss.NewStyle().Foreground(ColorMuted).Render(pick))
				}
			}
		}
	}

	// Indexer status section
	idxStatuses := indexer.Statuses()
	if len(idxStatuses) > 0 {
		lines = append(lines, "")
		lines = append(lines, indent+sectionStyle.Render("indexers"))
		for _, s := range idxStatuses {
			if s.Healthy {
				lines = append(lines, indent+"  "+lipgloss.NewStyle().Foreground(ColorSuccess).Render("●")+" "+labelStyle.Render(s.Name))
			} else {
				lines = append(lines, indent+"  "+lipgloss.NewStyle().Foreground(ColorFail).Render("●")+" "+labelStyle.Render(s.Name)+"  "+lipgloss.NewStyle().Foreground(ColorFail).Render(s.Error))
			}
		}
	}

	lines = append(lines, "")

	// Help bar
	var help string
	switch sm.mode {
	case settingsEditing:
		help = renderHelp(
			helpEntry{Key: "0-9", Label: "type"},
			helpEntry{Key: "bksp", Label: "delete"},
			helpKeyLabel(modalKeys, actConfirm, "save"),
			helpKeyLabel(modalKeys, actEscape, "cancel"),
		)
	case settingsPickList:
		help = renderHelp(
			helpPair(modalKeys, actDown, actUp, "select"),
			helpKeyLabel(modalKeys, actConfirm, "confirm"),
			helpKeyLabel(modalKeys, actEscape, "cancel"),
		)
	default:
		var extraEntries []helpEntry
		if selIdx < len(selItems) && selItems[selIdx].kind == siNumeric {
			extraEntries = append(extraEntries, helpEntry{Key: "+/-", Label: "adjust"})
		}
		help = renderHelp(append([]helpEntry{
			helpPair(modalKeys, actDown, actUp, "nav"),
			helpKeyLabel(modalKeys, actConfirm, "edit"),
		}, append(extraEntries, helpKeyLabel(modalKeys, actEscape, "close"))...)...)
	}
	lines = append(lines, indent+help)
	lines = append(lines, "")

	content := strings.Join(lines, "\n")

	contentW := w / 2
	if contentW < 50 {
		contentW = 50
	}
	if contentW > w-4 {
		contentW = w - 4
	}
	contentH := len(lines)
	if contentH > h-4 {
		contentH = h - 4
	}

	box := bentoBox.BorderForeground(ColorAccent).Width(contentW).Height(contentH).
		Render(content)
	box = injectPanelTitle(box, "settings", true)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
}
