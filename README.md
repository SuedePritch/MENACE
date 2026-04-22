# MENACE

A command center for AI-driven code changes.

![MENACE Workflow](https://vhs.charm.sh/vhs-78ioaWWNdQ5bG8PpIo4Q9e.gif)

---

## Why MENACE?

AI coding tools are too chatty. They make you watch them think—the internal monologue, the file-hopping, the endless streaming. You don't care about what it's doing; you just want to know what it’s going to do, and then see it done.

MENACE removes the friction. You talk to an **Architect** to shape a plan, not to watch an agent struggle. When it's ready, the Architect produces a **proposal**: a set of concrete subtasks scoped to specific files and functions.

You review it. You approve it. **Workers** pick up the subtasks and execute them in parallel. No watching. No babysitting. Plan, approve, move on.

- **Architect** — plans changes through AST-powered code intelligence tools
- **Workers** — cheap/fast models (Gemini Flash Lite, GPT-4.1 Nano, local Ollama) that execute subtasks in parallel
- **You** — always in control, never waiting

---

## Features

- **Code-aware planning** — pluggable AST indexing with built-in TS/JS support (tree-sitter), extensible to any language
- **Parallel execution** — conflict-aware scheduler runs multiple workers simultaneously without file collisions
- **Proposal review** — approve, reject, or modify before any code is touched
- **Diff inspection** — per-subtask git diffs captured and viewable in-app
- **Session persistence** — full chat history, proposals, and task state saved to SQLite
- **Multi-provider** — Anthropic, Google Gemini, OpenAI, Ollama — models fetched live from API
- **Vim keybindings** — fully customizable themes, keys, and layout
- **Theme system** — built-in themes, custom themes via TOML, or duplicate and edit with `$EDITOR`
- **Token tracking** — cumulative token usage displayed in the banner
- **Settings UI** — in-app settings modal for config, theme, auth, and indexer status

---

## How It Works

```
  You ──► Chat with Architect ──► Proposal (subtasks)
                                       │
                                  Review & Approve
                                       │
                              ┌────────┼────────┐
                              ▼        ▼        ▼
                           Worker   Worker   Worker
                           (edit)   (edit)   (edit)
                              │        │        │
                              └────────┼────────┘
                                       │
                                  Diffs captured
                                  Review results
```

1. **Chat** with the Architect about what you want to change
2. **Architect proposes** subtasks grounded in your actual code structure
3. **You review** the proposal — approve, reject, or ask for changes
4. **Workers execute** subtasks in parallel (conflict-aware scheduling)
5. **Inspect diffs**, check logs, retry or revert if needed

---

## Install

### Prerequisites

- Go 1.25+
- Git

### Build

```bash
git clone https://github.com/SuedePritch/menace.git
cd menace
make build
```

### Install to PATH

```bash
make install
# Installs menace to /usr/local/bin
```

---

## Quick Start

```bash
cd ~/my-project
menace
```

First run walks you through setup:

![MENACE Setup](https://vhs.charm.sh/vhs-4cyR1QZdv4ccCSvKBzHbDi.gif)

1. **Select provider** — Anthropic, Google, OpenAI, or Ollama
2. **Enter API key** — or set `ANTHROPIC_API_KEY` / `GEMINI_API_KEY` / `OPENAI_API_KEY` env var (Ollama runs locally, no key needed)
3. **Pick architect model** — fetched live from your provider's API
4. **Pick worker model** — cheap/fast model for task execution
5. **Start chatting**

API keys and model selections are stored in the local SQLite database. No config files to manage for auth.

### Example Workflow

```
You:        "Add error handling to all the API fetch calls in src/api/"

Architect:  Analyzes codebase via AST tools, proposes 4 subtasks:
            1. Wrap fetchUser() in try-catch with typed error
            2. Wrap fetchPosts() in try-catch with typed error
            3. Wrap fetchComments() in try-catch with typed error
            4. Add shared ApiError type to types.ts

You:        Review proposal → Approve

MENACE:     Schedules workers (3 concurrent, respects file conflicts)
            ██████████████░░ 3/4 complete

You:        Review diffs per subtask → Done
```

---

## Keybindings

### Normal Mode
| Key | Action |
|-----|--------|
| `j/k` | Navigate up/down |
| `h/l` | Switch panels |
| `Tab` | Next panel |
| `i` or `/` | Start typing |
| `Enter` | Open/confirm |
| `,` | Settings |
| `T` | Cycle theme |
| `S` | Sessions |
| `P` | Cycle project |
| `r` | Restart architect |
| `Ctrl+N` | New session |
| `Ctrl+C` | Quit |

### Insert Mode
| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Alt+Enter` | Newline |
| `Esc` | Back to normal mode |

### Modal (Proposal/Task Review)
| Key | Action |
|-----|--------|
| `a` | Approve |
| `x` | Cancel |
| `D` | Delete |
| `r` | Retry |
| `Tab` | Switch pane |
| `Esc/q` | Close |

All keybindings are customizable in `config.json` under the `keys` object.

---

## Configuration

MENACE stores config in `config.json` in the app directory:

```json
{
  "concurrency": 3,
  "max_retry": 2,
  "theme": "menace",
  "indexers": [
    {"binary": "/path/to/my-go-indexer"}
  ]
}
```

API keys are stored in the OS keychain (Keychain on macOS, Secret Service on Linux, wincred on Windows). Provider and model selections are stored in the local SQLite database. All managed through the setup flow and settings modal.

---

## Themes

Three built-in themes: `menace` (default), `system` (terminal colors), `omarchy` (reads from `~/.config/omarchy`).

**Custom themes**: Press `,` → navigate to "customize theme" → MENACE duplicates the current theme to `themes/custom.toml` and opens it in `$EDITOR`. Edit colors, banner art, personality strings — everything.

**Sharing themes**: Drop any `.toml` file in the `themes/` directory. It shows up in the theme picker automatically.

Theme TOML structure:

```toml
[meta]
name = "my-theme"
author = "you"

[colors]
active = "#3aff37"
accent = "#ff3dbe"
text = "#e0e0e0"
# ... 11 color slots total

[personality]
banner = "YOUR ASCII ART HERE"
welcome = "what are we breaking today?"
panel_architect = "brain"
panel_proposals = "proposals"
panel_tasks = "queue"
# ... full personality customization
```

---

## AST Indexers

MENACE ships with a built-in **TS/JS indexer** (tree-sitter) that gives the Architect proper code intelligence — accurate symbol extraction, export status, dependency tracking, source snippets.

For other languages, you can plug in **external indexers** — any binary that speaks a simple JSON protocol.

### Protocol

An external indexer is a CLI binary that responds to 4 commands:

```bash
# What file types do you handle?
./my-indexer extensions
# → [".go", ".rs"]

# List all symbols in a file
./my-indexer symbols /path/to/file.go
# → [{"name": "Foo", "kind": "function", "filePath": "...", "startLine": 1, "endLine": 10, "source": "func Foo() {...}"}]

# Index an entire directory
./my-indexer index /path/to/project
# → {"symbols": [...], "tokenEstimate": 5000}

# Find a symbol by name (optionally scoped to a file)
./my-indexer find MyFunc /path/to/file.go
# → [{"name": "MyFunc", ...}]
```

### Symbol Schema

```json
{
  "name": "string",
  "kind": "function|class|method|type|interface|enum",
  "filePath": "string",
  "startLine": 1,
  "endLine": 10,
  "source": "full source text",
  "exportStatus": "exported|unexported|default",
  "dependencies": ["calledFunction1", "calledFunction2"],
  "dependents": ["callerFunction1"]
}
```

### Configuration

Add external indexers in `config.json`:

```json
{
  "indexers": [
    {"binary": "/usr/local/bin/go-indexer"},
    {"binary": "./my-rust-indexer"}
  ]
}
```

On startup, each indexer is validated with a smoke test. The settings modal (`,`) shows indexer health status — green for working, red with error message for broken.

For files with no indexer, tools fall back to regex-based extraction (works on any language, less accurate).

---

## Architecture

```
MENACE/
├── main.go                    # Entry point, indexer registration
├── internal/
│   ├── tui/                   # Terminal UI (Bubble Tea)
│   │   ├── run.go             # Exported entry point
│   │   ├── model.go           # TUI state, project/session/theme grouping
│   │   ├── update.go          # Central event dispatch
│   │   ├── update_normal.go   # Normal/insert mode handlers
│   │   ├── update_modals.go   # Modal event handlers
│   │   ├── view.go            # Dashboard layout, banner, help bar
│   │   ├── view_panels.go     # Markdown rendering, table rendering
│   │   ├── panel_chat.go      # Architect chat panel (self-contained)
│   │   ├── panel_proposals.go # Proposal list panel
│   │   ├── panel_queue.go     # Task queue panel
│   │   ├── modal_review.go    # Task review: files, diffs, logs
│   │   ├── modal_proposal.go  # Proposal review modal
│   │   ├── modal_sessions.go  # Session picker modal
│   │   ├── modal_settings.go  # Settings modal
│   │   ├── modal_msgs.go      # Modal message types
│   │   ├── setup.go           # First-run setup wizard
│   │   ├── keys.go            # Keybinding system (vim-like)
│   │   ├── theme.go           # Colors + base styles
│   │   └── util.go            # Text wrapping, ANSI stripping
│   ├── agent/                 # LLM agent layer (go-llms)
│   │   ├── agent.go           # Agent wrapper, provider factory, usage tracking
│   │   ├── tools_read.go      # Read-only tools (architect): AST, grep, outline
│   │   └── tools_write.go     # Write tools (worker): edit, replace, insert
│   ├── engine/                # Orchestration layer
│   │   ├── architect.go       # Persistent architect process, proposal parsing
│   │   ├── orchestrator.go    # Conflict-aware task scheduler, git diff capture
│   │   ├── store.go           # TaskStore interface (orchestrator boundary)
│   │   ├── tasks.go           # Task creation helpers
│   │   ├── session.go         # Session creation
│   │   ├── providers.go       # Provider presets + defaults
│   │   └── models.go          # Live model fetching from APIs
│   ├── indexer/               # Pluggable code intelligence
│   │   ├── interface.go       # Indexer interface + Symbol type
│   │   ├── registry.go        # Extension-based registry + health checks
│   │   ├── builtin.go         # Built-in TS/JS adapter
│   │   └── external.go        # External binary protocol
│   ├── store/                 # SQLite persistence
│   │   ├── store.go           # Schema, migrations, project methods
│   │   ├── store_auth.go      # Auth (provider, model, keyring)
│   │   ├── store_tasks.go     # Task CRUD + status transitions
│   │   ├── store_proposals.go # Proposal persistence
│   │   ├── store_sessions.go  # Session persistence + chat history
│   │   ├── store_logs.go      # Task logs + diff storage
│   │   ├── types.go           # Data types (TaskData, Session, etc.)
│   │   └── crypt.go           # OS keychain integration (go-keyring)
│   ├── config/                # Config + theme management
│   │   ├── config.go          # MenaceConfig, load/save, validation
│   │   └── theme.go           # Theme loading, TOML, personality strings
│   ├── ollama/                # Ollama process management
│   ├── workspace/             # Project hash, directory picker
│   └── log/                   # Structured file logging (slog)
├── prompts/
│   ├── architect.md           # Architect system prompt (editable)
│   └── worker.md              # Worker system prompt (editable)
├── themes/                    # Custom theme TOML files
├── docs/ideas/                # Feature specs for contributors
└── code-indexer/              # Standalone TS/JS indexer (tree-sitter)
```

---

## Contributing

I've written up some things I want to build in [`docs/ideas/`](docs/ideas/). If something interests you, read [ARCHITECTURE.md](ARCHITECTURE.md) for the patterns and open a PR. See [CONTRIBUTING.md](CONTRIBUTING.md) for the ground rules.

If you notice a bug, fix it. Talk is cheap, send patches.

---

## License

This project is licensed under the [Functional Source License, Version 1.1, MIT Future License (FSL-1.1-MIT)](LICENSE.md).

You can use, modify, and distribute this software for any purpose except building a competing product or service. After two years, each version converts to MIT.

> Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), [go-llms](https://github.com/flitsinc/go-llms), and too much caffeine.
