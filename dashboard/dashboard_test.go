package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hud.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigResolvesRecipeOverrides(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, `
[recipe.items]
command = "list-items"
columns = ["Name"]
keys."enter o" = { run = ["open-item", "{{.Name}}"], quit = true }
keys."ctrl+d" = ["remove-item", "{{.Name}}"]

[recipe.split.keys.o]
run = ["open", "{{.Name}}"]
quit = true

[[page]]
title = "Main"
sections = [[
  { title = "Mine", recipe = "items", command = "list-items --mine", keys = { "x" = ["echo", "{{.Name}}"], "o" = ["gh", "browse"] } },
  { title = "Weather", command = "curl wttr.in", refresh = 300 },
]]
`))
	if err != nil {
		t.Fatal(err)
	}
	row := cfg.Pages[0].Sections[0]

	mine, err := cfg.resolve(row[0])
	if err != nil {
		t.Fatal(err)
	}
	if mine.Command != "list-items --mine" {
		t.Errorf("command override lost: %q", mine.Command)
	}
	if enter := mine.Keys["enter"]; !enter.Quit || !slices.Equal(enter.Run, []string{"open-item", "{{.Name}}"}) {
		t.Errorf("enter not inherited from recipe: %+v", enter)
	}
	if o := mine.Keys["o"]; o.Quit || o.Run[0] != "gh" {
		t.Errorf("section key should replace one key of a shared binding: %+v", o)
	}
	if del := mine.Keys["ctrl+d"]; del.Quit || len(del.Run) != 2 {
		t.Errorf("recipe keys dropped when section adds its own: %+v", del)
	}
	if o := cfg.Recipes["split"].Keys["o"]; !o.Quit || len(o.Run) != 2 {
		t.Errorf("key defined as a table not parsed: %+v", o)
	}
	if _, ok := cfg.Recipes["items"].Keys["x"]; ok {
		t.Error("section keys leaked into the shared recipe")
	}

	weather, _ := cfg.resolve(row[1])
	if weather.Refresh != 300 || len(weather.Columns) != 0 {
		t.Errorf("inline recipe not parsed: %+v", weather)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	if _, err := LoadConfig(filepath.Join(t.TempDir(), "missing.toml")); err == nil {
		t.Error("explicit missing config should error")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := LoadConfig(""); err == nil {
		t.Error("missing default config should error")
	}
	cfg, err := LoadConfig(writeConfig(t, `[[page]]
sections = [[{ recipe = "nope" }]]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(cfg); err == nil {
		t.Error("unknown recipe should error")
	}
	if _, err := LoadConfig(writeConfig(t, `[recipe.x]
keys = { "o" = "gh browse" }`)); err == nil {
		t.Error("a key bound to a plain string should error")
	}
	if _, err := LoadConfig(writeConfig(t, `[recipe.x]
enter = ["open", "{{.Name}}"]`)); err == nil || !strings.Contains(err.Error(), "enter") {
		t.Errorf("the removed enter field should be reported, got %v", err)
	}
}

func loadedList(t *testing.T, r Recipe) *listPane {
	t.Helper()
	p := newSection("test", r, nil).(*listPane)
	p.Update(p.fetch(nil)())
	if p.err != nil {
		t.Fatal(p.err)
	}
	return p
}

func key(s string) tea.KeyPressMsg {
	if s == "enter" {
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func TestListPaneParsesFiltersAndChooses(t *testing.T) {
	p := loadedList(t, Recipe{
		Command: `echo '[{"Name":"my custom binaries","Attached":1},{"Name":"notes","Attached":0},{"Name":"hud"}]'`,
		Columns: []string{"Name", "Attached"},
		Keys:    map[string]Action{"enter": {Run: []string{"open", "{{.Name}}"}, Quit: true}},
	})
	if got := p.cells[0]; !slices.Equal(got, []string{"my custom binaries", "1"}) {
		t.Errorf("cells = %q", got)
	}
	if p.cells[2][1] != "" {
		t.Errorf("missing field should render empty, got %q", p.cells[2][1])
	}

	p.Update(key("/"))
	for _, k := range "notes" {
		p.Update(key(string(k)))
	}
	if len(p.visible) != 1 {
		t.Fatalf("filter kept %d rows, want 1", len(p.visible))
	}
	p.Update(key("enter"))
	if !slices.Equal(p.Chosen(), []string{"open", "notes"}) {
		t.Errorf("chosen = %q", p.Chosen())
	}
	if p.Filtering() {
		t.Error("enter should leave filter mode")
	}

	p.chosen = nil
	p.Update(key("k"))
	p.Update(key("enter"))
	if got := p.Chosen(); len(got) != 2 || got[1] != "my custom binaries" {
		t.Errorf("name with spaces must stay one argv element: %q", got)
	}
}

func TestListPaneReportsBadJSON(t *testing.T) {
	p := newSection("test", Recipe{Command: "echo nope", Columns: []string{"Name"}}, nil).(*listPane)
	p.Update(p.fetch(nil)())
	if p.err == nil {
		t.Error("non-JSON output should surface an error")
	}
}

func TestQuitKeyQuitsWithAction(t *testing.T) {
	m, err := New(Config{Pages: []PageConfig{{Sections: [][]SectionConfig{{{
		Recipe: Recipe{Command: `echo '[{"Name":"a"}]'`, Columns: []string{"Name"}, Keys: map[string]Action{"o": {Run: []string{"echo", "{{.Name}}"}, Quit: true}}},
	}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	p := m.focused().(*listPane)
	p.Update(p.fetch(nil)())

	next, cmd := m.Update(key("o"))
	if !slices.Equal(next.(Model).Action(), []string{"echo", "a"}) || cmd == nil {
		t.Errorf("action = %q", next.(Model).Action())
	}
}

func TestEnterKeyRunsInBackground(t *testing.T) {
	p := loadedList(t, Recipe{
		Command: `echo '[{"Name":"a"}]'`,
		Columns: []string{"Name"},
		Keys:    map[string]Action{"enter": {Run: []string{"true"}}},
	})
	if cmd := p.Update(key("enter")); cmd == nil {
		t.Error("keys.enter should run as a background action")
	}
	if p.Chosen() != nil {
		t.Errorf("keys.enter should not quit, chosen = %q", p.Chosen())
	}

	p.Update(key("/"))
	if cmd := p.Update(key("enter")); cmd == nil || p.Filtering() {
		t.Error("enter while filtering should run keys.enter and leave filter mode")
	}
}

func TestPaneNavigationKeys(t *testing.T) {
	list := SectionConfig{Recipe: Recipe{Command: `echo '[{"Name":"a"}]'`, Columns: []string{"Name"}}}
	m, err := New(Config{Pages: []PageConfig{{Sections: [][]SectionConfig{{list, {}}, {{}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	press := func(k tea.KeyPressMsg) {
		next, _ := m.Update(k)
		m = next.(Model)
	}
	steps := []struct {
		key  tea.KeyPressMsg
		want int
	}{
		{key("l"), 1},
		{tea.KeyPressMsg{Code: tea.KeyRight}, 2},
		{key("l"), 0},
		{key("h"), 2},
		{tea.KeyPressMsg{Code: tea.KeyLeft}, 1},
		{key("h"), 0},
	}
	for i, s := range steps {
		press(s.key)
		if m.focus != s.want {
			t.Fatalf("step %d (%s): focus = %d, want %d", i, s.key, m.focus, s.want)
		}
	}

	press(key("/"))
	press(key("l"))
	if m.focus != 0 || m.focused().(*listPane).FilterQuery() != "l" {
		t.Errorf("l while filtering should type, got focus %d query %q", m.focus, m.focused().(*listPane).FilterQuery())
	}
}

func TestHumanize(t *testing.T) {
	for in, want := range map[string]string{
		"when":       "When",
		"listName":   "List Name",
		"Icon":       "Icon",
		"GitStatus":  "Git Status",
		"start_date": "Start Date",
		"listID":     "List ID",
		"dueDate":    "Due Date",
	} {
		if got := humanize(in); got != want {
			t.Errorf("humanize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLayout(t *testing.T) {
	if got := splitEvenly(10, 3); !slices.Equal(got, []int{4, 3, 3}) {
		t.Errorf("splitEvenly = %v", got)
	}
	widths := []int{5, 5}
	for x, want := range map[int]int{0: 0, 6: 0, 7: 1, 13: 1, 14: -1} {
		if got := paneCol(x, widths); got != want {
			t.Errorf("paneCol(%d) = %d, want %d", x, got, want)
		}
	}
}

func TestWorktreeRowRendersIntegersAndLists(t *testing.T) {
	p := loadedList(t, Recipe{
		Command: `printf '%s' '[{"Number":7235,"Alerts":["bell","activity"],"location":"Cafe\n626 E Ninth St"}]'`,
		Columns: []string{"Number", "Alerts", "location"},
		Keys:    map[string]Action{"enter": {Run: []string{"gh", "browse", "{{.Number}}"}, Quit: true}},
	})
	if got := p.cells[0]; !slices.Equal(got, []string{"7235", "bell activity", "Cafe 626 E Ninth St"}) {
		t.Errorf("cells = %q", got)
	}
	p.Update(key("enter"))
	if got := p.Chosen(); len(got) != 3 || got[2] != "7235" {
		t.Errorf("chosen = %q", got)
	}
}

func TestExampleConfigLoads(t *testing.T) {
	cfg, err := LoadConfig("../hud.example.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestCollapseCarriageReturns(t *testing.T) {
	if got := collapseCarriageReturns("a\r\n10%\r50%\r100%"); got != "a\n100%" {
		t.Errorf("got %q", got)
	}
}

func TestColumnColors(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, `
[recipe.prs]
command = '''echo '[{"State":"OPEN","cal":"Work","color":"#8295AF"},{"State":"CLOSED","cal":"Home","color":"d73a4a"},{"State":"DRAFT","cal":"x"}]' '''
columns = ["State", "cal"]
colors = { State = { OPEN = "green", CLOSED = "bold BrightRed" }, cal = "{{.color}}" }

[[page]]
sections = [[{ recipe = "prs" }]]
`))
	if err != nil {
		t.Fatal(err)
	}
	r, err := cfg.resolve(cfg.Pages[0].Sections[0][0])
	if err != nil {
		t.Fatal(err)
	}
	p := loadedList(t, r)
	want := [][]string{
		{"\x1b[32mOPEN\x1b[39m", "\x1b[38;2;130;149;175mWork\x1b[39m"},
		{"\x1b[1;91mCLOSED\x1b[22;39m", "\x1b[38;2;215;58;74mHome\x1b[39m"},
		{"DRAFT", "x"},
	}
	for i, row := range want {
		if !slices.Equal(p.painted[i], row) {
			t.Errorf("row %d = %q, want %q", i, p.painted[i], row)
		}
	}
	if p.cells[0][0] != "OPEN" {
		t.Errorf("filterable cell text must stay plain, got %q", p.cells[0][0])
	}

	line := strings.Split(p.View(40, 10, true), "\n")[1]
	if strings.Count(line, "\x1b[m") != 1 || !strings.HasSuffix(line, "\x1b[m") {
		t.Errorf("a colored cell must not reset the cursor background: %q", line)
	}

	cfg.Recipes["prs"] = Recipe{Colors: map[string]any{"State": map[string]any{"OPEN": "grene"}}}
	if _, err := cfg.resolve(cfg.Pages[0].Sections[0][0]); err == nil {
		t.Error("unknown color name should error")
	}
}

func TestLongColumnTruncatesToKeepOthersVisible(t *testing.T) {
	p := loadedList(t, Recipe{
		Command: `echo '[{"Title":"Update Internal Payment Method on Account to Only Show Org-Related","Number":7235,"State":"OPEN"}]'`,
		Columns: []string{"Title", "Number", "State"},
		Colors:  map[string]any{"Title": "red"},
	})
	for _, line := range strings.Split(p.View(40, 10, false), "\n") {
		if w := lipgloss.Width(line); w > 40 {
			t.Errorf("line is %d wide, want <= 40: %q", w, line)
		}
	}
	row := ansi.Strip(strings.Split(p.View(40, 10, false), "\n")[1])
	if !strings.Contains(row, "…  7235    OPEN") {
		t.Errorf("title should truncate so Number and State stay visible: %q", row)
	}
}

func TestHeaderLabelsAndHiding(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, `
[recipe.sessions]
command = '''echo '[{"Name":"hud","WindowNames":["a","b"]},{"Name":"sesh"}]' '''
columns = ["Name", "WindowNames"]
labels = { Name = "", WindowNames = "Windows" }

[[page]]
sections = [[{ recipe = "sessions" }, { recipe = "sessions", headers = false }]]
`))
	if err != nil {
		t.Fatal(err)
	}
	row := cfg.Pages[0].Sections[0]
	labeled, _ := cfg.resolve(row[0])
	hidden, _ := cfg.resolve(row[1])

	p := loadedList(t, labeled)
	if got := ansi.Strip(strings.Split(p.View(40, 10, false), "\n")[0]); got != "       Windows" {
		t.Errorf("header = %q", got)
	}

	p = loadedList(t, hidden)
	lines := strings.Split(ansi.Strip(p.View(40, 10, false)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], " hud") {
		t.Errorf("hidden headers should start with the first row: %q", lines)
	}
	p.ClickAt(1)
	if p.cursor != 1 {
		t.Errorf("click on second line selected row %d, want 1", p.cursor)
	}
}

func TestEmojiWidthsMatchTheRenderer(t *testing.T) {
	p := loadedList(t, Recipe{
		Command: `echo '[{"icon":"🧑‍🎨","name":"a"},{"icon":"✍️","name":"b"},{"icon":"","name":"c"},{"icon":"🛸","name":"d"}]'`,
		Columns: []string{"icon", "name"},
	})
	for _, line := range strings.Split(p.View(20, 10, false), "\n")[1:] {
		if w := lipgloss.Width(padWidth(line, 20)); w != 20 {
			t.Errorf("%q is %d cells wide, want 20", line, w)
		}
		if i := strings.IndexAny(line, "abcd"); lipgloss.Width(line[:i]) != 7 {
			t.Errorf("name column misaligned in %q", line)
		}
	}
}

func TestStackedColumn(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, `
[[page]]
sections = [
  [{ stack = [{ title = "Top", command = "echo top" }, { title = "Bottom", command = "echo bottom" }] }, { title = "Side", command = "echo side" }],
  [{ title = "Wide", command = "echo wide" }],
]
`))
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, sec := range m.allSections() {
		src := sec.(*textPane)
		src.Update(src.fetch(nil)())
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 23})
	m = next.(Model)

	lines := strings.Split(ansi.Strip(m.viewPage()), "\n")
	for i, line := range lines {
		if w := lipgloss.Width(line); w != 40 {
			t.Errorf("line %d is %d wide: %q", i, w, line)
		}
	}
	if !strings.Contains(lines[0], "1 Top") || !strings.Contains(lines[0], "3 Side") {
		t.Errorf("top border = %q", lines[0])
	}
	var divider string
	for _, line := range lines {
		if strings.Contains(line, "2 Bottom") {
			divider = line
		}
	}
	if !strings.HasPrefix(divider, "┌─ 2 Bottom") {
		t.Errorf("stacked pane should open its own box: %q", divider)
	}
	if !strings.Contains(lines[0], "┐┌─ 3 Side") {
		t.Errorf("side-by-side panes should each have their own border: %q", lines[0])
	}

	press := func(k tea.KeyPressMsg) {
		next, _ := m.Update(k)
		m = next.(Model)
	}
	ctrl := func(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }
	for i, step := range []struct {
		key  tea.KeyPressMsg
		want string
	}{{ctrl('j'), "Bottom"}, {ctrl('j'), "Wide"}, {ctrl('k'), "Bottom"}, {ctrl('k'), "Top"}, {key("l"), "Bottom"}, {key("l"), "Side"}} {
		press(step.key)
		if got := m.focused().Title(); got != step.want {
			t.Fatalf("step %d: focused %q, want %q", i, got, step.want)
		}
	}

	for line, want := range map[int]string{2: "Top", 5: "Bottom", 7: "Bottom", 12: "Wide"} {
		next, _ := m.handleMouseClick(tea.MouseClickMsg{X: 3, Y: line + 2, Button: tea.MouseLeft})
		if got := next.(Model).focused().Title(); got != want {
			t.Errorf("click on line %d focused %q, want %q", line, got, want)
		}
	}

	cfg.Pages[0].Sections[0][0].Stack[0].Stack = []SectionConfig{{}}
	if _, err := New(cfg); err == nil {
		t.Error("a nested stack should error")
	}
}

func TestFitShrinksStackedPaneToContent(t *testing.T) {
	stack := func(top Recipe) column {
		col, err := Config{}.column(SectionConfig{Stack: []SectionConfig{{Recipe: top}, {Recipe: Recipe{Command: "echo rest"}}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, sec := range col {
			sec.Update(sec.(*textPane).fetch(nil)())
		}
		return col
	}
	for _, tc := range []struct {
		top  Recipe
		want []int
	}{
		{Recipe{Command: "printf 'a\\nb\\n'", Fit: true}, []int{2, 14}},
		{Recipe{Command: "printf 'a\\nb\\n'"}, []int{8, 8}},
		{Recipe{Command: "seq 50", Fit: true}, []int{15, 1}},
	} {
		if got := stackHeights(20, stack(tc.top)); !slices.Equal(got, tc.want) {
			t.Errorf("%q fit=%v: heights = %v, want %v", tc.top.Command, tc.top.Fit, got, tc.want)
		}
	}
}

func drain(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = drain(m, c)
		}
	default:
		next, more := m.Update(msg)
		m = drain(next.(Model), more)
	}
	return m
}

func TestSharedFeedLoadsOnceAndLazily(t *testing.T) {
	dir := t.TempDir()
	runs, later := filepath.Join(dir, "runs"), filepath.Join(dir, "later")
	cfg, err := LoadConfig(writeConfig(t, `
[recipe.items]
command = '''echo x >> `+runs+`; echo '[{"Name":"a","State":"OPEN"},{"Name":"b","State":"MERGED"},{"Name":"c","State":"CLOSED"}]' '''
columns = ["Name"]

[[page]]
sections = [[
  { title = "Open", recipe = "items", where = { State = "OPEN" } },
  { title = "Done", recipe = "items", where = { State = ["MERGED", "CLOSED"] } },
]]

[[page]]
sections = [[{ title = "All", recipe = "items" }, { title = "Later", command = "touch `+later+`" }]]
`))
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	m = drain(m, m.Init())

	names := func(sec Section) string {
		var out []string
		for _, item := range sec.(*listPane).items {
			out = append(out, item["Name"].(string))
		}
		return strings.Join(out, ",")
	}
	secs := m.allSections()
	if got := names(secs[0]) + " " + names(secs[1]); got != "a b,c" {
		t.Errorf("filtered panes = %q", got)
	}
	if _, err := os.Stat(later); err == nil {
		t.Error("second page loaded before it was shown")
	}

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = drain(next.(Model), cmd)
	if _, err := os.Stat(later); err != nil {
		t.Error("second page didn't load when shown")
	}
	if got := names(secs[2]); got != "a,b,c" {
		t.Errorf("shared feed not replayed on the new page: %q", got)
	}
	if b, _ := os.ReadFile(runs); string(b) != "x\n" {
		t.Errorf("shared command ran %d times, want 1", strings.Count(string(b), "x"))
	}
}

func TestOpenPage(t *testing.T) {
	m, err := New(Config{Pages: []PageConfig{
		{Sections: [][]SectionConfig{{{}}}},
		{Title: "Nutiliti", Sections: [][]SectionConfig{{{}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if m, err = m.OpenPage("nutiliti"); err != nil || m.page != 1 {
		t.Errorf("page = %d, err = %v", m.page, err)
	}
	if _, err := m.OpenPage("nope"); err == nil {
		t.Error("want error for unknown page")
	}
}

func TestHidePages(t *testing.T) {
	m, err := New(Config{Pages: []PageConfig{
		{Title: "One", Sections: [][]SectionConfig{{{}}}},
		{Title: "Two", Sections: [][]SectionConfig{{{}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	m = m.HidePages()
	next, _ := m.Update(key("tab"))
	m = next.(Model)
	if m.page != 0 {
		t.Errorf("tab moved to page %d", m.page)
	}
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "Two") || strings.Contains(view, "tab") {
		t.Errorf("page tabs still shown:\n%s", view)
	}
}

func TestShiftRReloadsEachFeedOnPageOnce(t *testing.T) {
	m, err := New(Config{Pages: []PageConfig{{Sections: [][]SectionConfig{
		{{Recipe: Recipe{Command: "echo a"}}, {Recipe: Recipe{Command: "echo a"}}},
		{{Recipe: Recipe{Command: "echo b"}}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	_, cmd := m.Update(key("R"))
	if cmd == nil {
		t.Fatal("R returned no command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Errorf("want 2 reloads, got %#v", batch)
	}
}

func TestTabsSwitchWithBrackets(t *testing.T) {
	later := filepath.Join(t.TempDir(), "later")
	cfg, err := LoadConfig(writeConfig(t, `
[[page]]
sections = [[
  { title = "Side", command = "echo side" },
  { tabs = [{ title = "Files", command = "echo files" }, { title = "Worktrees", command = "touch `+later+`; echo trees" }, { title = "Submodules", command = "echo subs" }] },
]]
`))
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	m = drain(m, m.Init())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	m = next.(Model)
	if !strings.Contains(ansi.Strip(m.viewPage()), "┌─ 2 Files - Worktrees - Submodules ─") {
		t.Errorf("tab titles not in border:\n%s", ansi.Strip(m.viewPage()))
	}
	if _, err := os.Stat(later); err == nil {
		t.Error("hidden tab loaded before it was shown")
	}

	press := func(k string) {
		next, cmd := m.Update(key(k))
		m = drain(next.(Model), cmd)
	}
	press("]")
	if m.focused().Title() != "Side" {
		t.Error("] on a pane without tabs should do nothing")
	}
	press("2")
	for _, step := range []struct{ key, want string }{{"]", "Worktrees"}, {"]", "Submodules"}, {"]", "Files"}, {"[", "Submodules"}, {"[", "Worktrees"}} {
		press(step.key)
		if got := m.focused().Title(); got != step.want {
			t.Fatalf("%s: focused %q, want %q", step.key, got, step.want)
		}
	}
	if _, err := os.Stat(later); err != nil {
		t.Error("tab didn't load when shown")
	}
	if !strings.Contains(m.viewPage(), "trees") {
		t.Error("active tab's output not shown")
	}

	cfg.Pages[0].Sections[0][1].Tabs[0].Tabs = []SectionConfig{{}}
	if _, err := New(cfg); err == nil {
		t.Error("nested tabs should error")
	}
}

func TestCalendarGroupsUpcomingDaysIntoColumns(t *testing.T) {
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local)
	at := func(day, hour, minute int) string {
		return time.Date(2026, 10, day, hour, minute, 0, 0, time.Local).Format(time.RFC3339)
	}
	out := fmt.Sprintf(`[
		{"title":"Yesterday","start":%q},
		{"title":"Another event title","start":%q},
		{"title":"Event Title","start":%q},
		{"title":"Event title","start":%q},
		{"title":"Another event","start":%q},
		{"title":"Holiday","start":%q},
		{"title":"Too far","start":%q}
	]`, at(18, 10, 0), at(19, 14, 0), at(19, 7, 30), at(20, 18, 0), at(23, 19, 0), at(23, 0, 0), at(24, 9, 0))
	p := newSection("Calendar", Recipe{Command: "true", Columns: []string{"title"}, Date: "start", Days: 3}, nil).(*calendarPane)
	p.loading, p.out = false, []byte(out)
	p.parse(now)

	lines := strings.Split(ansi.Strip(p.view(90, 10, now)), "\n")
	want := []string{
		" Mon                           Tue                           Fri",
		" Event Title 7:30am            Event title 6pm               Holiday",
		" Another event title 2pm                                     Another event 7pm",
	}
	for i, w := range want {
		if strings.TrimRight(lines[i], " ") != w {
			t.Errorf("line %d = %q, want %q", i, lines[i], w)
		}
	}
	if len(lines) != len(want) {
		t.Errorf("got %d lines, want %d", len(lines), len(want))
	}
	for _, line := range strings.Split(p.View(30, 10, false), "\n") {
		if w := lipgloss.Width(line); w > 30 {
			t.Errorf("line is %d wide, want <= 30: %q", w, line)
		}
	}
}

func TestCalendarBoldsCurrentAndDimsPastEvents(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	at := func(hour int) string { return time.Date(2026, 10, 19, hour, 0, 0, 0, time.Local).Format(time.RFC3339) }
	out := fmt.Sprintf(`[
		{"title":"Past","start":%q,"end":%q},
		{"title":"Now","start":%q,"end":%q},
		{"title":"Later","start":%q,"end":%q}
	]`, at(9), at(10), at(11), at(13), at(15), at(16))
	p := newSection("Calendar", Recipe{Command: "true", Columns: []string{"title"}, Colors: map[string]any{"title": "red"}, Date: "start", End: "end", Days: 1}, nil).(*calendarPane)
	p.loading, p.out = false, []byte(out)
	p.parse(now)

	lines := strings.Split(p.view(40, 10, now), "\n")
	if want := DimmedStyle().Render("Past"); !strings.Contains(lines[1], want) {
		t.Errorf("past event should be dimmed without its color: %q", lines[1])
	}
	if want := lipgloss.NewStyle().Bold(true).Render(paint("Now", "red")); !strings.Contains(lines[2], want) {
		t.Errorf("current event should be bold and keep its color: %q", lines[2])
	}
	if want := paint("Later", "red"); !strings.Contains(lines[3], " "+want+" ") {
		t.Errorf("upcoming event should be unchanged: %q", lines[3])
	}
}

func TestConfirmKeyAsksBeforeRunning(t *testing.T) {
	p := loadedList(t, Recipe{
		Command: `echo '[{"Name":"a","Path":"/tmp/a"},{"Name":"b"}]'`,
		Columns: []string{"Name"},
		Keys:    map[string]Action{"x": {Run: []string{"rm", "{{.Path}}"}, Confirm: "Delete {{.Name}}?"}},
	})
	if cmd := p.Update(key("x")); cmd != nil || p.ConfirmPrompt() != "Delete a?" {
		t.Fatalf("x should ask first, prompt = %q", p.ConfirmPrompt())
	}
	if cmd := p.Update(key("n")); cmd != nil || p.ConfirmPrompt() != "" {
		t.Error("n should cancel without running")
	}
	p.Update(key("x"))
	if cmd := p.Update(key("y")); cmd == nil || !p.loading {
		t.Error("y should run the action and show loading")
	}

	p.loading = false
	p.Update(key("x"))
	p.Update(key("l"))
	if !p.ConfirmNo() {
		t.Fatal("l should select No")
	}
	if cmd := p.Update(key("enter")); cmd != nil || p.ConfirmPrompt() != "" {
		t.Error("enter on No should cancel without running")
	}
	p.Update(key("x"))
	p.Update(key("tab"))
	p.Update(key("h"))
	if cmd := p.Update(key("enter")); cmd == nil {
		t.Error("enter on Yes should run the action")
	}

	p.loading = false
	p.Update(key("j"))
	if cmd := p.Update(key("x")); cmd != nil || p.ConfirmPrompt() != "" || p.err != nil {
		t.Error("a row missing a templated field should skip the key")
	}
}

func TestConfirmPromptOverlaysTheScreen(t *testing.T) {
	m, err := New(Config{Pages: []PageConfig{{Sections: [][]SectionConfig{{{
		Title: "Worktrees",
		Recipe: Recipe{
			Command: `echo '[{"Name":"feature-branch"},{"Name":"main"}]'`,
			Columns: []string{"Name"},
			Keys:    map[string]Action{"x": {Run: []string{"rm", "{{.Name}}"}, Confirm: "Delete {{.Name}}?"}},
		},
	}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	p := m.allSections()[0].(*listPane)
	p.Update(p.fetch(nil)())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 14})
	m = next.(Model)
	before := m.View().Content

	next, _ = m.Update(key("x"))
	m = next.(Model)
	screen := ansi.Strip(m.View().Content)
	if testing.Verbose() {
		t.Log("\n" + screen)
	}
	if !strings.Contains(screen, "╭") || !strings.Contains(screen, "Delete feature-branch?") || !strings.Contains(screen, "Yes      No") {
		t.Errorf("prompt should show in a dialog:\n%s", screen)
	}
	if lipgloss.Height(screen) != lipgloss.Height(before) || lipgloss.Width(screen) != lipgloss.Width(before) {
		t.Errorf("dialog should not change the screen size:\n%s", screen)
	}
	if !strings.Contains(screen, "Worktrees") {
		t.Errorf("dashboard should stay visible behind the dialog:\n%s", screen)
	}
}

func TestCacheConfigForms(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, `
[recipe.short]
cache = "10m"

[recipe.inline]
cache = { name = "prs", ttl = "1h", watch = ["~/x"] }

[recipe.dotted]
cache.name = "wt"
keys.x = { run = ["true"], bust = ["prs"] }
`))
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := cfg.Recipes["short"].cache(); c == nil || c.TTL != "10m" {
		t.Errorf("duration shorthand not parsed: %+v", c)
	}
	if c, _ := cfg.Recipes["inline"].cache(); c == nil || c.Name != "prs" || c.TTL != "1h" || len(c.Watch) != 1 {
		t.Errorf("inline cache not parsed: %+v", c)
	}
	dotted := cfg.Recipes["dotted"]
	if c, _ := dotted.cache(); c == nil || c.Name != "wt" || !slices.Equal(dotted.Keys["x"].Bust, []string{"prs"}) {
		t.Errorf("dotted cache or bust not parsed: %+v", dotted)
	}
	for _, bad := range []string{`cache = "soon"`, `cache = { ttl = "1h", tll = "2h" }`, `cache = 5`, `cache.name = "../x"`, `keys.x = { run = ["true"], bust = ["a/b"] }`} {
		cfg, err := LoadConfig(writeConfig(t, "[[page]]\nsections = [[{ title = \"t\", command = \"true\", "+bad+" }]]"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := New(cfg); err == nil {
			t.Errorf("%s should be rejected", bad)
		}
	}
}

func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collect(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func feedAll(s Section, msgs []tea.Msg) {
	for len(msgs) > 0 {
		next := collect(s.Update(msgs[0]))
		msgs = append(msgs[1:], next...)
	}
}

func TestCacheServesThenRefreshesAndBusts(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := cachePath("items")
	r := Recipe{
		Command: `echo '[{"Name":"live"}]'`,
		Columns: []string{"Name"},
		Cache:   map[string]any{"name": "items", "ttl": "1h"},
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`[{"Name":"cached"}]`), 0o644)

	fresh := newSection("Items", r, feeds{}).(*listPane)
	feedAll(fresh, collect(fresh.Init()))
	if fresh.cells[0][0] != "cached" || !strings.HasPrefix(fresh.Title(), "Items · ") {
		t.Errorf("fresh cache should be shown without running: %v %q", fresh.cells, fresh.Title())
	}

	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(path, old, old)
	stale := newSection("Items", r, feeds{}).(*listPane)
	feedAll(stale, collect(stale.Init()))
	if stale.cells[0][0] != "live" || stale.Title() != "Items" {
		t.Errorf("stale cache should be refreshed: %v %q", stale.cells, stale.Title())
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "live") {
		t.Errorf("refresh should rewrite the cache, got %s", b)
	}

	r.Keys = map[string]Action{"x": {Run: []string{"true"}, Bust: []string{"items"}}}
	r.Cache = map[string]any{"name": "other"}
	actor := newSection("Actor", r, feeds{}).(*listPane)
	feedAll(actor, collect(actor.Init()))
	msgs := collect(actor.Update(key("x")))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("bust should delete the cache entry, stat err %v", err)
	}
	if len(collect(stale.Update(msgs[0]))) != 1 {
		t.Error("a pane whose cache was busted should reload")
	}
}

func TestWatchedFileMakesCacheStale(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	watched := filepath.Join(t.TempDir(), "sesh.toml")
	os.WriteFile(watched, nil, 0o644)
	c := &cache{name: "w", watch: []string{watched}}
	c.write([]byte("x"))
	if _, _, stale, ok := c.read(); !ok || stale {
		t.Fatalf("cache newer than watched file should be fresh: ok=%v stale=%v", ok, stale)
	}
	later := time.Now().Add(time.Minute)
	os.Chtimes(watched, later, later)
	if _, _, stale, _ := c.read(); !stale {
		t.Error("watched file newer than cache should make it stale")
	}
}

func TestMouseWheelMovesListUnderPointer(t *testing.T) {
	list := SectionConfig{Recipe: Recipe{Command: `echo '[{"Name":"a"},{"Name":"b"},{"Name":"c"}]'`, Columns: []string{"Name"}}}
	m, err := New(Config{Pages: []PageConfig{{Sections: [][]SectionConfig{{{}, list}}}}})
	if err != nil {
		t.Fatal(err)
	}
	_, sec, _, _ := m.hit(50, 4)
	p := sec.(*listPane)
	p.Update(p.fetch(nil)())
	wheel := func(b tea.MouseButton) {
		next, _ := m.Update(tea.MouseWheelMsg{X: 50, Y: 4, Button: b})
		m = next.(Model)
	}
	wheel(tea.MouseWheelDown)
	wheel(tea.MouseWheelDown)
	wheel(tea.MouseWheelDown)
	if p.cursor != 2 {
		t.Errorf("cursor = %d after three scrolls down, want 2", p.cursor)
	}
	wheel(tea.MouseWheelUp)
	if p.cursor != 1 || m.focus != 0 {
		t.Errorf("cursor = %d focus = %d after scroll up, want 1 and 0", p.cursor, m.focus)
	}
}

func TestDoubleClickRunsDoubleclickKey(t *testing.T) {
	list := SectionConfig{Recipe: Recipe{
		Command: `echo '[{"Name":"a"},{"Name":"b"}]'`,
		Columns: []string{"Name"},
		Keys:    map[string]Action{"doubleclick": {Run: []string{"open", "{{.Name}}"}, Quit: true}},
	}}
	m, err := New(Config{Pages: []PageConfig{{Sections: [][]SectionConfig{{list}}}}})
	if err != nil {
		t.Fatal(err)
	}
	p := m.focused().(*listPane)
	p.Update(p.fetch(nil)())
	click := func(y int) {
		next, _ := m.Update(tea.MouseClickMsg{X: 5, Y: y, Button: tea.MouseLeft})
		m = next.(Model)
	}
	click(4)
	click(5)
	if m.Action() != nil {
		t.Fatalf("clicks on different rows ran %q", m.Action())
	}
	click(5)
	if got := strings.Join(m.Action(), " "); got != "open b" {
		t.Errorf("double click ran %q, want %q", got, "open b")
	}

	list.Keys = map[string]Action{"enter": {Run: []string{"enter", "{{.Name}}"}, Quit: true}}
	m, _ = New(Config{Pages: []PageConfig{{Sections: [][]SectionConfig{{list}}}}})
	p = m.focused().(*listPane)
	p.Update(p.fetch(nil)())
	click(4)
	click(4)
	if got := strings.Join(m.Action(), " "); got != "enter a" {
		t.Errorf("double click without a doubleclick key ran %q, want %q", got, "enter a")
	}
}
