# Event Hooks

Git has hooks. Neovim has autocmds. MENACE just... does its thing and hopes you're watching.

I want a `hooks/` folder where you drop scripts named after events. Task finishes? Fire a script. Worker writes a file? Run the linter. Proposal lands? Send yourself a notification. Any executable, any language, MENACE passes context as env vars and gets out of the way.

Events worth having:
- Task completed / failed
- Proposal approved / received
- Worker wrote a file
- New session started

Hooks should be fire-and-forget. Async, timeboxed, failures logged but never blocking. They're side-effects, not gates.

**Things to figure out:**
- Global hooks vs per-project — probably both
- `before_` variants that can cancel actions? Powerful but complex. Maybe later.
- What env vars make sense for each event
- Where hook output goes
