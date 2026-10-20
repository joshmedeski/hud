package dashboard

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type calendarPane struct {
	source
	days []calendarDay
}

type calendarDay struct {
	date    time.Time
	entries []calendarEntry
}

type calendarEntry struct {
	at, end     time.Time
	text, plain string
	time        string
}

func (p *calendarPane) Update(msg tea.Msg) tea.Cmd {
	cmd, loaded := p.update(msg)
	if loaded {
		p.parse(time.Now())
	}
	return cmd
}

func (p *calendarPane) parse(now time.Time) {
	p.days = nil
	var items []map[string]any
	if p.err == nil && len(p.out) > 0 {
		if err := json.Unmarshal(p.out, &items); err != nil {
			p.err = fmt.Errorf("parsing %q output as a JSON array: %w", p.recipe.Command, err)
			return
		}
	}
	today := startOfDay(now)
	var entries []calendarEntry
	for _, item := range items {
		at, ok := parseDate(cellText(item[p.recipe.Date]))
		if !ok || at.Before(today) || !matches(item, p.recipe.Where) {
			continue
		}
		end, ok := parseDate(cellText(item[p.recipe.End]))
		if !ok {
			end = at
		}
		parts := make([]string, len(p.recipe.Columns))
		plain := make([]string, len(p.recipe.Columns))
		for c, col := range p.recipe.Columns {
			plain[c] = cellText(item[col])
			parts[c] = paint(plain[c], cellStyle(p.recipe.Colors[col], plain[c], item))
		}
		entries = append(entries, calendarEntry{
			at: at, end: end, time: clockTime(at),
			text: strings.Join(parts, " "), plain: strings.Join(plain, " "),
		})
	}
	slices.SortStableFunc(entries, func(a, b calendarEntry) int { return a.at.Compare(b.at) })
	for _, e := range entries {
		date := startOfDay(e.at)
		if n := len(p.days); n == 0 || !p.days[n-1].date.Equal(date) {
			if n == p.recipe.Days {
				break
			}
			p.days = append(p.days, calendarDay{date: date})
		}
		last := &p.days[len(p.days)-1]
		last.entries = append(last.entries, e)
	}
}

func parseDate(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t.Local(), true
		}
	}
	return time.Time{}, false
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func clockTime(t time.Time) string {
	if t.Equal(startOfDay(t)) {
		return ""
	}
	return strings.Replace(t.Format("3:04pm"), ":00", "", 1)
}

func (p *calendarPane) ContentHeight() int {
	if _, ok := p.status(); ok || len(p.days) == 0 {
		return 1
	}
	rows := 0
	for _, d := range p.days {
		rows = max(rows, len(d.entries))
	}
	return rows + 1
}

func (p *calendarPane) View(width, height int, focused bool) string {
	return p.view(width, height, time.Now())
}

func (p *calendarPane) view(width, height int, now time.Time) string {
	if s, ok := p.status(); ok {
		return s
	}
	if len(p.days) == 0 {
		return DimmedStyle().Render("  Nothing to show")
	}
	colWidth := max(width/len(p.days)-1, 1)
	lines := make([]string, min(height, p.ContentHeight()))
	for _, day := range p.days {
		for row := range lines {
			cell := ""
			switch {
			case row == 0:
				cell = DimmedStyle().Render(ansi.Truncate(day.date.Format("Mon"), colWidth, "…"))
			case row <= len(day.entries):
				cell = entryLine(day.entries[row-1], colWidth, now)
			}
			lines[row] += " " + padWidth(cell, colWidth)
		}
	}
	return strings.Join(lines, "\n")
}

func entryLine(e calendarEntry, width int, now time.Time) string {
	textWidth := width
	if e.time != "" {
		textWidth = max(width-lipgloss.Width(e.time)-1, 1)
	}
	text := ansi.Truncate(e.text, textWidth, "…")
	switch {
	case !e.end.After(now):
		text = DimmedStyle().Render(ansi.Truncate(e.plain, textWidth, "…"))
	case !e.at.After(now):
		text = lipgloss.NewStyle().Bold(true).Render(text)
	}
	if e.time == "" {
		return text
	}
	return text + " " + DimmedStyle().Render(e.time)
}
