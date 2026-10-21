package whoopble

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"
)

// Real packets captured from a WHOOP 4.0, as published in the
// reverse-engineering write-up this package is based on.
var (
	capturedCommands = []string{
		"aa0800a823070e00c7e40f08", // broadcast HR off
		"aa0800a823080e016c935474", // broadcast HR on
		"aa0800a8238c03017d5ec627", // realtime HR start
		"aa0800a8238d0300dc040351", // realtime HR stop
		"aa100057236d4201d036656600000000f62deb81",
	}
	capturedRealtime = []string{
		"aa1800ff2802ad896566f0654201670600000000000001013ba00d4d",
		"aa1800ff2802bd896566b016440000000000000000000101" + "3b07bb4b",
	}
	// Each history record was printed in two halves in the write-up; these
	// are the halves re-paired by matching the CRC-32.
	capturedHistory = []string{
		"aa5c00f02f0c078bb70900c8326966e03c8054cc015801b9020000000000000000f96dff00a7e73b140eafbe298c5b3e715d753f00001046140eafbe298c5b3e715d753fe50146022003470282016004010c020c3000000000000002833a28fd",
		"aa5c00f02f0c078cb70900c9326966f0378054cc015801b5020000000000000000a16eff0070b43b1fedaebe3d9a5b3ecd34753f00005ac61fedaebe3d9a5b3ecd34753fe50146022003450282016004010c020c3000000000000002149f6d4f",
		"aa5c00f02f0c078db70900ca326966f8328054cc015802b802b9020000000000008b63ff0041a43b14e6aebea4b05b3e4819753f0000c0c514e6aebea4b05b3e4819753fe50146021e03450282016004010c020c3000000000000002a4d38c63",
		"aa5c00f02f0c078eb70900cb326966082e8054cc015801ce0200000000000000004661ff001bb03b8fcaaebe1fd55b3e0008753f0000b2c68fcaaebe1fd55b3e0008753fe50146021d03440287016004010c020c30000000000000022891f236",
		"aa5c00f02f0c078fb70900cc32696610298054cc015801c1020000000000000000d161ff00f3a43ba480aebe859b5b3ed7e7743f0000e046a480aebe859b5b3ed7e7743fe50146021d03450287016004010c020c3000000000000002c197f209",
		"aa5c00f02f0c0790b70900cd32696620248054cc015802df02d302000000000000f961ff0041fd3b7bb4aebeae475b3e1fd9743f000082467bb4aebeae475b3e1fd9743fe50146021d03440287016004010c020c3000000000000002aec41896",
		"aa5c00f02f0c0791b70900ce326966281f8054cc015701f8020000000000000000a16aff0061c63b48a1aebe3d0a5c3e66aa743f0000d84548a1aebe3d0a5c3e66aa743fe50146021c03440287016004010c020c3000000000000002dfafea59",
		"aa5c00f02f0c0792b70900cf326966381a8054cc015701fb0200000000000000008c6aff00c8b43bec71aebe33c35b3e9ac1743f000050c5ec71aebe33c35b3e9ac1743fe50146021c03440287016004010c020c300000000000000276ae1006",
	}
	capturedMetadata = "aa1c00ab311802f65c70668040430000002e470100040000000000007f873cf3"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestChecksumsMatchCapturedPackets(t *testing.T) {
	all := append(append(append(append([]string{}, capturedCommands...), capturedRealtime...), capturedHistory...), capturedMetadata)
	for _, s := range all {
		frame := unhex(t, s)
		body, err := Parse(frame)
		if err != nil {
			t.Fatalf("%s…: %v", s[:16], err)
		}
		if got := Frame(body); !bytes.Equal(got, frame) {
			t.Errorf("re-framing %s… gave %x", s[:16], got)
		}
	}
}

func TestBuildersReproduceTheAppsBytes(t *testing.T) {
	if got := hex.EncodeToString(RealtimeHR(0x8c, true)); got != "aa0800a8238c03017d5ec627" {
		t.Errorf("realtime on = %s", got)
	}
	if got := hex.EncodeToString(RealtimeHR(0x8d, false)); got != "aa0800a8238d0300dc040351" {
		t.Errorf("realtime off = %s", got)
	}
}

func TestParseRejectsCorruption(t *testing.T) {
	good := unhex(t, capturedCommands[0])
	for i := range good {
		bad := append([]byte(nil), good...)
		bad[i] ^= 0x40
		if _, err := Parse(bad); err == nil {
			t.Errorf("flipping byte %d went unnoticed", i)
		}
	}
	if _, err := Parse(good[:7]); err != ErrShort {
		t.Errorf("truncated frame: %v", err)
	}
}

func TestDecodeRealtime(t *testing.T) {
	body, _ := Parse(unhex(t, capturedRealtime[1]))
	s, ok := DecodeRealtime(body)
	if !ok || s.HR != 0x44 || len(s.RR) != 0 || !s.Time.Equal(time.Unix(0x666589bd, 0)) {
		t.Fatalf("decoded %+v", s)
	}
	// The first capture claims one RR of 1639 ms at 66 bpm (~909 ms): an
	// implausible start-of-stream value that must not be trusted blindly.
	body, _ = Parse(unhex(t, capturedRealtime[0]))
	if s, _ := DecodeRealtime(body); s.HR != 66 || len(s.RR) != 1 || s.RR[0] != 1639 {
		t.Fatalf("decoded %+v", s)
	}
}

func TestDecodeHistory(t *testing.T) {
	var prev uint32
	for i, h := range capturedHistory {
		body, _ := Parse(unhex(t, h))
		s, n, ok := DecodeHistory(body)
		if !ok || s.HR < 80 || s.HR > 95 || len(s.RR) == 0 {
			t.Fatalf("record %d: %+v", i, s)
		}
		for _, v := range s.RR {
			// RR must agree with the heart rate within ~20%.
			if expected := 60000 / s.HR; v < expected*8/10 || v > expected*12/10 {
				t.Errorf("record %d: RR %d ms disagrees with %d bpm", i, v, s.HR)
			}
		}
		if i > 0 && (n != prev+1 || !s.Time.Equal(time.Unix(0x666932c8+int64(i), 0))) {
			t.Errorf("record %d: number %d after %d, time %v", i, n, prev, s.Time)
		}
		prev = n
	}
}

func TestReassemblerHandlesSplitsAndGarbage(t *testing.T) {
	var stream []byte
	stream = append(stream, 0x00, 0xAA, 0x13) // noise, including a fake start byte
	for _, h := range capturedHistory[:3] {
		stream = append(stream, unhex(t, h)...)
	}
	stream = append(stream, unhex(t, capturedRealtime[1])...)

	// Deliver in awkward 20-byte notifications (the default BLE MTU payload).
	var r Reassembler
	var bodies [][]byte
	for len(stream) > 0 {
		n := min(20, len(stream))
		bodies = append(bodies, r.Write(stream[:n])...)
		stream = stream[n:]
	}
	if len(bodies) != 4 || bodies[0][0] != TypeHistory || bodies[3][0] != TypeRealtime {
		t.Fatalf("got %d bodies", len(bodies))
	}
}
