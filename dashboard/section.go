package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"text/template"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type Section interface {
	Title() string
	Fit() bool
	ContentHeight() int
	Init() tea.Cmd
	Update(msg tea.Msg) tea.Cmd
	View(width, height int, focused bool) string
}

func newSection(title string, r Recipe) Section {
	src := source{title: title, recipe: r, loading: true}
	if len(r.Columns) > 0 {
		var headers []string
		if r.Headers == nil || *r.Headers {
			headers = make([]string, len(r.Columns))
			for i, col := range r.Columns {
				headers[i] = humanize(col)
				if label, ok := r.Labels[col]; ok {
					headers[i] = label
				}
			}
		}
		return &listPane{source: src, headers: headers}
	}
	return &textPane{source: src}
}

type loadedMsg struct {
	src *source
	out []byte
	err error
}

type refreshMsg struct{ src *source }

type source struct {
	title   string
	recipe  Recipe
	loading bool
	out     []byte
	err     error
}

func (s *source) Title() string { return s.title }

func (s *source) Fit() bool { return s.recipe.Fit }

func (s *source) Init() tea.Cmd { return tea.Batch(s.fetch(nil), s.tick()) }

func (s *source) fetch(before []string) tea.Cmd {
	return func() tea.Msg {
		if len(before) > 0 {
			if err := runArgv(before); err != nil {
				return loadedMsg{src: s, err: err}
			}
		}
		out, err := runShell(s.recipe.Command)
		return loadedMsg{src: s, out: out, err: err}
	}
}

func (s *source) tick() tea.Cmd {
	if s.recipe.Refresh <= 0 {
		return nil
	}
	return tea.Tick(time.Duration(s.recipe.Refresh)*time.Second, func(time.Time) tea.Msg {
		return refreshMsg{src: s}
	})
}

func (s *source) update(msg tea.Msg) (cmd tea.Cmd, loaded bool) {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.src == s {
			s.loading, s.out, s.err = false, msg.out, msg.err
			return nil, true
		}
	case refreshMsg:
		if msg.src == s {
			return tea.Batch(s.fetch(nil), s.tick()), false
		}
	case tea.KeyPressMsg:
		if msg.String() == "r" {
			return s.fetch(nil), false
		}
	}
	return nil, false
}

func (s *source) status() (string, bool) {
	switch {
	case s.recipe.Command == "":
		return "  No command configured", true
	case s.loading:
		return DimmedStyle().Render("  Loading…"), true
	case s.err != nil:
		return WarningStyle().Render("  Error: " + s.err.Error()), true
	}
	return "", false
}

func runShell(command string) ([]byte, error) {
	if command == "" {
		return nil, nil
	}
	out, err := exec.Command("sh", "-c", command).Output()
	if ee, ok := errors.AsType[*exec.ExitError](err); ok && len(ee.Stderr) > 0 {
		return out, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
	}
	return out, err
}

func runArgv(argv []string) error {
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	if err != nil && len(out) > 0 {
		return fmt.Errorf("%s: %s", argv[0], strings.TrimSpace(string(out)))
	}
	return err
}

func expand(argv []string, item map[string]any) ([]string, error) {
	out := make([]string, len(argv))
	for i, arg := range argv {
		t, err := template.New("").Option("missingkey=error").Parse(arg)
		if err != nil {
			return nil, err
		}
		var b strings.Builder
		if err := t.Execute(&b, item); err != nil {
			return nil, err
		}
		out[i] = b.String()
	}
	return out, nil
}

type textPane struct {
	source
	text string
}

func (p *textPane) Update(msg tea.Msg) tea.Cmd {
	cmd, loaded := p.update(msg)
	if loaded {
		p.text = collapseCarriageReturns(string(p.out))
	}
	return cmd
}

func (p *textPane) ContentHeight() int {
	if _, ok := p.status(); ok || p.text == "" {
		return 1
	}
	return strings.Count(strings.TrimRight(p.text, "\n"), "\n") + 1
}

