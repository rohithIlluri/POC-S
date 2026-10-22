package whoopble

import (
	"encoding/binary"
	"math"
	"time"
)

// What the rest of a history record holds. Everything here was read off the
// user's own strap and checked against a full recorded night (see
// ~/whoop-study/whoop-protocol-findings.md, Round 10); nothing is copied from
// WHOOP code. Offsets are into the 0x2F body (CRC removed), little-endian.

// SleepState is the strap's own per-record sleep classification, the high nibble
// of body[80]. The strap's console log names the states of the same machine
// STILL / SLEEP / WAKE / UP, and each change in the records lines up, to about
// five seconds, with the matching log line. It is the strap's label, not
// something vitals computes; the science package scores sleep independently.
type SleepState uint8

const (
	StateWake  SleepState = 0
	StateStill SleepState = 1
	StateSleep SleepState = 2
	StateUp    SleepState = 3
)

func (s SleepState) String() string {
	switch s {
	case StateWake:
		return "wake"
	case StateStill:
		return "still"
	case StateSleep:
		return "sleep"
	case StateUp:
		return "up"
	}
	return "unknown"
}

// SubsecondTicks is the rate of the 16-bit sub-second counter (1/32768 s).
const SubsecondTicks = 32768

// minFullBody is the shortest body DecodeHistoryFull reads (it needs byte 80).
const minFullBody = 81

// HistoryRecord is a version-24 / kind-5 heart record, fully read.
type HistoryRecord struct {
	Number uint32
	// Time is exact: whole seconds plus the sub-second counter. Records are
	// 0.9614 s apart (about 1.04 per second), so two records can share a whole
	// second; keying by Time keeps both.
	Time time.Time
	HR   int   // beats per minute; 0 while the optical sensor has no lock
	RR   []int // beat-to-beat intervals, ms
	// Gravity is the accelerometer's gravity vector in g, one per record. Its
	// length is 1.026 +/- 0.05 over a night and it is stable at rest. Which
	// axis points where on the wrist is not known.
	Gravity [3]float32
	// Movement is a non-negative motion measure (unit not known). It sat at
	// 0.006-0.017 through deep sleep and 0.06-0.09 while awake and moving.
	Movement float32
	// Sleep is the strap's own sleep state (see SleepState).
	Sleep SleepState
	// Flags are the low four bits of body[80]; bit 0 flickers with small
	// movements. Meaning of the bits is not established.
	Flags uint8
}

// DecodeHistoryFull reads a verified heart-record layout (HasHeartLayout) that
// is also the strap's own version 24. Version 12 records, from the public
// samples, have no verified fields beyond HR and RR, so they decode as not ok.
func DecodeHistoryFull(body []byte) (HistoryRecord, bool) {
	if len(body) < minFullBody || body[0] != TypeHistory || body[1] != 24 || body[2] != 5 {
		return HistoryRecord{}, false
	}
	var g [3]float32
	for i := range g {
		g[i] = math.Float32frombits(binary.LittleEndian.Uint32(body[36+4*i:]))
	}
	return HistoryRecord{
		Number:   binary.LittleEndian.Uint32(body[3:7]),
		Time:     exactTime(body[7:11], body[11:13]),
		HR:       int(body[17]),
		RR:       rr(body, 18, 19, 4),
		Gravity:  g,
		Movement: math.Float32frombits(binary.LittleEndian.Uint32(body[32:36])),
		Sleep:    SleepState(body[80] >> 4),
		Flags:    body[80] & 0x0f,
	}, true
}

// exactTime joins a u32 unix-seconds field and a u16 1/32768 s counter.
func exactTime(sec, sub []byte) time.Time {
	ns := int64(binary.LittleEndian.Uint16(sub)) * int64(time.Second) / SubsecondTicks
	return time.Unix(int64(binary.LittleEndian.Uint32(sec)), ns)
}

// PulseSamples is how many samples one sensor record carries.
const PulseSamples = 24

// PulseRate is the sensor stream's sample rate: 24 samples per 0.9614 s record.
const PulseRate = 24.96 // Hz, about 25

// PulseRecord is a version-25 / kind-0 record: a short piece of a 25 Hz optical
// pulse waveform. The strap sends 40 consecutive records (about 38 s) roughly
// every 19.9 minutes. Its dominant frequency matches the heart rate in 29 of 31
// bursts on the recorded night, so it is a heartbeat waveform. Which LED and
// what unit are not known.
type PulseRecord struct {
	Number  uint32
	Time    time.Time
	Samples [PulseSamples]int16
	// Level is a slowly changing u32 that sits beside the waveform (about
	// 155,000-170,000 on the recorded night); likely the signal's DC level.
	Level uint32
}

// DecodePulse reads a version-25 / kind-0 record.
func DecodePulse(body []byte) (PulseRecord, bool) {
	const end = 19 + 2*PulseSamples
	if len(body) < end || body[0] != TypeHistory || body[1] != 25 || body[2] != 0 {
		return PulseRecord{}, false
	}
	p := PulseRecord{
		Number: binary.LittleEndian.Uint32(body[3:7]),
		Time:   exactTime(body[7:11], body[11:13]),
		Level:  binary.LittleEndian.Uint32(body[15:19]),
	}
	for i := range p.Samples {
		p.Samples[i] = int16(binary.LittleEndian.Uint16(body[19+2*i:]))
	}
	return p, true
}

// EventBattery is event number 3 (BATTERY_LEVEL).
const EventBattery = 3

// Battery is a decoded BATTERY_LEVEL event. The strap sends one about every
// 85 s; the state of charge matched the console log's "SOC report" to 0.1 %.
type Battery struct {
	Time       time.Time
	Percent    float64 // state of charge, from tenths of a percent at payload[1:3]
	Millivolts int     // battery voltage, payload[5:7]
	// TempC is payload[15:17] in tenths of a degree. It rose smoothly from 27.8
	// to 29.0 over the night, so it is a slow-moving internal temperature
	// (probably the battery's); which sensor it is has not been established.
	TempC float64
}

// DecodeBattery reads a 0x30 event body whose number is EventBattery. The event
// payload follows a u16 length at body[10:12).
func DecodeBattery(body []byte) (Battery, bool) {
	if len(body) < 12+17 || body[0] != TypeEvent || binary.LittleEndian.Uint16(body[2:4]) != EventBattery {
		return Battery{}, false
	}
	p := body[12:]
	return Battery{
		Time:       exactTime(body[4:8], body[8:10]),
		Percent:    float64(binary.LittleEndian.Uint16(p[1:3])) / 10,
		Millivolts: int(binary.LittleEndian.Uint16(p[5:7])),
		TempC:      float64(binary.LittleEndian.Uint16(p[15:17])) / 10,
	}, true
}
