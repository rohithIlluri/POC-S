package tui

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/daemon"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/leaderboard"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/save"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

// update regenerates golden files from the current renderer output. Run:
//
//	go test ./internal/tui/ -run TestCard -update
var update = flag.Bool("update", false, "update golden files")

var fixedNow = time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

// eggSnapshot is a freshly-started, unhatched pet: 2 of 5 qualifying
// sessions in, no dex or spend history yet.
func eggSnapshot() *daemon.Snapshot {
	egg := sim.NewEgg(sim.NewDNA([]byte("card-egg-fixture")), fixedNow)
	egg.EggSessionCount = 2
	egg.ActiveDayCount = 1
	return &daemon.Snapshot{
		UpdatedAt: fixedNow,
		Pet:       egg,
		Dex:       save.NewDexState(),
	}
}

// hatchedCheerfulSnapshot is a healthy, leveled-up pet with dex progress and
// spend/journal history — the "everything is going well" fixture.
func hatchedCheerfulSnapshot() *daemon.Snapshot {
	p := sim.NewEgg(sim.NewDNA([]byte("card-cheerful-fixture")), fixedNow)
	p.SpeciesID = "cindling"
	p.Line = "ember"
	p.Stage = sim.Stage1
	p.Level = 4
	p.XP = 190 // between the level-4 floor (160) and level-5 floor (250)
	p.Health = 86
	p.Mood = sim.MoodCheerful
	p.GritStreak = 3
	p.ActiveDayCount = 5

	dex := save.NewDexState()
	dex.Record("cindling", "2026-07-08", "common", true)
	dex.Record("staleout", "2026-07-09", "rare", false)

	return &daemon.Snapshot{
		UpdatedAt: fixedNow,
		Pet:       p,
		Dex:       dex,
		Stats: store.Stats{
			TodayCost: 0.42,
			TotalCost: 12.34,
			Turns:     58,
			ByModel:   map[string]float64{"claude-opus-4": 8.10},
			ByProject: map[string]float64{"webapp": 12.34},
		},
		Board: leaderboard.Board{
			TopProjects: []leaderboard.Entry{{Name: "webapp", Value: 12.34}},
			Records: leaderboard.Records{
				CurrentStreak: 3, LongestStreak: 5,
				BiggestDayUSD: leaderboard.Entry{Name: "2026-07-01", Value: 9.99},
				FirstSeen:     "2026-06-01", ActiveDays: 20,
			},
		},
	}
}

// hatchedWorriedSnapshot is a struggling pet: low health, a status effect,
// no dex progress — the "something's wrong" fixture.
func hatchedWorriedSnapshot() *daemon.Snapshot {
	p := sim.NewEgg(sim.NewDNA([]byte("card-worried-fixture")), fixedNow)
	p.SpeciesID = "cindling"
	p.Line = "ember"
	p.Stage = sim.Stage1
	p.Level = 2
	p.XP = 8
	p.Health = 22
	p.Mood = sim.MoodWorried
	p.Statuses = []sim.Status{sim.StatusTokenBloat}
	p.GritStreak = 0
	p.ActiveDayCount = 6

	return &daemon.Snapshot{
		UpdatedAt: fixedNow,
		Pet:       p,
		Dex:       save.NewDexState(),
		Stats: store.Stats{
			TodayCost: 5.00,
			TotalCost: 40.00,
			Turns:     200,
		},
	}
}

func fixtureJournal() []save.Entry {
	return []save.Entry{
		{Day: "2026-07-08", Kind: "diet", Text: "Good mix today — right-sized model, warm cache. Textbook."},
		{Day: "2026-07-09", Kind: "diet", Text: "Cold starts galore, and a lot of junk food model calls."},
	}
}

func goldenPath(view, fixture string) string {
	return filepath.Join("testdata", "card_"+view+"_"+fixture+".golden")
}

func checkGolden(t *testing.T, view, fixture, got string) {
	t.Helper()
	p := goldenPath(view, fixture)
	if *update {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", p, err)
	}
	if got != string(want) {
		t.Errorf("card %s/%s mismatch. To regenerate: go test ./internal/tui/ -run TestCard -update\n--- got ---\n%s\n--- want ---\n%s", view, fixture, got, string(want))
	}
}

// TestCardViews is the H1/R10 golden-test gate: every view × relevant
// fixture must match a committed golden file byte-for-byte. ASCII color
// profile is forced inside Card itself, so this test doesn't need to (and
// running with -update regenerates deterministically regardless of TTY).
func TestCardViews(t *testing.T) {
	type fixture struct {
		name string
		snap *daemon.Snapshot
	}
	fixtures := []fixture{
		{"egg", eggSnapshot()},
		{"cheerful", hatchedCheerfulSnapshot()},
		{"worried", hatchedWorriedSnapshot()},
		{"nil", nil},
	}
	views := []string{"pet", "dex", "records", "overview"}

	for _, view := range views {
		for _, f := range fixtures {
			t.Run(view+"/"+f.name, func(t *testing.T) {
				var journal []save.Entry
				if f.name == "cheerful" {
					journal = fixtureJournal()
				}
				got, err := Card(view, f.snap, journal, defaultCardWidth)
				if err != nil {
					t.Fatalf("Card(%q, %s): %v", view, f.name, err)
				}
				checkGolden(t, view, f.name, got)
			})
		}
	}
}

// TestCardDefaultViewIsPet verifies "" behaves exactly like "pet" (R2: empty
// view whitelists to pet).
func TestCardDefaultViewIsPet(t *testing.T) {
	snap := hatchedCheerfulSnapshot()
	empty, err := Card("", snap, nil, defaultCardWidth)
	if err != nil {
		t.Fatal(err)
	}
	pet, err := Card("pet", snap, nil, defaultCardWidth)
	if err != nil {
		t.Fatal(err)
	}
	if empty != pet {
		t.Errorf("Card(\"\") should equal Card(\"pet\"); got:\n%s\n---\n%s", empty, pet)
	}
}

// TestCardUnknownViewErrors is the R2 whitelist gate: any view outside
// pet/dex/records/overview must error, never best-effort render — view is
// user-controlled input from $ARGUMENTS.
func TestCardUnknownViewErrors(t *testing.T) {
	if _, err := Card("../../etc/passwd", hatchedCheerfulSnapshot(), nil, defaultCardWidth); err == nil {
		t.Fatal("expected an error for an unknown view")
	}
	if _, err := Card("PET", hatchedCheerfulSnapshot(), nil, defaultCardWidth); err == nil {
		t.Fatal("view whitelist should be case-sensitive, not fuzzy-matched")
	}
}

// TestCardDefaultWidth verifies width<=0 falls back to defaultCardWidth
// rather than producing a degenerate zero-width render.
func TestCardDefaultWidth(t *testing.T) {
	out, err := Card("pet", eggSnapshot(), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 20 {
		t.Fatalf("width<=0 should fall back to a sane default, got too-short output: %q", out)
	}
}
