// Package tui renders the companion — a small terminal "pet" that shows live
// spend and money-saving suggestions. It reads the daemon's snapshot on a
// timer, so the UI stays responsive and works even if the daemon is briefly
// down. No network or model calls happen here.
package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/daemon"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/leaderboard"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/save"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

// headerFaces maps the pet's OWN mood (sim.Mood — grown from real diet and
// health, not from budget math) to a two-frame blinking face. Pre-hatch has
// its own egg-specific frames, keyed separately since there's no sim.Mood
// yet worth showing.
var headerFaces = map[sim.Mood][]string{
	sim.MoodCheerful: {"( ^_^ )", "( ^.^ )"},
	sim.MoodContent:  {"( o_o )", "( -_o )"},
	sim.MoodTired:    {"( -_- )", "( u_u )"},
	sim.MoodWorried:  {"( ;_; )", "( >_< )"},
	sim.MoodAsleep:   {"( -.- )zzz", "( u.u )zzz"},
}

var eggFaces = []string{"( • )", "( ° )"}

type tickMsg time.Time

// collectTickMsg fires on a timer to say "time to run another collection
// cycle." collectDoneMsg fires once that cycle (run off the UI thread via a
// tea.Cmd) actually finishes. Splitting these two lets the collect itself —
// parsing potentially-large JSONL files — run without blocking Bubble Tea's
// event loop or the 1s animation tick.
//
// This whole loop exists because the TUI process has no external `aipet
// daemon` running by default: without it, opening `aipet` and coding for
// hours would never grow the pet past its initial snapshot. Collection is
// just parsing session logs already on disk (0 tokens, 0 network), so
// running it from inside the TUI process costs nothing extra.
type collectTickMsg time.Time
type collectDoneMsg struct{}

// Model is the Bubble Tea model for the pet.
type Model struct {
	cfg            config.Config
	snap           *daemon.Snapshot
	snapMod        time.Time // snapshot file mtime at last parse
	frame          int
	tab            int // 0 = pet, 1 = overview, 2 = suggestions, 3 = records
	width          int
	height         int
	daemonUp       bool
	err            string
	journalEntries []save.Entry
}

const tabCount = 5

// New builds the TUI model, loading any existing snapshot immediately.
func New(cfg config.Config) Model {
	m := Model{cfg: cfg}
	m.refresh(true)
	return m
}

func (m Model) Init() tea.Cmd {
	// A collection cycle already ran once in runTUI (cmd/aipet/main.go)
	// before the program starts, so Init's job is only to arm the repeating
	// collect timer, not to collect again immediately.
	return tea.Batch(tick(), m.scheduleCollectTick())
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// collectInterval is how often the TUI's own background loop re-collects,
// mirroring cfg.CollectIntervalMin (the same cadence `aipet daemon` would
// use) so the two paths behave consistently.
func (m Model) collectInterval() time.Duration {
	interval := time.Duration(m.cfg.CollectIntervalMin) * time.Minute
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	return interval
}

// scheduleCollectTick waits one collectInterval, then fires collectTickMsg
// (time to collect again).
func (m Model) scheduleCollectTick() tea.Cmd {
	return tea.Tick(m.collectInterval(), func(t time.Time) tea.Msg { return collectTickMsg(t) })
}

// runCollect performs one collection cycle (parsing session logs already on
// disk — no network, no tokens) as a Bubble Tea Cmd, off the UI thread, then
// reports completion via collectDoneMsg. This is what makes `aipet` (bare)
// grow the pet on its own, without requiring the user to also run `aipet
// daemon` in a second terminal.
func (m Model) runCollect() tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		_, _ = daemon.Run(cfg)
		return collectDoneMsg{}
	}
}

// refresh re-reads daemon state. The snapshot JSON is only re-parsed when the
// file's mtime moved — the TUI ticks every second for the face animation, and
// parsing the full snapshot 60x/minute for a file that changes every couple of
// minutes is wasted work. A cheap stat decides; force skips that check.
func (m *Model) refresh(force bool) {
	if p, err := daemon.SnapshotPath(); err == nil {
		fi, statErr := os.Stat(p)
		if force || statErr != nil || !fi.ModTime().Equal(m.snapMod) {
			if s, err := daemon.ReadSnapshot(); err == nil {
				m.snap = s
				if statErr == nil {
					m.snapMod = fi.ModTime()
				}
			}
		}
	}
	if j, err := save.ReadJournal(); err == nil {
		m.journalEntries = j
	}
	_, m.daemonUp = daemon.Running()
}

