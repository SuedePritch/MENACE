package tui

import "menace/internal/config"

// action represents a semantic user action, decoupled from the physical key.
type action int

const (
	actNone action = iota

	actDown
	actUp
	actLeft
	actRight
	actNextPanel
	actPrevPanel

	actHalfDown
	actHalfUp
	actTop
	actBottom

	actConfirm
	actApprove
	actDelete
	actRetry
	actCancel

	actClearAll
	actNewSession
	actSessions
	actProjectCycle
	actProjectAdd

	actInsert
	actEscape
	actQuit
	actTheme

	actSend
	actNewline
	actClearInput
	actClearChat

	actSwitchPane
	actCycleScope
	actToggleLogs

	actPageUp
	actPageDown

	actSettings
	actRestart
)

var normalKeys = map[string]action{
	"j":         actDown,
	"down":      actDown,
	"k":         actUp,
	"up":        actUp,
	"h":         actLeft,
	"left":      actLeft,
	"l":         actRight,
	"right":     actRight,
	"tab":       actNextPanel,
	"shift+tab": actPrevPanel,
	"d":         actHalfDown,
	"u":         actHalfUp,
	"G":         actBottom,
	"g":         actTop,
	"enter":     actConfirm,
	"i":         actInsert,
	"/":         actInsert,
	"X":         actClearAll,
	"ctrl+n":    actNewSession,
	"S":         actSessions,
	"P":         actProjectCycle,
	"ctrl+p":    actProjectAdd,
	"ctrl+c":    actQuit,
	"T":         actTheme,
	",":         actSettings,
	"ctrl+l":    actClearChat,
	"x":         actCancel,
	"r":         actRestart,
}

var insertKeys = map[string]action{
	"esc":       actEscape,
	"enter":     actSend,
	"alt+enter": actNewline,
	"ctrl+u":    actClearInput,
	"ctrl+l":    actClearChat,
	"pgup":      actPageUp,
	"pgdown":    actPageDown,
}

var modalKeys = map[string]action{
	"j":     actDown,
	"down":  actDown,
	"k":     actUp,
	"up":    actUp,
	"d":     actHalfDown,
	"u":     actHalfUp,
	"G":     actBottom,
	"g":     actTop,
	"tab":   actSwitchPane,
	"s":     actCycleScope,
	"l":     actToggleLogs,
	"esc":   actEscape,
	"q":     actEscape,
	"a":     actApprove,
	"x":     actCancel,
	"D":     actDelete,
	"r":     actRetry,
	"enter": actConfirm,
}

func resolve(keymap map[string]action, key string) action {
	if a, ok := keymap[key]; ok {
		return a
	}
	return actNone
}

func resolveNormal(key string) action { return resolve(normalKeys, key) }
func resolveInsert(key string) action { return resolve(insertKeys, key) }
func resolveModal(key string) action  { return resolve(modalKeys, key) }

var actionLabels = map[action]string{
	actDown: "scroll", actUp: "scroll",
	actHalfDown: "½ page", actHalfUp: "½ page",
	actTop: "top", actBottom: "bottom",
	actLeft: "left", actRight: "right",
	actNextPanel: "focus", actPrevPanel: "focus",
	actConfirm: "open", actApprove: "approve",
	actDelete: "delete", actRetry: "retry",
	actCancel: "cancel", actClearAll: "clear all",
	actNewSession: "new session", actSessions: "sessions",
	actProjectCycle: "next", actProjectAdd: "add project", actTheme: "theme", actInsert: "chat",
	actSettings: "settings",
	actEscape: "back", actQuit: "quit",
	actSend: "send", actNewline: "newline",
	actClearInput: "clear", actClearChat: "reset",
	actSwitchPane: "focus", actCycleScope: "scope",
	actToggleLogs: "logs",
	actPageUp: "pg up", actPageDown: "pg down",
	actRestart: "restart",
}

var actionNames = map[string]action{
	"down": actDown, "up": actUp, "left": actLeft, "right": actRight,
	"next_panel": actNextPanel, "prev_panel": actPrevPanel,
	"half_down": actHalfDown, "half_up": actHalfUp,
	"top": actTop, "bottom": actBottom,
	"confirm": actConfirm, "approve": actApprove,
	"delete": actDelete, "retry": actRetry,
	"cancel": actCancel, "clear_all": actClearAll,
	"new_session": actNewSession, "sessions": actSessions,
	"project_cycle": actProjectCycle, "project_add": actProjectAdd, "theme": actTheme, "settings": actSettings, "insert": actInsert,
	"escape": actEscape, "quit": actQuit,
	"send": actSend, "newline": actNewline, "restart": actRestart,
	"clear_input": actClearInput, "clear_chat": actClearChat,
	"switch_pane": actSwitchPane,
	"page_up": actPageUp, "page_down": actPageDown,
}

func applyKeyOverrides(k config.KeysConfig) {
	mergeKeys(normalKeys, k.Normal)
	mergeKeys(insertKeys, k.Insert)
	mergeKeys(modalKeys, k.Modal)
}

func mergeKeys(keymap map[string]action, overrides map[string]string) {
	for key, actName := range overrides {
		if act, ok := actionNames[actName]; ok {
			keymap[key] = act
		}
	}
}

func keyFor(keymap map[string]action, act action) string {
	best := ""
	for k, a := range keymap {
		if a != act {
			continue
		}
		if best == "" || len(k) < len(best) {
			best = k
		}
	}
	if best == "" {
		return "?"
	}
	return best
}

func keysFor(keymap map[string]action, a1, a2 action) string {
	return keyFor(keymap, a1) + "/" + keyFor(keymap, a2)
}

type helpEntry struct {
	Key   string
	Label string
}

func helpPair(keymap map[string]action, a1, a2 action, label string) helpEntry {
	return helpEntry{Key: keysFor(keymap, a1, a2), Label: label}
}

func helpKey(keymap map[string]action, act action) helpEntry {
	return helpEntry{Key: keyFor(keymap, act), Label: actionLabels[act]}
}

func helpKeyLabel(keymap map[string]action, act action, label string) helpEntry {
	return helpEntry{Key: keyFor(keymap, act), Label: label}
}
