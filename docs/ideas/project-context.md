# Project Context & Prompt Overrides

The agents don't know anything about your project until they start reading files. A React app and a Go CLI get the same generic prompts. That's dumb — you should be able to tell MENACE "this project uses Next.js, we test with Vitest, never use `any`" and have that baked into every agent interaction.

Most of the plumbing already exists. It just needs a face.

**Project context** — The `projects` table has a `context` column. `GetProjectContext()` and `SetProjectContext()` work. The orchestrator already injects it into worker prompts. There's just no UI. A settings entry that opens `$EDITOR` and saves it back — same pattern as theme customization — would close the loop.

**Per-project prompts** — `prompts/architect.md` and `worker.md` are global. Let users override them per-project with a `.menace/prompts/` directory. Settings gets a "customize prompts" action that copies the global as a starting point and opens it.

**Agent iteration limits** — `MaxArchitectIterations` (50) and `MaxWorkerIterations` (30) are hardcoded. Some projects are massive and need more tool calls. Some people want to cap costs. Make these configurable.
