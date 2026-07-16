package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/daemon"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/leaderboard"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/llm"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/save"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/voice"
)

// defaultCardWidth is safe inside a chat message: wide enough for the sprite
// and meters, narrow enough that most chat UIs don't wrap it.
const defaultCardWidth = 60

// forceAsciiOnce strips lipgloss down to a plain-text color profile exactly
// once. lipgloss styles are package-level globals (see styles.go), so this
// only needs to run once per process — a card renders identically on the
// first and the hundredth call. Forcing ASCII (rather than trusting
// auto-detection) is what makes card output byte-deterministic for golden
// tests and identical whether it's read from a TTY or piped into a host's
// chat renderer (R3/R4 in the host-integration plan).
var forceAsciiOnce sync.Once

func forceAscii() {
	forceAsciiOnce.Do(func() {
		lipgloss.SetColorProfile(termenv.Ascii)
	})
}

// CardOpts shapes one card render. Personality/Voice drive the voice footer
// (see cardVoiceFooter) — the token-budget contract of HOST_INTEGRATION.md
// §10: the footer is how the pet stays entertaining without spending the
// user's tokens on generation (unless they opted into api/live voice).
type CardOpts struct {
	Width       int    // 0 = defaultCardWidth
	Personality string // voice pack name; unknown values fall back inside voice.Line
	Voice       string // "canned" (embedded), "api" (aipet generates, budgeted), "live" (host improvises), "off"
	VoiceModel  string // api mode only; empty = llm.DefaultModel
}

// Card renders one-shot plain text for `aipet card`, the chat-facing
// projection of the TUI's views. It deliberately does NOT call Model.View —
// that renders host chrome (tab bar, footer, frame sizing) that has no place
// in a chat message. Card instead composes the same leaf helpers (meter,
// moodBubble, headerFaces/eggFaces, wrap, RenderDex, species art) that the
// TUI itself is built from, at a fixed width instead of a terminal size.
//
// view is whitelisted to exactly "", "pet", "dex", "records", "overview"
// (R2): any other value is a usage error, not a best-effort render, since
// view flows in from `$ARGUMENTS` in a Claude Code slash command.
func Card(view string, snap *daemon.Snapshot, journal []save.Entry, opts CardOpts) (string, error) {
	forceAscii()
	width := opts.Width
	if width <= 0 {
		width = defaultCardWidth
	}

	switch view {
	case "", "pet":
		out := cardPet(snap, journal, width)
		if footer := cardVoiceFooter(snap, opts); footer != "" {
			out += "\n---\n" + footer
		}
		return out, nil
	case "dex":
		return cardDex(snap, width), nil
	case "records":
		return cardRecords(snap, width), nil
	case "overview":
		return cardOverview(snap, width), nil
	default:
		return "", fmt.Errorf("unknown view %q (want: pet, dex, records, overview)", view)
	}
}

// cardVoiceFooter is the machine-readable trailer after the card's `---`
// separator that tells the host model what (if anything) to say as the pet.
// The slash command's prompt is static — written once at setup — so the
// personality/voice configuration has to travel through the card output
// itself; this footer is that channel.
//
//   - canned: the line is already written (picked deterministically from the
//     embedded phrasebook, internal/voice) — the host model just repeats it.
//     Zero generation, which is why it's the default.
//   - live: a one-line improvisation directive — the only aipet feature that
//     spends the user's tokens on the pet, and only because they opted in.
//   - off (or no pet to speak for): no footer at all.
func cardVoiceFooter(snap *daemon.Snapshot, opts CardOpts) string {
	if snap == nil || snap.PetError != "" {
		return ""
	}
	p := snap.Pet
	day := snap.UpdatedAt.Local().Format("2006-01-02")
	switch opts.Voice {
	case "live":
		return fmt.Sprintf("pet persona: %s — improvise ONE line (max 20 words) as the pet, matching the mood above. Nothing else.", opts.Personality)
	case "off":
		return ""
	case "api":
		// aipet generates the line itself, on the user's credentials, under
		// internal/llm's budget contract (once/day cache, hard cap, 3s
		// timeout). Any failure falls through to the canned line — api
		// voice degrades, it never breaks or blocks the card.
		hint := ""
		if len(p.Statuses) > 0 {
			hint = string(p.Statuses[0])
		}
		if line, ok := llm.Line(context.Background(), opts.VoiceModel, opts.Personality, p.IsEgg(), string(p.Mood), hint, day); ok {
			return "pet says: " + line
		}
		fallthrough
	default: // "canned" and anything unrecognized: the zero-token path
		line := voice.Line(opts.Personality, p.IsEgg(), p.Mood, day, p.SpeciesID)
		return "pet says: " + line
	}
}

