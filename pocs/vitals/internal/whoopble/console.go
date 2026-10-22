package whoopble

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The strap writes its firmware console to the phone as TypeConsoleLog (0x32)
// packets. Each body is a 13-byte header (type, counter, two bytes, u32 unix
// seconds at [4:8), u16 sub-second ticks at [8:10), a length and a flag) and up
// to 51 bytes of ASCII, so one log line is usually split across packets. Lines
// look like "10, 14925135: BLE: PullStats: ..." (module, uptime ms, text).

const consoleTextOffset = 13

// ConsoleLine is one reassembled log line with the time its first packet was written.
type ConsoleLine struct {
	Time time.Time
	Text string
}

// ConsoleLines reassembles console packets, given in the order received, into
// lines. Bodies that are not console packets are skipped.
func ConsoleLines(bodies [][]byte) []ConsoleLine {
	var text []byte
	type mark struct {
		off int
		t   time.Time
	}
	var marks []mark
	for _, b := range bodies {
		if len(b) <= consoleTextOffset || b[0] != TypeConsoleLog {
			continue
		}
		marks = append(marks, mark{len(text), exactTime(b[4:8], b[8:10])})
		text = append(text, bytes.ReplaceAll(b[consoleTextOffset:], []byte{0}, nil)...)
	}
	var out []ConsoleLine
	pos := 0
	for _, raw := range bytes.Split(text, []byte("\n")) {
		start := pos
		pos += len(raw) + 1
		line := strings.TrimSpace(string(raw))
		if line == "" {
			continue
		}
		t := marks[0].t
		for _, m := range marks {
			if m.off <= start {
				t = m.t
			}
		}
		out = append(out, ConsoleLine{Time: t, Text: line})
	}
	return out
}

var (
	socRE       = regexp.MustCompile(`SUPERVISOR: SOC report: ([0-9.]+)`)
	sleepFlagRE = regexp.MustCompile(`SLEEPFLAG: moving from state '([A-Z]+)' to '([A-Z]+)'`)
)

// BatterySOC reads the battery state of charge, in percent, from a
// "SUPERVISOR: SOC report" line. The strap logs one about every ten minutes.
func BatterySOC(line string) (float64, bool) {
	m := socRE.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	return v, err == nil
}

// SleepTransition reads a "SLEEPFLAG: moving from state 'A' to 'B'" line: the
// strap's own sleep-state machine announcing a change. The packet time is a few
// seconds after the change (the matching records change first).
func SleepTransition(line string) (from, to string, ok bool) {
	m := sleepFlagRE.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}
