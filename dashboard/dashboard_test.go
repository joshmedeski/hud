package dashboard

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
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
[[page]]
title = "Main"
sections = [[
  { title = "Tmux", recipe = "sesh", command = "sesh list -t --json", keys = { "x" = ["echo", "{{.Name}}"] } },
  { title = "Weather", command = "curl wttr.in", refresh = 300 },
]]
`))
	if err != nil {
		t.Fatal(err)
	}
	row := cfg.Pages[0].Sections[0]

	tmux, err := cfg.resolve(row[0])
	if err != nil {
		t.Fatal(err)
	}
	if tmux.Command != "sesh list -t --json" {
		t.Errorf("command override lost: %q", tmux.Command)
	}
	if !slices.Equal(tmux.Enter, builtinRecipes["sesh"].Enter) {
		t.Errorf("enter not inherited from recipe: %v", tmux.Enter)
	}
	if _, ok := tmux.Keys["ctrl+d"]; !ok {
		t.Error("recipe keys dropped when section adds its own")
	}
	if _, ok := builtinRecipes["sesh"].Keys["x"]; ok {
		t.Error("section keys leaked into the builtin recipe")
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
	p := newSection("test", r).(*listPane)
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
		Command: `echo '[{"Name":"my custom binaries","Attached":1},{"Name":"sesh","Attached":0},{"Name":"hud"}]'`,
		Columns: []string{"Name", "Attached"},
		Enter:   []string{"sesh", "connect", "{{.Name}}"},
	})
	if got := p.cells[0]; !slices.Equal(got, []string{"my custom binaries", "1"}) {
		t.Errorf("cells = %q", got)
	}
	if p.cells[2][1] != "" {
		t.Errorf("missing field should render empty, got %q", p.cells[2][1])
	}

	p.Update(key("/"))
	for _, k := range "sesh" {
		p.Update(key(string(k)))
	}
	if len(p.visible) != 1 {
		t.Fatalf("filter kept %d rows, want 1", len(p.visible))
	}
	p.Update(key("enter"))
	if !slices.Equal(p.Chosen(), []string{"sesh", "connect", "sesh"}) {
		t.Errorf("chosen = %q", p.Chosen())
	}
	if p.Filtering() {
		t.Error("enter should leave filter mode")
	}

	p.chosen = nil
	p.Update(key("k"))
	p.Update(key("enter"))
	if got := p.Chosen(); len(got) != 3 || got[2] != "my custom binaries" {
		t.Errorf("name with spaces must stay one argv element: %q", got)
	}
}

func TestListPaneReportsBadJSON(t *testing.T) {
	p := newSection("test", Recipe{Command: "echo nope", Columns: []string{"Name"}}).(*listPane)
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

func TestLayout(t *testing.T) {
	if got := splitEvenly(10, 3); !slices.Equal(got, []int{4, 3, 3}) {
		t.Errorf("splitEvenly = %v", got)
	}
	widths := []int{5, 5}
	for x, want := range map[int]int{0: -1, 1: 0, 5: 0, 6: -1, 7: 1, 11: 1, 12: -1} {
		if got := paneCol(x, widths); got != want {
			t.Errorf("paneCol(%d) = %d, want %d", x, got, want)
		}
	}
}

func TestWorktreeRowRendersIntegersAndLists(t *testing.T) {
	p := loadedList(t, Recipe{
		Command: `printf '%s' '[{"Number":7235,"Alerts":["bell","activity"],"location":"Cafe\n626 E Ninth St"}]'`,
		Columns: []string{"Number", "Alerts", "location"},
		Enter:   []string{"sesh", "worktree", "connect", "{{.Number}}"},
	})
	if got := p.cells[0]; !slices.Equal(got, []string{"7235", "bell activity", "Cafe 626 E Ninth St"}) {
		t.Errorf("cells = %q", got)
	}
	p.Update(key("enter"))
	if got := p.Chosen(); len(got) != 4 || got[3] != "7235" {
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
