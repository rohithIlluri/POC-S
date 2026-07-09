package sim

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
)

// DNASize is the seed length in bytes, per GAME_DESIGN.md §5.3.
const DNASize = 32

// DNA is a pet's permanent, immutable identity seed, rolled once at egg
// creation. Every random-looking outcome for this pet (IVs, Lucent roll,
// battle RNG stream) derives from it plus deterministic inputs (date,
// history) — never from wall-clock entropy at roll time.
type DNA [DNASize]byte

// MarshalJSON encodes DNA as base64, matching the on-disk shape documented
// in GAME_DESIGN.md §5.3 ("dna": "b64…") instead of a raw byte-array dump.
func (d DNA) MarshalJSON() ([]byte, error) {
	return json.Marshal(base64.StdEncoding.EncodeToString(d[:]))
}

// UnmarshalJSON decodes DNA from the base64 string MarshalJSON produces.
func (d *DNA) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	if len(raw) != DNASize {
		return fmt.Errorf("sim: DNA must be %d bytes, got %d", DNASize, len(raw))
	}
	copy(d[:], raw)
	return nil
}

// NewDNA rolls a fresh seed from a caller-supplied entropy source. This is
// the ONE place true randomness may enter the sim — at egg creation, when
// there is no prior state to derive from. Everything downstream of the
// resulting DNA is a pure function of it.
func NewDNA(entropy []byte) DNA {
	// entropy is expected to come from crypto/rand at the call site (egg
	// creation in internal/save); hashing it here just normalizes length
	// and gives every derived roll a consistent 32-byte seed shape.
	sum := sha256.Sum256(entropy)
	return DNA(sum)
}

// derive produces a deterministic 8-byte stream for (dna, purpose, extra),
// the same construction pattern used throughout docs/design/rarity.md and
// docs/design/moves.md so all Codelings randomness is replay-compatible.
func derive(dna DNA, purpose string, extra string) uint64 {
	h := sha256.New()
	h.Write(dna[:])
	h.Write([]byte(purpose))
	h.Write([]byte(extra))
	sum := h.Sum(nil)
	return binary.BigEndian.Uint64(sum[:8])
}

// IVs are hidden per-stat modifiers rolled once at hatch, giving two pets
// with identical history a small, permanent, recognizable difference. Range
// and math match docs/design/rarity.md §4: 0-31 raw, modifier = (iv-16)/4,
// integer division truncating toward zero, so the spread is exactly -4..+3.
type IVs struct {
	Vigor, Focus, Wit, Grit, Spark int // raw 0-31 values
}

// RollIVs derives all five IVs from the pet's DNA. Deterministic and
// idempotent — calling it twice on the same DNA always yields the same IVs.
func RollIVs(dna DNA) IVs {
	roll := func(stat string) int {
		return int(derive(dna, "iv", stat) % 32)
	}
	return IVs{
		Vigor: roll("vigor"),
		Focus: roll("focus"),
		Wit:   roll("wit"),
		Grit:  roll("grit"),
		Spark: roll("spark"),
	}
}

// Modifier converts a raw 0-31 IV into its integer stat modifier, -4..+3.
func Modifier(iv int) int { return (iv - 16) / 4 }

// AsStats returns the IV block's modifiers as a species.Stats-shaped delta,
// convenient for adding onto a grown stat line in tick.go.
func (iv IVs) AsStats() species.Stats {
	return species.Stats{
		Vigor: Modifier(iv.Vigor),
		Focus: Modifier(iv.Focus),
		Wit:   Modifier(iv.Wit),
		Grit:  Modifier(iv.Grit),
		Spark: Modifier(iv.Spark),
	}
}

// LucentDenominator is the Lucent roll's denominator for a given GRIT
// streak, per docs/design/rarity.md §3.1 — a pure step function, floor 1/64.
func LucentDenominator(gritStreakDays int) uint64 {
	switch {
	case gritStreakDays >= 28:
		return 64
	case gritStreakDays >= 14:
		return 128
	case gritStreakDays >= 7:
		return 256
	default:
		return 512
	}
}

// IsLucent rolls the cosmetic-only Lucent variant for a hatch event, per
// docs/design/rarity.md §3.2: SHA256(dna||"lucent"||streak) mod denom == 0.
func IsLucent(dna DNA, gritStreakDays int) bool {
	denom := LucentDenominator(gritStreakDays)
	roll := derive(dna, "lucent", itoa(gritStreakDays)) % denom
	return roll == 0
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// PickLine resolves which starter line an egg hatches into, from the
// dominant playstyle signal across the hatch window (GAME_DESIGN.md §4.4:
// "the dominant playstyle in the hatch window picks the species line").
//
// Signals, one score per line, summed across all digests in the window:
//   - Ember (deep work):  turns in sessions with few turns/session are a
//     WORSE signal; Ember scores on long MaxGapMin-free sessions — approximated
//     here as turns-per-session (higher = more sustained single-thread work).
//   - Stream (fast iteration): high cache ratio and high turn count with
//     LOW turns-per-session (many quick separate exchanges).
//   - Vector (breadth): distinct projects + distinct models touched.
//
// Ties are broken deterministically by DNA (never by map order or wall
// clock), so replaying the same digests+DNA always picks the same line.
func PickLine(dna DNA, window []Digest) species.Line {
	var emberScore, streamScore, vectorScore float64
	for _, d := range window {
		if d.Sessions > 0 {
			turnsPerSession := float64(d.Turns) / float64(d.Sessions)
			emberScore += turnsPerSession // sustained single-thread work
		}
		streamScore += d.CacheRatio() * float64(d.Turns)
		vectorScore += float64(d.Projects + d.Models)
	}

	type scored struct {
		line  species.Line
		score float64
	}
	candidates := []scored{
		{species.Ember, emberScore},
		{species.StreamLine, streamScore},
		{species.Vector, vectorScore},
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		} else if c.score == best.score {
			// Deterministic tiebreak: DNA picks among tied lines rather than
			// favoring declaration order, so ties aren't silently biased.
			if derive(dna, "line-tiebreak", string(c.line)) > derive(dna, "line-tiebreak", string(best.line)) {
				best = c
			}
		}
	}
	return best.line
}
