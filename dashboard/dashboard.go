package dashboard

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type page struct {
	title string
	rows  [][]Section
}

type Model struct {
	pages []page
	page  int
	focus int

	width, height int
	tooSmall      bool
	showHelp      bool
	quit          bool
	action        []string

	contentHeight int
	rowWidths     [][]int
	rowHeights    []int
}

func New(cfg Config) (Model, error) {
	var pages []page
	for i, pc := range cfg.Pages {
		var rows [][]Section
		for _, rowCfg := range pc.Sections {
			var row []Section
			for _, sc := range rowCfg {
				r, err := cfg.resolve(sc)
				if err != nil {
					return Model{}, err
				}
				row = append(row, newSection(sc.Title, r))
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

func (m Model) Action() []string { return m.action }

func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, sec := range m.allSections() {
		cmds = append(cmds, sec.Init())
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
		m.page, m.focus = (m.page+1)%len(m.pages), 0
		return m.withLayout(), nil
	case "shift+tab":
		m.page, m.focus = (m.page-1+len(m.pages))%len(m.pages), 0
		return m.withLayout(), nil
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
	cy := e.Y - 2
	top, base := 0, 0
	for r, panes := range m.rows() {
		if cy >= top && cy < top+m.rowHeights[r] {
			if col := paneCol(e.X, m.rowWidths[r]); col >= 0 {
				m.focus = base + col
				if l, ok := panes[col].(*listPane); ok {
					l.ClickAt(cy - top - 1)
				}
			}
			return m
		}
		top += m.rowHeights[r]
		base += len(panes)
	}
	return m
}

func paneCol(x int, widths []int) int {
	cursor := 1
	for i, w := range widths {
		if x >= cursor && x < cursor+w {
			return i
		}
		cursor += w + 1
	}
	return -1
}

func (m Model) rows() [][]Section { return m.pages[m.page].rows }

func (m Model) paneCount() int {
	n := 0
	for _, row := range m.rows() {
		n += len(row)
	}
	return n
}

func (m Model) paneAt(idx int) (row, col int, ok bool) {
	for r, panes := range m.rows() {
		if idx < len(panes) {
			return r, idx, idx >= 0
		}
		idx -= len(panes)
	}
	return 0, 0, false
}

func (m Model) focused() Section {
	r, c, ok := m.paneAt(m.focus)
	if !ok {
		return nil
	}
	return m.rows()[r][c]
}

func (m Model) allSections() []Section {
	var out []Section
	for _, p := range m.pages {
		for _, row := range p.rows {
			out = append(out, row...)
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
	r, c, ok := m.paneAt(m.focus)
	rows := m.rows()
	target := r + dir
	if !ok || target < 0 || target >= len(rows) {
		return m
	}
	m.focus = min(c, len(rows[target])-1)
	for _, panes := range rows[:target] {
		m.focus += len(panes)
	}
	return m
}

func (m Model) withLayout() Model {
	m.contentHeight = max(m.height-3, 1)
	rows := m.rows()
	m.rowWidths = make([][]int, len(rows))
	for r, panes := range rows {
		m.rowWidths[r] = splitEvenly(max(m.width-len(panes)-1, len(panes)), len(panes))
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

	titles := make([]string, len(m.pages))
	for i, p := range m.pages {
		titles[i] = p.title
	}
	header := renderHeader(m.page, m.width, titles)

	filtering, query := false, ""
	if l, ok := m.focused().(*listPane); ok {
		filtering, query = l.Filtering(), l.FilterQuery()
	}
	footer := renderFooter(m.width, filtering, query)

	var content string
	if m.showHelp {
		var keys map[string][]string
		if l, ok := m.focused().(*listPane); ok {
			keys = l.recipe.Keys
		}
		content = renderHelp(m.width, m.contentHeight, keys)
	} else {
		content = m.viewPage()
	}

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Top, header, content, footer))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) viewPage() string {
	frames := make([]string, 0, len(m.rows()))
	base := 0
	for r, panes := range m.rows() {
		fp := make([]framePane, len(panes))
		for i, s := range panes {
			w := m.rowWidths[r][i]
			focused := m.focus == base+i
			fp[i] = framePane{
				title:   fmt.Sprintf("%d %s", base+i+1, s.Title()),
				content: s.View(w, m.rowHeights[r]-2, focused),
				width:   w,
				focused: focused,
			}
		}
		frames = append(frames, renderFrame(fp, m.rowHeights[r]))
		base += len(panes)
	}
	return lipgloss.JoinVertical(lipgloss.Top, frames...)
}
