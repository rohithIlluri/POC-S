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
	cfg      config.Config
	snap     *daemon.Snapshot
	snapMod  time.Time // snapshot file mtime at last parse
	mood     mood
	frame    int
	tab      int // 0 = overview, 1 = suggestions, 2 = records
	width    int
	height   int
	daemonUp bool
	err      string
}

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
			m.tab = (m.tab + 1) % 3
		case "left", "h":
			m.tab = (m.tab + 2) % 3
		case "1":
			m.tab = 0
		case "2":
			m.tab = 1
		case "3":
			m.tab = 2
		case "r":
			m.refresh(true)
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
		b.WriteString(m.overview())
	case 1:
		b.WriteString(m.suggestions())
	case 2:
		b.WriteString(m.records())
	}
	b.WriteString("\n")
	b.WriteString(m.footer())
	return b.String()
}

func (m Model) header() string {
	face := faces[m.mood][m.frame%2]
	name := titleStyle.Render(" aipet ")
	faceCol := faceStyle(m.mood).Render(face)

	status := dimStyle.Render("daemon: off")
	if m.daemonUp {
		status = okStyle.Render("daemon: live")
	}
	var bubble string
	switch m.mood {
	case moodWorried:
		bubble = warnStyle.Render("Let's trim some spend!")
	case moodThinking:
		bubble = tipStyle.Render("Watching your usage...")
	default:
		bubble = okStyle.Render("Looking efficient!")
	}
	line := lipgloss.JoinHorizontal(lipgloss.Center, faceCol, "  ", name, "  ", bubble)
	return lipgloss.JoinHorizontal(lipgloss.Center, line, "   ", status)
}

func (m Model) tabBar() string {
	labels := []string{"Overview", "Suggestions", "Records"}
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
