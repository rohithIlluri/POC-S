package whoopble

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

// meta builds a metadata body like the strap's: [0x31][counter][subtype]
// [unix][ticks][data], padded to the 15 data bytes seen in the capture.
func meta(subtype byte, data ...byte) []byte {
	b := []byte{TypeMetadata, 0x18, subtype, 0xf6, 0x5c, 0x70, 0x66, 0x80, 0x40}
	d := append([]byte(nil), data...)
	for len(d) < 15 {
		d = append(d, 0)
	}
	return append(b, d...)
}

func end(expected uint32, token [8]byte) []byte {
	d := binary.LittleEndian.AppendUint32(nil, expected)
	return meta(MetaHistoryEnd, append(d, token[:]...)...)
}

var testToken = [8]byte{0x2e, 0x47, 0x01, 0x00, 0x04, 0, 0, 0}

// historyBodies returns the eight real history records published with the
// write-up this package is based on.
func historyBodies(t *testing.T) [][]byte {
	t.Helper()
	var out [][]byte
	for _, s := range capturedHistory {
		body, err := Parse(unhex(t, s))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, body)
	}
	return out
}

type recSink struct {
	bursts []Burst
	err    error
	calls  *[]string
}

func (s *recSink) SaveBurst(b Burst) error {
	if s.calls != nil {
		*s.calls = append(*s.calls, "save")
	}
	if s.err != nil {
		return s.err
	}
	s.bursts = append(s.bursts, b)
	return nil
}

func TestDecodeRealHistoryEndMarker(t *testing.T) {
	body, err := Parse(unhex(t, capturedMetadata))
	if err != nil {
		t.Fatal(err)
	}
	m, ok := DecodeMetadata(body)
	if !ok || m.Subtype != MetaHistoryEnd {
		t.Fatalf("decoded %+v", m)
	}
	e, ok := m.BurstEnd()
	if !ok || e.Expected != 67 || e.Token != testToken {
		t.Fatalf("burst end %+v %v; the capture announces 67 records and token 2e47010004000000", e, ok)
	}
	if _, ok := DecodeMetadata(body[:8]); ok {
		t.Error("accepted a truncated metadata packet")
	}
	short := Metadata{Subtype: MetaHistoryEnd, Data: make([]byte, 12)}
	if _, ok := short.BurstEnd(); ok {
		t.Error("the app needs more than 12 data bytes; 12 must be refused")
	}
}

func TestAckLayout(t *testing.T) {
	body, err := Parse(HistoryAck(9, testToken))
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte{TypeCommand, 9, CmdHistoryResult, 1}, testToken[:]...)
	if !bytes.Equal(body, want) {
		t.Errorf("ack = %x, want %x", body, want)
	}
	nack, _ := Parse(HistoryNack(10))
	if !bytes.Equal(nack, append([]byte{TypeCommand, 10, CmdHistoryResult}, make([]byte, 9)...)) {
		t.Errorf("nack = %x", nack)
	}
	if IsBlocked(CmdHistoryResult) {
		t.Error("the history ack must not be blocked")
	}
}

func TestSyncStoresBeforeItAcknowledges(t *testing.T) {
	var calls []string
	sink := &recSink{calls: &calls}
	h := NewHistorySync(sink, 0x40)
	recs := historyBodies(t)

	if r, err := h.Feed(meta(MetaHistoryStart)); r != nil || err != nil {
		t.Fatalf("start: %x %v", r, err)
	}
	for _, rec := range recs {
		if r, err := h.Feed(rec); r != nil || err != nil {
			t.Fatalf("record: %x %v", r, err)
		}
	}
	if len(sink.bursts) != 0 {
		t.Fatal("stored before the end marker arrived")
	}
	reply, err := h.Feed(end(8, testToken))
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("the burst must be stored before the ack is produced: %v", calls)
	}
	body, _ := Parse(reply)
	if !bytes.Equal(body, append([]byte{TypeCommand, 0x40, CmdHistoryResult, 1}, testToken[:]...)) {
		t.Errorf("reply = %x", body)
	}
	if len(sink.bursts) != 1 || len(sink.bursts[0].Records) != 8 || sink.bursts[0].Token != testToken {
		t.Fatalf("stored %+v", sink.bursts)
	}
	if h.Bursts != 1 || h.Stored != 8 {
		t.Errorf("counters %d %d", h.Bursts, h.Stored)
	}
	if r, _ := h.Feed(meta(MetaHistoryComplete)); r != nil || !h.Done {
		t.Errorf("complete: %x done=%v", r, h.Done)
	}
}

