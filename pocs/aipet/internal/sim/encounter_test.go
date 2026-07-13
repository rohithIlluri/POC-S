package sim

import (
	"testing"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

func TestDayTriggers(t *testing.T) {
	d := Digest{NewProjects: 1, NewModels: 1}
	got := DayTriggers(d, true)
	if len(got) != 3 {
		t.Fatalf("expected 3 triggers (new project, new model, clean day), got %v", got)
	}
	quiet := Digest{}
	if got := DayTriggers(quiet, false); len(got) != 0 {
		t.Fatalf("quiet unhealthy day should fire no triggers, got %v", got)
	}
}

func TestRollEncountersDeterministic(t *testing.T) {
	dna := NewDNA([]byte("enc-det"))
	d := Digest{NewProjects: 1, NewModels: 1}
	dex := DexView{Caught: map[string]bool{}}

	a, wa := RollEncounters(dna, "2026-07-13", d, true, dex)
	b, wb := RollEncounters(dna, "2026-07-13", d, true, dex)
	if wa != wb || len(a) != len(b) {
		t.Fatal("RollEncounters must be deterministic")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("encounter %d differs between identical rolls: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestRollEncountersTierDistribution(t *testing.T) {
	// Across many DNAs, one trigger each: COMMON should dominate, RELIC should
	// be rare but present, matching the 696/256/64/8 out of 1024 design.
	counts := map[species.Rarity]int{}
	d := Digest{NewModels: 1}
	const n = 4096
	for i := 0; i < n; i++ {
		dna := NewDNA([]byte{byte(i), byte(i >> 8), 0x7})
		encs, _ := RollEncounters(dna, "2026-07-13", d, false, DexView{Caught: map[string]bool{}})
		if len(encs) != 1 {
			t.Fatalf("expected exactly 1 encounter, got %d", len(encs))
		}
		counts[encs[0].Rarity]++
	}
	if counts[species.Common] < n/2 {
		t.Errorf("COMMON should dominate: got %d of %d", counts[species.Common], n)
	}
	if counts[species.Relic] == 0 {
		t.Error("RELIC should appear at least once in 4096 rolls (expected ~32)")
	}
	if counts[species.Relic] > n/32 {
		t.Errorf("RELIC too frequent: %d of %d (expected ~1/128)", counts[species.Relic], n)
	}
	if counts[species.Mythic] != 0 {
		t.Error("MYTHIC must never come from the odds table")
	}
}

func TestHealthyDietShiftsTierUp(t *testing.T) {
	// With a healthy diet, COMMON becomes impossible for the base roll's
	// COMMON range — every encounter lands UNCOMMON or better.
	d := Digest{NewModels: 1}
	for i := 0; i < 512; i++ {
		dna := NewDNA([]byte{byte(i), byte(i >> 8), 0x9})
		encs, _ := RollEncounters(dna, "2026-07-13", d, true, DexView{Caught: map[string]bool{}})
		if len(encs) != 2 { // new model + clean day
			t.Fatalf("expected 2 encounters on a healthy day with a new model, got %d", len(encs))
		}
		for _, e := range encs {
			if e.Rarity == species.Common {
				t.Fatalf("healthy-diet day must shift COMMON up, got COMMON for dna %d", i)
			}
		}
	}
}

func TestWhiffPityFloors(t *testing.T) {
	d := Digest{NewModels: 1}
	dna := NewDNA([]byte("pity-test"))

	encs, _ := RollEncounters(dna, "2026-07-13", d, false, DexView{Caught: map[string]bool{}, WhiffsSinceRare: whiffFloorRare})
	if r := encs[0].Rarity; r != species.Rare && r != species.Relic {
		t.Errorf("40+ whiffs must floor the roll at RARE, got %v", r)
	}
	encs, _ = RollEncounters(dna, "2026-07-13", d, false, DexView{Caught: map[string]bool{}, WhiffsSinceRare: whiffFloorRelic})
	if r := encs[0].Rarity; r != species.Relic {
		t.Errorf("120+ whiffs must floor the roll at RELIC, got %v", r)
	}
}

func TestWhiffCounterThreading(t *testing.T) {
	// A COMMON/UNCOMMON result increments whiffs; RARE/RELIC resets to 0.
	d := Digest{NewModels: 1}
	for i := 0; i < 256; i++ {
		dna := NewDNA([]byte{byte(i), 0x3})
		encs, whiffs := RollEncounters(dna, "2026-07-13", d, false, DexView{Caught: map[string]bool{}, WhiffsSinceRare: 10})
		r := encs[0].Rarity
		if (r == species.Rare || r == species.Relic) && whiffs != 0 {
			t.Fatalf("RARE+ result must reset whiffs, got %d", whiffs)
		}
		if (r == species.Common || r == species.Uncommon) && whiffs != 11 {
			t.Fatalf("whiff result must increment counter, got %d", whiffs)
		}
	}
}

func TestUncaughtFirstWeighting(t *testing.T) {
	// With every COMMON species caught except one, any COMMON encounter must
	// pick exactly that one.
	caught := map[string]bool{}
	var lastCommon string
	for _, s := range species.All {
		if s.Rarity == species.Common {
			caught[s.ID] = true
			lastCommon = s.ID
		}
	}
	delete(caught, lastCommon)

	d := Digest{NewModels: 1}
	found := false
	for i := 0; i < 512; i++ {
		dna := NewDNA([]byte{byte(i), byte(i >> 8), 0x5})
		encs, _ := RollEncounters(dna, "2026-07-13", d, false, DexView{Caught: caught})
		if encs[0].Rarity == species.Common {
			found = true
			if encs[0].SpeciesID != lastCommon {
				t.Fatalf("uncaught-first violated: got %s, want %s", encs[0].SpeciesID, lastCommon)
			}
		}
	}
	if !found {
		t.Skip("no COMMON roll in sample (statistically near-impossible)")
	}
}

func TestCatchByDoing(t *testing.T) {
	dna := NewDNA([]byte("catch-test"))
	d := Digest{NewModels: 1}
	healthy, _ := RollEncounters(dna, "2026-07-13", d, true, DexView{Caught: map[string]bool{}})
	for _, e := range healthy {
		if !e.Caught {
			t.Error("encounters on a healthy day must be caught (catch-by-doing)")
		}
	}
	unhealthy, _ := RollEncounters(dna, "2026-07-13", d, false, DexView{Caught: map[string]bool{}})
	for _, e := range unhealthy {
		if e.Caught {
			t.Error("encounters on an unhealthy day are seen, not caught")
		}
	}
}

func TestMythicEncounterGates(t *testing.T) {
	none := MythicEncounter("2026-07-13", Digest{}, 10, map[string]bool{})
	if none != nil {
		t.Fatal("no mythic should appear on an ordinary day")
	}

	wyrm := MythicEncounter("2026-07-13", Digest{}, 365, map[string]bool{})
	if wyrm == nil || wyrm.SpeciesID != "uptimewyrm" || !wyrm.Caught {
		t.Fatalf("365-day streak must surface uptimewyrm, caught: %+v", wyrm)
	}

	ever := MythicEncounter("2026-07-13", Digest{TokensIn: 1_500_000, CacheRead: 600_000}, 1, map[string]bool{})
	if ever == nil || ever.SpeciesID != "everfile" {
		t.Fatalf("mega-context day must surface everfile: %+v", ever)
	}

	// Already caught: never re-triggers.
	again := MythicEncounter("2026-07-13", Digest{}, 400, map[string]bool{"uptimewyrm": true})
	if again != nil {
		t.Fatal("a caught mythic must not re-trigger")
	}
}
