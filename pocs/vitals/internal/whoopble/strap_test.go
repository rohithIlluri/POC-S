package whoopble

import (
	"encoding/binary"
	"testing"
	"time"
)

// Frames captured from a WHOOP 4.0 over vitals' own connection. They hold no
// identifiers: just the strap's clock, a heart rate and a command answer.
const (
	strapEventRTCLost    = "aa10005730090d009433e101487f0000b2fe7b32"
	strapEventFlashInit  = "aa100057300a1c009433e101487f00006572f563"
	strapEventWristOn    = "aa100057300e09009533e10160750000d24910b6"
	strapEventRealtimeOn = "aa10005730df2100bd3fe101984f0000147056a3"
	strapResponse        = "aa0c00fc2401030102000000e42b94af" // answer to RealtimeHR(1, true)
	strapRealtimeTwoRR   = "aa1800ff2802be3fe101304761025b026802000000000101950d86a0"
	strapRealtimeNoRR    = "aa1800ff2802c23fe1016033600000000000000000000101e8370b10"
)

func TestDecodeCommandResponseFromStrap(t *testing.T) {
	body, err := Parse(unhex(t, strapResponse))
	if err != nil {
		t.Fatal(err)
	}
	r, ok := DecodeCommandResponse(body)
	if !ok || r.Cmd != CmdToggleRealtimeHR || r.OriginSeq != 1 || r.Result != ResultPending || len(r.Data) != 3 {
		t.Fatalf("decoded %+v", r)
	}
	if _, ok := DecodeCommandResponse(body[:4]); ok {
		t.Error("accepted a truncated response")
	}
	if _, ok := DecodeCommandResponse(append([]byte{TypeRealtime}, body[1:]...)); ok {
		t.Error("accepted the wrong packet type")
	}
}

func TestDecodeEventsFromStrap(t *testing.T) {
	for _, tc := range []struct {
		frame  string
		number uint16
		unix   int64
		ticks  int64
	}{
		{strapEventRTCLost, EventRTCLost, 0x01e13394, 0x7f48},
		{strapEventFlashInit, EventFlashInitComplete, 0x01e13394, 0x7f48},
		{strapEventWristOn, EventWristOn, 0x01e13395, 0x7560},
		{strapEventRealtimeOn, EventRealtimeHROn, 0x01e13fbd, 0x4f98},
	} {
		body, err := Parse(unhex(t, tc.frame))
		if err != nil {
			t.Fatal(err)
		}
		e, ok := DecodeEvent(body)
		if !ok || e.Number != tc.number {
			t.Fatalf("%s: decoded %+v", tc.frame[:16], e)
		}
		want := time.Unix(tc.unix, tc.ticks*1e9/ClockTicksPerSecond)
		if !e.Time.Equal(want) {
			t.Errorf("event %d time = %v, want %v", tc.number, e.Time, want)
		}
		// This strap had not been given the time: it counted from 1971-01-01.
		if !ClockUnset(e.Time) {
			t.Errorf("event %d: clock %v not flagged as unset", tc.number, e.Time)
		}
	}
	if ClockUnset(time.Now()) {
		t.Error("a real clock was flagged as unset")
	}
}

func TestDecodeRealtimeFromStrap(t *testing.T) {
	body, _ := Parse(unhex(t, strapRealtimeTwoRR))
	s, ok := DecodeRealtime(body)
	if !ok || s.HR != 97 || len(s.RR) != 2 || s.RR[0] != 603 || s.RR[1] != 616 || !s.Time.Equal(time.Unix(0x01e13fbe, 0)) {
		t.Fatalf("decoded %+v", s)
	}
	// Most realtime packets carry no RR at all.
	body, _ = Parse(unhex(t, strapRealtimeNoRR))
	if s, ok := DecodeRealtime(body); !ok || s.HR != 96 || len(s.RR) != 0 {
		t.Fatalf("decoded %+v", s)
	}
}

func TestSetClockLayout(t *testing.T) {
	frame := SetClock(7, time.Unix(1700000000, 500_000_000))
	body, err := Parse(frame)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 12 || body[0] != TypeCommand || body[1] != 7 || body[2] != CmdSetClock || body[11] != 0 {
		t.Fatalf("body = %x", body)
	}
	if got := binary.LittleEndian.Uint32(body[3:7]); got != 1700000000 {
		t.Errorf("seconds = %d", got)
	}
	// Half a second is half of 32768 ticks.
	if got := binary.LittleEndian.Uint32(body[7:11]); got != ClockTicksPerSecond/2 {
		t.Errorf("ticks = %d", got)
	}
}

func TestDangerousCommandsCannotBeBuilt(t *testing.T) {
	if len(blockedCommands) == 0 {
		t.Fatal("empty block list")
	}
	for cmd := range blockedCommands {
		if !IsBlocked(cmd) {
			t.Errorf("IsBlocked(%#x) = false", cmd)
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("command(%#x) built a blocked opcode", cmd)
				}
			}()
			command(1, cmd)
		}()
	}
	// The commands the package does offer are not on the list.
	for _, cmd := range []byte{CmdToggleRealtimeHR, CmdSetClock} {
		if IsBlocked(cmd) {
			t.Errorf("offered command %#x is blocked", cmd)
		}
	}
}

func TestRealtimePacketSaysWhereAndWhetherItIsWorn(t *testing.T) {
	body, _ := Parse(unhex(t, strapRealtimeTwoRR))
	smp, ok := DecodeRealtime(body)
	if !ok || smp.OffWrist || smp.Location != LocationWrist {
		t.Fatalf("a strap worn on the wrist decoded as %+v", smp)
	}
	// The same packet with the flag byte cleared is what the app treats as off-wrist.
	off := append([]byte(nil), body...)
	off[18] = 0
	if smp, _ := DecodeRealtime(off); !smp.OffWrist {
		t.Error("[18] == 0 must read as off-wrist")
	}
	// A short (18-byte) body has no flag: it is not reported as off-wrist.
	if smp, ok := DecodeRealtime(body[:18]); !ok || smp.OffWrist || smp.Location != LocationUnknown {
		t.Errorf("18-byte body: %+v %v", smp, ok)
	}
}
