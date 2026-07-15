package tui

import (
	"fmt"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/daemon"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

// StatusLine renders the one-line, plain-text summary Claude Code's
// statusLine hook shows on every render (§4.2 of the host-integration
// plan). No ANSI (R4): the statusline runs as a pipe and lipgloss would
// strip color on a non-TTY stdout anyway, so v1 is deliberately plain rather
// than silently degrading.
//
// budget is the configured daily budget in USD; 0 means budget nudges are
// disabled, matching config.Config.DailyBudgetUSD's own convention.
func StatusLine(snap *daemon.Snapshot, budget float64) string {
	if snap == nil {
		return "(no pet yet — run /aipet)"
	}
	p := snap.Pet
	if p.IsEgg() {
		n := p.EggSessionCount
		if n > sim.HatchSessionThreshold {
			n = sim.HatchSessionThreshold
		}
		return fmt.Sprintf("%s egg %d/%d · %s today", eggFaces[0], n, sim.HatchSessionThreshold, money(snap.Stats.TodayCost))
	}

	name := p.SpeciesID
	// An unrecognized SpeciesID (future save format, corrupt data) still
	// yields a valid (if slightly less pretty) statusline rather than
	// falling back to the no-pet copy — a statusline must never look broken
	// on data it doesn't fully understand.
	if sp, ok := species.ByID(p.SpeciesID); ok {
		name = sp.Name
	}
	face := eggFaces[0]
	if frames, ok := headerFaces[p.Mood]; ok {
		face = frames[0]
	}

	line := fmt.Sprintf("%s %s lv%d · %s · %s today", face, name, p.Level, p.Mood, money(snap.Stats.TodayCost))
	if budget > 0 && snap.Stats.TodayCost >= budget {
		line += " · budget over"
	}
	return line
}
