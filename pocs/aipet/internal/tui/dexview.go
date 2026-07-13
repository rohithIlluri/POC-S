package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/save"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

var (
	dexCaughtStyle = lipgloss.NewStyle().Foreground(cGreen)
	dexSeenStyle   = lipgloss.NewStyle().Foreground(cYellow)
	dexUnknown     = lipgloss.NewStyle().Foreground(cGray)
	rarityStyles   = map[species.Rarity]lipgloss.Style{
		species.Common:   lipgloss.NewStyle().Foreground(cGray),
		species.Uncommon: lipgloss.NewStyle().Foreground(cBlue),
		species.Rare:     lipgloss.NewStyle().Foreground(cPurple),
		species.Relic:    lipgloss.NewStyle().Foreground(cYellow),
		species.Mythic:   lipgloss.NewStyle().Foreground(cRed).Bold(true),
	}
)

// dex renders the Dex tab: all 30 species in dex order with seen/caught
// markers. Unseen species show as ??? — you have to meet them first.
func (m Model) dex() string {
	if m.snap == nil {
		return dimStyle.Render("No data yet. Start the daemon with `aipet daemon` or run `aipet status`.")
	}
	return RenderDex(m.snap.Dex)
}

// RenderDex is the shared Dex listing used by both the TUI tab and the
// `aipet dex` CLI command.
func RenderDex(dex save.DexState) string {
	var b strings.Builder

	caught, seen := 0, 0
	for _, sp := range species.All {
		if _, ok := dex.Caught[sp.ID]; ok {
			caught++
		} else if _, ok := dex.Seen[sp.ID]; ok {
			seen++
		}
	}
	b.WriteString(sectionStyle.Render(fmt.Sprintf("Dex — %d/%d caught · %d seen", caught, len(species.All), seen)))
	if dex.EchoEssence > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("   ✦ %d echo essence", dex.EchoEssence)))
	}
	b.WriteString("\n\n")

	for _, sp := range species.All {
		rs := rarityStyles[sp.Rarity]
		switch {
		case dex.Caught[sp.ID] != "":
			b.WriteString(fmt.Sprintf("  %s #%03d %-12s %s  %s\n",
				dexCaughtStyle.Render("●"), sp.Dex, sp.Name,
				rs.Render(fmt.Sprintf("%-8s", sp.Rarity)),
				dimStyle.Render("caught "+dex.Caught[sp.ID])))
		case dex.Seen[sp.ID] != "":
			b.WriteString(fmt.Sprintf("  %s #%03d %-12s %s  %s\n",
				dexSeenStyle.Render("◐"), sp.Dex, sp.Name,
				rs.Render(fmt.Sprintf("%-8s", sp.Rarity)),
				dimStyle.Render("seen "+dex.Seen[sp.ID])))
		default:
			b.WriteString(dexUnknown.Render(fmt.Sprintf("  ○ #%03d %-12s", sp.Dex, "???")))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("wild Codelings appear on real events — new projects, new models, clean days.\nthey join you when the day they appeared met the balanced-diet bar."))
	return b.String()
}
