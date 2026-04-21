# Custom Tools

The agents can read files, write files, grep, and do AST lookups. That's it. They can't run your tests, check your linter, hit your API, query your database, or do anything specific to your project. For a tool that's supposed to take over your workflow, that's a gap.

One thing I feel strongly about: **no bash tool.** Ever. I don't want a generic "run any shell command" escape hatch. That's the laziest thing every other AI coding tool does — just hand the model a shell and pray. And that's why these things fucking scare people. We run in yolo mode. There are no permissions. No "can I use this tool?" confirmations. The agent has tools, it uses them. Like a carpenter asking if he can use his hammer — that's what I'm paying you for, just do it.

That only works if the tools are *specific*. `run_tests`, `lint_file`, `query_db`. Named, described, typed parameters. The model knows exactly what it's calling and why. You don't hand someone a chainsaw and say "do whatever" — you give them the right tool for the job. If you need bash, write a tool that does the specific thing you need bash for.

I want users to define their own tools and have them show up in the agent's toolbox. Drop a file in a `tools/` folder, MENACE picks it up, the architect and workers can call it.

A tool needs to declare a name, description, and typed parameters (that's what the LLM sees as the tool schema). Then it needs an implementation that does the thing. Scope matters — some tools are read-only (architect), some write (workers get both). Same split as the built-in `ReadTools` / `WriteTools`.

Settings modal should show loaded tools with health status, same pattern as indexers.

**Things to figure out:**
- Format and language for tool definitions — needs typed params, needs to be something our users would actually write
- Runtime — bundle something or require it installed?
- Per-project vs global tools
- Hot-reload vs restart
