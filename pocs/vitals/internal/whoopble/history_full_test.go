package whoopble

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

// bodyV24 builds a version-24 / kind-5 heart record body the way the strap lays it out.
func bodyV24(number, sec uint32, sub uint16, hr byte, g [3]float32, movement float32, state, flags byte, rrs ...uint16) []byte {
	b := make([]byte, 96)
	b[0], b[1], b[2] = TypeHistory, 24, 5
	binary.LittleEndian.PutUint32(b[3:], number)
	binary.LittleEndian.PutUint32(b[7:], sec)
	binary.LittleEndian.PutUint16(b[11:], sub)
	b[17], b[18] = hr, byte(len(rrs))
	for i, v := range rrs {
		binary.LittleEndian.PutUint16(b[19+2*i:], v)
	}
	binary.LittleEndian.PutUint32(b[32:], math.Float32bits(movement))
	for i, v := range g {
		binary.LittleEndian.PutUint32(b[36+4*i:], math.Float32bits(v))
		binary.LittleEndian.PutUint32(b[52+4*i:], math.Float32bits(v)) // the strap repeats the vector
	}
	b[80] = state<<4 | flags
	return b
}

func TestDecodeHistoryFull(t *testing.T) {
	g := [3]float32{-0.25, 0.5, 0.82}
	b := bodyV24(2956521, 1792635141, 16384, 76, g, 0.0123, 2, 1, 812, 790)
	r, ok := DecodeHistoryFull(b)
	if !ok {
		t.Fatal("not decoded")
	}
	if r.Number != 2956521 || r.HR != 76 || len(r.RR) != 2 || r.RR[0] != 812 {
		t.Errorf("record %+v", r)
	}
	if r.Gravity != g || r.Movement != 0.0123 {
		t.Errorf("motion fields %+v", r)
	}
	if r.Sleep != StateSleep || r.Flags != 1 || r.Sleep.String() != "sleep" {
		t.Errorf("state %v flags %d", r.Sleep, r.Flags)
	}
	// 16384 ticks of 1/32768 s is exactly half a second.
	if want := time.Unix(1792635141, 500_000_000); !r.Time.Equal(want) {
		t.Errorf("time %v, want %v", r.Time, want)
	}
}

func TestTwoRecordsInTheSameWholeSecondHaveDifferentExactTimes(t *testing.T) {
	// The strap writes a record every 0.9614 s, so the same whole second
	// appears twice about every 25 s. The sub-second counter tells them apart.
	a, _ := DecodeHistoryFull(bodyV24(1, 1792635141, 3000, 70, [3]float32{}, 0, 0, 0))
	b, _ := DecodeHistoryFull(bodyV24(2, 1792635141, 31500, 71, [3]float32{}, 0, 0, 0))
	if a.Time.Equal(b.Time) || !b.Time.After(a.Time) {
		t.Errorf("times %v and %v", a.Time, b.Time)
	}
}

func TestDecodeHistoryFullOnlyReadsTheStrapsOwnLayout(t *testing.T) {
	v12 := bodyV24(1, 1792635141, 0, 70, [3]float32{}, 0, 0, 0)
	v12[1], v12[2] = 12, 7
	if _, ok := DecodeHistoryFull(v12); ok {
		t.Error("a version 12 record has no verified motion fields")
	}
	sensor := bodyV24(1, 1792635141, 0, 3, [3]float32{}, 0, 0, 0)
	sensor[1], sensor[2] = 25, 0
	if _, ok := DecodeHistoryFull(sensor); ok {
		t.Error("a pulse record is not a heart record")
	}
	if _, ok := DecodeHistoryFull(bodyV24(1, 1792635141, 0, 70, [3]float32{}, 0, 0, 0)[:80]); ok {
		t.Error("a body too short for byte 80 must not decode")
	}
}

