# Design Principles

How MENACE should feel. Not every feature idea, but the philosophy behind them.

## UI over config files

Users shouldn't have to edit files to configure things. Not everyone is an Arch user. If it's a setting, it belongs in the settings modal. If it's an action, it needs a keybinding. The settings page should be the first place someone goes to make MENACE theirs, not a docs page explaining which JSON file to find and what keys to add.

Config files are fine as the backing store — but there should always be a UI path to get there. As we add features, the settings modal needs to grow with them. Project context, prompt overrides, agent limits, notification preferences — all reachable from `,` without ever touching a file.

## Everything gets a keybinding

If you can do it, you should be able to do it from the keyboard. No hidden features. No "oh you have to know this menu exists." The keybinding system already supports normal/insert/modal modes with per-mode remapping — use it.

New actions get an `act*` constant in `keys.go`, a default binding, a help bar entry, and config remapping. That's the deal.

## You see results, not process

Workers run in the background. The architect thinks in the background. The engine schedules in the background. You don't watch any of them work. You don't see their tool calls. You don't see their intermediate reasoning. You see status, elapsed time, and results. The logs are there if you want the gory details, but the default experience is clean.

The architect panel shows tool call names while it's working — that's just so you know it's doing something, not so you can follow along. The point of MENACE is that you don't have to babysit. You talk, you approve, you move on.

## Total control

MENACE should be the only thing open. If you're switching to another terminal to do something that's part of the workflow — git, config, checking status — that's a hole in the product. This thing is a command center. You should be able to sit in it, plan changes, approve them, watch them land, commit the results, and move on to the next thing. One screen. Full control.

## Composable, not monolithic

Unix philosophy where it makes sense. Hooks let users wire MENACE into their existing tools. External indexers let anyone add language support. Prompts are just markdown files. The system should have clear extension points rather than trying to absorb the entire universe.
