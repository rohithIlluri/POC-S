// Package codeling implements the .codeling interchange file — GAME_DESIGN
// §4.7's trade format and §4.6's battle card carrier in one versioned JSON
// document. Files cross the trust boundary (they arrive from other
// keepers over Slack, gists, anywhere), so import treats every byte as
// hostile exactly like the collector treats session logs: bounded reads,
// schema and version checks, species whitelisting, stat clamping, and
// terminal-escape sanitization (INTEGRATION_PLAN.md §9).
package codeling

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/battle"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

// FormatVersion is bumped on any incompatible change; import rejects
// versions it doesn't know rather than guessing (§9 rule 2).
const FormatVersion = 1

// maxFileSize bounds the read before any parsing (§9 rule 1). A real
// .codeling is ~1KB; 64KB leaves room without letting a hostile file
// balloon memory.
const maxFileSize = 64 * 1024

// File is the on-disk .codeling document. Self-describing and versioned.
type File struct {
	Format     string      `json:"format"` // always "codeling"
	Version    int         `json:"version"`
	ExportedAt string      `json:"exported_at"` // YYYY-MM-DD, informational
	Card       battle.Card `json:"card"`
	History    string      `json:"history,omitempty"`  // one-line life summary, free text (sanitized on import)
	Traveler   bool        `json:"traveler,omitempty"` // set once the pet has been imported somewhere
	Sig        string      `json:"sig"`                // integrity signature, see Sign
}

// Sign computes the tamper-evidence signature over the card's gameplay
// fields. This is NOT a security boundary (GAME_DESIGN accepts that local
// files can be forged) — it catches *casual* tampering: bump your level in
// an editor and the file is flagged counterfeit on import.
func Sign(c battle.Card) string {
	moves := append([]string(nil), c.Moves...)
	sort.Strings(moves)
	payload := fmt.Sprintf("codeling-v%d|%s|%d|%d/%d/%d/%d/%d|%s|%s",
		FormatVersion, c.SpeciesID, c.Level,
		c.Stats.Vigor, c.Stats.Focus, c.Stats.Wit, c.Stats.Grit, c.Stats.Spark,
		strings.Join(moves, ","), c.DNAHash)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:8])
}

// Export builds a .codeling from the live pet. Moves are auto-equipped
// deterministically (moves.md §1 leaves the selection UI out of scope):
// the line's signature move first if the species has one, then the type
// pool in table order, up to 4.
func Export(p sim.Pet, history string) (File, error) {
	if p.IsEgg() {
		return File{}, fmt.Errorf("an egg can't battle or travel — hatch it first")
	}
	sp, ok := species.ByID(p.SpeciesID)
	if !ok {
		return File{}, fmt.Errorf("unknown species %q", p.SpeciesID)
	}

	var moves []string
	if sig, ok := battle.Signatures[sp.ID]; ok {
		moves = append(moves, sig.ID)
	}
	for _, m := range battle.Pools[sp.Type] {
		if len(moves) >= 4 {
			break
		}
		moves = append(moves, m.ID)
	}

	dna := sha256.Sum256(p.DNA[:])
	card := battle.Card{
		SpeciesID: p.SpeciesID,
		Level:     p.Level,
		Stats:     p.Stats,
		Moves:     moves,
		DNAHash:   hex.EncodeToString(dna[:16]),
	}
	if card.Level < 1 {
		card.Level = 1
	}
	if err := battle.ValidateCard(card); err != nil {
		return File{}, fmt.Errorf("exported card invalid (bug): %w", err)
	}
	f := File{
		Format:  "codeling",
		Version: FormatVersion,
		Card:    card,
		History: sanitize(history, 140),
		Sig:     Sign(card),
	}
	return f, nil
}

// WriteFile writes a .codeling with restrictive permissions.
func WriteFile(path string, f File) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// ImportResult reports what Import did to make the file safe — clamps and
// substitutions are surfaced, not silent, so the keeper knows their trade
// arrived dented.
type ImportResult struct {
	File        File
	Counterfeit bool     // signature mismatch — tampered or hand-built
	Adjustments []string // human-readable list of clamps/fixes applied
}