// cardPet is the primary card: face + name/level/mood/stage, sprite (or egg
// art + hatch meter pre-hatch), xp/hp meters, the latest journal line, and a
// closing streak/dex/spend summary — the layout sketched in §4.1 of the
// host-integration plan.
func cardPet(snap *daemon.Snapshot, journal []save.Entry, w int) string {
	if snap == nil {
		return cardColdStart()
	}
	if snap.PetError != "" {
		return "Pet error: " + snap.PetError
	}

	p := snap.Pet
	var b strings.Builder
	if p.IsEgg() {
		writeEggCard(&b, p, w)
	} else {
		writePetCard(&b, p, w)
	}

	if line := latestJournalLine(journal); line != "" {
		b.WriteString("\n\n")
		b.WriteString(line)
	}

	b.WriteString("\n\n")
	b.WriteString(cardSummaryLine(snap))
	return b.String()
}

// cardColdStart mirrors the TUI/status empty-state copy: no snapshot yet
// (the collector has never found a Claude Code or Codex session).
func cardColdStart() string {
	return "Getting to know your machine — run a Claude Code or Codex session and I'll wake up."
}

func writeEggCard(b *strings.Builder, p sim.Pet, w int) {
	face := eggFaces[0]
	fmt.Fprintf(b, "%s  an egg, warming\n\n", face)
	b.WriteString(eggArt)
	b.WriteString("\n\n")
	n := p.EggSessionCount
	if n > sim.HatchSessionThreshold {
		n = sim.HatchSessionThreshold
	}
	fmt.Fprintf(b, "hatch %d/%d sessions\n", n, sim.HatchSessionThreshold)
	b.WriteString(meter(float64(p.EggSessionCount)/float64(sim.HatchSessionThreshold), w-10, cAccent))
}

func writePetCard(b *strings.Builder, p sim.Pet, w int) {
	sp, ok := species.ByID(p.SpeciesID)
	name := p.SpeciesID
	if ok {
		name = sp.Name
	}

	face := headerFaces[sim.MoodContent][0]
	if frames, ok := headerFaces[p.Mood]; ok {
		face = frames[0]
	}
	fmt.Fprintf(b, "%s  %s · lv %d · %s          stage %d/3\n\n", face, name, p.Level, p.Mood, p.Stage)

	if ok {
		b.WriteString(sp.Art)
		b.WriteString("\n\n")
	}
	if len(p.Statuses) > 0 {
		var statuses []string
		for _, s := range p.Statuses {
			statuses = append(statuses, string(s))
		}
		fmt.Fprintf(b, "status: %s\n\n", strings.Join(statuses, ", "))
	}

	fmt.Fprintf(b, "xp   %s  %d\n", meter(xpRatio(p), w-10, cAccent), p.XP)
	healthCol := cSuccess
	switch {
	case p.Health < 30:
		healthCol = cDanger
	case p.Health < 60:
		healthCol = cWarn
	}
	fmt.Fprintf(b, "hp   %s  %d/100", meter(float64(p.Health)/100, w-10, healthCol), p.Health)
}

// latestJournalLine surfaces the single most recent diet/life-log line — the
// "why" behind the pet's current mood — without the multi-line journal
// section the full TUI tab shows.
func latestJournalLine(journal []save.Entry) string {
	if len(journal) == 0 {
		return ""
	}
	return journal[len(journal)-1].Text
}

