package save

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
)

// DexState is the Daemonkeeper's collection log: which species have been
// seen and caught (and when), the whiff-pity counter, and Echo Essence from
// duplicates (docs/design/rarity.md §7.1). Persisted as ~/.aipet/dex.json
// with the same atomic-write pattern as pet.json.
type DexState struct {
	SaveVersion     int               `json:"save_version"`
	Seen            map[string]string `json:"seen"`   // species id -> YYYY-MM-DD first seen
	Caught          map[string]string `json:"caught"` // species id -> YYYY-MM-DD first caught
	WhiffsSinceRare int               `json:"whiffs_since_rare"`
	EchoEssence     int               `json:"echo_essence"`

	// LastEncounterDay is the most recent COMPLETED day whose encounters
	// have been rolled. Encounter sweeps lag the pet tick by one day (a
	// day's diet verdict — and so its catch outcome — only closes when the
	// day does), so this cursor advances independently of pet.LastTickDay.
	LastEncounterDay string `json:"last_encounter_day,omitempty"`
}

// NewDexState returns an empty collection.
func NewDexState() DexState {
	return DexState{SaveVersion: 1, Seen: map[string]string{}, Caught: map[string]string{}}
}

// DexPath returns the path to the dex save file.
func DexPath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "dex.json"), nil
}

// LoadDex reads the saved dex, or returns a fresh empty one if none exists.
func LoadDex() (DexState, error) {
	p, err := DexPath()
	if err != nil {
		return DexState{}, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return NewDexState(), nil
	}
	if err != nil {
		return DexState{}, err
	}
	dex := NewDexState()
	if err := json.Unmarshal(b, &dex); err != nil {
		return DexState{}, err
	}
	if dex.Seen == nil {
		dex.Seen = map[string]string{}
	}
	if dex.Caught == nil {
		dex.Caught = map[string]string{}
	}
	return dex, nil
}

// SaveDex writes the dex atomically (tmp + rename).
func SaveDex(d DexState) error {
	p, err := DexPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// essenceForRarity is the duplicate-conversion table from rarity.md §7.1.
func essenceForRarity(rarity string) int {
	switch rarity {
	case "uncommon":
		return 2
	case "rare":
		return 4
	case "relic":
		return 8
	case "mythic":
		return 16
	default:
		return 1 // common
	}
}

// Record applies one encounter to the collection: first sighting marks
// Seen, a catch marks Caught, and a caught duplicate converts to Echo
// Essence instead (a duplicate is never wasted). Returns the essence
// gained (0 unless it was a caught duplicate).
func (d *DexState) Record(speciesID, day, rarity string, caught bool) int {
	if _, ok := d.Seen[speciesID]; !ok {
		d.Seen[speciesID] = day
	}
	if !caught {
		return 0
	}
	if _, already := d.Caught[speciesID]; already {
		gain := essenceForRarity(rarity)
		d.EchoEssence += gain
		return gain
	}
	d.Caught[speciesID] = day
	return 0
}
