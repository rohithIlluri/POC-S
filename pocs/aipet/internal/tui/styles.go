package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/advisor"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

// Color roles — each has exactly one meaning, and each is an adaptive pair so
// the app reads correctly on both light and dark terminal backgrounds. Never
// reach for a bare lipgloss.Color here; add a role instead.
var (
	cText       = lipgloss.AdaptiveColor{Light: "235", Dark: "252"} // body prose
	cTextStrong = lipgloss.AdaptiveColor{Light: "232", Dark: "255"} // bold "answer" values
	cMuted      = lipgloss.AdaptiveColor{Light: "245", Dark: "243"} // labels, help, dates
	cFaint      = lipgloss.AdaptiveColor{Light: "250", Dark: "238"} // rules, empty tracks, placeholders

	cAccent  = lipgloss.AdaptiveColor{Light: "26", Dark: "39"}   // active tab, navigation, TIP
	cSuccess = lipgloss.AdaptiveColor{Light: "28", Dark: "42"}   // under budget, caught, healthy
	cWarn    = lipgloss.AdaptiveColor{Light: "130", Dark: "214"} // approaching a limit, WARN, seen
	cDanger  = lipgloss.AdaptiveColor{Light: "124", Dark: "203"} // over budget, errors, critical health — reserved for MoodWorried/daemon-down, not general "high spend"
	cBrand   = lipgloss.AdaptiveColor{Light: "97", Dark: "141"}  // title glyph, Lucent, Rare rarity
)

var (
	sBody  = lipgloss.NewStyle().Foreground(cText)
	sValue = lipgloss.NewStyle().Bold(true).Foreground(cTextStrong)
	sMuted = lipgloss.NewStyle().Foreground(cMuted)
	sFaint = lipgloss.NewStyle().Foreground(cFaint)

	sAccent  = lipgloss.NewStyle().Foreground(cAccent)
	sSuccess = lipgloss.NewStyle().Foreground(cSuccess)
	sWarn    = lipgloss.NewStyle().Foreground(cWarn)
	sDanger  = lipgloss.NewStyle().Bold(true).Foreground(cDanger)
	sBrand   = lipgloss.NewStyle().Foreground(cBrand)

	sSection = lipgloss.NewStyle().Bold(true).Foreground(cText)
	sRule    = lipgloss.NewStyle().Foreground(cFaint)
	sTab     = lipgloss.NewStyle().Foreground(cMuted)
	sTabOn   = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(cAccent)
	sTitle   = lipgloss.NewStyle().Bold(true).Foreground(cBrand)
	sKeyHint = lipgloss.NewStyle().Foreground(cText)

	spriteCardStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cFaint).Padding(0, 1)
)

// badge renders a fixed-width severity label so suggestion titles always
// align regardless of which severity precedes them.
func badge(s advisor.Severity) string {
	switch s {
	case advisor.Warn:
		return sWarn.Render("WARN  ")
	case advisor.Tip:
		return sAccent.Render("TIP   ")
	default:
		return sMuted.Render("INFO  ")
	}
}

// accentBlock prefixes every line of content with a colored left bar,
// grouping a card without drawing a full box around it.
func accentBlock(color lipgloss.TerminalColor, content string) string {
	bar := lipgloss.NewStyle().Foreground(color).Render("▎") + " "
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		lines[i] = bar + l
	}
	return strings.Join(lines, "\n")
}

// renderSuggestion renders one suggestion card: badge + title (+ savings,
// right-aligned to w), then the wrapped detail body, wrapped in an
// accent-colored left bar matching the severity.
func renderSuggestion(s advisor.Suggestion, w int) string {
	var color lipgloss.TerminalColor
	switch s.Severity {
	case advisor.Warn:
		color = cWarn
	case advisor.Tip:
		color = cAccent
	default:
		color = cFaint
	}

	innerW := w - 2 // room the accent bar + space consumes
	head := badge(s.Severity) + sValue.Render(s.Title)
	if s.SavingUSD >= 0.01 {
		head = spread(head, sSuccess.Render(fmt.Sprintf("save ~%s/day", money(s.SavingUSD))), innerW)
	}
	body := sBody.Render(lipgloss.NewStyle().Width(innerW).Render(s.Detail))
	return accentBlock(color, head+"\n"+body)
}

