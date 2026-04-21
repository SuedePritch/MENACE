# Contributing

## The short version

"Talk is cheap. Send patches." - FFmpeg

Check [`docs/ideas/`](docs/ideas/) for things I want to build. If something in there gets you fired up, read [ARCHITECTURE.md](ARCHITECTURE.md) and go.

Found a bug? Fix it. You don't need permission. You don't need to open an issue. Just send the PR.

## Rules

**Squash your commits.** I don't want to see your thought process. One PR = one commit. Format:

```
YourGitHubHandle - what you did
```

`SuedePritch - fixed the stash-based diff capture`. That's it. No conventional commits, no ticket numbers, no emoji. Who and what.

**One thing per PR.** Fixing a bug AND adding a feature? That's two branches. Two PRs. Keep it tight.

**No drive-by refactors.** You're here to fix a bug? Fix the bug. Don't touch anything else. Don't rename variables, don't reorganize imports, don't "clean up" some code you were reading. You think something needs refactoring? Cool, that's its own PR.

**Tests or it didn't happen.** Changed behavior? Prove it works. There are tests for the orchestrator, engine, store, config, indexer, tools, and TUI workflows. Follow the patterns that are already there.

**Match the style.** This codebase is consistent. Read the code around what you're changing and write code that looks like it belongs. If yours sticks out, I'm going to ask you to redo it.

**No new deps without a damn good reason.** Standard library or something we already import. That's your first choice. If you genuinely need something new, explain why. "It was easier" isn't a reason.

**No AI slop.** Using AI to help write code is fine — I literally built a tool for that. But if your PR reads like you pasted a prompt into ChatGPT and opened a PR without reading what came back, it's getting closed on sight.

## Platform

I built this on a Mac and run it on my Omarchy machine. macOS and Linux are first-class. If you hit a Windows-specific bug, don't open an issue — open a PR. Or just get a better machine.

I use Ghostty. If something's broken in the default macOS Terminal or some other emulator, that's on you brother. I'm not debugging your terminal. Fix it, send a patch, and make sure you didn't break my setup in the process.

## Setup

```bash
git clone https://github.com/SuedePritch/menace.git
cd menace
make build
make test
```

Go 1.25+ and Git. That's the whole stack.

## Structure

`tui/` is the terminal UI, `engine/` is orchestration, `agent/` is LLM interaction, `store/` is persistence, `config/` is configuration, `indexer/` is code intelligence. Clean boundaries. [ARCHITECTURE.md](ARCHITECTURE.md) has the full map if you need it.

## Ideas

[`docs/ideas/`](docs/ideas/) has things I want built. If you build one, delete its file — it's done.
