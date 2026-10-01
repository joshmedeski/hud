package dashboard

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
enter = ["open-item", "{{.Name}}"]
keys = { "ctrl+d" = ["remove-item", "{{.Name}}"] }

[[page]]
title = "Main"
sections = [[
  { title = "Mine", recipe = "items", command = "list-items --mine", keys = { "x" = ["echo", "{{.Name}}"] } },
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
	if !slices.Equal(mine.Enter, []string{"open-item", "{{.Name}}"}) {
		t.Errorf("enter not inherited from recipe: %v", mine.Enter)
	}
	if _, ok := mine.Keys["ctrl+d"]; !ok {
		t.Error("recipe keys dropped when section adds its own")
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
		Enter:   []string{"open", "{{.Name}}"},
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

func TestEnterQuitsWithAction(t *testing.T) {
	m, err := New(Config{Pages: []PageConfig{{Sections: [][]SectionConfig{{{
		Recipe: Recipe{Command: `echo '[{"Name":"a"}]'`, Columns: []string{"Name"}, Enter: []string{"echo", "{{.Name}}"}},
	}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	p := m.focused().(*listPane)
	p.Update(p.fetch(nil)())

	next, cmd := m.Update(key("enter"))
	if !slices.Equal(next.(Model).Action(), []string{"echo", "a"}) || cmd == nil {
		t.Errorf("action = %q", next.(Model).Action())
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
		Enter:   []string{"gh", "browse", "{{.Number}}"},
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
		next := m.handleMouseClick(tea.MouseClickMsg{X: 3, Y: line + 2, Button: tea.MouseLeft})
		if got := next.focused().Title(); got != want {
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
