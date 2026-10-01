package dashboard

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type column []Section

type page struct {
	title string
	rows  [][]column
}

type Model struct {
	pages []page
	page  int
	focus int

	width, height int
	tooSmall      bool
	showHelp      bool
	hidePages     bool
	quit          bool
	action        []string

	contentHeight int
	rowWidths     [][]int
	rowHeights    []int
}

func New(cfg Config) (Model, error) {
	var pages []page
	fs := feeds{}
	for i, pc := range cfg.Pages {
		var rows [][]column
		for _, rowCfg := range pc.Sections {
			var row []column
			for _, sc := range rowCfg {
				col, err := cfg.column(sc, fs)
				if err != nil {
					return Model{}, err
				}
				row = append(row, col)
			}
			if len(row) > 0 {
				rows = append(rows, row)
			}
		}
		if len(rows) == 0 {
			continue
		}
		title := pc.Title
		if title == "" {
			title = fmt.Sprintf("Page %d", i+1)
		}
		pages = append(pages, page{title: title, rows: rows})
	}
	if len(pages) == 0 {
		return Model{}, fmt.Errorf("no page has any sections")
	}
	m := Model{pages: pages, width: 80, height: 24}
	return m.withLayout(), nil
}

func (c Config) column(sc SectionConfig, fs feeds) (column, error) {
	members := sc.Stack
	if len(members) == 0 {
		members = []SectionConfig{sc}
	}
	var col column
	for _, member := range members {
		if len(member.Stack) > 0 {
			return nil, fmt.Errorf("section %q: a stack can't contain another stack", member.Title)
		}
		r, err := c.resolve(member)
		if err != nil {
			return nil, err
		}
		col = append(col, newSection(member.Title, r, fs))
	}
	return col, nil
}

func (m Model) Action() []string { return m.action }

func (m Model) OpenPage(title string) (Model, error) {
	for i, p := range m.pages {
		if strings.EqualFold(p.title, title) {
			m.page = i
			return m.withLayout(), nil
		}
	}
	return m, fmt.Errorf("no page titled %q", title)
}

func (m Model) HidePages() Model {
	m.hidePages = true
	return m.withLayout()
}

func (m Model) headerHeight() int {
	if m.hidePages {
		return 0
	}
	return 2
}

func useGraphemeWidths() tea.Msg {
	return tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(useGraphemeWidths, m.initPage())
}

func (m Model) initPage() tea.Cmd {
	var cmds []tea.Cmd
	for _, cols := range m.rows() {
		for _, panes := range cols {
			for _, sec := range panes {
				cmds = append(cmds, sec.Init())
			}
		}
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.tooSmall = m.width < 20 || m.height < 5
		return m.withLayout(), nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg), nil
	}
	var cmds []tea.Cmd
	for _, sec := range m.allSections() {
		cmds = append(cmds, sec.Update(msg))
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.quit = true
		return m, tea.Quit
	}
	if f, ok := m.focused().(*listPane); ok && f.Filtering() {
		return m.routeKey(msg)
	}
	if m.showHelp {
		switch msg.String() {
		case "?", "esc", "q":
			m.showHelp = false
		}
		return m, nil
	}

	switch msg.String() {
	case "?":
		m.showHelp = true
		return m, nil
	case "q", "esc":
		m.quit = true
		return m, tea.Quit
	case "tab":
		if m.hidePages {
			return m, nil
		}
		m.page, m.focus = (m.page+1)%len(m.pages), 0
		m = m.withLayout()
		return m, m.initPage()
	case "shift+tab":
		if m.hidePages {
			return m, nil
		}
		m.page, m.focus = (m.page-1+len(m.pages))%len(m.pages), 0
		m = m.withLayout()
		return m, m.initPage()
	}

	switch {
	case isBackspaceKey(msg), msg.String() == "h", msg.String() == "left":
		return m.moveFocus(-1), nil
	case isCtrlKey(msg, 'l'), msg.String() == "l", msg.String() == "right":
		return m.moveFocus(1), nil
	case isCtrlKey(msg, 'j'):
		return m.moveFocusRow(1), nil
	case isCtrlKey(msg, 'k'):
		return m.moveFocusRow(-1), nil
	case isDigitKey(msg):
		if idx := int(msg.String()[0] - '1'); idx < m.paneCount() {
			m.focus = idx
		}
		return m, nil
	}
	return m.routeKey(msg)
}

func (m Model) routeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	sec := m.focused()
	if sec == nil {
		return m, nil
	}
	cmd := sec.Update(msg)
	if l, ok := sec.(*listPane); ok && l.Chosen() != nil {
		m.action = l.Chosen()
		return m, tea.Quit
	}
	return m, cmd
}

func (m Model) handleMouseClick(msg tea.MouseClickMsg) Model {
	e := msg.Mouse()
	if m.showHelp || e.Button != tea.MouseLeft {
		return m
	}
	cy := e.Y - m.headerHeight()
	top, base := 0, 0
	for r, cols := range m.rows() {
		if cy >= top && cy < top+m.rowHeights[r] {
			c := paneCol(e.X, m.rowWidths[r])
			if c < 0 {
				return m
			}
			for _, prev := range cols[:c] {
				base += len(prev)
			}
			line, start := cy-top, 0
			for s, h := range stackHeights(m.rowHeights[r], cols[c]) {
				if line < start+h+2 || s == len(cols[c])-1 {
					m.focus = base + s
					if l, ok := cols[c][s].(*listPane); ok {
						l.ClickAt(line - start - 1)
					}
					return m
				}
				start += h + 2
			}
			return m
		}
		top += m.rowHeights[r]
		base += paneTotal(cols)
	}
	return m
}

