package save

import (
	"testing"
)

func TestLoadDexFreshWhenMissing(t *testing.T) {
	isolateHome(t)
	dex, err := LoadDex()
	if err != nil {
		t.Fatal(err)
	}
	if len(dex.Seen) != 0 || len(dex.Caught) != 0 || dex.EchoEssence != 0 {
		t.Errorf("fresh dex should be empty, got %+v", dex)
	}
}

func TestDexSaveLoadRoundtrip(t *testing.T) {
	isolateHome(t)
	dex := NewDexState()
	dex.Record("cindling", "2026-07-10", "common", true)
	dex.Record("staleout", "2026-07-11", "rare", false)
	dex.WhiffsSinceRare = 7

	if err := SaveDex(dex); err != nil {
		t.Fatal(err)
	}
	got, err := LoadDex()
	if err != nil {
		t.Fatal(err)
	}
	if got.Caught["cindling"] != "2026-07-10" {
		t.Errorf("caught date lost: %+v", got.Caught)
	}
	if got.Seen["staleout"] != "2026-07-11" {
		t.Errorf("seen date lost: %+v", got.Seen)
	}
	if _, caught := got.Caught["staleout"]; caught {
		t.Error("seen-only species must not be recorded as caught")
	}
	if got.WhiffsSinceRare != 7 {
		t.Errorf("whiff counter lost: %d", got.WhiffsSinceRare)
	}
}

func TestRecordFirstCatchAndSighting(t *testing.T) {
	dex := NewDexState()
	if gain := dex.Record("rivulet", "2026-07-10", "common", true); gain != 0 {
		t.Errorf("first catch should give no essence, got %d", gain)
	}
	if dex.Seen["rivulet"] == "" || dex.Caught["rivulet"] == "" {
		t.Error("a catch marks both seen and caught")
	}

	dex2 := NewDexState()
	dex2.Record("rivulet", "2026-07-10", "common", false)
	if dex2.Seen["rivulet"] == "" {
		t.Error("an uncaught encounter still marks seen")
	}
	if _, ok := dex2.Caught["rivulet"]; ok {
		t.Error("an uncaught encounter must not mark caught")
	}
}

func TestRecordDuplicateConvertsToEssence(t *testing.T) {
	dex := NewDexState()
	dex.Record("staleout", "2026-07-10", "rare", true)
	gain := dex.Record("staleout", "2026-07-12", "rare", true)
	if gain != 4 {
		t.Errorf("rare duplicate should convert to 4 essence, got %d", gain)
	}
	if dex.EchoEssence != 4 {
		t.Errorf("essence not accumulated: %d", dex.EchoEssence)
	}
	if dex.Caught["staleout"] != "2026-07-10" {
		t.Error("first-caught date must not be overwritten by a duplicate")
	}
}

func TestEssenceTable(t *testing.T) {
	want := map[string]int{"common": 1, "uncommon": 2, "rare": 4, "relic": 8, "mythic": 16}
	for rarity, n := range want {
		if got := essenceForRarity(rarity); got != n {
			t.Errorf("essenceForRarity(%s) = %d, want %d", rarity, got, n)
		}
	}
}
