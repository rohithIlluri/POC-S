package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/enterprise/aipet/internal/advisor"
	"github.com/enterprise/aipet/internal/store"
)

// Color palette — chosen to read well on both dark and light terminals.
var (
	cGreen  = lipgloss.Color("42")
	cYellow = lipgloss.Color("214")
	cRed    = lipgloss.Color("203")
	cBlue   = lipgloss.Color("39")
	cGray   = lipgloss.Color("245")
	cPurple = lipgloss.Color("141")
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(cPurple)
	dimStyle   = lipgloss.NewStyle().Foreground(cGray)
	okStyle    = lipgloss.NewStyle().Foreground(cGreen)
	tipStyle   = lipgloss.NewStyle().Foreground(cBlue)
	warnStyle  = lipgloss.NewStyle().Foreground(cRed).Bold(true)

	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(cBlue)

	tabStyle       = lipgloss.NewStyle().Foreground(cGray).Padding(0, 0)
	activeTabStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(cBlue)
)

func faceStyle(m mood) lipgloss.Style {
	switch m {
	case moodWorried:
		return lipgloss.NewStyle().Bold(true).Foreground(cRed)
	case moodThinking:
		return lipgloss.NewStyle().Bold(true).Foreground(cYellow)
	default:
		return lipgloss.NewStyle().Bold(true).Foreground(cGreen)
	}
}

func severityBadge(s advisor.Severity) string {
	switch s {
	case advisor.Warn:
		return warnStyle.Render("⚠ WARN")
	case advisor.Tip:
		return tipStyle.Render("● TIP ")
	default:
		return dimStyle.Render("· INFO")
	}
}

func renderSuggestion(s advisor.Suggestion) string {
	var b strings.Builder
	head := fmt.Sprintf("  %s  %s", severityBadge(s.Severity), lipgloss.NewStyle().Bold(true).Render(s.Title))
	b.WriteString(head)
	if s.SavingUSD >= 0.01 {
		b.WriteString(okStyle.Render(fmt.Sprintf("   ~$%.2f/day", s.SavingUSD)))
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("     "+wrap(s.Detail, 72)) + "\n\n")
	return b.String()
}

// budgetBar renders a colored progress bar for today's spend vs. budget.
func budgetBar(spent, budget float64, width int) string {
	if width <= 0 {
		width = 60
	}
	barW := width - 24
	if barW < 10 {
		barW = 10
	}
	ratio := spent / budget
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(barW))
	col := cGreen
	switch {
	case spent/budget >= 1.0:
		col = cRed
	case spent/budget >= 0.75:
		col = cYellow
	}
	bar := lipgloss.NewStyle().Foreground(col).Render(strings.Repeat("█", filled)) +
		dimStyle.Render(strings.Repeat("░", barW-filled))
	label := fmt.Sprintf(" $%.2f / $%.2f", spent, budget)
	return "  " + bar + lipgloss.NewStyle().Foreground(col).Render(label)
}

func kv(k, v string) string {
	return fmt.Sprintf("  %-16s %s\n", dimStyle.Render(k), lipgloss.NewStyle().Bold(true).Render(v))
}

func cacheReuse(s store.Stats) string {
	total := s.TokensIn + s.CacheRead
	if total == 0 {
		return "—"
	}
	pct := float64(s.CacheRead) / float64(total) * 100
	style := okStyle
	if pct < 30 {
		style = warnStyle
	}
	return style.Render(fmt.Sprintf("%.0f%%", pct))
}

// human formats a token count compactly (e.g. 1.2M, 340k).
func human(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// wrap soft-wraps text to width, indenting continuation lines to align with the
// first. Simple word wrap is enough for the short bodies we render.
func wrap(s string, width int) string {
	if width < 20 {
		width = 20
	}
	words := strings.Fields(s)
	var lines []string
	var cur string
	for _, w := range words {
		if cur == "" {
			cur = w
			continue
		}
		if len(cur)+1+len(w) > width {
			lines = append(lines, cur)
			cur = w
		} else {
			cur += " " + w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return strings.Join(lines, "\n     ")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