func TestNoAckWhenStoringFails(t *testing.T) {
	boom := errors.New("disk full")
	h := NewHistorySync(&recSink{err: boom}, 1)
	h.Feed(meta(MetaHistoryStart))
	for _, rec := range historyBodies(t) {
		h.Feed(rec)
	}
	reply, err := h.Feed(end(8, testToken))
	if reply != nil {
		t.Fatal("acknowledged a burst that was not stored: the strap would free data we don't have")
	}
	if !errors.Is(err, ErrStore) || h.Stored != 0 {
		t.Fatalf("err = %v stored = %d", err, h.Stored)
	}
}

func TestShortBurstGetsANackAndToleranceKicksInAfterThree(t *testing.T) {
	sink := &recSink{}
	h := NewHistorySync(sink, 1)
	recs := historyBodies(t)
	run := func(n int, expected uint32) []byte {
		h.Feed(meta(MetaHistoryStart))
		for _, r := range recs[:n] {
			h.Feed(r)
		}
		reply, err := h.Feed(end(expected, testToken))
		if err != nil {
			t.Fatal(err)
		}
		return reply
	}
	// Strap says 8, only 7 arrive: refused three times in a row.
	for i := 0; i < 3; i++ {
		reply := run(7, 8)
		body, _ := Parse(reply)
		if len(body) != 3+9 || body[3] != 0 {
			t.Fatalf("attempt %d: expected a nack, got %x", i, body)
		}
	}
	if len(sink.bursts) != 0 {
		t.Fatal("a short burst was stored")
	}
	// After three failures a shortfall of 2 is accepted, so 7-of-8 now passes.
	body, _ := Parse(run(7, 8))
	if body[3] != 1 || len(sink.bursts) != 1 {
		t.Fatalf("expected an ack after tolerance, got %x (stored %d)", body, len(sink.bursts))
	}
	// A clean burst resets the failure count: a shortfall is refused again.
	run(8, 8)
	if body, _ := Parse(run(6, 8)); body[3] != 0 {
		t.Errorf("after a success, a 6-of-8 burst must be refused, got %x", body)
	}
}

func TestOddOrderingIsHandledSafely(t *testing.T) {
	sink := &recSink{}
	h := NewHistorySync(sink, 1)
	recs := historyBodies(t)

	// Records outside a burst are ignored; a duplicate end marker is ignored.
	if r, err := h.Feed(recs[0]); r != nil || err != nil {
		t.Fatalf("record outside a burst: %x %v", r, err)
	}
	if r, err := h.Feed(end(1, testToken)); r != nil || err != nil {
		t.Fatalf("end with no burst: %x %v", r, err)
	}
	// A start in the middle of a burst throws the partial burst away.
	h.Feed(meta(MetaHistoryStart))
	h.Feed(recs[0])
	h.Feed(recs[1])
	h.Feed(meta(MetaHistoryStart))
	if h.Discarded != 1 {
		t.Errorf("discarded = %d", h.Discarded)
	}
	h.Feed(recs[2])
	reply, err := h.Feed(end(1, testToken))
	if err != nil || reply == nil {
		t.Fatalf("%x %v", reply, err)
	}
	if len(sink.bursts[0].Records) != 1 {
		t.Errorf("the restarted burst kept %d records", len(sink.bursts[0].Records))
	}
	// Records are copied, not aliased to the caller's buffer.
	buf := append([]byte(nil), recs[3]...)
	h.Feed(meta(MetaHistoryStart))
	h.Feed(buf)
	buf[0] = 0
	h.Feed(end(1, testToken))
	if sink.bursts[1].Records[0][0] != TypeHistory {
		t.Error("the stored record aliases the caller's buffer")
	}
	if r, err := h.Feed(nil); r != nil || err != nil {
		t.Error("an empty body must be ignored")
	}
}

