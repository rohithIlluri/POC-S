package battle

import (
	"crypto/sha256"
	"encoding/binary"
)

// stream is the counter-mode deterministic RNG from moves.md §3.5:
// next() = first 8 bytes (big-endian) of SHA256(seed || draw_index_u64_be),
// draw_index incrementing once per call across the WHOLE battle, never
// reset per turn. Same construction rarity.md §2.4 uses, reused for
// consistency across the content docs.
type stream struct {
	seed  [32]byte
	index uint64
}

func newStream(seed [32]byte) *stream {
	return &stream{seed: seed}
}

func (s *stream) next() uint64 {
	var buf [40]byte
	copy(buf[:32], s.seed[:])
	binary.BigEndian.PutUint64(buf[32:], s.index)
	s.index++
	sum := sha256.Sum256(buf[:])
	return binary.BigEndian.Uint64(sum[:8])
}
