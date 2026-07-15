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
// yet worth showing. The header suppresses this face on the Pet tab itself
// (see header()) so the sprite card owns the animation there instead of two
// faces blinking at once.
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
	tab            int // 0 = pet, 1 = overview, 2 = suggestions, 3 = records, 4 = dex
	width          int
	height         int
	daemonUp       bool
	err            string
	journalEntries []save.Entry
}

const tabCount = 5
const minWidth = 40

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

// contentWidth is the width the frame renders at: clamped to a sane minimum
// before the first WindowSizeMsg arrives, and capped so tables don't stretch
// illegibly across an ultrawide terminal.
func (m Model) contentWidth() int {
	w := m.width
	if w <= 0 {
		w = 80
	}
	if w > 110 {
		w = 110
	}
	return w
}

// View composes the whole frame: header, tab bar, a rule, the active tab's
// body (padded and, when the terminal is tall enough, stretched so the
// footer sits pinned to the bottom), another rule, and the footer.
func (m Model) View() string {
	if m.width > 0 && m.width < minWidth {
		return sMuted.Render(fmt.Sprintf("aipet needs a wider terminal — resize to at least %d columns.", minWidth))
	}
	w := m.contentWidth()

	head := lipgloss.JoinVertical(lipgloss.Left, m.header(w), m.tabBar(w), rule(w))
	foot := lipgloss.JoinVertical(lipgloss.Left, rule(w), m.footer(w))

	bw := w - 4 // body width inside the 2-col gutter on each side
	var content string
	switch m.tab {
	case 0:
		content = m.pet(bw)
	case 1:
		content = m.overview(bw)
	case 2:
		content = m.suggestions(bw)
	case 3:
		content = m.records(bw)
	case 4:
		content = m.dex(bw)
	}
	body := lipgloss.NewStyle().Padding(1, 2).Width(w).Render(content)

	if m.height > 0 {
		bodyH := m.height - lipgloss.Height(head) - lipgloss.Height(foot)
		if bodyH > lipgloss.Height(body) {
			body = lipgloss.Place(w, bodyH, lipgloss.Left, lipgloss.Top, body)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, head, body, foot)
}

// header shows the app title, daemon status, and (everywhere except the Pet
// tab) the pet's face + mood bubble. On the Pet tab the sprite card owns
// that same face/bubble, so showing it twice would mean two things blinking
// independently — the header collapses to just title + daemon status there.
func (m Model) header(w int) string {
	if m.tab == 0 {
		left := sTitle.Render("aipet")
		return spread(left, m.daemonStatus(), w)
	}
	face, bubble, faceSt := m.faceAndBubble()
	left := faceSt.Render(face) + "  " + sTitle.Render("aipet") + sFaint.Render(" · ") + bubble
	return spread(left, m.daemonStatus(), w)
}

// faceAndBubble picks the header's face + speech bubble + style from the
// PET'S OWN state: still an egg, hatched with a real sim.Mood, or no data
// collected yet at all. Budget/spend concerns live in the Overview and
// Suggestions tabs, not the pet's face.
func (m Model) faceAndBubble() (face, bubble string, st lipgloss.Style) {
	if m.snap == nil {
		return eggFaces[m.frame%2], sAccent.Render("Getting to know your machine..."), moodStyle[sim.MoodContent]
	}
	if m.snap.PetError != "" {
		return eggFaces[m.frame%2], sDanger.Render("Something's off — check the Pet tab."), moodStyle[sim.MoodWorried]
	}
	p := m.snap.Pet
	if p.IsEgg() {
		return eggFaces[m.frame%2], sAccent.Render("Warming up..."), moodStyle[sim.MoodContent]
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

func (m Model) daemonStatus() string {
	if m.daemonUp {
		return sSuccess.Render("●") + " " + sMuted.Render("daemon live")
	}
	status := sFaint.Render("○") + " " + sMuted.Render("daemon off")
	if m.overBudget() {
		status = sWarn.Render("budget: over") + "  " + status
	}
	return status
}

func (m Model) tabBar(w int) string {
	labels := []string{"Pet", "Overview", "Suggestions", "Records", "Dex"}
	narrow := w < 60
	var tabs []string
	for i, l := range labels {
		t := l
		if !narrow {
			t = fmt.Sprintf("%d %s", i+1, l)
		}
		if i == m.tab {
			tabs = append(tabs, sTabOn.Render(t))
		} else {
			tabs = append(tabs, sTab.Render(t))
		}
	}
	return strings.Join(tabs, "   ")
}

func (m Model) footer(w int) string {
	hint := func(key, action string) string {
		return sKeyHint.Render(key) + " " + sMuted.Render(action)
	}
	switchHint := "tab/←→"
	if w < 60 {
		switchHint = "tab"
	}
	left := strings.Join([]string{
		hint(switchHint, "switch"),
		hint("r", "refresh"),
		hint("q", "quit"),
	}, "   ")

	right := sFaint.Render("updated " + m.snapTime())
	if m.snap != nil {
		if n := len(m.snap.CollectErrors); n > 0 {
			noun := "error"
			if n != 1 {
				noun = "errors"
			}
			right = sWarn.Render(fmt.Sprintf("%d collect %s", n, noun))
		}
	}
	return spread(left, right, w)
}

func (m Model) snapTime() string {
	if m.snap == nil {
		return "—"
	}
	return m.snap.UpdatedAt.Format("15:04")
}

// overview renders spend at a glance: the budget bar, key usage stats, and
// the top models/projects by cost. On wide terminals usage and rankings sit
// side by side; otherwise they stack.
func (m Model) overview(w int) string {
	if m.snap == nil {
		return emptyState(eggFaces[0], "I haven't seen any sessions yet. Run a Claude Code session and I'll start counting.")
	}
	s := m.snap.Stats

	var budget string
	if m.cfg.DailyBudgetUSD > 0 {
		budget = stack(sSection.Render("Today's budget"), budgetBar(s.TodayCost, m.cfg.DailyBudgetUSD, w))
	}

	wide := w >= 90
	colW := w
	if wide {
		colW = w/2 - 2
	}

	usage := stack(sSection.Render("Usage"), strings.Join([]string{
		kvRow("Today", sValue.Render(money(s.TodayCost)), colW),
		kvRow("All-time", sValue.Render(money(s.TotalCost)), colW),
		kvRow("Turns", sValue.Render(commas(int64(s.Turns))), colW),
		kvRow("Tokens in / out", sValue.Render(human(s.TokensIn)+" / "+human(s.TokensOut)), colW),
		kvRow("Cache reuse", cacheReuse(s), colW),
	}, "\n"))

	models := stack(sSection.Render("Top models"), moneyRowsOrHint(store.TopN(s.ByModel, 3), colW,
		"nothing yet; keep coding and this fills in."))
	projects := stack(sSection.Render("Top projects"), moneyRowsOrHint(store.TopN(s.ByProject, 3), colW,
		"nothing yet; keep coding and this fills in."))

	var body string
	if wide {
		left := lipgloss.NewStyle().Width(colW).Render(usage)
		right := lipgloss.NewStyle().Width(colW).Render(stack(models, projects))
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	} else {
		body = stack(usage, models, projects)
	}

	return stack(budget, body)
}

func moneyRowsOrHint(entries []store.KV, w int, hint string) string {
	if len(entries) == 0 {
		return sFaint.Render(hint)
	}
	var lines []string
	for _, kvp := range entries {
		lines = append(lines, moneyRow(kvp.Key, sValue.Render(money(kvp.Value)), w))
	}
	return strings.Join(lines, "\n")
}

// suggestions renders efficiency advice as accent-barred cards, newest/most
// severe first (the advisor already orders them), capped so the tab never
// overflows a normal terminal height.
func (m Model) suggestions(w int) string {
	if m.snap == nil {
		return emptyState(eggFaces[0], "No advice yet — I need a few sessions to learn your habits.")
	}
	if len(m.snap.Suggestions) == 0 {
		return sSuccess.Render("No suggestions right now — you're running lean.")
	}

	const maxShown = 6
	shown := m.snap.Suggestions
	more := 0
	if len(shown) > maxShown {
		more = len(shown) - maxShown
		shown = shown[:maxShown]
	}

	var totalSaving float64
	for _, s := range m.snap.Suggestions {
		totalSaving += s.SavingUSD
	}
	summary := fmt.Sprintf("%d suggestion", len(m.snap.Suggestions))
	if len(m.snap.Suggestions) != 1 {
		summary += "s"
	}
	if totalSaving >= 0.01 {
		summary += sMuted.Render(" · est. savings ") + sSuccess.Render(money(totalSaving)+"/day")
	}

	var cards []string
	for _, s := range shown {
		cards = append(cards, renderSuggestion(s, w))
	}
	body := strings.Join(cards, "\n\n")
	if more > 0 {
		body = stack(body, sFaint.Render(fmt.Sprintf("…and %d more", more)))
	}

	return stack(sMuted.Render(summary), body)
}

// records renders the local leaderboard: rankings and personal bests. Every
// number is computed on-device from the event log.
func (m Model) records(w int) string {
	if m.snap == nil {
		return emptyState(eggFaces[0], "Your records start with your first session — check back after you code.")
	}
	board := m.snap.Board

	wide := w >= 90
	colW := w
	if wide {
		colW = w/2 - 2
	}

	rankings := stack(
		stack(sSection.Render("Top projects"), rankingRows(board.TopProjects, colW, func(e leaderboard.Entry) string {
			return money(e.Value)
		})),
		stack(sSection.Render("Best cache-reuse days"), rankingRows(board.BestCacheDays, colW, func(e leaderboard.Entry) string {
			return fmt.Sprintf("%.1f%%", e.Value)
		})),
	)

	r := board.Records
	streakCtx := sFaint.Render(fmt.Sprintf("best %d", r.LongestStreak))
	if r.CurrentStreak > 0 && r.CurrentStreak == r.LongestStreak {
		streakCtx = sSuccess.Render("★ best")
	}
	var recLines []string
	recLines = append(recLines, recordRow("Streak", fmt.Sprintf("%d days", r.CurrentStreak), streakCtx, colW))
	if r.BiggestDayUSD.Name != "" {
		recLines = append(recLines, recordRow("Biggest day", money(r.BiggestDayUSD.Value), sFaint.Render(r.BiggestDayUSD.Name), colW))
	}
	if r.BusiestDay.Name != "" {
		recLines = append(recLines, recordRow("Busiest day", fmt.Sprintf("%.0f turns", r.BusiestDay.Value), sFaint.Render(r.BusiestDay.Name), colW))
	}
	if r.FirstSeen != "" {
		recLines = append(recLines, recordRow("Keeper since", r.FirstSeen, sFaint.Render(fmt.Sprintf("%d active days", r.ActiveDays)), colW))
	}
	records := stack(sSection.Render("Personal records"), strings.Join(recLines, "\n"))

	if wide {
		left := lipgloss.NewStyle().Width(colW).Render(rankings)
		right := lipgloss.NewStyle().Width(colW).Render(records)
		return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	}
	return stack(rankings, records)
}

func rankingRows(entries []leaderboard.Entry, w int, val func(leaderboard.Entry) string) string {
	if len(entries) == 0 {
		return sFaint.Render("nothing yet; keep coding and this fills in.")
	}
	var lines []string
	for i, e := range entries {
		rank := sFaint.Render(fmt.Sprintf("%d ", i+1))
		lines = append(lines, rank+moneyRow(e.Name, sValue.Render(val(e)), w-lipgloss.Width(rank)))
	}
	return strings.Join(lines, "\n")
}

// recordRow renders a three-column personal-record line: label, bold value
// (right-aligned to the column midpoint), and faint context (right-aligned
// to the edge) — replaces the old "5 day(s)  (best 9)" prose with something
// scannable.
func recordRow(label, value, context string, w int) string {
	left := kvRow(label, sValue.Render(value), w*2/3)
	return spread(left, context, w)
}
