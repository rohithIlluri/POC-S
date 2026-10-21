// Package whoopble speaks the WHOOP 4.0 strap's own Bluetooth protocol,
// enough to stream live heart rate and beat-to-beat (RR) intervals straight
// from the strap — no WHOOP app, account or membership involved.
//
// The format was worked out from the packet captures published in
// "Reverse Engineering Whoop 4.0 for fun and FREEDOM"
// (github.com/bWanShiTong/reverse-engineering-whoop-post). Both checksums
// below were derived from those captures, and the tests check every
// decoder against them byte for byte. No code from other WHOOP projects is
// included.
//
// Frame layout (all integers little-endian):
//
//	0xAA | length u16 | crc8(length bytes) | body … | crc32(body) u32
//
// length counts the body plus the 4-byte CRC-32. The header CRC is
// CRC-8/SMBUS (poly 0x07, init 0); the trailer is standard CRC-32 (IEEE).
// The body starts with a packet type; commands to the strap are
//
//	0x23 | sequence | command | payload …
//
// Safety: the only commands this package can build are RealtimeHR on/off,
// which change nothing on the strap that outlives the connection. The
// generic builder is unexported, so nothing outside can construct the
// strap's erase, trim, reboot or persistent-setting commands.
package whoopble

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"time"
)

// GATT identifiers of the WHOOP custom service.
const (
	ServiceUUID         = "61080001-8d6d-82b8-614a-1c8cb0f8dcc6"
	CmdToStrapUUID      = "61080002-8d6d-82b8-614a-1c8cb0f8dcc6"
	CmdFromStrapUUID    = "61080003-8d6d-82b8-614a-1c8cb0f8dcc6"
	EventsFromStrapUUID = "61080004-8d6d-82b8-614a-1c8cb0f8dcc6"
	DataFromStrapUUID   = "61080005-8d6d-82b8-614a-1c8cb0f8dcc6"
)

const sof = 0xAA

// Packet types (first body byte).
const (
	TypeCommand  = 0x23
	TypeRealtime = 0x28 // 1 Hz live heart rate + RR while realtime is on
	TypeHistory  = 0x2F // 1 Hz stored records during a history sync
	TypeMetadata = 0x31 // e.g. end-of-batch markers during a sync
)

// Commands (third body byte of a TypeCommand packet) this package builds.
const (
	CmdToggleRealtimeHR = 0x03 // payload 01 = start, 00 = stop; not persisted
)

// ErrShort means more bytes are needed before a frame can be read.
var ErrShort = errors.New("whoopble: incomplete frame")

// CRC8 is CRC-8/SMBUS (poly 0x07, init 0, no reflection), used on the two
// length bytes of the header.
func CRC8(data []byte) byte {
	var c byte
	for _, b := range data {
		c ^= b
		for i := 0; i < 8; i++ {
			if c&0x80 != 0 {
				c = c<<1 ^ 0x07
			} else {
				c <<= 1
			}
		}
	}
	return c
}

// Frame wraps a body in the header and trailer.
func Frame(body []byte) []byte {
	out := make([]byte, 4, 4+len(body)+4)
	out[0] = sof
	binary.LittleEndian.PutUint16(out[1:3], uint16(len(body)+4))
	out[3] = CRC8(out[1:3])
	out = append(out, body...)
	return binary.LittleEndian.AppendUint32(out, crc32.ChecksumIEEE(body))
}

// command builds a framed command packet. It is unexported on purpose:
// outside this package, only the safe builders below can produce commands.
// The strap doesn't check the sequence number, but callers should still
// increment it like the app does.
func command(seq byte, cmd byte, payload ...byte) []byte {
	return Frame(append([]byte{TypeCommand, seq, cmd}, payload...))
}

// RealtimeHR starts (on) or stops the 1 Hz realtime heart-rate stream.
func RealtimeHR(seq byte, on bool) []byte {
	var v byte
	if on {
		v = 1
	}
	return command(seq, CmdToggleRealtimeHR, v)
}

