package species

import "testing"

// bstBand mirrors docs/design/species.md's tier bands — a species' base stat
// total must fall inside its tier's band, or the design/code have drifted.
func bstBand(r Rarity) (lo, hi int) {
	switch r {
	case Common:
		return 200, 239
	case Uncommon:
		return 240, 279
	case Rare:
		return 280, 319
	case Relic:
		return 320, 359
	case Mythic:
		return 360, 400
	default:
		return -1, -1
	}
}

func TestRosterSize(t *testing.T) {
	if len(All) != 30 {
		t.Fatalf("expected 30 launch species, got %d", len(All))
	}
}

func TestUniqueIDsAndDexNumbers(t *testing.T) {
	ids := map[string]bool{}
	dex := map[int]bool{}
	for _, s := range All {
		if ids[s.ID] {
			t.Errorf("duplicate id %q", s.ID)
		}
		ids[s.ID] = true
		if dex[s.Dex] {
			t.Errorf("duplicate dex number %d (%s)", s.Dex, s.ID)
		}
		dex[s.Dex] = true
		if s.Dex < 1 || s.Dex > 30 {
			t.Errorf("%s: dex number %d out of range", s.ID, s.Dex)
		}
	}
}

func TestBSTWithinRarityBand(t *testing.T) {
	for _, s := range All {
		lo, hi := bstBand(s.Rarity)
		bst := s.Base.Sum()
		if bst < lo || bst > hi {
			t.Errorf("%s: BST %d outside %s band [%d,%d]", s.ID, bst, s.Rarity, lo, hi)
		}
	}
}

func TestEvolutionTargetsExist(t *testing.T) {
	for _, s := range All {
		if s.EvolvesTo == "" {
			continue
		}
		if _, ok := byID[s.EvolvesTo]; !ok {
			t.Errorf("%s evolves into unknown species %q", s.ID, s.EvolvesTo)
		}
	}
}

func TestStarterLinesComplete(t *testing.T) {
	for _, l := range []Line{Ember, StreamLine, Vector} {
		ids, ok := lines[l]
		if !ok || len(ids) != 3 {
			t.Fatalf("line %q: expected 3 stages, got %d", l, len(ids))
		}
		starter, ok := LineStarter(l)
		if !ok {
			t.Fatalf("line %q: no starter resolved", l)
		}
		sp, _ := ByID(starter)
		if sp.Stage != 1 {
			t.Errorf("line %q starter %q has stage %d, want 1", l, starter, sp.Stage)
		}
		// Walk the chain stage1 -> stage2 -> stage3, must be exactly 3 long
		// and end without EvolvesTo.
		cur := sp
		seen := 1
		for cur.EvolvesTo != "" {
			next, ok := ByID(cur.EvolvesTo)
			if !ok {
				t.Fatalf("line %q: broken chain at %q", l, cur.ID)
			}
			if next.Line != l {
				t.Errorf("line %q: %q evolves into %q which belongs to line %q", l, cur.ID, next.ID, next.Line)
			}
			cur = next
			seen++
		}
		if seen != 3 {
			t.Errorf("line %q: chain length %d, want 3", l, seen)
		}
	}
}

func TestByID(t *testing.T) {
	sp, ok := ByID("cindling")
	if !ok || sp.Name != "Cindling" {
		t.Fatalf("ByID(cindling) = %+v, %v", sp, ok)
	}
	if _, ok := ByID("does-not-exist"); ok {
		t.Error("ByID should report false for an unknown id")
	}
}

func TestMythicCountExactlyTwo(t *testing.T) {
	n := 0
	for _, s := range All {
		if s.Rarity == Mythic {
			n++
			if s.EvolvesTo != "" || s.Line != NoLine {
				t.Errorf("mythic %s must be standalone/encounter-only, got Line=%q EvolvesTo=%q", s.ID, s.Line, s.EvolvesTo)
			}
		}
	}
	if n != 2 {
		t.Errorf("expected exactly 2 mythic species, got %d", n)
	}
}

func TestArtNonEmpty(t *testing.T) {
	for _, s := range All {
		if s.Art == "" {
			t.Errorf("%s: missing sprite art", s.ID)
		}
		if s.DexEntry == "" {
			t.Errorf("%s: missing dex entry", s.ID)
		}
	}
}
