package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

var moodStyle = map[sim.Mood]lipgloss.Style{
	sim.MoodCheerful: lipgloss.NewStyle().Foreground(cSuccess).Bold(true),
	sim.MoodContent:  lipgloss.NewStyle().Foreground(cAccent),
	sim.MoodTired:    sWarn,
	sim.MoodWorried:  lipgloss.NewStyle().Bold(true).Foreground(cDanger),
	sim.MoodAsleep:   lipgloss.NewStyle().Italic(true).Foreground(cMuted),
}

// moodBorder maps a hatched pet's mood to its sprite card's border color —
// the single strongest "the pet has a state" signal in the Pet tab, since
// it's visible even before reading any text.
var moodBorder = map[sim.Mood]lipgloss.AdaptiveColor{
	sim.MoodCheerful: cSuccess,
	sim.MoodContent:  cAccent,
	sim.MoodTired:    cWarn,
	sim.MoodWorried:  cDanger,
	sim.MoodAsleep:   cMuted,
}

const eggArt = "   .-\"\"-.\n  /      \\\n |  ....  |\n  \\      /\n   '-..-'"

// pet renders the Pet tab: the egg-or-hatchling screen plus a short recent
// journal. This is the game itself — Overview/Suggestions/Records exist to
// explain why the pet feels the way it does.
func (m Model) pet(w int) string {
	if m.snap == nil {
		return emptyState(eggFaces[0], "Getting to know your machine — run a Claude Code session and I'll wake up.")
	}
	if m.snap.PetError != "" {
		return sDanger.Render("Pet error: " + m.snap.PetError)
	}
	p := m.snap.Pet
	if p.IsEgg() {
		return eggView(p, m.frame, w)
	}

	body := hatchlingView(p, m.frame, w)
	streak := sMuted.Render(fmt.Sprintf("streak %d days · %d active days total", p.GritStreak, p.ActiveDayCount))
	header := spread(sSection.Render("Journal"), streak, w)
	return stack(body, stack(header, m.journal()))
}

// spriteCard wraps a sprite in the app's one deliberate box: a quiet
// rounded border, tinted to the pet's current mood post-hatch (or accent
// while still an egg). Every other grouping in the UI uses spacing or an
// accent bar instead.
func spriteCard(art string, border lipgloss.AdaptiveColor) string {
	return spriteCardStyle.BorderForeground(border).Render(art)
}

// eggView is the very first thing a brand-new user sees: face + hatch
// meter + one line of copy, centered in the sprite card so the egg reads
// as the app's hero content rather than a paragraph of text.
func eggView(p sim.Pet, frame, w int) string {
	face := sAccent.Render(eggFaces[frame%2])
	sprite := lipgloss.JoinVertical(lipgloss.Center, "", eggArt, "", face, sAccent.Render("Warming up..."))
	card := spriteCard(sprite, cAccent)

	infoW := w
	if w >= 90 {
		infoW = w - lipgloss.Width(card) - 3
	}
	if infoW < 24 {
		infoW = 24
	}

	label := spread(sMuted.Render("Hatching"), sMuted.Render(fmt.Sprintf("%d/%d sessions",
		min(p.EggSessionCount, sim.HatchSessionThreshold), sim.HatchSessionThreshold)), infoW)
	hatchMeter := meter(float64(p.EggSessionCount)/float64(sim.HatchSessionThreshold), infoW, cAccent)

	lines := []string{
		sValue.Render("An egg, warming."),
		label,
		hatchMeter,
		sMuted.Render(wrap("Hatches from real coding sessions — no clicking required.", infoW)),
	}
	if p.ActiveDayCount > 0 {
		lines = append(lines, sFaint.Render(wrap(fmt.Sprintf("(%d active day(s) so far — a real week of casual use always hatches too)", p.ActiveDayCount), infoW)))
	}
	info := strings.Join(lines, "\n")

	if w >= 90 {
		return lipgloss.JoinHorizontal(lipgloss.Center, card, "   ", info)
	}
	return stack(card, info)
}

func hatchlingView(p sim.Pet, frame, w int) string {
	sp, ok := species.ByID(p.SpeciesID)
	if !ok {
		return sDanger.Render("Unknown species: " + p.SpeciesID)
	}

	border := moodBorder[p.Mood]
	frames, hasFace := headerFaces[p.Mood]
	sprite := sp.Art
	if hasFace {
		face := frames[frame%2]
		sprite = lipgloss.JoinVertical(lipgloss.Center, sp.Art, "", sAccent.Render(face), moodStyle[p.Mood].Render(moodBubble(p.Mood)))
	}
	if p.Mood == sim.MoodAsleep {
		sprite = lipgloss.NewStyle().Faint(true).Render(sprite)
	}
	card := spriteCard(sprite, border)
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

	healthColor := cSuccess
	switch {
	case p.Health < 30:
		healthColor = cDanger
	case p.Health < 60:
		healthColor = cWarn
	}

	lines := []string{
		spread(name, stageLine, w),
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