func TestStartAndAbortAreOneZeroPadByteLong(t *testing.T) {
	for name, tc := range map[string]struct {
		frame []byte
		cmd   byte
	}{
		"start": {StartHistory(5), CmdSendHistory},
		"abort": {AbortHistory(6), CmdAbortHistory},
	} {
		body, err := Parse(tc.frame)
		if err != nil {
			t.Fatal(err)
		}
		// [0x23][seq][cmd] and one zero of padding: the app's packet is 4 bytes, and a
		// 3-byte one was ignored by a real strap.
		if len(body) != 4 || body[0] != TypeCommand || body[2] != tc.cmd || body[3] != 0 {
			t.Errorf("%s body = %x", name, body)
		}
		if IsBlocked(tc.cmd) {
			t.Errorf("%s is on the block list", name)
		}
	}
}

func TestEveryCommandBodyIsAlignedToFourBytes(t *testing.T) {
	frames := map[string][]byte{
		"realtime on":  RealtimeHR(1, true),
		"realtime off": RealtimeHR(2, false),
		"set clock":    SetClock(3, time.Unix(1700000000, 123)),
		"start":        StartHistory(4),
		"abort":        AbortHistory(5),
		"ack":          HistoryAck(6, [8]byte{1, 2, 3, 4, 5, 6, 7, 8}),
		"nack":         HistoryNack(7),
	}
	for name, f := range frames {
		body, err := Parse(f)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(body)%4 != 0 {
			t.Errorf("%s body is %d bytes: not a multiple of 4", name, len(body))
		}
	}
	// The fixtures captured from the official app obey the same rule.
	for _, s := range capturedCommands {
		if body, _ := Parse(unhex(t, s)); len(body)%4 != 0 {
			t.Errorf("a command from the real app is %d bytes: the rule is wrong", len(body))
		}
	}
}

// consoleLog builds a console-log packet (type 0x32), the kind a real strap
// sent in its first history burst: no heart-rate record in it at all.
func consoleLog(n int) []byte {
	b := []byte{TypeConsoleLog, 0x4b, 0x02, 0x00}
	return append(b, []byte("log line "+string(rune('A'+n%26)))...)
}

// The first burst a real strap sent: a start marker, 54 console-log packets,
// then an end marker announcing 54 with token 05d9000002000000. It was sent 511
// times because the old engine counted only 0x2F records and never acknowledged.
func TestARealStrapsFirstBurstIsAllConsoleLogsAndMustBeAcknowledged(t *testing.T) {
	realStartData := unhex(t, "06000000100000000200000029000000110000000400000000000000080600")[:15]
	realToken := [8]byte{0x05, 0xd9, 0, 0, 0x02, 0, 0, 0}
	sink := &recSink{}
	h := NewHistorySync(sink, 0x40)

	h.Feed(meta(MetaHistoryStart, realStartData...))
	for i := 0; i < 54; i++ {
		if r, err := h.Feed(consoleLog(i)); r != nil || err != nil {
			t.Fatalf("console log %d: %x %v", i, r, err)
		}
		if i == 20 { // the live stream keeps flowing on the same channel; it is not part of the burst
			h.Feed(unhex2(t, "2802035ce101f8235c0000000000000000000101"))
		}
	}
	reply, err := h.Feed(end(54, realToken))
	if err != nil || reply == nil {
		t.Fatalf("a 54-packet burst announced as 54 was not acknowledged: reply=%x err=%v", reply, err)
	}
	body, _ := Parse(reply)
	if body[2] != CmdHistoryResult || body[3] != 1 || !bytes.Equal(body[4:12], realToken[:]) {
		t.Fatalf("reply = %x, want status 1 and the token echoed", body)
	}
	if len(sink.bursts) != 1 || len(sink.bursts[0].Records) != 54 {
		t.Fatalf("stored %d bursts, %d packets", len(sink.bursts), len(sink.bursts[0].Records))
	}
	if h.Bursts != 1 || h.Stored != 54 {
		t.Errorf("counters %d %d", h.Bursts, h.Stored)
	}
	// A burst missing a packet is still refused (never acknowledged short).
	h.Feed(meta(MetaHistoryStart))
	for i := 0; i < 53; i++ {
		h.Feed(consoleLog(i))
	}
	reply, _ = h.Feed(end(54, realToken))
	nack, _ := Parse(reply)
	if nack[3] != 0 {
		t.Errorf("a short burst got status %d, want a nack", nack[3])
	}
}

// unhex2 is unhex for a body (not a frame).
func unhex2(t *testing.T, s string) []byte { return unhex(t, s) }
