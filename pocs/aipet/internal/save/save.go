// Package save persists a Daemonkeeper's pet across runs: an atomically
// written pet.json snapshot (same pattern as the daemon's usage snapshot)
// plus an append-only journal.jsonl life log. Both live under ~/.aipet next
// to the existing usage.db and config.json.
package save

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
)

// PetPath returns the path to the pet's save file.
func PetPath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "pet.json"), nil
}

// JournalPath returns the path to the pet's append-only life log.
func JournalPath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "journal.jsonl"), nil
}

// LoadPet reads the saved pet, or creates and persists a brand new egg if
// none exists yet — this is the one place true entropy (crypto/rand) enters
// the game, at first run, matching sim.NewDNA's documented contract.
func LoadPet(now time.Time) (sim.Pet, error) {
	p, ok, err := TryLoadPet()
	if err != nil {
		return sim.Pet{}, err
	}
	if ok {
		return p, nil
	}
	entropy := make([]byte, sim.DNASize)
	if _, err := rand.Read(entropy); err != nil {
		return sim.Pet{}, err
	}
	egg := sim.NewEgg(sim.NewDNA(entropy), now)
	if err := SavePet(egg); err != nil {
		return sim.Pet{}, err
	}
	if err := AppendJournal(Entry{
		Day: now.Format("2006-01-02"), At: now, Kind: "egg_started",
		VoiceID: "hatch_general_01", Text: "An egg arrived. Something in it is already paying attention.",
	}); err != nil {
		return sim.Pet{}, err
	}
	return egg, nil
}

// TryLoadPet reads the saved pet without creating one. ok is false if no
// save file exists yet (first run, before LoadPet has ever been called).
func TryLoadPet() (sim.Pet, bool, error) {
	p, err := PetPath()
	if err != nil {
		return sim.Pet{}, false, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return sim.Pet{}, false, nil
	}
	if err != nil {
		return sim.Pet{}, false, err
	}
	var pet sim.Pet
	if err := json.Unmarshal(b, &pet); err != nil {
		return sim.Pet{}, false, err
	}
	return pet, true, nil
}

// SavePet writes the pet atomically (tmp file + rename), matching
// internal/daemon's snapshot-publish pattern so a crash mid-write can never
// leave a torn pet.json on disk.
func SavePet(p sim.Pet) error {
	path, err := PetPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Entry is one line of the pet's life journal.
type Entry struct {
	Day     string    `json:"day"` // YYYY-MM-DD, local
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"` // "egg_started" | "hatched" | "evolved" | "diet" | "hibernate" | "wake"
	VoiceID string    `json:"voice_id,omitempty"`
	Text    string    `json:"text"` // fallback/plain text if the TUI doesn't resolve VoiceID
}

// AppendJournal appends one entry to journal.jsonl. The log is append-only
// and never rewritten, mirroring internal/store's event log so it stays
// crash-safe and trivially inspectable.
func AppendJournal(e Entry) error {
	path, err := JournalPath()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = f.Write(b)
	return err
}

// maxJournalLineBytes bounds a single journal line, matching the pattern in
// internal/store/store.go so no component in the codebase silently truncates
// a line another component can write.
const maxJournalLineBytes = 1 << 20 // 1MB — journal lines are short text, this is a generous ceiling

// ReadJournal loads the full journal, oldest first. Returns an empty slice
// (not an error) if the journal doesn't exist yet.
func ReadJournal() ([]Entry, error) {
	path, err := JournalPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxJournalLineBytes)
	var out []Entry
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue // a corrupt line must not take down the whole journal read
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
