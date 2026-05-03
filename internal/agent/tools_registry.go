package agent

import (
	"path/filepath"

	"github.com/flitsinc/go-llms/tools"
)

// ToolFactory creates a tool bound to the project working directory.
type ToolFactory func(cwd string) tools.Tool

type registeredTool struct {
	scope   LuaScope
	factory ToolFactory
}

var toolRegistry []registeredTool

// RegisterTool registers a tool factory with an explicit scope.
// Use ScopeArchitect, ScopeWorker, or ScopeBoth.
// Call from an init() function in any file in this package.
func RegisterTool(scope LuaScope, f ToolFactory) {
	toolRegistry = append(toolRegistry, registeredTool{scope, f})
}

// buildTools assembles the tool list for a given caller scope (architect or worker).
// A tool is included when its declared scope is ScopeBoth or matches the caller.
func buildTools(menaceDir, cwd string, caller LuaScope) []tools.Tool {
	var t []tools.Tool
	for _, rt := range toolRegistry {
		if rt.scope == ScopeBoth || rt.scope == caller {
			t = append(t, rt.factory(cwd))
		}
	}
	t = append(t, LoadLuaTools(filepath.Join(menaceDir, "tools"), cwd, caller)...)
	return t
}