// overBudget reports whether today's spend has passed the soft daily
// budget — shown as a small inline warning in the header, but no longer
// drives the pet's face: the pet's own sim.Mood (grown from its diet and
// health) does that now. Budget pressure is real, useful information, it
// just isn't the pet's emotional state.
func (m Model) overBudget() bool {
	if m.snap == nil || m.cfg.DailyBudgetUSD <= 0 {
		return false
	}
	return m.snap.Stats.TodayCost >= m.cfg.DailyBudgetUSD
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		m.frame++
		m.refresh(false) // animation tick: stat-check only, parse on change
		return m, tick()
	case collectTickMsg:
		// Timer fired: run the actual (potentially slower) collection off
		// the UI thread rather than blocking Update.
		return m, m.runCollect()
	case collectDoneMsg:
		// The background collect cycle just finished; re-read whatever it
		// published, then arm the next cycle.
		m.refresh(true)
		return m, m.scheduleCollectTick()
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "tab", "right", "l":
			m.tab = (m.tab + 1) % tabCount
		case "left", "h":
			m.tab = (m.tab + tabCount - 1) % tabCount
		case "1":
			m.tab = 0
		case "2":
			m.tab = 1
		case "3":
			m.tab = 2
		case "4":
			m.tab = 3
		case "5":
			m.tab = 4
		case "r":
			// A real collect-then-refresh, not just a snapshot re-stat —
			// "refresh" should actually go look for new activity.
			m.refresh(true)
			return m, m.runCollect()
		}
	}
	return m, nil
}

// View composes the whole frame: pet header, tab bar, and the active tab body.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n")
	b.WriteString(m.tabBar())
	b.WriteString("\n\n")
	switch m.tab {
	case 0:
		b.WriteString(m.pet())
	case 1:
		b.WriteString(m.overview())
	case 2:
		b.WriteString(m.suggestions())
	case 3:
		b.WriteString(m.records())
	case 4:
		b.WriteString(m.dex())
	}
	b.WriteString("\n")
	b.WriteString(m.footer())
	return b.String()
}

func (m Model) header() string {
	name := titleStyle.Render(" Codelings ")

	face, bubble, faceSt := m.faceAndBubble()
	faceCol := faceSt.Render(face)

	status := dimStyle.Render("daemon: off (r or 'aipet daemon' grows it)")
	if m.daemonUp {
		status = okStyle.Render("daemon: live")
	}
	if m.overBudget() {
		status = warnStyle.Render("budget: over") + "  " + status
	}

	line := lipgloss.JoinHorizontal(lipgloss.Center, faceCol, "  ", name, "  ", bubble)
	return lipgloss.JoinHorizontal(lipgloss.Center, line, "   ", status)
}

// faceAndBubble picks the header's face + speech bubble + style from the
// PET'S OWN state: still an egg, hatched with a real sim.Mood, or no data
// collected yet at all. Budget/spend concerns live in the Overview and
// Suggestions tabs, not the pet's face.
func (m Model) faceAndBubble() (face, bubble string, st lipgloss.Style) {
	if m.snap == nil {
		return eggFaces[m.frame%2], tipStyle.Render("Getting to know your machine..."), moodStyle[sim.MoodContent]
	}
	if m.snap.PetError != "" {
		return eggFaces[m.frame%2], warnStyle.Render("Something's off — check the Pet tab."), moodStyle[sim.MoodWorried]
	}
	p := m.snap.Pet
	if p.IsEgg() {
		return eggFaces[m.frame%2], tipStyle.Render("Warming up..."), moodStyle[sim.MoodContent]
	}
	frames, ok := headerFaces[p.Mood]
	if !ok {
		frames = headerFaces[sim.MoodContent]
	}
	return frames[m.frame%2], moodStyle[p.Mood].Render(moodBubble(p.Mood)), moodStyle[p.Mood]
}

// moodBubble is the pet's own one-line status, in its voice — matching the
// warm, explainable tone of the journal (docs/design/lore.md).
func moodBubble(mo sim.Mood) string {
	switch mo {
	case sim.MoodCheerful:
		return "Feeling great — keep it up!"
	case sim.MoodTired:
		return "A little worn out today."
	case sim.MoodWorried:
		return "Something's not sitting right..."
	case sim.MoodAsleep:
		return "Sleeping — no rush, I'll be here."
	default:
		return "Just here, watching your work."
	}
}