func (p *textPane) View(width, height int, focused bool) string {
	if s, ok := p.status(); ok {
		return s
	}
	if p.text == "" {
		return DimmedStyle().Render("  No output")
	}
	return p.text
}

func collapseCarriageReturns(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if j := strings.LastIndex(line, "\r"); j >= 0 {
			lines[i] = line[j+1:]
		}
	}
	return strings.Join(lines, "\n")
}

type listPane struct {
	source
	headers []string
	items   []map[string]any
	cells   [][]string
	painted [][]string
	visible []int

	cursor, offset, viewHeight int
	filtering                  bool
	query                      string
	chosen                     []string
}

func (p *listPane) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		return p.handleKey(key)
	}
	cmd, loaded := p.update(msg)
	if loaded {
		p.parse()
	}
	return cmd
}

func (p *listPane) parse() {
	p.items = nil
	if p.err == nil && len(p.out) > 0 {
		if err := json.Unmarshal(p.out, &p.items); err != nil {
			p.err = fmt.Errorf("parsing %q output as a JSON array: %w", p.recipe.Command, err)
		}
	}
	p.cells = make([][]string, len(p.items))
	p.painted = make([][]string, len(p.items))
	for i, item := range p.items {
		p.cells[i] = make([]string, len(p.recipe.Columns))
		p.painted[i] = make([]string, len(p.recipe.Columns))
		for c, col := range p.recipe.Columns {
			p.cells[i][c] = cellText(item[col])
			p.painted[i][c] = paint(p.cells[i][c], cellStyle(p.recipe.Colors[col], p.cells[i][c], item))
		}
	}
	p.applyFilter()
}

func cellText(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return strings.Join(strings.Fields(v), " ")
	case []any:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = cellText(e)
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(v)
	}
}

func cellStyle(rule any, cell string, item map[string]any) string {
	if byValue, ok := rule.(map[string]any); ok {
		rule = byValue[cell]
	}
	s, _ := rule.(string)
	if strings.Contains(s, "{{") {
		out, err := expand([]string{s}, item)
		if err != nil {
			return ""
		}
		s = out[0]
	}
	return s
}

func (p *listPane) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if p.filtering {
		p.handleFilterKey(msg)
		return nil
	}
	switch msg.String() {
	case "j", "down":
		p.cursorDown()
	case "k", "up":
		p.cursorUp()
	case "enter":
		p.selectItem()
	case "/":
		p.filtering, p.query = true, ""
		p.applyFilter()
	case "r":
		return p.fetch(nil)
	default:
		if argv, ok := p.recipe.Keys[msg.String()]; ok {
			return p.runKey(argv)
		}
	}
	return nil
}

func (p *listPane) handleFilterKey(msg tea.KeyPressMsg) {
	if isBackspaceKey(msg) {
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
		}
		p.applyFilter()
		return
	}
	switch msg.String() {
	case "esc":
		p.filtering, p.query = false, ""
		p.applyFilter()
	case "enter":
		p.selectItem()
		p.filtering, p.query = false, ""
		p.applyFilter()
	case "down":
		p.cursorDown()
	case "up":
		p.cursorUp()
	default:
		if msg.Text != "" {
			p.query += msg.Text
			p.applyFilter()
		}
	}
}

func (p *listPane) Filtering() bool     { return p.filtering }
func (p *listPane) FilterQuery() string { return p.query }
func (p *listPane) Chosen() []string    { return p.chosen }

func (p *listPane) hovered() (map[string]any, bool) {
	if len(p.visible) == 0 {
		return nil, false
	}
	return p.items[p.visible[p.cursor]], true
}

func (p *listPane) selectItem() {
	item, ok := p.hovered()
	if !ok || len(p.recipe.Enter) == 0 {
		return
	}
	argv, err := expand(p.recipe.Enter, item)
	if err != nil {
		p.err = err
		return
	}
	p.chosen = argv
}