func stackHeights(height int, panes column) []int {
	remaining := max(height-2*len(panes), len(panes))
	heights := make([]int, len(panes))
	var flex []int
	fitted := 0
	for i, p := range panes {
		if p.Fit() {
			fitted++
		} else {
			flex = append(flex, i)
		}
	}
	for i, p := range panes {
		if !p.Fit() {
			continue
		}
		fitted--
		heights[i] = max(min(p.ContentHeight(), remaining-len(flex)-fitted), 1)
		remaining -= heights[i]
	}
	for i, h := range splitEvenly(max(remaining, len(flex)), len(flex)) {
		heights[flex[i]] = h
	}
	return heights
}

func paneTotal(cols []column) int {
	n := 0
	for _, col := range cols {
		n += len(col)
	}
	return n
}

func paneCol(x int, widths []int) int {
	cursor := 0
	for i, w := range widths {
		if x >= cursor && x < cursor+w+2 {
			return i
		}
		cursor += w + 2
	}
	return -1
}

func (m Model) rows() [][]column { return m.pages[m.page].rows }

func (m Model) paneCount() int {
	n := 0
	for _, cols := range m.rows() {
		n += paneTotal(cols)
	}
	return n
}

func (m Model) paneAt(idx int) (row, col, stack int, ok bool) {
	if idx < 0 {
		return 0, 0, 0, false
	}
	for r, cols := range m.rows() {
		for c, panes := range cols {
			if idx < len(panes) {
				return r, c, idx, true
			}
			idx -= len(panes)
		}
	}
	return 0, 0, 0, false
}

func (m Model) indexOf(row, col, stack int) int {
	idx := stack
	for r, cols := range m.rows()[:row+1] {
		for c, panes := range cols {
			if r == row && c == col {
				return idx
			}
			idx += len(panes)
		}
	}
	return idx
}

func (m Model) focused() Section {
	r, c, s, ok := m.paneAt(m.focus)
	if !ok {
		return nil
	}
	return m.rows()[r][c][s]
}

func (m Model) allSections() []Section {
	var out []Section
	for _, p := range m.pages {
		for _, cols := range p.rows {
			for _, panes := range cols {
				out = append(out, panes...)
			}
		}
	}
	return out
}

func (m Model) moveFocus(delta int) Model {
	n := m.paneCount()
	m.focus = ((m.focus+delta)%n + n) % n
	return m
}

func (m Model) moveFocusRow(dir int) Model {
	r, c, s, ok := m.paneAt(m.focus)
	if !ok {
		return m
	}
	rows := m.rows()
	if s+dir >= 0 && s+dir < len(rows[r][c]) {
		m.focus += dir
		return m
	}
	target := r + dir
	if target < 0 || target >= len(rows) {
		return m
	}
	c = min(c, len(rows[target])-1)
	s = 0
	if dir < 0 {
		s = len(rows[target][c]) - 1
	}
	m.focus = m.indexOf(target, c, s)
	return m
}

func (m Model) withLayout() Model {
	m.contentHeight = max(m.height-m.headerHeight()-1, 1)
	rows := m.rows()
	m.rowWidths = make([][]int, len(rows))
	for r, cols := range rows {
		m.rowWidths[r] = splitEvenly(max(m.width-2*len(cols), len(cols)), len(cols))
	}
	m.rowHeights = splitEvenly(m.contentHeight, len(rows))
	return m
}

func splitEvenly(total, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = max(total/n, 1)
		if i < total%n {
			out[i]++
		}
	}
	return out
}

func (m Model) View() tea.View {
	if m.quit || m.action != nil {
		return tea.NewView("")
	}
	if m.tooSmall {
		return tea.NewView("Terminal too small for hud")
	}

	var parts []string
	if !m.hidePages {
		titles := make([]string, len(m.pages))
		for i, p := range m.pages {
			titles[i] = p.title
		}
		parts = append(parts, renderHeader(m.page, m.width, titles))
	}

	filtering, query := false, ""
	if l, ok := m.focused().(*listPane); ok {
		filtering, query = l.Filtering(), l.FilterQuery()
	}
	footer := renderFooter(m.width, !m.hidePages, filtering, query)

	var content string
	if m.showHelp {
		var keys map[string][]string
		if l, ok := m.focused().(*listPane); ok {
			keys = l.recipe.Keys
		}
		content = renderHelp(m.width, m.contentHeight, !m.hidePages, keys)
	} else {
		content = m.viewPage()
	}

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Top, append(parts, content, footer)...))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) viewPage() string {
	frames := make([]string, 0, len(m.rows()))
	base := 0
	for r, cols := range m.rows() {
		fc := make([]frameColumn, len(cols))
		for i, panes := range cols {
			w := m.rowWidths[r][i]
			heights := stackHeights(m.rowHeights[r], panes)
			fc[i] = frameColumn{width: w, panes: make([]framePane, len(panes))}
			for s, sec := range panes {
				focused := m.focus == base
				fc[i].panes[s] = framePane{
					title:   fmt.Sprintf("%d %s", base+1, sec.Title()),
					content: sec.View(w, heights[s], focused),
					height:  heights[s],
					focused: focused,
				}
				base++
			}
		}
		frames = append(frames, renderFrame(fc, m.rowHeights[r]))
	}
	return lipgloss.JoinVertical(lipgloss.Top, frames...)
}
