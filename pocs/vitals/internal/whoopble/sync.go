package whoopble

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// History sync, as the official app does it (from its code; the ack and
// metadata layouts match the public capture in the tests, but nothing here has
// been run against your strap yet):
//
//  1. a HISTORY_START metadata packet opens a burst
//  2. the burst's packets stream in (type 0x2F history records, 0x32 console logs, ...)
//  3. a HISTORY_END metadata packet closes it, carrying how many records were
//     sent and an 8-byte token
//  4. the app checks the count, STORES the burst, and only then acknowledges
//     with that token; the ack lets the strap free that memory
//  5. a HISTORY_COMPLETE metadata packet ends the whole sync
//
// HistorySync implements steps 1-5 and never acknowledges before the sink has
// stored the burst.

// Metadata subtypes (body[2] of a TypeMetadata packet).
const (
	MetaHistoryStart    = 1
	MetaHistoryEnd      = 2
	MetaHistoryComplete = 3
)

// History commands.
const (
	CmdSendHistory   = 0x16 // start the download; empty payload
	CmdHistoryResult = 0x17 // acknowledge a burst
	CmdAbortHistory  = 0x14 // stop the download; empty payload
)

// StartHistory asks the strap to send what it has recorded. The official app
// sends this with an empty payload. Nothing is freed on the strap until a
// burst is acknowledged with HistoryAck.
func StartHistory(seq byte) []byte { return command(seq, CmdSendHistory) }

// AbortHistory stops a download in progress (empty payload).
func AbortHistory(seq byte) []byte { return command(seq, CmdAbortHistory) }

// Metadata is a TypeMetadata packet:
//
//	[0] 0x31 [1] counter [2] subtype [3:7] unix seconds [7:9] sub-second ticks [9:] data
type Metadata struct {
	Subtype byte
	Time    time.Time
	Data    []byte
}

// DecodeMetadata reads a TypeMetadata body.
func DecodeMetadata(body []byte) (Metadata, bool) {
	if len(body) < 9 || body[0] != TypeMetadata {
		return Metadata{}, false
	}
	sub := int64(binary.LittleEndian.Uint16(body[7:9]))
	return Metadata{
		Subtype: body[2],
		Time:    time.Unix(int64(binary.LittleEndian.Uint32(body[3:7])), sub*1e9/ClockTicksPerSecond),
		Data:    body[9:],
	}, true
}

// BurstEnd is the content of a HISTORY_END packet: how many records the
// strap says it sent, and the token to echo back in the ack.
type BurstEnd struct {
	Expected uint32
	Token    [8]byte
}

// BurstEnd reads a HISTORY_END packet's data: [u32 expected][8-byte token].
// The official app requires more than 12 bytes of data.
func (m Metadata) BurstEnd() (BurstEnd, bool) {
	if m.Subtype != MetaHistoryEnd || len(m.Data) < 13 {
		return BurstEnd{}, false
	}
	var e BurstEnd
	e.Expected = binary.LittleEndian.Uint32(m.Data[0:4])
	copy(e.Token[:], m.Data[4:12])
	return e, true
}

// HistoryAck acknowledges a stored burst: status 1 and the token echoed back
// exactly as received. Write it WITH response. The strap frees the burst's
// memory when it receives this, so send it only after the data is stored.
func HistoryAck(seq byte, token [8]byte) []byte {
	return command(seq, CmdHistoryResult, append([]byte{1}, token[:]...)...)
}

// HistoryNack tells the strap the burst was not stored (status 0, empty token).
func HistoryNack(seq byte) []byte {
	return command(seq, CmdHistoryResult, make([]byte, 9)...)
}

// Burst is one stretch of history ready to store.
type Burst struct {
	Records  [][]byte // every packet of the burst (history records, console logs, ...), raw, in arrival order
	Expected uint32   // the count the strap announced for ALL of them
	Token    [8]byte
}

// Sink stores a burst durably. HistorySync acknowledges only after it
// returns nil.
type Sink interface {
	SaveBurst(Burst) error
}

// maxBurstRecords bounds memory if a strap never sends an end marker.
const maxBurstRecords = 100000

// shortfallAfterFailures is how many records a burst may fall short of the
// strap's count once 3 bursts in a row have failed, as the official app does.
const (
	failuresBeforeTolerance = 3
	shortfallTolerance      = 2
)

// HistorySync turns the strap's history packets into stored bursts and the
// replies to send. Feed it every TypeMetadata and TypeHistory body in order.
type HistorySync struct {
	sink Sink
	seq  byte

	open     bool
	records  [][]byte
	failures int

	Bursts    int // bursts stored
	Stored    int // records stored
	Discarded int // partial bursts thrown away
	Done      bool
}

// NewHistorySync starts a sync that stores into sink and numbers its replies
// from firstSeq.
func NewHistorySync(sink Sink, firstSeq byte) *HistorySync {
	return &HistorySync{sink: sink, seq: firstSeq}
}

func (h *HistorySync) nextSeq() byte { s := h.seq; h.seq++; return s }

// ErrStore wraps a sink failure. No ack was produced, so the strap still
// holds the data; stop the sync and try again later.
var ErrStore = errors.New("whoopble: storing a history burst failed")

// ErrBurstTooLong means a burst never ended.
var ErrBurstTooLong = errors.New("whoopble: history burst has no end marker")

// Feed processes one frame body. reply, when non-nil, must be written to the
// strap (with response). A non-nil error stops the sync with nothing
// acknowledged for the failing burst.
func (h *HistorySync) Feed(body []byte) (reply []byte, err error) {
	if len(body) == 0 {
		return nil, nil
	}
	switch body[0] {
	case TypeMetadata:
		m, ok := DecodeMetadata(body)
		if !ok {
			return nil, nil
		}
		switch m.Subtype {
		case MetaHistoryStart:
			if h.open && len(h.records) > 0 {
				h.Discarded++
			}
			h.open, h.records = true, nil
		case MetaHistoryEnd:
			return h.end(m)
		case MetaHistoryComplete:
			h.open, h.records = false, nil
			h.Done = true
		}
	case TypeRealtime, TypeCommand, TypeCommandResponse:
		// The live stream and command traffic share the channel; they are not part of a burst.
	default:
		// Everything else the strap sends inside a burst counts toward the total it announces:
		// history records (0x2F), console logs (0x32), motion and so on. A real strap's first
		// burst was 54 console-log packets and no heart-rate records at all.
		if !h.open {
			return nil, nil
		}
		if len(h.records) >= maxBurstRecords {
			h.open, h.records = false, nil
			return nil, ErrBurstTooLong
		}
		h.records = append(h.records, append([]byte(nil), body...))
	}
	return nil, nil
}

func (h *HistorySync) end(m Metadata) ([]byte, error) {
	if !h.open {
		return nil, nil // a duplicate end marker
	}
	e, ok := m.BurstEnd()
	if !ok {
		return nil, nil
	}
	allowed := uint32(0)
	if h.failures >= failuresBeforeTolerance {
		allowed = shortfallTolerance
	}
	got := uint32(len(h.records))
	if got+allowed < e.Expected {
		// Records went missing: don't store, don't ack with the token.
		h.failures++
		h.open, h.records = false, nil
		return HistoryNack(h.nextSeq()), nil
	}
	if err := h.sink.SaveBurst(Burst{Records: h.records, Expected: e.Expected, Token: e.Token}); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStore, err)
	}
	h.failures = 0
	h.Bursts++
	h.Stored += len(h.records)
	h.open, h.records = false, nil
	return HistoryAck(h.nextSeq(), e.Token), nil
}
