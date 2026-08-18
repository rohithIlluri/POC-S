package codeling

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/battle"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

func livePet(t *testing.T) sim.Pet {
	t.Helper()
	p := sim.NewEgg(sim.NewDNA([]byte("trade-test")), time.Now())
	p.SpeciesID = "forgeon"
	p.Line = species.Ember
	p.Stage = 2
	p.Level = 20
	p.Stats = species.Stats{Vigor: 75, Focus: 40, Wit: 35, Grit: 90, Spark: 45}
	return p
}

func TestExportRoundTripsThroughImport(t *testing.T) {
	f, err := Export(livePet(t), "Raised on warm caches and long streaks.")
	if err != nil {
		t.Fatal(err)
	}
	if f.Card.Moves[0] != "slow_burn" {
		t.Errorf("forgeon's signature move should be equipped first, got %v", f.Card.Moves)
	}
	b, _ := json.Marshal(f)
	res, err := ImportBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	if res.Counterfeit {
		t.Error("a genuine export must not be flagged counterfeit")
	}
	if len(res.Adjustments) != 0 {
		t.Errorf("a genuine export needs no adjustments, got %v", res.Adjustments)
	}
	if !res.File.Traveler {
		t.Error("an imported pet is a traveler")
	}
}

func TestExportRejectsEgg(t *testing.T) {
	egg := sim.NewEgg(sim.NewDNA([]byte("egg")), time.Now())
	if _, err := Export(egg, ""); err == nil {
		t.Error("eggs must not be exportable")
	}
}

func TestImportFlagsTamperedLevel(t *testing.T) {
	f, _ := Export(livePet(t), "")
	f.Card.Level = 99 // casual cheat: edit the level, keep the old sig
	b, _ := json.Marshal(f)
	res, err := ImportBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Counterfeit {
		t.Error("a level edited after signing must be flagged counterfeit")
	}
}

func TestImportClampsHostileStats(t *testing.T) {
	f, _ := Export(livePet(t), "")
	f.Card.Stats.Vigor = 9999
	f.Card.Stats.Grit = -5
	f.Sig = Sign(f.Card) // even a re-signed file can't exceed the bands
	b, _ := json.Marshal(f)
	res, err := ImportBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	sp, _ := species.ByID("forgeon")
	if hi := sp.Base.Vigor*2 + 50; res.File.Card.Stats.Vigor != hi {
		t.Errorf("vigor should clamp to %d, got %d", hi, res.File.Card.Stats.Vigor)
	}
	if res.File.Card.Stats.Grit != 1 {
		t.Errorf("negative grit should clamp to 1, got %d", res.File.Card.Stats.Grit)
	}
	if len(res.Adjustments) == 0 {
		t.Error("clamps must be reported, not silent")
	}
	if err := battle.ValidateCard(res.File.Card); err != nil {
		t.Errorf("hardened card must be battle-legal: %v", err)
	}
}

func TestImportFixesIllegalMoves(t *testing.T) {
	f, _ := Export(livePet(t), "")
	f.Card.Moves = []string{"cache_cascade", "nonsense_move"} // another line's signature + junk
	f.Sig = Sign(f.Card)
	b, _ := json.Marshal(f)
	res, err := ImportBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range res.File.Card.Moves {
		if _, ok := battle.MoveByID(id); !ok {
			t.Errorf("unknown move %q survived import", id)
		}
	}
	if len(res.File.Card.Moves) == 0 {
		t.Error("import must refill an empty move list")
	}
}

func TestImportSanitizesText(t *testing.T) {
	f, _ := Export(livePet(t), "")
	f.Card.Nickname = "evil\x1b]0;pwned\x07name"
	f.History = "\x1b[31mred\x1b[0m " + strings.Repeat("x", 500)
	f.Sig = Sign(f.Card)
	b, _ := json.Marshal(f)
	res, err := ImportBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(res.File.Card.Nickname, "\x1b\x07") {
		t.Errorf("control bytes survived in nickname: %q", res.File.Card.Nickname)
	}
	if len(res.File.History) > 140 {
		t.Errorf("history not capped: %d chars", len(res.File.History))
	}
}

func TestImportRejects(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"not json", "hello"},
		{"wrong format", `{"format":"pokemon","version":1}`},
		{"future version", `{"format":"codeling","version":99}`},
		{"unknown species", `{"format":"codeling","version":1,"card":{"species":"missingno","level":5,"moves":["hotfix"],"dna_hash":"ab"}}`},
	}
	for _, tc := range cases {
		if _, err := ImportBytes([]byte(tc.body)); err == nil {
			t.Errorf("%s: expected an import error", tc.name)
		}
	}
}

// TestExportedCardBattles closes the loop: two exports fight and produce a
// deterministic result — the full trade→battle path works end to end.
func TestExportedCardBattles(t *testing.T) {
	a, err := Export(livePet(t), "")
	if err != nil {
		t.Fatal(err)
	}
	q := livePet(t)
	q.SpeciesID = "cascada"
	q.Line = species.StreamLine
	q.Stats = species.Stats{Vigor: 60, Focus: 90, Wit: 40, Grit: 50, Spark: 60}
	b, err := Export(q, "")
	if err != nil {
		t.Fatal(err)
	}
	r1, err := battle.Fight(a.Card, b.Card, "2026-07-16")
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := battle.Fight(b.Card, a.Card, "2026-07-16")
	if strings.Join(r1.Log, "\n") != strings.Join(r2.Log, "\n") {
		t.Error("traded-card battle is not load-order symmetric")
	}
}

// FuzzImportBytes is the §9 fuzz gate: hostile bytes must never panic —
// they either error out or produce a battle-legal, sanitized card.
func FuzzImportBytes(f *testing.F) {
	valid, _ := Export(sim.Pet{
		SpeciesID: "forgeon", Level: 20, Stage: 2,
		Stats: species.Stats{Vigor: 75, Focus: 40, Wit: 35, Grit: 90, Spark: 45},
	}, "seed corpus")
	b, _ := json.Marshal(valid)
	f.Add(b)
	f.Add([]byte(`{"format":"codeling","version":1,"card":{"species":"forgeon","level":-1,"stats":{"vigor":99999},"moves":["x"],"dna_hash":"[31m"}}`))
	f.Add([]byte("{}"))
	f.Fuzz(func(t *testing.T, data []byte) {
		res, err := ImportBytes(data)
		if err != nil {
			return
		}
		if verr := battle.ValidateCard(res.File.Card); verr != nil {
			t.Errorf("import accepted an illegal card: %v", verr)
		}
		for _, r := range res.File.Card.Nickname + res.File.History {
			if r < 0x20 || r == 0x7f {
				t.Errorf("control rune %q survived sanitization", r)
			}
		}
	})
}
