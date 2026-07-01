// Package tui renders the companion — a small terminal "pet" that shows live
// spend, money-saving suggestions, and market updates. It reads the daemon's
// snapshot on a timer, so the UI stays responsive and works even if the daemon
// is briefly down. No network or model calls happen here.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/enterprise/aipet/internal/advisor"
	"github.com/enterprise/aipet/internal/config"
	"github.com/enterprise/aipet/internal/daemon"
	"github.com/enterprise/aipet/internal/store"
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
	mood     mood
	frame    int
	tab      int // 0 = overview, 1 = suggestions, 2 = market
	width    int
	height   int
	daemonUp bool
	err      string
}

// New builds the TUI model, loading any existing snapshot immediately.
func New(cfg config.Config) Model {
	m := Model{cfg: cfg}
	m.refresh()
	return m
}

func (m Model) Init() tea.Cmd { return tick() }

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) refresh() {
	if s, err := daemon.ReadSnapshot(); err == nil {
		m.snap = s
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
		m.refresh()
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
			m.refresh()
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
		b.WriteString(m.market())
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
	labels := []string{"Overview", "Suggestions", "Market"}
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
		if s.Source == "feed" {
			continue // feed items live in the Market tab
		}
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

func (m Model) market() string {
	if m.snap == nil || len(m.snap.Tips) == 0 {
		return dimStyle.Render("  No market updates yet. The feed refreshes periodically.")
	}
	var b strings.Builder
	if m.snap.UpdateAvailable && m.snap.UpdateInfo != nil {
		b.WriteString(updateStyle.Render(fmt.Sprintf("  ⬆ Update available: v%s — run `aipet update`", m.snap.UpdateInfo.LatestVersion)))
		b.WriteString("\n\n")
	}
	for _, t := range m.snap.Tips {
		cat := categoryStyle.Render(strings.ToUpper(t.Category))
		b.WriteString(fmt.Sprintf("  %s  %s\n", cat, lipgloss.NewStyle().Bold(true).Render(t.Title)))
		b.WriteString(dimStyle.Render("  "+wrap(t.Body, max(40, m.width-4))) + "\n\n")
	}
	return b.String()
}

func (m Model) footer() string {
	help := "tab/←→ switch · r refresh · q quit"
	feed := ""
	if m.snap != nil {
		if m.snap.FeedOK {
			feed = okStyle.Render("feed ok")
		} else {
			feed = warnStyle.Render("feed: " + trunc(m.snap.FeedError, 30))
		}
		if n := len(m.snap.CollectErrors); n > 0 {
			feed += "  " + warnStyle.Render(fmt.Sprintf("%d collect error(s)", n))
		}
	}
	return dimStyle.Render(help) + "   " + feed
}
