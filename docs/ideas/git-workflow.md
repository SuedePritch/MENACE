# Git Workflow

I keep a second terminal open for git. That's embarrassing for a tool called MENACE.

The diff capture we have now is built on `git stash create` which is fragile garbage. Stashes don't compose, they break in weird working tree states, and they make it impossible to get clean per-subtask breakdowns. I want proper change tracking at every level — all changes, per-task, per-subtask — with reliable diffs that don't fall apart.

What I want:
- **Real diff tracking** — not stash-based. Clean diffs at subtask/task boundaries so you can see exactly what each piece of work touched.
- **Scope switching** — the review modal already cycles subtask/task/all with `s`. The diffs behind it just need to not suck.
- **Stage and commit** — see what changed, pick what you want, write a message, done. No terminal switching.
- **Revert at any level** — undo a subtask. Undo a whole task. Nuke everything from a session. The diffs are there, just reverse them.
- **Branch management** — spin up a branch before a risky task, switch back if it goes sideways.

Stretch goal: architect-assisted commit messages. It already knows what changed and why — let it draft the message.

**Things to figure out:**
- What replaces stashes? Probably `git diff HEAD` snapshots at the right moments, stored in the DB like we already do. Timing around subtask boundaries is the hard part.
- Auto-branching per task? Cleaner for reverting but adds friction.
- Modal vs panel — leaning modal to keep it contained.