func TestDecodePulse(t *testing.T) {
	b := make([]byte, 76)
	b[0], b[1], b[2] = TypeHistory, 25, 0
	binary.LittleEndian.PutUint32(b[3:], 3682)
	binary.LittleEndian.PutUint32(b[7:], 1792635165)
	binary.LittleEndian.PutUint16(b[11:], 29768)
	binary.LittleEndian.PutUint32(b[15:], 170183)
	want := []int16{-419, -459, -499, 138, 1382}
	for i, v := range want {
		binary.LittleEndian.PutUint16(b[19+2*i:], uint16(v))
	}
	p, ok := DecodePulse(b)
	if !ok {
		t.Fatal("not decoded")
	}
	if p.Number != 3682 || p.Level != 170183 {
		t.Errorf("pulse %+v", p)
	}
	for i, v := range want {
		if p.Samples[i] != v {
			t.Errorf("sample %d = %d, want %d", i, p.Samples[i], v)
		}
	}
	b[1] = 24
	if _, ok := DecodePulse(b); ok {
		t.Error("a heart record is not a pulse record")
	}
}

func TestDecodeBattery(t *testing.T) {
	b := make([]byte, 32)
	b[0] = TypeEvent
	binary.LittleEndian.PutUint16(b[2:], EventBattery)
	binary.LittleEndian.PutUint32(b[4:], 1792672506)
	binary.LittleEndian.PutUint16(b[10:], 20) // payload length
	p := b[12:]
	p[0] = 2
	binary.LittleEndian.PutUint16(p[1:], 339)  // 33.9 %
	binary.LittleEndian.PutUint16(p[5:], 3789) // mV
	binary.LittleEndian.PutUint16(p[15:], 292) // 29.2 C
	got, ok := DecodeBattery(b)
	if !ok {
		t.Fatal("not decoded")
	}
	if got.Percent != 33.9 || got.Millivolts != 3789 || got.TempC != 29.2 || got.Time.Unix() != 1792672506 {
		t.Errorf("battery %+v", got)
	}
	binary.LittleEndian.PutUint16(b[2:], 63)
	if _, ok := DecodeBattery(b); ok {
		t.Error("a different event must not decode as battery")
	}
}

func consolePacket(counter byte, sec uint32, text string) []byte {
	b := make([]byte, consoleTextOffset+len(text))
	b[0], b[1] = TypeConsoleLog, counter
	binary.LittleEndian.PutUint32(b[4:], sec)
	copy(b[consoleTextOffset:], text)
	return b
}

func TestConsoleLinesAreReassembledAcrossPackets(t *testing.T) {
	bodies := [][]byte{
		consolePacket(1, 1000, "\n 10, 41, SUPERVISOR: SOC re"),
		consolePacket(2, 1001, "port: 46.21\n 10, 42: SLEEPFLAG: moving from state 'STILL' to 'SLEEP', cnt = 21\n 10, 4"),
		consolePacket(3, 1002, "3: BLE: Command Send Historical Data\x00\x00"),
		{TypeEvent, 0, 0, 0}, // not a console packet
	}
	lines := ConsoleLines(bodies)
	if len(lines) != 3 {
		t.Fatalf("%d lines: %+v", len(lines), lines)
	}
	if v, ok := BatterySOC(lines[0].Text); !ok || v != 46.21 {
		t.Errorf("SOC %v %v from %q", v, ok, lines[0].Text)
	}
	if lines[0].Time.Unix() != 1000 {
		t.Errorf("a line takes the time of the packet it starts in: %v", lines[0].Time)
	}
	if from, to, ok := SleepTransition(lines[1].Text); !ok || from != "STILL" || to != "SLEEP" {
		t.Errorf("transition %q %q %v", from, to, ok)
	}
	if lines[2].Text != "10, 43: BLE: Command Send Historical Data" {
		t.Errorf("last line %q", lines[2].Text)
	}
	if _, ok := BatterySOC("BLE: nothing here"); ok {
		t.Error("an unrelated line is not a SOC report")
	}
}