func (p *listPane) runKey(argv []string) tea.Cmd {
	item, ok := p.hovered()
	if !ok {
		return nil
	}
	expanded, err := expand(argv, item)
	if err != nil {
		p.err = err
		return nil
	}
	return p.fetch(expanded)
}

func (p *listPane) applyFilter() {
	q := strings.ToLower(p.query)
	p.visible = p.visible[:0]
	for i, row := range p.cells {
		if q == "" || strings.Contains(strings.ToLower(strings.Join(row, " ")), q) {
			p.visible = append(p.visible, i)
		}
	}
	p.cursor = min(p.cursor, max(len(p.visible)-1, 0))
	p.offset = min(p.offset, p.cursor)
}

func (p *listPane) cursorUp() {
	p.cursor = max(p.cursor-1, 0)
	p.offset = min(p.offset, p.cursor)
}

func (p *listPane) cursorDown() {
	p.cursor = min(p.cursor+1, max(len(p.visible)-1, 0))
	if p.cursor >= p.offset+p.rowsInView() {
		p.offset = p.cursor - p.rowsInView() + 1
	}
}

func (p *listPane) rowsInView() int {
	if p.viewHeight <= 0 {
		return 20
	}
	return p.viewHeight
}

func (p *listPane) headerRows() int {
	if p.headers == nil {
		return 0
	}
	return 1
}

func (p *listPane) ClickAt(row int) {
	row -= p.headerRows()
	if len(p.visible) == 0 || row < 0 {
		return
	}
	p.cursor = min(p.offset+row, len(p.visible)-1)
}

func (p *listPane) ContentHeight() int {
	if _, ok := p.status(); ok || len(p.items) == 0 {
		return 1
	}
	return p.headerRows() + len(p.items)
}

func (p *listPane) View(width, height int, focused bool) string {
	p.viewHeight = max(height-p.headerRows(), 1)
	if s, ok := p.status(); ok {
		return s
	}
	if len(p.items) == 0 {
		return DimmedStyle().Render("  Nothing to show")
	}

	widths := p.columnWidths(width)
	var lines []string
	if p.headers != nil {
		lines = append(lines, DimmedStyle().Render(joinCells(p.headers, widths)))
	}
	end := min(p.offset+p.viewHeight, len(p.visible))
	for i := p.offset; i < end; i++ {
		line := joinCells(p.painted[p.visible[i]], widths)
		if i == p.cursor {
			line = cursorStyle(focused).Render(padWidth(line, width))
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (p *listPane) columnWidths(width int) []int {
	widths := make([]int, len(p.recipe.Columns))
	total := 1 + 2*(len(widths)-1)
	for c := range widths {
		if p.headers != nil {
			widths[c] = lipgloss.Width(p.headers[c])
		}
		for _, row := range p.cells {
			widths[c] = max(widths[c], lipgloss.Width(row[c]))
		}
		total += widths[c]
	}
	for ; total > width; total-- {
		widest := slices.Index(widths, slices.Max(widths))
		if widths[widest] <= 1 {
			break
		}
		widths[widest]--
	}
	return widths
}

func humanize(key string) string {
	var b strings.Builder
	prev := ' '
	for _, r := range strings.ReplaceAll(key, "_", " ") {
		switch {
		case prev == ' ':
			r = unicode.ToUpper(r)
		case unicode.IsUpper(r) && unicode.IsLower(prev):
			b.WriteRune(' ')
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}

func joinCells(cells []string, widths []int) string {
	var b strings.Builder
	b.WriteString(" ")
	for c, cell := range cells {
		cell = ansi.Truncate(cell, widths[c], "…")
		b.WriteString(cell)
		if c < len(cells)-1 {
			b.WriteString(strings.Repeat(" ", widths[c]-lipgloss.Width(cell)+2))
		}
	}
	return b.String()
}