// Parse validates one complete frame and returns its body. A frame that is
// merely incomplete returns ErrShort.
func Parse(frame []byte) ([]byte, error) {
	if len(frame) < 4 {
		return nil, ErrShort
	}
	if frame[0] != sof {
		return nil, fmt.Errorf("whoopble: bad start byte %#x", frame[0])
	}
	if CRC8(frame[1:3]) != frame[3] {
		return nil, errors.New("whoopble: header checksum mismatch")
	}
	n := int(binary.LittleEndian.Uint16(frame[1:3]))
	if n < 5 {
		return nil, fmt.Errorf("whoopble: implausible length %d", n)
	}
	if len(frame) < 4+n {
		return nil, ErrShort
	}
	body := frame[4 : 4+n-4]
	if crc32.ChecksumIEEE(body) != binary.LittleEndian.Uint32(frame[4+n-4:4+n]) {
		return nil, errors.New("whoopble: body checksum mismatch")
	}
	return body, nil
}

// Reassembler turns a stream of BLE notifications — which can split a frame
// across several notifications, or carry several frames in one — into
// validated frame bodies. Garbage between frames is skipped.
type Reassembler struct {
	buf     []byte
	Dropped int // frames discarded for bad checksums
}

// Write adds notification bytes and returns any bodies now complete.
func (r *Reassembler) Write(p []byte) [][]byte {
	r.buf = append(r.buf, p...)
	var out [][]byte
	for {
		i := indexByte(r.buf, sof)
		if i < 0 {
			r.buf = r.buf[:0]
			return out
		}
		r.buf = r.buf[i:]
		body, err := Parse(r.buf)
		switch {
		case errors.Is(err, ErrShort):
			return out
		case err != nil:
			r.Dropped++
			r.buf = r.buf[1:] // resync on the next start byte
		default:
			out = append(out, append([]byte(nil), body...))
			r.buf = r.buf[4+len(body)+4:]
		}
	}
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

// Sample is one second of heart data from the strap.
type Sample struct {
	Time time.Time
	HR   int
	RR   []int // beat-to-beat intervals in ms, 0–4 per second
}

const (
	minRRms = 300
	maxRRms = 2000
)

// rr reads count u16 intervals starting at off, keeping plausible ones.
func rr(body []byte, countOff, valuesOff, slots int) []int {
	if len(body) <= countOff {
		return nil
	}
	n := int(body[countOff])
	if n > slots {
		return nil // a corrupted count; don't trust any of it
	}
	var out []int
	for i := 0; i < n; i++ {
		o := valuesOff + 2*i
		if o+2 > len(body) {
			break
		}
		if v := int(binary.LittleEndian.Uint16(body[o:])); v >= minRRms && v <= maxRRms {
			out = append(out, v)
		}
	}
	return out
}

// DecodeRealtime reads a TypeRealtime body:
//
//	[0] 0x28 [1] ? [2:6] unix seconds [6:8] sub-second [8] HR [9] RR count [10:18] 4 × RR u16 ms
func DecodeRealtime(body []byte) (Sample, bool) {
	if len(body) < 18 || body[0] != TypeRealtime {
		return Sample{}, false
	}
	return Sample{
		Time: time.Unix(int64(binary.LittleEndian.Uint32(body[2:6])), 0),
		HR:   int(body[8]),
		RR:   rr(body, 9, 10, 4),
	}, true
}

// DecodeHistory reads the heart-rate part of a TypeHistory record (the rest
// of the 92-byte body holds sensor data that isn't decoded here):
//
//	[0] 0x2F [1] ? [2] record kind [3:7] record number [7:11] unix seconds [11:17] ? [17] HR [18] RR count [19:27] 4 × RR u16 ms
func DecodeHistory(body []byte) (rec Sample, number uint32, ok bool) {
	if len(body) < 27 || body[0] != TypeHistory {
		return Sample{}, 0, false
	}
	return Sample{
		Time: time.Unix(int64(binary.LittleEndian.Uint32(body[7:11])), 0),
		HR:   int(body[17]),
		RR:   rr(body, 18, 19, 4),
	}, binary.LittleEndian.Uint32(body[3:7]), true
}
