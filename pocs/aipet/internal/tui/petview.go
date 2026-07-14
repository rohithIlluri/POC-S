package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

var (
	petNameStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231"))
	petLucentStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("219"))
	spriteStyle    = lipgloss.NewStyle().Foreground(cBlue)
	moodStyle      = map[sim.Mood]lipgloss.Style{
		sim.MoodCheerful: lipgloss.NewStyle().Foreground(cGreen).Bold(true),
		sim.MoodContent:  lipgloss.NewStyle().Foreground(cBlue),
		sim.MoodTired:    lipgloss.NewStyle().Foreground(cYellow),
		sim.MoodWorried:  lipgloss.NewStyle().Foreground(cRed).Bold(true),
		sim.MoodAsleep:   lipgloss.NewStyle().Foreground(cGray).Italic(true),
	}
)

// pet renders the Pet tab: the egg-or-hatchling screen plus a short recent
// journal. This is the game itself — Overview/Suggestions/Records exist to
// explain why the pet feels the way it does.
func (m Model) pet() string {
	if m.snap == nil {
		return dimStyle.Render("No data yet. Start the daemon with `aipet daemon` or run `aipet status`.")
	}
	if m.snap.PetError != "" {
		return warnStyle.Render("Pet error: " + m.snap.PetError)
	}
	p := m.snap.Pet
	var body string
	if p.IsEgg() {
		body = eggView(p)
	} else {
		body = hatchlingView(p, m.width)
	}
	if j := m.journal(); j != "" {
		body += "\n" + sectionStyle.Render("Journal") + "\n" + j
	}
	return body
}

func eggView(p sim.Pet) string {
	var b strings.Builder
	egg := spriteStyle.Render("   .-\"\"-.\n  /      \\\n |  ....  |\n  \\      /\n   '-..-'")
	b.WriteString(egg)
	b.WriteString("\n\n")
	b.WriteString(petNameStyle.Render("An egg, warming."))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf(
		"Egg warming: %d/%d qualifying sessions. Keep coding with Claude Code or Codex —",
		min(p.EggSessionCount, sim.HatchSessionThreshold), sim.HatchSessionThreshold)))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("it hatches from your real activity, no clicking required."))
	b.WriteString("\n")
	b.WriteString(hatchProgressBar(p.EggSessionCount, sim.HatchSessionThreshold))
	if p.ActiveDayCount > 0 {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(fmt.Sprintf(
			"(%d active day(s) so far — a real week of casual use always hatches too)", p.ActiveDayCount)))
	}
	return b.String()
}

func hatchlingView(p sim.Pet, width int) string {
	sp, ok := species.ByID(p.SpeciesID)
	if !ok {
		return warnStyle.Render("Unknown species: " + p.SpeciesID)
	}
	var b strings.Builder

	b.WriteString(spriteStyle.Render(sp.Art))
	b.WriteString("\n\n")

	name := petNameStyle.Render(sp.Name)
	if p.Lucent {
		name = petLucentStyle.Render("✦ " + sp.Name + " (Lucent)")
	}
	stageLabel := dimStyle.Render(fmt.Sprintf("  stage %d/3 · %s · %s", p.Stage, strings.ToUpper(string(sp.Type)), sp.Habitat))
	b.WriteString(name + stageLabel)
	b.WriteString("\n")
	moodSt := moodStyle[p.Mood]
	b.WriteString(moodSt.Render(strings.ToUpper(string(p.Mood))))
	b.WriteString("\n\n")

	b.WriteString(kv("Level", fmt.Sprintf("%d", p.Level)))
	b.WriteString(xpBar(p))
	b.WriteString(kv("Health", ""))
	b.WriteString(healthBar(p.Health))
	b.WriteString("\n")

	if len(p.Statuses) > 0 {
		var statuses []string
		for _, s := range p.Statuses {
			statuses = append(statuses, string(s))
		}
		b.WriteString(warnStyle.Render("status: " + strings.Join(statuses, ", ")))
		b.WriteString("\n\n")
	}

	b.WriteString(sectionStyle.Render("Stats"))
	b.WriteString("\n")
	b.WriteString(statLine("VIGOR", p.Stats.Vigor))
	b.WriteString(statLine("FOCUS", p.Stats.Focus))
	b.WriteString(statLine("WIT", p.Stats.Wit))
	b.WriteString(statLine("GRIT", p.Stats.Grit))
	b.WriteString(statLine("SPARK", p.Stats.Spark))
	b.WriteString("\n")

	entryWidth := width - 2
	if entryWidth < 40 {
		entryWidth = 72 // sane default when the window size hasn't been reported yet (e.g. in tests)
	}
	b.WriteString(dimStyle.Render(wrap(sp.DexEntry, entryWidth)))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("streak %d day(s) · %d active day(s) total", p.GritStreak, p.ActiveDayCount)))

	return b.String()
}

