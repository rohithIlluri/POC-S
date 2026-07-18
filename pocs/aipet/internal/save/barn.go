package save

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/battle"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
)

// BarnEntry is one pet resting in the Barn (GAME_DESIGN §4.7): imported
// travelers live here — one ACTIVE pet at a time, the Barn holds the rest.
type BarnEntry struct {
	Card       battle.Card `json:"card"`
	History    string      `json:"history,omitempty"`
	Traveler   bool        `json:"traveler"`
	ImportedAt string      `json:"imported_at"` // YYYY-MM-DD
}

func barnPath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "barn.json"), nil
}

// LoadBarn returns the Barn's contents; a missing file is an empty Barn.
func LoadBarn() ([]BarnEntry, error) {
	p, err := barnPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []BarnEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// SaveBarn writes the Barn atomically, same pattern as every other save.
func SaveBarn(entries []BarnEntry) error {
	p, err := barnPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
