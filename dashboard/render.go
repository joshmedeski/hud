package dashboard

import (
	"maps"
	"slices"
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
	colorHighlight    = lipgloss.ANSIColor(8)
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

func borderText(s string) string { return DimmedStyle().Render(s) }

var separator = borderText(" │ ")

type framePane struct {
	title   string
	content string
	width   int
	focused bool
}

func renderFrame(panes []framePane, height int) string {
	if len(panes) == 0 {
		return ""
	}
	innerHeight := max(height-2, 1)
	lines := make([][]string, len(panes))
	for i, p := range panes {
		lines[i] = strings.Split(p.content, "\n")
	}

	var b strings.Builder
	b.WriteString(borderText("┌"))
	for i, p := range panes {
		if i > 0 {
			b.WriteString(borderText("┬"))
		}
		b.WriteString(frameSegment(p.title, p.width, p.focused))
	}
	b.WriteString(borderText("┐") + "\n")

	for row := range innerHeight {
		b.WriteString(borderText("│"))
		for i, p := range panes {
			line := ""
			if row < len(lines[i]) {
				line = lines[i][row]
			}
			b.WriteString(padWidth(line, p.width))
			b.WriteString(borderText("│"))
		}
		b.WriteString("\n")
	}

	b.WriteString(borderText("└"))
	for i, p := range panes {
		if i > 0 {
			b.WriteString(borderText("┴"))
		}
		b.WriteString(borderText(strings.Repeat("─", p.width)))
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
	{"enter", "open"},
	{"/", "filter"},
	{"r", "refresh"},
	{"1-9", "panes"},
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
		{"ctrl+h / ctrl+l", "focus pane left / right"},
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
