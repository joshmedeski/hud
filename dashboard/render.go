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
	colorAccent       = lipgloss.ANSIColor(14)
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
	title   string
	content string
	height  int
	focused bool
}

type frameColumn struct {
	width int
	panes []framePane
}

func (c frameColumn) lines(innerHeight int) (lines []string, divider []bool) {
	for s, p := range c.panes {
		if s > 0 {
			lines = append(lines, frameSegment(p.title, c.width, p.focused))
			divider = append(divider, true)
		}
		content := strings.Split(p.content, "\n")
		for row := range p.height {
			line := ""
			if row < len(content) {
				line = content[row]
			}
			lines = append(lines, padWidth(line, c.width))
			divider = append(divider, false)
		}
	}
	for len(lines) < innerHeight {
		lines = append(lines, strings.Repeat(" ", c.width))
		divider = append(divider, false)
	}
	return lines[:innerHeight], divider[:innerHeight]
}

var junctions = map[[2]bool]string{
	{false, false}: "│", {true, false}: "┤", {false, true}: "├", {true, true}: "┼",
}

func renderFrame(cols []frameColumn, height int) string {
	if len(cols) == 0 {
		return ""
	}
	innerHeight := max(height-2, 1)
	lines := make([][]string, len(cols))
	dividers := make([][]bool, len(cols))
	for i, c := range cols {
		lines[i], dividers[i] = c.lines(innerHeight)
	}

	var b strings.Builder
	b.WriteString(borderText("┌"))
	for i, c := range cols {
		if i > 0 {
			b.WriteString(borderText("┬"))
		}
		b.WriteString(frameSegment(c.panes[0].title, c.width, c.panes[0].focused))
	}
	b.WriteString(borderText("┐"))
	b.WriteString("\n")

	for row := range innerHeight {
		for i := range cols {
			b.WriteString(borderText(junctions[[2]bool{i > 0 && dividers[i-1][row], dividers[i][row]}]))
			b.WriteString(lines[i][row])
		}
		b.WriteString(borderText(junctions[[2]bool{dividers[len(cols)-1][row], false}]))
		b.WriteString("\n")
	}

	b.WriteString(borderText("└"))
	for i, c := range cols {
		if i > 0 {
			b.WriteString(borderText("┴"))
		}
		b.WriteString(borderText(strings.Repeat("─", c.width)))
	}
	b.WriteString(borderText("┘"))
	return b.String()
}

func frameSegment(title string, width int, focused bool) string {
	if width < 4 {
		return borderText(strings.Repeat("─", max(width, 1)))
	}
	style := DimmedStyle()
	if focused {
		style = accentStyle()
	}
	title = ansi.Truncate(title, width-4, "…")
	filler := max(width-3-lipgloss.Width(title), 1)
	return borderText("─") + style.Render(" "+title+" ") + borderText(strings.Repeat("─", filler))
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
	{"tab", "page"},
	{"j/k", "move"},
	{"h/l", "pane"},
	{"enter", "open"},
	{"/", "filter"},
	{"r", "refresh"},
	{"1-9", "jump"},
	{"q", "quit"},
}

func renderFooter(width int, filtering bool, query string) string {
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
		parts := make([]string, len(footerBinds))
		for i, b := range footerBinds {
			parts[i] = format(b, labels)
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

func renderHelp(width, height int, extra map[string][]string) string {
	binds := []keybind{
		{"tab / shift+tab", "next / previous page"},
		{"j/k ↑/↓", "move"},
		{"enter", "open"},
		{"/", "filter"},
		{"r", "refresh"},
		{"h/l ←/→", "previous / next pane"},
		{"ctrl+h / ctrl+l", "previous / next pane"},
		{"ctrl+j / ctrl+k", "focus pane below / above"},
		{"1-9", "focus pane"},
		{"click", "focus pane and select row"},
		{"?", "close help"},
		{"q / esc", "quit"},
	}
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
