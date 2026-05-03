package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	gollms "github.com/flitsinc/go-llms/tools"
	"github.com/metalim/jsonmap"
)

const luaToolTimeout = 30 * time.Second

// LuaScope controls which agent type a Lua tool is available to.
type LuaScope string

const (
	ScopeArchitect LuaScope = "architect"
	ScopeWorker    LuaScope = "worker"
	ScopeBoth      LuaScope = "both"
)

// LoadLuaTools scans dir for *.lua files and returns those matching the given scope.
// Each file must declare: name, description, scope, params, and a run() function.
// Files that fail to load are skipped with a stderr warning.
func LoadLuaTools(dir string, cwd string, scope LuaScope) []gollms.Tool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []gollms.Tool
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lua") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		t, toolScope, err := loadLuaTool(path, cwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "menace: skipping %s: %v\n", e.Name(), err)
			continue
		}
		if toolScope == ScopeBoth || toolScope == scope {
			out = append(out, t)
		}
	}
	return out
}

// luaParamDef holds the parsed param metadata from the Lua file header.
type luaParamDef struct {
	Name        string
	Type        string // "string" | "number" | "boolean"
	Description string
	Required    bool
}

func loadLuaTool(path, cwd string) (gollms.Tool, LuaScope, error) {
	L := lua.NewState()
	defer L.Close()

	if err := L.DoFile(path); err != nil {
		return nil, "", fmt.Errorf("lua error: %w", err)
	}

	name, err := luaString(L, "name")
	if err != nil {
		return nil, "", err
	}
	desc, err := luaString(L, "description")
	if err != nil {
		return nil, "", err
	}
	scopeStr, err := luaString(L, "scope")
	if err != nil {
		return nil, "", err
	}
	toolScope := LuaScope(scopeStr)
	if toolScope != ScopeArchitect && toolScope != ScopeWorker && toolScope != ScopeBoth {
		return nil, "", fmt.Errorf("scope must be \"architect\", \"worker\", or \"both\"; got %q", scopeStr)
	}

	params, err := luaParams(L)
	if err != nil {
		return nil, "", err
	}

	schema := buildSchema(name, desc, params)

	t := gollms.External(name, schema, func(r gollms.Runner, raw json.RawMessage) gollms.Result {
		var paramMap map[string]any
		if err := json.Unmarshal(raw, &paramMap); err != nil {
			return gollms.SuccessFromString(fmt.Sprintf("error: bad params: %v", err))
		}
		return runLuaTool(path, cwd, paramMap)
	})
	return t, toolScope, nil
}

func runLuaTool(path, cwd string, params map[string]any) gollms.Result {
	L := lua.NewState()
	defer L.Close()

	// Expose exec(cwd, cmd, ...args) — runs a process inside the project directory
	// and returns combined stdout+stderr. No shell, no pipes — named commands only.
	L.SetGlobal("exec", L.NewFunction(func(L *lua.LState) int {
		dir := L.CheckString(1)
		if dir == "" {
			dir = cwd
		}
		cmdName := L.CheckString(2)
		var args []string
		for i := 3; i <= L.GetTop(); i++ {
			args = append(args, L.CheckString(i))
		}
		ctx, cancel := context.WithTimeout(context.Background(), luaToolTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, cmdName, args...)
		cmd.Dir = dir
		out, _ := cmd.CombinedOutput()
		L.Push(lua.LString(strings.TrimSpace(string(out))))
		return 1
	}))

	if err := L.DoFile(path); err != nil {
		return gollms.SuccessFromString(fmt.Sprintf("error loading tool: %v", err))
	}

	runFn := L.GetGlobal("run")
	if runFn == lua.LNil {
		return gollms.SuccessFromString("error: tool has no run() function")
	}

	tbl := L.NewTable()
	for k, v := range params {
		switch val := v.(type) {
		case string:
			tbl.RawSetString(k, lua.LString(val))
		case float64:
			tbl.RawSetString(k, lua.LNumber(val))
		case bool:
			if val {
				tbl.RawSetString(k, lua.LTrue)
			} else {
				tbl.RawSetString(k, lua.LFalse)
			}
		}
	}

	if err := L.CallByParam(lua.P{
		Fn:      runFn,
		NRet:    1,
		Protect: true,
	}, lua.LString(cwd), tbl); err != nil {
		return gollms.SuccessFromString(fmt.Sprintf("error: %v", err))
	}

	result := L.Get(-1)
	L.Pop(1)
	return gollms.SuccessFromString(result.String())
}

// ── Lua header parsing helpers ─────────────────────────────────────────────

func luaString(L *lua.LState, name string) (string, error) {
	v := L.GetGlobal(name)
	if v == lua.LNil {
		return "", fmt.Errorf("missing required field: %s", name)
	}
	s, ok := v.(lua.LString)
	if !ok {
		return "", fmt.Errorf("field %s must be a string", name)
	}
	return string(s), nil
}

func luaParams(L *lua.LState) ([]luaParamDef, error) {
	v := L.GetGlobal("params")
	if v == lua.LNil {
		return nil, nil
	}
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("params must be a table")
	}
	var defs []luaParamDef
	var parseErr error
	tbl.ForEach(func(_, val lua.LValue) {
		if parseErr != nil {
			return
		}
		entry, ok := val.(*lua.LTable)
		if !ok {
			parseErr = fmt.Errorf("each param must be a table")
			return
		}
		def := luaParamDef{
			Name:        tableString(entry, "name"),
			Type:        tableString(entry, "type"),
			Description: tableString(entry, "description"),
			Required:    true,
		}
		if req := entry.RawGetString("required"); req != lua.LNil {
			def.Required = lua.LVAsBool(req)
		}
		if def.Name == "" {
			parseErr = fmt.Errorf("param missing name")
			return
		}
		if def.Type == "" {
			def.Type = "string"
		}
		defs = append(defs, def)
	})
	return defs, parseErr
}

func tableString(t *lua.LTable, key string) string {
	v := t.RawGetString(key)
	if s, ok := v.(lua.LString); ok {
		return string(s)
	}
	return ""
}

// buildSchema constructs a FunctionSchema from the parsed param definitions.
func buildSchema(name, description string, params []luaParamDef) *gollms.FunctionSchema {
	props := jsonmap.New()
	var required []string
	for _, p := range params {
		props.Set(p.Name, gollms.ValueSchema{
			Type:        p.Type,
			Description: p.Description,
		})
		if p.Required {
			required = append(required, p.Name)
		}
	}
	return &gollms.FunctionSchema{
		Name:        name,
		Description: description,
		Parameters: gollms.ValueSchema{
			Type:       "object",
			Properties: props,
			Required:   required,
		},
	}
}