func hatchProgressBar(done, total int) string {
	if total <= 0 {
		total = 1
	}
	if done > total {
		done = total
	}
	filled := strings.Repeat("●", done)
	empty := strings.Repeat("○", total-done)
	return "  " + okStyle.Render(filled) + dimStyle.Render(empty)
}

func xpBar(p sim.Pet) string {
	// A lightweight visual: filled dots for progress within the current
	// level band, not a precise fraction — precision lives in the Level
	// number itself, this is just a "getting there" cue.
	const width = 20
	span := xpSpanForLevel(p.Level)
	into := p.XP - xpFloorForLevel(p.Level)
	if span <= 0 {
		span = 1
	}
	filled := into * width / span
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	bar := okStyle.Render(strings.Repeat("█", filled)) + dimStyle.Render(strings.Repeat("░", width-filled))
	return "  " + bar + dimStyle.Render(fmt.Sprintf(" %d XP", p.XP)) + "\n"
}

// xpFloorForLevel/xpSpanForLevel mirror sim's quadratic curve (level*level*10)
// just closely enough to draw a progress bar; the TUI never decides
// leveling itself, only visualizes what the sim already computed.
func xpFloorForLevel(level int) int {
	if level <= 1 {
		return 0
	}
	return level * level * 10
}

func xpSpanForLevel(level int) int {
	return xpFloorForLevel(level+1) - xpFloorForLevel(level)
}

func healthBar(health int) string {
	const width = 20
	filled := health * width / 100
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	col := okStyle
	switch {
	case health < 30:
		col = warnStyle
	case health < 60:
		col = tipStyle
	}
	bar := col.Render(strings.Repeat("█", filled)) + dimStyle.Render(strings.Repeat("░", width-filled))
	return "  " + bar + dimStyle.Render(fmt.Sprintf(" %d/100", health)) + "\n"
}

func statLine(name string, val int) string {
	return fmt.Sprintf("  %-6s %s\n", name, strings.Repeat("▪", clampStat(val)))
}

func clampStat(v int) int {
	// Purely cosmetic scaling for the terminal width: stats run roughly
	// 0-140 across the roster, so /5 keeps the bar readable.
	n := v / 5
	if n < 0 {
		n = 0
	}
	if n > 30 {
		n = 30
	}
	return n
}

// journal renders the pet's recent life log — the "why" behind its mood.
// Returns "" when there's nothing to show yet, so callers can skip the
// section header entirely on a brand new save.
func (m Model) journal() string {
	if len(m.journalEntries) == 0 {
		return ""
	}
	var b strings.Builder
	const maxShown = 5
	start := 0
	if len(m.journalEntries) > maxShown {
		start = len(m.journalEntries) - maxShown
	}
	for i := len(m.journalEntries) - 1; i >= start; i-- {
		e := m.journalEntries[i]
		b.WriteString(dimStyle.Render(e.Day) + "  " + e.Text + "\n")
	}
	return b.String()
}
