package dashboard

import (
	"image/color"
	"maps"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	colorAccent       = lipgloss.ANSIColor(4)
	colorDimmed       = lipgloss.ANSIColor(8)
	colorText         = lipgloss.ANSIColor(15)
	colorWarning      = lipgloss.ANSIColor(11)
	colorHighlight    = lipgloss.ANSIColor(0)
	colorHighlightDim = lipgloss.ANSIColor(236)
)

func accentStyle() lipgloss.Style  { return lipgloss.NewStyle().Foreground(colorAccent).Bold(true) }
func DimmedStyle() lipgloss.Style  { return lipgloss.NewStyle().Foreground(colorDimmed) }
func TextStyle() lipgloss.Style    { return lipgloss.NewStyle().Foreground(colorText) }
func WarningStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(colorWarning) }

func cursorStyle(focused bool) lipgloss.Style {
	if focused {
		return lipgloss.NewStyle().Background(colorHighlight)
	}
	return lipgloss.NewStyle().Background(colorHighlightDim)
}

var colorNames = map[string]color.Color{
	"black": lipgloss.Black, "red": lipgloss.Red, "green": lipgloss.Green, "yellow": lipgloss.Yellow,
	"blue": lipgloss.Blue, "magenta": lipgloss.Magenta, "cyan": lipgloss.Cyan, "white": lipgloss.White,
	"brightblack": lipgloss.BrightBlack, "brightred": lipgloss.BrightRed, "brightgreen": lipgloss.BrightGreen,
	"brightyellow": lipgloss.BrightYellow, "brightblue": lipgloss.BrightBlue, "brightmagenta": lipgloss.BrightMagenta,
	"brightcyan": lipgloss.BrightCyan, "brightwhite": lipgloss.BrightWhite,
	"gray": lipgloss.BrightBlack, "grey": lipgloss.BrightBlack,
}

func parseColor(s string) color.Color {
	s = strings.ToLower(strings.TrimSpace(s))
	if c, ok := colorNames[s]; ok {
		return c
	}
	if _, err := strconv.ParseUint(s, 16, 32); err == nil && len(s) == 6 {
		s = "#" + s
	}
	if c := lipgloss.Color(s); c != (lipgloss.NoColor{}) {
		return c
	}
	return nil
}

func parseStyle(spec string) (on, off ansi.Style, ok bool) {
	for _, word := range strings.Fields(spec) {
		switch strings.ToLower(word) {
		case "bold":
			on, off = on.Bold(), off.Normal()
		case "italic":
			on, off = on.Italic(true), off.Italic(false)
		default:
			c := parseColor(word)
			if c == nil {
				return nil, nil, false
			}
			on, off = on.ForegroundColor(c), off.ForegroundColor(nil)
		}
	}
	return on, off, len(on) > 0
}

func paint(s, spec string) string {
	on, off, ok := parseStyle(spec)
	if !ok || s == "" {
		return s
	}
	return on.String() + s + off.String()
}

func borderText(s string) string { return DimmedStyle().Render(s) }

var separator = borderText(" │ ")

type framePane struct {
	number  int
	tabs    []string
	active  int
	content string
	height  int
	focused bool
}

type frameColumn struct {
	width int
	panes []framePane
}

func (p framePane) box(width int) []string {
	border := DimmedStyle()
	if p.focused {
		border = lipgloss.NewStyle().Foreground(colorAccent)
	}
	lines := []string{border.Render("┌") + p.titleBar(width) + border.Render("┐")}
	content := strings.Split(p.content, "\n")
	for row := range p.height {
		line := ""
		if row < len(content) {
			line = content[row]
		}
		lines = append(lines, border.Render("│")+padWidth(line, width)+border.Render("│"))
	}
	return append(lines, border.Render("└"+strings.Repeat("─", width)+"┘"))
}

func renderFrame(cols []frameColumn, height int) string {
	rows := make([]string, height)
	for _, c := range cols {
		var lines []string
		for _, p := range c.panes {
			lines = append(lines, p.box(c.width)...)
		}
		for row := range rows {
			if row < len(lines) {
				rows[row] += lines[row]
			} else {
				rows[row] += strings.Repeat(" ", c.width+2)
			}
		}
	}
	return strings.Join(rows, "\n")
}

