# Ideas

Things I want to build or would love help with. Not specs — just what I'm thinking and why.

Read [design-principles.md](design-principles.md) first — it's the vibe check. [ARCHITECTURE.md](../../ARCHITECTURE.md) has the component map and patterns.

**If you build one of these, delete its file.** It's done. Ship it.

| Idea | What | Why |
|------|------|-----|
| [Project Context](project-context.md) | Per-project agent customization | A React app and a Go CLI need different agent behavior |
| [Notifications](notifications.md) | Know when stuff finishes | Background tasks are pointless if you miss the results |
| [Hooks](hooks.md) | React to MENACE events with scripts | The app should be composable, not a walled garden |
| [Task Templates](task-templates.md) | Reusable prompts | I keep typing the same requests |
| [Git Workflow](git-workflow.md) | Git without leaving MENACE | One screen. Full control. |
| [Custom Tools](custom-tools.md) | Give agents project-specific capabilities | Built-in tools aren't enough for real workflows |
| [Language Indexers](multi-language-indexers.md) | Better code intelligence for more languages | Regex fallback works but misses a lot |
| [Own LLM Layer](own-llm-layer.md) | Replace go-llms with our own provider layer | Own your core, don't rent it |
| [Rate Limiting](rate-limiting.md) | Graceful throttling, auto-retry, proactive backoff | Hitting a 429 shouldn't kill a task |
