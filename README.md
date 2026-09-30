# hud

A terminal dashboard built from shell commands. Each pane runs a command and
shows what it prints: a JSON array becomes a navigable table, anything else is
shown as text. hud knows nothing about the tools it runs. Everything on screen
comes from recipes you write.

This is [`hud.example.toml`](hud.example.toml):

```
Dashboard │ Life
─────────────────────────────────────────────────────────────────
┌─ 1 Sessions ───────────────────┐┌─ 2 Weather ─────────────────┐
│ Attached  Name                 ││Weather report: Houston, TX  │
│ 1         hud                  ││                Cloudy       │
│ 0         sesh                 ││       .--.     +91(96) °F   │
└────────────────────────────────┘└─────────────────────────────┘
┌─ 3 Config + Zoxide ────────────┐┌─ 4 joshmedeski/sesh ────────┐
│ Name                           ││ Title                 State │
│ second brain                   ││ Tmuxifier Support     OPEN  │
└────────────────────────────────┘└─────────────────────────────┘
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
Use `hud -C path/to/hud.toml` to load a different file.

## Recipes

A recipe is a `[recipe.<name>]` table:

| Field     | Meaning                                                                                   |
| --------- | ----------------------------------------------------------------------------------------- |
| `command` | Shell command to run.                                                                     |
| `columns` | JSON keys to show as table columns. Leave it out and the output is shown as plain text.   |
| `enter`   | Command to run on `enter`. hud quits first, then runs it in your terminal.                |
| `keys`    | Map of key → command. Runs in the background while hud stays open, then the pane reloads. |
| `refresh` | Reload every N seconds.                                                                   |
| `colors`  | Map of column → color, or column → table of value → color. See [Colors](#colors).         |
| `labels`  | Map of column → header text, replacing the generated title.                               |
| `headers` | Set to `false` to hide the header row.                                                    |
| `fit`     | In a `stack`, shrink the pane to its content and give the rest to the other panes.         |

`enter` and `keys` commands are lists of arguments, not shell strings. Each
argument is a Go template that gets the selected row, so `{{.Name}}` becomes
that row's `Name` value. A value with spaces stays a single argument, so no
quoting is needed.

With `columns` set, the command must print a JSON array of objects. Column
titles are made readable from the keys (`listName` → "List Name",
`start_date` → "Start Date"), or set your own with
`labels = { WindowNames = "Windows" }`. List values are joined with spaces, and line
breaks are collapsed so each row stays on one line.

### Example: git worktrees

```toml
[recipe.sesh-worktrees]
command = "sesh worktree list -r joshmedeski/sesh --json"
columns = ["Title", "State"]
enter = ["sesh", "worktree", "connect", "{{.Number}}", "-r", "joshmedeski/sesh"]
keys = { "o" = ["gh", "browse", "{{.Number}}", "-R", "joshmedeski/sesh"] }
```

The command prints one object per worktree:

```json
[{ "Number": 89, "Path": "~/c/sesh/w/89", "Title": "Tmuxifier Support", "State": "OPEN" }]
```

The pane shows each object as a row with its `Title` and `State`. Every other
field, such as `Number` and `Path`, is still there for templates. With
"Tmuxifier Support" selected:

- `enter` closes hud and runs `sesh worktree connect 89 -r joshmedeski/sesh`
  in your terminal, which switches you to that worktree's session.
- `o` runs `gh browse 89 -R joshmedeski/sesh` in the background to open
  issue #89 in your browser. hud stays open and reloads the pane.

`?` lists the focused pane's keys and the commands they run.

### Colors

`colors` sets a column's text color. Give a table to color by the cell's value
(exact match), or a single color for every row:

```toml
colors = { State = { OPEN = "green", CLOSED = "red" }, Title = "brightwhite" }
```

A color is an ANSI name (`black` `red` `green` `yellow` `blue` `magenta`
`cyan` `white` `gray`, or `bright` + any of them, like `brightred`), an ANSI
number (`0`–`255`), or a hex code (`#8295AF`, `8295AF`). Add `bold` and
`italic`, separated by spaces: `colors = { when = "gray italic" }`. A color
can also be a template, so a hex code already in the JSON can color its row:

```toml
colors = { title = "{{.color}}" }
```

Values with no matching color are left uncolored.

### Example: reshaping output with `jq`

When the raw JSON isn't what you want to show, reshape it inside `command`:

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
```

## Pages and sections

A section has a `title`, an optional `recipe` to start from, and any recipe
field to override. This lets several panes share one recipe. When a section
overrides a field:

- `command`, `columns`, `colors`, `labels`, `headers`, `enter` and `refresh`
  replace the recipe's value.
- `keys` are merged with the recipe's keys.

```toml
[recipe.sesh]
command = "sesh list --json"
columns = ["Icon", "Name", "Path"]
enter = ["sesh", "connect", "{{.Name}}"]
keys = { "ctrl+d" = ["tmux", "kill-session", "-t", "{{.Name}}"] }

[[page]]
title = "Dashboard"
sections = [
  [
    { title = "Sessions", recipe = "sesh", command = "sesh list -t --json", columns = ["Icon", "Attached", "Name"] },
    { title = "Weather", command = "curl -s 'wttr.in?0'", refresh = 300 },
  ],
  [
    { title = "Config + Zoxide", recipe = "sesh", command = "sesh list -c -z -d --json", columns = ["Icon", "Name"] },
    { title = "joshmedeski/sesh", recipe = "sesh-worktrees" },
  ],
]

[[page]]
title = "Life"
sections = [
  [{ title = "Calendars", recipe = "calendar" }],
  [{ title = "Reminders", recipe = "reminders" }],
]
```

The first page has two rows of two panes each. `Sessions` and
`Config + Zoxide` share the `sesh` recipe with different commands and columns.
`Weather` has no recipe and no columns, so its output (colors included) is
shown as-is. The second page stacks two full-width panes.

To stack panes inside one column of a row, give the row a `stack` of sections
instead of a single section:

```toml
[[page]]
title = "Life"
sections = [
  [
    { stack = [{ title = "Calendars", recipe = "calendar" }, { title = "Weather", command = "curl -s 'wttr.in?0'" }] },
    { title = "Reminders", recipe = "reminders" },
  ],
]
```

Calendars and Weather share the left half, one above the other, and Reminders
fills the right half. Panes are numbered row by row, top to bottom within a
stack, and `ctrl+j` / `ctrl+k` move through a stack before leaving it.

Stacked panes split the column evenly. Set `fit = true` on a recipe whose
output is short, and its pane takes only the lines it needs, leaving the rest
to the other panes in the stack.

## Keybindings

| Key                                   | Action                                |
| ------------------------------------- | ------------------------------------- |
| `tab` / `shift+tab`                   | next / previous page                  |
| `j` `k` / `↑` `↓`                     | move within a table                   |
| `h` `l` / `←` `→` / `ctrl+h` `ctrl+l` | previous / next pane                  |
| `ctrl+j` / `ctrl+k`                   | pane below / above                    |
| `1`–`9`                               | jump to pane                          |
| `enter`                               | run the recipe's `enter` command      |
| `/`                                   | filter the table (`esc` clears)       |
| `r`                                   | reload the pane                       |
| click                                 | focus pane and select row             |
| `?`                                   | help, including the pane's `keys`     |
| `q` / `esc`                           | quit                                  |