func (p framePane) titleBar(width int) string {
	border, text, current := DimmedStyle(), DimmedStyle(), DimmedStyle()
	if len(p.tabs) > 1 {
		current = TextStyle()
	}
	if p.focused {
		border, text, current = lipgloss.NewStyle().Foreground(colorAccent), accentStyle(), accentStyle()
	}
	if width < 4 {
		return border.Render(strings.Repeat("─", max(width, 1)))
	}
	tabs := make([]string, len(p.tabs))
	for i, tab := range p.tabs {
		tabs[i] = DimmedStyle().Render(tab)
		if i == p.active {
			tabs[i] = current.Render(tab)
		}
	}
	title := text.Render(strconv.Itoa(p.number)+" ") + strings.Join(tabs, DimmedStyle().Render(" - "))
	title = ansi.Truncate(title, width-4, "…")
	filler := max(width-3-lipgloss.Width(title), 1)
	return border.Render("─") + " " + title + " " + border.Render(strings.Repeat("─", filler))
}

func padWidth(s string, width int) string {
	width = max(width, 1)
	s = ansi.Truncate(s, width, "")
	return s + strings.Repeat(" ", max(width-lipgloss.Width(s), 0))
}

func renderHeader(active, width int, tabs []string) string {
	parts := make([]string, len(tabs))
	for i, t := range tabs {
		if i == active {
			parts[i] = accentStyle().Render(t)
		} else {
			parts[i] = DimmedStyle().Render(t)
		}
	}
	return padWidth(strings.Join(parts, separator), width) + "\n" + borderText(strings.Repeat("─", width))
}

type keybind struct{ key, label string }

var footerBinds = []keybind{
	{"j/k", "move"},
	{"h/l", "pane"},
	{"enter", "open"},
	{"/", "filter"},
	{"r/R", "refresh"},
	{"1-9", "jump"},
	{"q", "quit"},
}

func renderFooter(width int, pages, filtering bool, query string) string {
	if filtering {
		line := strings.Join([]string{
			accentStyle().Render("filter:") + " " + TextStyle().Render(query),
			accentStyle().Render("esc") + " " + DimmedStyle().Render("clear"),
			accentStyle().Render("enter") + " " + DimmedStyle().Render("done"),
		}, separator)
		return padWidth(line, width)
	}

	format := func(b keybind, labels bool) string {
		if labels {
			return accentStyle().Render(b.key) + " " + DimmedStyle().Render(b.label)
		}
		return accentStyle().Render(b.key)
	}
	line := func(labels bool) string {
		var parts []string
		if pages {
			parts = append(parts, format(keybind{"tab", "page"}, labels))
		}
		for _, b := range footerBinds {
			parts = append(parts, format(b, labels))
		}
		left := strings.Join(parts, separator)
		right := format(keybind{"?", "help"}, labels)
		return left + strings.Repeat(" ", max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)) + right
	}
	if l := line(true); lipgloss.Width(l) <= width {
		return l
	}
	return padWidth(line(false), width)
}

func renderHelp(width, height int, pages bool, extra map[string][]string) string {
	var binds []keybind
	if pages {
		binds = append(binds, keybind{"tab / shift+tab", "next / previous page"})
	}
	binds = append(binds, []keybind{
		{"j/k ↑/↓", "move"},
		{"enter", "open"},
		{"/", "filter"},
		{"r", "refresh pane"},
		{"R", "refresh page"},
		{"[ / ]", "previous / next tab"},
		{"h/l ←/→", "previous / next pane"},
		{"ctrl+h / ctrl+l", "previous / next pane"},
		{"ctrl+j / ctrl+k", "focus pane below / above"},
		{"1-9", "focus pane"},
		{"click", "focus pane and select row"},
		{"?", "close help"},
		{"q / esc", "quit"},
	}...)
	for _, key := range slices.Sorted(maps.Keys(extra)) {
		binds = append(binds, keybind{key, strings.Join(extra[key], " ")})
	}

	keyWidth := 0
	for _, b := range binds {
		keyWidth = max(keyWidth, lipgloss.Width(b.key))
	}
	lines := []string{"", "  " + accentStyle().Render("Keybindings"), ""}
	for _, b := range binds {
		lines = append(lines, "  "+accentStyle().Width(keyWidth).Render(b.key)+"  "+TextStyle().Render(b.label))
	}
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(strings.Join(lines, "\n"))
}

func isCtrlKey(msg tea.KeyPressMsg, letter rune) bool {
	return msg.String() == "ctrl+"+string(letter) || (msg.Mod.Contains(tea.ModCtrl) && msg.Code == letter)
}

func isBackspaceKey(msg tea.KeyPressMsg) bool {
	return msg.Code == tea.KeyBackspace || isCtrlKey(msg, 'h')
}

func isDigitKey(msg tea.KeyPressMsg) bool {
	s := msg.String()
	return len(s) == 1 && s[0] >= '1' && s[0] <= '9'
}
