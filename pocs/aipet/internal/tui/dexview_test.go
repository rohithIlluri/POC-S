package tui

import (
	"strings"
	"testing"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/daemon"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/save"
)

func TestDexTabHidesUnseenSpecies(t *testing.T) {
	m := New(config.Default())
	m.tab = 4
	m.snap = &daemon.Snapshot{Dex: save.NewDexState()}
	out := m.View()
	if strings.Contains(out, "Cindling") {
		t.Error("unseen species names must render as ???")
	}
	if !strings.Contains(out, "0/30 caught") {
		t.Errorf("expected completion counter, got:\n%s", out)
	}
}

func TestDexTabShowsSeenAndCaught(t *testing.T) {
	m := New(config.Default())
	m.tab = 4
	dex := save.NewDexState()
	dex.Record("cindling", "2026-07-10", "common", true)
	dex.Record("staleout", "2026-07-11", "rare", false)
	dex.EchoEssence = 5
	m.snap = &daemon.Snapshot{Dex: dex}

	out := m.View()
	for _, want := range []string{"Cindling", "Staleout", "1/30 caught", "1 seen", "5 echo essence", "caught 2026-07-10", "seen 2026-07-11"} {
		if !strings.Contains(out, want) {
			t.Errorf("dex tab missing %q", want)
		}
	}
	if strings.Contains(out, "Rivulet") {
		t.Error("still-unseen species must stay hidden as ???")
	}
}
