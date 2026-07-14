package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

var moodStyle = map[sim.Mood]lipgloss.Style{
	sim.MoodCheerful: lipgloss.NewStyle().Foreground(cSuccess),
	sim.MoodContent:  sMuted,
	sim.MoodTired:    sMuted,
	sim.MoodWorried:  lipgloss.NewStyle().Bold(true).Foreground(cWarn),
	sim.MoodAsleep:   lipgloss.NewStyle().Italic(true).Foreground(cFaint),
}

const eggArt = "   .-\"\"-.\n  /      \\\n |  ....  |\n  \\      /\n   '-..-'"

// pet renders the Pet tab: the egg-or-hatchling screen plus a short recent
// journal. This is the game itself — Overview/Suggestions/Records exist to
// explain why the pet feels the way it does.
func (m Model) pet(w int) string {
	if m.snap == nil {
		return sMuted.Render("No data yet. Start the daemon with `aipet daemon` or run `aipet status`.")
	}
	if m.snap.PetError != "" {
		return sDanger.Render("Pet error: " + m.snap.PetError)
	}
	p := m.snap.Pet
	if p.IsEgg() {
		return eggView(p, w)
	}

	body := hatchlingView(p, w)
	streak := sMuted.Render(fmt.Sprintf("streak %d days · %d active days total", p.GritStreak, p.ActiveDayCount))
	header := spread(sSection.Render("Journal"), streak, w)
	return stack(body, stack(header, m.journal()))
}

// spriteCard wraps a sprite in the app's one deliberate box: a quiet
// rounded border. Every other grouping in the UI uses spacing or an accent
// bar instead.
func spriteCard(art string) string {
	return spriteCardStyle.Render(sAccent.Render(art))
}

func eggView(p sim.Pet, w int) string {
	card := spriteCard(eggArt)

	remaining := sim.HatchWindowDays - p.ActiveDayCount
	if remaining < 0 {
		remaining = 0
	}
	hatchInfo := sMuted.Render(fmt.Sprintf(
		"Hatches after %d active day(s) — %d so far.", sim.HatchWindowDays, p.ActiveDayCount)) +
		"\n" + hatchProgress(p.ActiveDayCount, sim.HatchWindowDays)

	info := stack(
		sValue.Render("An egg, warming."),
		hatchInfo,
		sMuted.Render("It'll pick a line from how you've been working."),
	)

	if w >= 90 {
		return lipgloss.JoinHorizontal(lipgloss.Center, card, "   ", info)
	}
	return stack(card, info)
}

func hatchProgress(done, total int) string {
	if total <= 0 {
		total = 1
	}
	if done > total {
		done = total
	}
	if done < 0 {
		done = 0
	}
	return sSuccess.Render(strings.Repeat("●", done)) + sFaint.Render(strings.Repeat("○", total-done))
}

func hatchlingView(p sim.Pet, w int) string {
	sp, ok := species.ByID(p.SpeciesID)
	if !ok {
		return sDanger.Render("Unknown species: " + p.SpeciesID)
	}

	card := spriteCard(sp.Art)
	cardW := lipgloss.Width(card)

	twoCol := w >= 90
	rightW := w
	if twoCol {
		rightW = w - cardW - 3
	}
	if rightW < 24 {
		rightW = 24
	}
	right := hatchlingVitals(p, sp, rightW)

	var top string
	if twoCol {
		top = lipgloss.JoinHorizontal(lipgloss.Top, card, "   ", right)
	} else {
		top = stack(card, right)
	}

	parts := []string{top}
	if len(p.Statuses) > 0 {
		var statuses []string
		for _, s := range p.Statuses {
			statuses = append(statuses, string(s))
		}
		parts = append(parts, accentBlock(cWarn, sWarn.Render("status: "+strings.Join(statuses, ", "))))
	}
	parts = append(parts,
		stack(sSection.Render("Stats"), statGrid(p.Stats, w)),
		sBody.Render(lipgloss.NewStyle().Width(w).Render(sp.DexEntry)),
	)
	return stack(parts...)
}