// cardSummaryLine is the closing "streak X days · dex N/30 · today $Y" line
// every pet card ends with, per §4.1.
func cardSummaryLine(snap *daemon.Snapshot) string {
	streak := snap.Pet.GritStreak
	caught := 0
	for _, sp := range species.All {
		if _, ok := snap.Dex.Caught[sp.ID]; ok {
			caught++
		}
	}
	return fmt.Sprintf("streak %d days · dex %d/%d · today %s", streak, caught, len(species.All), money(snap.Stats.TodayCost))
}

// cardDex projects the Dex tab: same RenderDex free function the TUI tab
// itself calls, just at the card's fixed width instead of terminal width.
func cardDex(snap *daemon.Snapshot, w int) string {
	if snap == nil {
		return RenderDex(save.DexState{}, w)
	}
	return RenderDex(snap.Dex, w)
}

// cardRecords projects the Records tab: rankings + personal bests, composed
// from the same rankingRows/recordRow helpers petview.go's records() uses.
func cardRecords(snap *daemon.Snapshot, w int) string {
	if snap == nil {
		return "Your records start with your first session — check back after you code."
	}
	board := snap.Board
	r := board.Records

	var b strings.Builder
	b.WriteString(sSection.Render("Top projects"))
	b.WriteString("\n")
	b.WriteString(rankingRows(board.TopProjects, w, func(e leaderboard.Entry) string { return money(e.Value) }))
	b.WriteString("\n\n")
	b.WriteString(sSection.Render("Best cache-reuse days"))
	b.WriteString("\n")
	b.WriteString(rankingRows(board.BestCacheDays, w, func(e leaderboard.Entry) string { return fmt.Sprintf("%.1f%%", e.Value) }))

	b.WriteString("\n\n")
	b.WriteString(sSection.Render("Personal records"))
	b.WriteString("\n")
	streakCtx := fmt.Sprintf("best %d", r.LongestStreak)
	if r.CurrentStreak > 0 && r.CurrentStreak == r.LongestStreak {
		streakCtx = "★ best"
	}
	b.WriteString(recordRow("Streak", fmt.Sprintf("%d days", r.CurrentStreak), streakCtx, w))
	if r.BiggestDayUSD.Name != "" {
		b.WriteString("\n")
		b.WriteString(recordRow("Biggest day", money(r.BiggestDayUSD.Value), r.BiggestDayUSD.Name, w))
	}
	if r.BusiestDay.Name != "" {
		b.WriteString("\n")
		b.WriteString(recordRow("Busiest day", fmt.Sprintf("%.0f turns", r.BusiestDay.Value), r.BusiestDay.Name, w))
	}
	if r.FirstSeen != "" {
		b.WriteString("\n")
		b.WriteString(recordRow("Keeper since", r.FirstSeen, fmt.Sprintf("%d active days", r.ActiveDays), w))
	}
	return b.String()
}

// cardOverview projects the Overview tab: budget bar + usage stats + top
// models/projects, composed from the same helpers petview.go's overview()
// uses.
func cardOverview(snap *daemon.Snapshot, w int) string {
	if snap == nil {
		return "I haven't seen any sessions yet. Run a Claude Code session and I'll start counting."
	}
	s := snap.Stats

	var b strings.Builder
	b.WriteString(sSection.Render("Usage"))
	b.WriteString("\n")
	b.WriteString(kvRow("Today", money(s.TodayCost), w))
	b.WriteString("\n")
	b.WriteString(kvRow("All-time", money(s.TotalCost), w))
	b.WriteString("\n")
	b.WriteString(kvRow("Turns", commas(int64(s.Turns)), w))
	b.WriteString("\n")
	b.WriteString(kvRow("Tokens in / out", human(s.TokensIn)+" / "+human(s.TokensOut), w))
	b.WriteString("\n")
	b.WriteString(kvRow("Cache reuse", cacheReuse(s), w))

	b.WriteString("\n\n")
	b.WriteString(sSection.Render("Top models"))
	b.WriteString("\n")
	b.WriteString(moneyRowsOrHint(store.TopN(s.ByModel, 3), w, "nothing yet; keep coding and this fills in."))

	b.WriteString("\n\n")
	b.WriteString(sSection.Render("Top projects"))
	b.WriteString("\n")
	b.WriteString(moneyRowsOrHint(store.TopN(s.ByProject, 3), w, "nothing yet; keep coding and this fills in."))
	return b.String()
}
