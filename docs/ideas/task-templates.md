# Task Templates

I keep typing the same things. "Add tests for this." "Refactor this to use X pattern." "Add error handling to these endpoints." Every time I re-explain the same conventions.

I want saved prompts with variables. Pick a template, fill in the blanks, it goes to the architect as a message. That's it. It's not a slash command. It's not a skill. It doesn't tell MENACE to do anything — it tells the *architect* something. The architect still thinks, still plans, still proposes. You're just saving yourself from typing the same conversation starter for the hundredth time.

The difference matters: a skill executes. A template starts a conversation.

Could ship some starters out of the box so there's something useful immediately — "add tests", "refactor function", "fix bug", "add documentation".

The *what* is clear: saved prompts that become architect messages. The *how* is wide open. If you pick this up and have a better idea for the format, the storage, the UI — go for it. I care about the result, not the implementation.

**Things to figure out:**
- Template format — markdown with `{{variables}}`? Frontmatter? Something else entirely?
- Where do they live — `templates/` folder? Database? Both?
- Auto-fill variables like project name, current directory — or keep it manual?
- UI — picker modal? Fuzzy search? Inline completion? Whatever feels right.