func (m Model) tabBar() string {
	labels := []string{"Pet", "Overview", "Suggestions", "Records", "Dex"}
	var tabs []string
	for i, l := range labels {
		if i == m.tab {
			tabs = append(tabs, activeTabStyle.Render(fmt.Sprintf(" %d %s ", i+1, l)))
		} else {
			tabs = append(tabs, tabStyle.Render(fmt.Sprintf(" %d %s ", i+1, l)))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Bottom, tabs...)
}

func (m Model) overview() string {
	if m.snap == nil {
		return dimStyle.Render("No data yet. Start the daemon with `aipet daemon` or run `aipet status`.")
	}
	s := m.snap.Stats
	var b strings.Builder

	// Budget bar for today.
	if m.cfg.DailyBudgetUSD > 0 {
		b.WriteString(budgetBar(s.TodayCost, m.cfg.DailyBudgetUSD, m.width))
		b.WriteString("\n\n")
	}

	b.WriteString(kv("Today", fmt.Sprintf("$%.2f", s.TodayCost)))
	b.WriteString(kv("All-time", fmt.Sprintf("$%.2f", s.TotalCost)))
	b.WriteString(kv("Turns", fmt.Sprintf("%d", s.Turns)))
	b.WriteString(kv("Tokens in/out", fmt.Sprintf("%s / %s", human(s.TokensIn), human(s.TokensOut))))
	b.WriteString(kv("Cache reuse", cacheReuse(s)))
	b.WriteString("\n")

	b.WriteString(sectionStyle.Render("Top models"))
	b.WriteString("\n")
	for _, kvp := range store.TopN(s.ByModel, 3) {
		b.WriteString(fmt.Sprintf("  %-28s $%6.2f\n", trunc(kvp.Key, 28), kvp.Value))
	}
	b.WriteString("\n")
	b.WriteString(sectionStyle.Render("Top projects"))
	b.WriteString("\n")
	for _, kvp := range store.TopN(s.ByProject, 3) {
		b.WriteString(fmt.Sprintf("  %-28s $%6.2f\n", trunc(kvp.Key, 28), kvp.Value))
	}
	return b.String()
}

func (m Model) suggestions() string {
	if m.snap == nil || len(m.snap.Suggestions) == 0 {
		return okStyle.Render("  No suggestions right now — you're running lean. ")
	}
	var b strings.Builder
	shown := 0
	for _, s := range m.snap.Suggestions {
		b.WriteString(renderSuggestion(s))
		shown++
		if shown >= 6 {
			break
		}
	}
	if shown == 0 {
		return okStyle.Render("  No efficiency issues detected. ")
	}
	return b.String()
}

// records renders the local leaderboard: rankings and personal bests. Every
// number is computed on-device from the event log.
func (m Model) records() string {
	if m.snap == nil {
		return dimStyle.Render("No data yet. Start the daemon with `aipet daemon` or run `aipet status`.")
	}
	board := m.snap.Board
	var b strings.Builder

	b.WriteString(sectionStyle.Render("Top projects"))
	b.WriteString("\n")
	writeRanking(&b, board.TopProjects, func(e leaderboard.Entry) string {
		return fmt.Sprintf("$%6.2f", e.Value)
	})
	b.WriteString("\n")
	b.WriteString(sectionStyle.Render("Best cache-reuse days"))
	b.WriteString("\n")
	writeRanking(&b, board.BestCacheDays, func(e leaderboard.Entry) string {
		return fmt.Sprintf("%5.1f%%", e.Value)
	})
	b.WriteString("\n")

	r := board.Records
	b.WriteString(sectionStyle.Render("Personal records"))
	b.WriteString("\n")
	streak := fmt.Sprintf("%d day(s)", r.CurrentStreak)
	if r.CurrentStreak > 0 && r.CurrentStreak == r.LongestStreak {
		streak = okStyle.Render(streak + "  ★ personal best")
	} else {
		streak += dimStyle.Render(fmt.Sprintf("  (best %d)", r.LongestStreak))
	}
	b.WriteString(kv("Streak", streak))
	if r.BiggestDayUSD.Name != "" {
		b.WriteString(kv("Biggest day", fmt.Sprintf("$%.2f on %s", r.BiggestDayUSD.Value, r.BiggestDayUSD.Name)))
	}
	if r.BusiestDay.Name != "" {
		b.WriteString(kv("Busiest day", fmt.Sprintf("%.0f turns on %s", r.BusiestDay.Value, r.BusiestDay.Name)))
	}
	if r.FirstSeen != "" {
		b.WriteString(kv("Keeper since", fmt.Sprintf("%s · %d active day(s)", r.FirstSeen, r.ActiveDays)))
	}
	return b.String()
}

func writeRanking(b *strings.Builder, entries []leaderboard.Entry, val func(leaderboard.Entry) string) {
	if len(entries) == 0 {
		b.WriteString(dimStyle.Render("  (no qualifying data yet)") + "\n")
		return
	}
	for i, e := range entries {
		b.WriteString(fmt.Sprintf("  %d. %-28s %s\n", i+1, trunc(e.Name, 28), val(e)))
	}
}

func (m Model) footer() string {
	help := "tab/←→ switch · r refresh · q quit"
	status := ""
	if m.snap != nil {
		if n := len(m.snap.CollectErrors); n > 0 {
			status = warnStyle.Render(fmt.Sprintf("%d collect error(s)", n))
		}
	}
	return dimStyle.Render(help) + "   " + status
}
