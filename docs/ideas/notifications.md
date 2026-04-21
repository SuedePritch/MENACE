# Notifications

The whole point is that tasks run in the background while you think about the next thing. But if you're deep in thought and a task finishes... you just miss it. The queue updates quietly and that's it. Cool, thanks.

Three levels, roughly in order of effort:

**Terminal title** — Update it with state. `MENACE · 2 running · my-project`. People with multiple tabs see it instantly. Bubble Tea has `tea.SetWindowTitle`. Almost free.

**In-app toasts** — A little notification that slides in when a task completes or fails. Auto-dismiss after a few seconds. `TaskCompletedMsg` already fires, it just needs something visual beyond the queue panel updating.

**System notifications** — For when MENACE isn't even the focused window. macOS has notification APIs, Linux has `notify-send`, terminal bell works everywhere. Should be configurable because not everyone wants popups.

**Things to figure out:**
- Toast duration and style
- Per-event notification preferences (notify on failure but not success?)
- Terminal title format — useful without being noisy