func hatchlingVitals(p sim.Pet, sp species.Species, w int) string {
	name := sValue.Render(sp.Name)
	if p.Lucent {
		name = sBrand.Render("✦ "+sp.Name) + sMuted.Render(" · Lucent")
	}
	stageLine := sMuted.Render(fmt.Sprintf("stage %d/3 · %s · %s", p.Stage, strings.ToLower(string(sp.Type)), sp.Habitat))
	moodLine := moodStyle[p.Mood].Render(string(p.Mood)) + sMuted.Render(" mood")

	healthColor := cSuccess
	switch {
	case p.Health < 30:
		healthColor = cDanger
	case p.Health < 60:
		healthColor = cWarn
	}

	lines := []string{
		spread(name, stageLine, w),
		moodLine,
		"",
		sMuted.Render("Level") + "  " + sValue.Render(fmt.Sprintf("%d", p.Level)),
		meter(xpRatio(p), 20, cAccent) + sMuted.Render(fmt.Sprintf("  %d xp", p.XP)),
		sMuted.Render("Health") + "  " + sValue.Render(fmt.Sprintf("%d/100", p.Health)),
		meter(float64(p.Health)/100, 20, healthColor),
	}
	return strings.Join(lines, "\n")
}

// statGrid renders the five stats as labeled meters, two per row on normal+
// terminals and one per row when narrow.
func statGrid(s species.Stats, w int) string {
	line := func(label string, val int) string {
		return sMuted.Render(fmt.Sprintf("%-6s", label)) + meter(float64(clampStat(val))/30, 16, cAccent)
	}
	cells := []string{
		line("vigor", s.Vigor),
		line("focus", s.Focus),
		line("wit", s.Wit),
		line("grit", s.Grit),
		line("spark", s.Spark),
	}
	cols, cellW := 2, w/2-1
	if w < 60 {
		cols, cellW = 1, w
	}
	return grid(cells, cellW, cols)
}

// clampStat scales a stat (roughly 0-140 across the roster) into a 0-30
// meter range purely for display.
func clampStat(v int) int {
	n := v / 5
	if n < 0 {
		n = 0
	}
	if n > 30 {
		n = 30
	}
	return n
}

// xpRatio mirrors sim's quadratic leveling curve (level*level*10) just
// closely enough to draw a progress bar; the TUI never decides leveling
// itself, only visualizes what the sim already computed.
func xpRatio(p sim.Pet) float64 {
	span := xpSpanForLevel(p.Level)
	if span <= 0 {
		return 0
	}
	r := float64(p.XP-xpFloorForLevel(p.Level)) / float64(span)
	if r < 0 {
		r = 0
	}
	if r > 1 {
		r = 1
	}
	return r
}

func xpFloorForLevel(level int) int {
	if level <= 1 {
		return 0
	}
	return level * level * 10
}

func xpSpanForLevel(level int) int {
	return xpFloorForLevel(level+1) - xpFloorForLevel(level)
}

// journal renders the pet's recent life log — the "why" behind its mood.
// Returns "" when there's nothing to show yet, so callers can skip the
// section entirely on a brand new save.
func (m Model) journal() string {
	if len(m.journalEntries) == 0 {
		return ""
	}
	const maxShown = 5
	start := 0
	if len(m.journalEntries) > maxShown {
		start = len(m.journalEntries) - maxShown
	}
	var lines []string
	for i := len(m.journalEntries) - 1; i >= start; i-- {
		e := m.journalEntries[i]
		date := e.Day
		if len(date) == 10 {
			date = date[5:] // YYYY-MM-DD -> MM-DD
		}
		text := sBody.Render(e.Text)
		if strings.Contains(strings.ToLower(e.Text), "junk food") {
			text = sWarn.Render(e.Text)
		}
		lines = append(lines, sFaint.Render(date)+"  "+text)
	}
	return strings.Join(lines, "\n")
}
