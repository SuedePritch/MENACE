# MENACE Architecture

## Overview

MENACE is a terminal UI for AI-assisted code transformation. It implements a **plan-first, human-in-the-loop** workflow: an architect agent plans changes as proposals, the user reviews and approves them, and ephemeral worker agents execute the approved work.

## Core Design: Architect-Worker Pattern

### Why two tiers?

AI coding tools face a tension: powerful models are expensive and slow, fast models lack planning ability. MENACE splits the problem:

- **Architect** — A persistent, conversational agent using a strong model. It reads the codebase via AST-powered tools, discusses with the user, and produces structured proposals. It never writes files.
- **Workers** — One-shot agents using a cheap/fast model. Each worker executes a single task or subtask, has write access to the filesystem, and is discarded after completion.

This gives users control over _what_ changes are made (review proposals before execution) while keeping execution costs low.

### Why one-shot workers?

Persistent worker processes would accumulate context and potentially drift. One-shot workers start fresh with a focused prompt (task description + instructions), execute, and exit. This makes them:
- Predictable: same input → same behavior
- Cheap: no wasted context on conversation history
- Parallelizable: no shared state between workers

### Why SQLite?

Tasks, sessions, proposals, and chat history need to survive crashes and restarts. File-based storage (JSON/YAML) was used initially but caused issues with concurrent writes from multiple goroutines. SQLite with WAL journaling provides:
- ACID transactions for task state transitions
- Concurrent read access during worker execution
- Single-file deployment (no external database)
- Built-in migration support via idempotent ALTER TABLE

## Component Map

```
┌─────────────────────────────────────────────────────┐
│  TUI (Bubble Tea)              internal/tui/        │
│  model.go, view*.go, update*.go, modal_*.go         │
│  - Screen management (setup, dashboard, modals)     │
│  - Vim-like modal input (normal/insert/modal modes) │
│  - Each modal is a self-contained pointer-typed     │
│    struct communicating via tea.Cmd messages         │
└──────────┬──────────────────────────┬───────────────┘
           │                          │
    ┌──────▼──────┐           ┌───────▼────────┐
    │  Architect   │           │  Orchestrator  │
    │  Process     │           │                │
    │  (engine/)   │           │  (engine/)     │
    │              │           │                │
    │  Persistent  │           │  Schedules     │
    │  conversation│           │  workers with  │
    │  via go-llms │           │  conflict-aware│
    │              │           │  concurrency   │
    └──────┬───────┘           └───────┬────────┘
           │                           │
    ┌──────▼───────────────────────────▼──────┐
    │  Agent (internal/agent/)                 │
    │  - Wraps go-llms provider abstraction    │
    │  - Streams events to TUI                 │
    │  - Read tools (architect) / Write tools  │
    └──────┬───────────────────────────────────┘
           │
    ┌──────▼──────────────────────────────────┐
    │  Indexer (internal/indexer/)              │
    │  - Pluggable code intelligence           │
    │  - Built-in: tree-sitter for TS/JS       │
    │  - External: any binary implementing the │
    │    indexer protocol                       │
    │  - Regex fallback for other languages    │
    └─────────────────────────────────────────┘
```

## Key Design Decisions

### Proposal Protocol

The architect embeds proposals in its response using fenced code blocks:

````
```proposal
description: Refactor auth
instruction: |
  Replace session tokens with encrypted cookies.
subtasks:
  - Extract token logic
  - Update middleware
```
````

This allows natural language discussion alongside structured output. Proposals can be JSON or YAML. The parser (`ParseProposalBlocks`) extracts them; `CleanResponse` strips them from the displayed chat.

### Conflict-Aware Scheduling

Tasks declare which files they touch via `Touches`. The orchestrator prevents concurrent execution of tasks with overlapping file sets. This avoids merge conflicts without requiring file locking.

### Serialized Architect Messages

The architect process uses a channel-based message queue to ensure only one prompt executes at a time. This prevents concurrent access to the underlying LLM client, which maintains conversation history and is not goroutine-safe.

### Modal Sub-Models

Each modal (review, proposal, sessions, settings) is a self-contained struct with pointer receiver methods, stored as `*ReviewModal`, `*ProposalModal`, etc. in the main model. This solves two problems:

1. **Value-receiver copies**: Bubble Tea's `Update()` uses value receivers, copying the entire model each frame. Modals containing viewport buffers are expensive to copy. Pointer-typed modals are 8 bytes regardless of internal state.
2. **Mutation safety**: Modals mutate their own state directly via pointer receivers. They communicate back to the parent exclusively through `tea.Cmd` returning typed messages (`modalCloseMsg`, `proposalApprovedMsg`, etc.), never by mutating parent fields.

An open modal is determined by which pointer is non-nil. The parent dispatches keys and handles returned messages.

### Path Sandboxing

All tool file operations are resolved through `resolvePath()`, which ensures paths cannot escape the project's working directory. This prevents an LLM from reading or writing files outside the project.

## Customization Points

- **Themes**: TOML files in `themes/` with colors, banner art, and personality text. Per-project overrides via the settings modal. Duplicate and edit with `$EDITOR`.
- **Keybindings**: Config-driven key overrides for normal, insert, and modal modes. Three keymaps (normal, insert, modal) with semantic action names.
- **Indexers**: External binaries can provide code intelligence for additional languages via a JSON protocol.
- **System prompts**: Editable markdown files in `prompts/` for architect and worker behavior.
- **Config**: `config.json` for concurrency, retry limits, and other tuning parameters.

## Design Boundaries

MENACE is a single-user desktop application. It runs on one machine, for one person, against their local codebase. This is deliberate and shapes every decision below.

### No telemetry, no phoning home

MENACE does not collect usage data, crash reports, or analytics. The only network calls are to the LLM provider the user explicitly configures. API keys live in the OS keychain, never on disk. This is a trust decision — the tool has write access to your code, so it should not be doing anything you didn't ask for.

### No multi-user, no server mode

There is no authentication layer, no user accounts, no shared state. SQLite with WAL is the database because it's a single file that survives crashes and handles concurrent goroutine access within one process. If this needed to serve multiple users, almost everything about the storage layer would change. It doesn't, so it won't.

### No horizontal scaling

The orchestrator runs N workers (configurable, default 3) as goroutines in one process. This is bounded by the machine's resources and the LLM provider's rate limits. There is no work queue, no distributed scheduler, no container orchestration. The ceiling is one person's machine, and that's fine.

### Resilience model

MENACE caps resource usage at known boundaries: 10MB file reads, 1MB file writes, 512KB diffs, 10MB LLM response accumulation, 4KB log lines. These limits exist to prevent a misbehaving model from exhausting memory or disk — not to support high-throughput workloads. The shutdown path has a 10-second timeout to avoid hanging on stuck LLM calls.

### What would change if the constraints changed

If MENACE needed to support teams or remote execution, the first things to change would be: SQLite → Postgres, OS keychain → encrypted secrets service, goroutine workers → job queue with external runners, and the TUI would need to become a client to a separate daemon process. None of that complexity is justified for a desktop tool.

See [`docs/ideas/`](docs/ideas/) for planned features and [`docs/ideas/design-principles.md`](docs/ideas/design-principles.md) for the ground rules (UI over config files, everything gets a keybinding, background by default).
