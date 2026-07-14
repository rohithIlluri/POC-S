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

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/advisor"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/daemon"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/leaderboard"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/save"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

// The pet's mood reflects how spend tracks against budget — a quick emotional
// read before the user even looks at numbers.
type mood int

const (
	moodHappy mood = iota
	moodThinking
	moodWorried
)

var faces = map[mood][]string{
	moodHappy:    {"( ^_^ )", "( ^.^ )"},
	moodThinking: {"( o_o )", "( -_o )"},
	moodWorried:  {"( >_< )", "( ;_; )"},
}

type tickMsg time.Time

// Model is the Bubble Tea model for the pet.
type Model struct {
	cfg            config.Config
	snap           *daemon.Snapshot
	snapMod        time.Time // snapshot file mtime at last parse
	mood           mood
	frame          int
	tab            int // 0 = pet, 1 = overview, 2 = suggestions, 3 = records, 4 = dex
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

func (m Model) Init() tea.Cmd { return tick() }

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
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
	m.mood = m.computeMood()
}

func (m Model) computeMood() mood {
	if m.snap == nil {
		return moodThinking
	}
	if m.cfg.DailyBudgetUSD > 0 {
		r := m.snap.Stats.TodayCost / m.cfg.DailyBudgetUSD
		switch {
		case r >= 1.0:
			return moodWorried
		case r >= 0.75:
			return moodThinking
		}
	}
	for _, s := range m.snap.Suggestions {
		if s.Severity == advisor.Warn {
			return moodWorried
		}
	}
	return moodHappy
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		m.frame++
		m.refresh(false) // animation tick: stat-check only, parse on change
		return m, tick()
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
			m.refresh(true)
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

const minWidth = 40

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

func (m Model) header(w int) string {
	face := faces[m.mood][m.frame%2]
	left := faceStyle(m.mood).Render(face) + "  " + sTitle.Render("aipet") + sFaint.Render(" · ") + m.moodBubble()
	return spread(left, m.daemonStatus(), w)
}

func (m Model) moodBubble() string {
	switch m.mood {
	case moodWorried:
		return sWarn.Render("let's trim some spend")
	case moodThinking:
		return sMuted.Render("watching your usage")
	default:
		return sMuted.Render("looking efficient")
	}
}

func (m Model) daemonStatus() string {
	if m.daemonUp {
		return sSuccess.Render("●") + " " + sMuted.Render("daemon live")
	}
	return sFaint.Render("○") + " " + sMuted.Render("daemon off")
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
	left := strings.Join([]string{
		hint("tab/←→", "switch"),
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
		return sMuted.Render("No data yet. Start the daemon with `aipet daemon` or run `aipet status`.")
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

	models := stack(sSection.Render("Top models"), moneyRows(store.TopN(s.ByModel, 3), colW))
	projects := stack(sSection.Render("Top projects"), moneyRows(store.TopN(s.ByProject, 3), colW))

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

func moneyRows(entries []store.KV, w int) string {
	if len(entries) == 0 {
		return sFaint.Render("(no data yet)")
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
		return sMuted.Render("No data yet. Start the daemon with `aipet daemon` or run `aipet status`.")
	}
	if len(m.snap.Suggestions) == 0 {
		return sMuted.Render("No suggestions right now — you're running lean.")
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
		return sMuted.Render("No data yet. Start the daemon with `aipet daemon` or run `aipet status`.")
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
		return sFaint.Render("(no qualifying data yet)")
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
