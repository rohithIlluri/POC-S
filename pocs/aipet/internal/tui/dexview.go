package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/save"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

var rarityStyle = map[species.Rarity]lipgloss.Style{
	species.Common:   sMuted,
	species.Uncommon: sAccent,
	species.Rare:     sBrand,
	species.Relic:    sWarn,
	species.Mythic:   sDanger,
}

// dex renders the Dex tab: all 30 species as a responsive grid with
// seen/caught markers. Unseen species show as a faint dash — you have to
// meet them first.
func (m Model) dex(w int) string {
	if m.snap == nil {
		return sMuted.Render("No data yet. Start the daemon with `aipet daemon` or run `aipet status`.")
	}
	return RenderDex(m.snap.Dex, w)
}

// RenderDex is the shared Dex listing used by both the TUI tab and the
// `aipet dex` CLI command. It lays species out as a grid — at 30 entries,
// mostly unseen, a single column wastes most of a screen's height.
func RenderDex(dex save.DexState, w int) string {
	caught, seen := 0, 0
	for _, sp := range species.All {
		switch {
		case has(dex.Caught, sp.ID):
			caught++
		case has(dex.Seen, sp.ID):
			seen++
		}
	}
	header := spread(
		sSection.Render("Dex")+sMuted.Render(fmt.Sprintf("  %d/%d caught · %d seen", caught, len(species.All), seen)),
		essenceLabel(dex.EchoEssence),
		w,
	)

	const cellW = 30
	cols := (w + 2) / (cellW + 2)
	if cols < 1 {
		cols = 1
	}
	if cols > 4 {
		cols = 4
	}

	cells := make([]string, 0, len(species.All))
	for _, sp := range species.All {
		cells = append(cells, dexCell(sp, dex))
	}

	footer := sFaint.Render("wild Codelings appear on real events — new projects, new models, clean days.")
	return stack(header, grid(cells, cellW, cols), footer)
}

func essenceLabel(n int) string {
	if n <= 0 {
		return ""
	}
	return sBrand.Render(fmt.Sprintf("✦ %d echo essence", n))
}

func dexCell(sp species.Species, dex save.DexState) string {
	num := sFaint.Render(fmt.Sprintf("%03d", sp.Dex))
	switch {
	case has(dex.Caught, sp.ID):
		line1 := sSuccess.Render("●") + " " + num + " " + sValue.Render(sp.Name)
		line2 := rarityStyle[sp.Rarity].Render(string(sp.Rarity)) + sFaint.Render(" · caught "+dex.Caught[sp.ID])
		return line1 + "\n" + line2
	case has(dex.Seen, sp.ID):
		line1 := sWarn.Render("◐") + " " + num + " " + sBody.Render(sp.Name)
		line2 := rarityStyle[sp.Rarity].Render(string(sp.Rarity)) + sFaint.Render(" · seen "+dex.Seen[sp.ID])
		return line1 + "\n" + line2
	default:
		return sFaint.Render("○ "+fmt.Sprintf("%03d", sp.Dex)+" —") + "\n"
	}
}

func has(m map[string]string, key string) bool {
	_, ok := m[key]
	return ok
}