// Import reads and hardens a .codeling per §9. Any structural problem is
// an error; recoverable oddities (illegal stats, bad moves, oversized
// text) are clamped/fixed and reported in Adjustments. The returned card
// always passes battle.ValidateCard.
func Import(path string) (ImportResult, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return ImportResult{}, err
	}
	if fi.Size() > maxFileSize {
		return ImportResult{}, fmt.Errorf("%s is %d bytes — a .codeling is never larger than %d", path, fi.Size(), maxFileSize)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ImportResult{}, err
	}
	return ImportBytes(b)
}

// ImportBytes is Import without the filesystem — the fuzz target and the
// unit for anything that already holds the bytes.
func ImportBytes(b []byte) (ImportResult, error) {
	if len(b) > maxFileSize {
		return ImportResult{}, fmt.Errorf("input exceeds the %d-byte bound", maxFileSize)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return ImportResult{}, fmt.Errorf("not a valid .codeling: %w", err)
	}
	if f.Format != "codeling" {
		return ImportResult{}, fmt.Errorf("not a .codeling file (format %q)", f.Format)
	}
	if f.Version != FormatVersion {
		return ImportResult{}, fmt.Errorf("unsupported .codeling version %d (this build reads v%d)", f.Version, FormatVersion)
	}

	res := ImportResult{}
	c := f.Card

	// §9 rule 3 — species whitelist against the embedded Dex.
	sp, ok := species.ByID(c.SpeciesID)
	if !ok {
		return ImportResult{}, fmt.Errorf("unknown species %q — not in this build's Dex", c.SpeciesID)
	}

	// Counterfeit check BEFORE any clamping, against the file as it arrived.
	res.Counterfeit = f.Sig != Sign(c)

	// Level clamp.
	if c.Level < 1 {
		c.Level = 1
		res.Adjustments = append(res.Adjustments, "level raised to 1")
	}
	if c.Level > 100 {
		c.Level = 100
		res.Adjustments = append(res.Adjustments, "level capped at 100")
	}

	// §9 rule 4 — per-stat clamp to the species' plausible grown band:
	// [1, 2×base+50]. A hostile file cannot inject a 9999-stat pet; a
	// legitimately grown pet sits comfortably inside the band.
	clamp := func(name string, v, base int) int {
		hi := base*2 + 50
		if v < 1 {
			res.Adjustments = append(res.Adjustments, name+" raised to 1")
			return 1
		}
		if v > hi {
			res.Adjustments = append(res.Adjustments, fmt.Sprintf("%s clamped to %d", name, hi))
			return hi
		}
		return v
	}
	c.Stats.Vigor = clamp("vigor", c.Stats.Vigor, sp.Base.Vigor)
	c.Stats.Focus = clamp("focus", c.Stats.Focus, sp.Base.Focus)
	c.Stats.Wit = clamp("wit", c.Stats.Wit, sp.Base.Wit)
	c.Stats.Grit = clamp("grit", c.Stats.Grit, sp.Base.Grit)
	c.Stats.Spark = clamp("spark", c.Stats.Spark, sp.Base.Spark)

	// Moves: drop illegal/duplicate ids, refill deterministically if empty.
	legal := battle.LegalMoves(sp)
	var kept []string
	seen := map[string]bool{}
	for _, id := range c.Moves {
		if legal[id] && !seen[id] && len(kept) < 4 {
			kept = append(kept, id)
			seen[id] = true
		}
	}
	if len(kept) != len(c.Moves) {
		res.Adjustments = append(res.Adjustments, "illegal or duplicate moves removed")
	}
	if len(kept) == 0 {
		for _, m := range battle.Pools[sp.Type] {
			if len(kept) >= 4 {
				break
			}
			kept = append(kept, m.ID)
		}
		res.Adjustments = append(res.Adjustments, "moves replaced with the type-pool defaults")
	}
	c.Moves = kept

	// §9 rule 5 — free text is hostile until sanitized.
	c.Nickname = sanitize(c.Nickname, 40)
	f.History = sanitize(f.History, 140)
	c.DNAHash = sanitize(c.DNAHash, 64)
	if c.DNAHash == "" {
		return ImportResult{}, fmt.Errorf("card carries no dna_hash")
	}

	if err := battle.ValidateCard(c); err != nil {
		return ImportResult{}, fmt.Errorf("card unusable even after hardening: %w", err)
	}

	f.Card = c
	f.Traveler = true // it traveled to get here
	res.File = f
	return res, nil
}

// sanitize strips control runes (the same posture as the collector's
// boundary sanitizer) and caps length — imported text reaches a terminal.
func sanitize(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if len(out) > max {
		out = out[:max]
	}
	return out
}