// meter renders a filled/track bar of the given width — the single bar
// primitive used for budget, XP, health, and stat visualizations.
func meter(ratio float64, width int, fill lipgloss.TerminalColor) string {
	if width < 1 {
		width = 1
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	if filled > width {
		filled = width
	}
	return lipgloss.NewStyle().Foreground(fill).Render(strings.Repeat("█", filled)) +
		sFaint.Render(strings.Repeat("░", width-filled))
}

// budgetBar renders today's spend against the configured daily budget.
func budgetBar(spent, budget float64, width int) string {
	barW := width - 24
	if barW < 10 {
		barW = 10
	}
	ratio := spent / budget
	col := cSuccess
	switch {
	case ratio >= 1.0:
		col = cDanger
	case ratio >= 0.75:
		col = cWarn
	}
	pct := int(ratio * 100)
	label := lipgloss.NewStyle().Foreground(col).Render(fmt.Sprintf("%3d%%", pct)) +
		"  " + sValue.Render(money(spent)) + sMuted.Render(" of "+money(budget))
	return meter(ratio, barW, col) + "  " + label
}

// kvRow renders a label/value row with the value right-aligned to column w.
func kvRow(label, value string, w int) string {
	l := sMuted.Render(label)
	pad := w - lipgloss.Width(l) - lipgloss.Width(value)
	if pad < 1 {
		pad = 1
	}
	return l + strings.Repeat(" ", pad) + value
}

// moneyRow renders a name/amount row (e.g. top models, top projects) with
// the amount right-aligned to column w and truncating a too-long name.
func moneyRow(name string, amount string, w int) string {
	nameW := w - lipgloss.Width(amount) - 1
	if nameW < 1 {
		nameW = 1
	}
	n := truncate(name, nameW)
	pad := w - lipgloss.Width(n) - lipgloss.Width(amount)
	if pad < 1 {
		pad = 1
	}
	return sBody.Render(n) + strings.Repeat(" ", pad) + amount
}

// money formats a USD amount with thousands separators, no internal padding
// — alignment is the caller's job (kvRow/moneyRow), never the formatter's.
func money(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	whole := int64(v)
	frac := int64((v-float64(whole))*100 + 0.5)
	if frac >= 100 {
		whole++
		frac -= 100
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s$%s.%02d", sign, groupThousands(whole), frac)
}

// commas formats a non-negative count with thousands separators (e.g. turn
// counts, token counts before the k/M compaction kicks in).
func commas(n int64) string {
	return groupThousands(n)
}

func groupThousands(n int64) string {
	s := fmt.Sprintf("%d", n)
	var grouped strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(c)
	}
	return grouped.String()
}

func cacheReuse(s store.Stats) string {
	total := s.TokensIn + s.CacheRead
	if total == 0 {
		return sFaint.Render("—")
	}
	pct := float64(s.CacheRead) / float64(total) * 100
	style := sBody
	switch {
	case pct >= 60:
		style = sSuccess
	case pct < 30:
		style = sWarn
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

// truncate is rune/wide-char safe (unlike a byte-slice cut), needed because
// project names, sprites, and journal text all carry multibyte runes.
func truncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return ansi.Truncate(s, w, "")
	}
	return ansi.Truncate(s, w, "…")
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

// rule draws a full-width horizontal divider.
func rule(w int) string {
	if w < 0 {
		w = 0
	}
	return sRule.Render(strings.Repeat("─", w))
}

// spread places left at the left edge and right at the right edge of width
// w, filling the middle with spaces. If both sides don't fit, left is
// truncated to make room.
func spread(left, right string, w int) string {
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	gap := w - lw - rw
	if gap < 1 {
		if w-rw-1 > 0 {
			left = truncate(left, w-rw-1)
			lw = lipgloss.Width(left)
		}
		gap = w - lw - rw
		if gap < 1 {
			gap = 1
		}
	}
	return left + strings.Repeat(" ", gap) + right
}

// stack joins non-empty parts with exactly one blank line between them —
// the single place blank-line spacing is decided, so callers never
// hand-manage "\n\n" and drift out of sync with each other.
func stack(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n\n")
}

// grid lays cells out left-to-right, top-to-bottom in a fixed-width column
// count, padding every cell to cellW so columns line up.
func grid(cells []string, cellW, cols int) string {
	if cols < 1 {
		cols = 1
	}
	cellStyle := lipgloss.NewStyle().Width(cellW)
	var rows []string
	for i := 0; i < len(cells); i += cols {
		end := i + cols
		if end > len(cells) {
			end = len(cells)
		}
		var row []string
		for _, c := range cells[i:end] {
			row = append(row, cellStyle.Render(c))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, row...))
	}
	return strings.Join(rows, "\n\n")
}

// emptyState renders a consistent cold-start card: a faint face, a
// first-person line in the pet's voice, and a concrete next action —
// replacing bare "(no data yet)" strings across every tab.
func emptyState(face, line string) string {
	return sFaint.Render(face) + "  " + sMuted.Render(line)
}
