package whoopble

import (
	"encoding/binary"
	"time"
)

// Results a strap reports in a command response.
const (
	ResultFailure     = 0
	ResultSuccess     = 1
	ResultPending     = 2 // seen when realtime HR was switched on
	ResultUnsupported = 3
)

// CommandResponse is the strap's answer to a command (TypeCommandResponse):
//
//	[0] 0x24 [1] seq [2] command [3] sequence of the command answered [4] result [5:] data
type CommandResponse struct {
	Seq       byte
	Cmd       byte
	OriginSeq byte
	Result    byte
	Data      []byte
}

// DecodeCommandResponse reads a TypeCommandResponse body.
func DecodeCommandResponse(body []byte) (CommandResponse, bool) {
	if len(body) < 5 || body[0] != TypeCommandResponse {
		return CommandResponse{}, false
	}
	return CommandResponse{
		Seq:       body[1],
		Cmd:       body[2],
		OriginSeq: body[3],
		Result:    body[4],
		Data:      body[5:],
	}, true
}

// Event numbers seen in captures from a WHOOP 4.0 (u16 at body[2] of a
// TypeEvent packet). The official app knows many more.
const (
	EventBatteryLevel      = 3
	EventWristOn           = 9
	EventRTCLost           = 13
	EventFlashInitComplete = 28
	EventRealtimeHROn      = 33
)

// Event is one housekeeping packet from the strap (TypeEvent):
//
//	[0] 0x30 [1] counter [2:4] event number [4:8] unix seconds [8:10] sub-second ticks [10:] event data
type Event struct {
	Number uint16
	Time   time.Time // on the strap's own clock, which may be unset
	Data   []byte
}

// DecodeEvent reads a TypeEvent body.
func DecodeEvent(body []byte) (Event, bool) {
	if len(body) < 10 || body[0] != TypeEvent {
		return Event{}, false
	}
	sub := int64(binary.LittleEndian.Uint16(body[8:10]))
	return Event{
		Number: binary.LittleEndian.Uint16(body[2:4]),
		Time:   time.Unix(int64(binary.LittleEndian.Uint32(body[4:8])), sub*1e9/ClockTicksPerSecond),
		Data:   body[10:],
	}, true
}

// ClockUnset reports whether a strap timestamp is obviously wrong. A strap
// that lost its clock counts from 1971-01-01 at power-on, so anything before
// 2020 means SetClock hasn't been sent since it last booted.
func ClockUnset(t time.Time) bool {
	return t.Year() < 2020
}
