# hud

A terminal dashboard built from shell commands. Each pane runs a command and
shows what it prints: a JSON array becomes a navigable table, anything else is
shown as text. [sesh](https://github.com/joshmedeski/sesh) is used as an
external tool through `sesh list --json`; hud doesn't import any of its code.

```
Dashboard │ Life
──────────────────────────────────────────────────────────────────
┌─ 1 Sessions ─────────────────────┬─ 2 Weather ──────────────────┐
│ Icon  Attached  Name             │Weather report: Houston, TX   │
│ 📊    1         hud              │                Cloudy        │
│ ⚡    0         sesh             │       .--.     +91(96) °F    │
└──────────────────────────────────┴──────────────────────────────┘
┌─ 3 Config + Zoxide ──────────────┬─ 4 joshmedeski/sesh ─────────┐
│ Icon  Name                       │ Number  State  Title         │
│ 🧠    second brain               │ 89      OPEN   Tmuxifier ... │
└──────────────────────────────────┴──────────────────────────────┘
tab page │ j/k move │ h/l pane │ enter open │ / filter │ q quit  ? help
```

## Install

From a clone of this repo:

```sh
just build   # installs to $GOPATH/bin/hud
```

## How it works

- **Pages** are tabs. Switch between them with `tab` / `shift+tab`.
- A page's **sections** are rows of panes. Rows stack top to bottom, and
  panes in a row sit side by side. Space is split evenly.
- Every pane is a **recipe**: a command, plus optional instructions for turning
  its output into a table and acting on the selected row.
- Commands run through `sh -c` when the dashboard starts, again when you press
  `r`, and every `refresh` seconds if that's set.

hud reads `$XDG_CONFIG_HOME/hud/hud.toml` (or `~/.config/hud/hud.toml`).
Use `hud -C path/to/hud.toml` to load a different file. With no config, hud
shows a single pane of sesh sessions.

## Recipes

| Field     | Meaning                                                                                   |
| --------- | ----------------------------------------------------------------------------------------- |
| `command` | Shell command to run.                                                                     |
| `columns` | JSON keys to show as table columns. Leave it out and the output is shown as plain text.   |
| `enter`   | Command to run on `enter`. hud quits first, then runs it in your terminal.                |
| `keys`    | Map of key → command. Runs in the background, then the pane reloads.                      |
| `refresh` | Reload every N seconds.                                                                   |

`enter` and `keys` commands are lists of arguments, not shell strings. Each
argument is a Go template that gets the selected row, so `{{.Name}}` becomes
that row's `Name` value. A value with spaces stays a single argument, so no
quoting is needed.

With `columns` set, the command must print a JSON array of objects. Column
titles are made readable from the keys (`listName` → "List Name",
`start_date` → "Start Date"). List values are joined with spaces, and line
breaks are collapsed so each row stays on one line.

### Built-in: `sesh`

```toml
[recipe.sesh]
command = "sesh list --json"
columns = ["Icon", "Name", "Path"]
enter = ["sesh", "connect", "{{.Name}}"]
keys = { "ctrl+d" = ["tmux", "kill-session", "-t", "{{.Name}}"] }
```

Defining `[recipe.sesh]` in your config replaces it.

### Writing your own

Reshape output with `jq` inside `command` when the raw JSON isn't what you
want to show:

```toml
[recipe.calendar]
command = '''
ical upcoming --exclude-calendar Birthdays -o json | jq 'map(. as $e | .when = ($e.start_date | fromdateiso8601
  | strflocaltime(if $e.all_day then "%a %b %e" else "%a %b %e %l:%M %p" end)))'
'''
columns = ["when", "title", "calendar", "location"]
refresh = 300

[recipe.reminders]
command = "remindctl show --json | jq 'sort_by(.dueDate // \"~\")'"
columns = ["title", "listName"]
keys = { "c" = ["remindctl", "complete", "{{.id}}"], "o" = ["remindctl", "open", "{{.id}}"] }
refresh = 300

[recipe.worktree]
columns = ["Number", "State", "Title"]
enter = ["sesh", "connect", "{{.Path}}"]
```

A recipe with no `command`, like `worktree` above, is a template: each section
that uses it supplies its own command.

## Pages and sections

A section has a `title`, an optional `recipe` to start from, and any recipe
field to override. When a section overrides a field:

- `command`, `columns`, `enter` and `refresh` replace the recipe's value.
- `keys` are merged with the recipe's keys.

```toml
[[page]]
title = "Dashboard"
sections = [
  [
    { title = "Sessions", recipe = "sesh", command = "sesh list -t --json", columns = ["Icon", "Attached", "Name"] },
    { title = "Weather", command = "curl -s 'wttr.in?0'", refresh = 300 },
  ],
  [
    { title = "Config + Zoxide", recipe = "sesh", command = "sesh list -c -z -d --json", columns = ["Icon", "Name"] },
    { title = "joshmedeski/sesh", recipe = "worktree", command = "sesh worktree list -r joshmedeski/sesh --json" },
  ],
]

[[page]]
title = "Life"
sections = [
  [{ title = "Calendars", recipe = "calendar" }],
  [{ title = "Reminders", recipe = "reminders" }],
]
```

The first page has two rows of two panes each. `Weather` has no columns, so
its output (colors included) is shown as-is. The second page stacks two
full-width panes.

See [`hud.example.toml`](hud.example.toml) for a complete config.

## Keybindings

| Key                             | Action                                |
| ------------------------------- | ------------------------------------- |
| `tab` / `shift+tab`             | next / previous page                  |
| `j` `k` / `↑` `↓`               | move within a table                   |
| `h` `l` / `←` `→` / `ctrl+h` `ctrl+l` | previous / next pane            |
| `ctrl+j` / `ctrl+k`             | pane below / above                    |
| `1`–`9`                         | jump to pane                          |
| `enter`                         | run the recipe's `enter` command      |
| `/`                             | filter the table (`esc` clears)       |
| `r`                             | reload the pane                       |
| click                           | focus pane and select row             |
| `?`                             | help, including the pane's `keys`     |
| `q` / `esc`                     | quit                                  |
