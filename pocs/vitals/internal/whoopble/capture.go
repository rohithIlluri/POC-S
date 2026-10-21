package whoopble

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
)

// A capture is what the dashboard's "Download capture" button saves: one
// line per BLE notification or command write,
//
//	<ms since connect> TAB <channel> TAB <hex bytes>
//
// where channel is data, cmd, events (from the strap) or write (to it).
// Lines starting with # are comments.

// TypeStats counts the frames of one packet type seen on one channel.
type TypeStats struct {
	Channel string
	Type    byte
	Count   int
	Example string // hex of the first frame body, for eyeballing
}

// CaptureSummary describes a capture file.
type CaptureSummary struct {
	Lines    int
	Frames   int
	Dropped  int // frames that failed a checksum
	Types    []TypeStats
	Realtime struct {
		Samples      int
		MinHR, MaxHR int
		RRIntervals  int
		FirstUnix    int64
		LastUnix     int64
	}
	History struct {
		Records int
	}
}

// SummarizeCapture reassembles every channel of a capture and tallies what
// came through, so a new firmware's behaviour can be read at a glance.
func SummarizeCapture(r io.Reader) (CaptureSummary, error) {
	var s CaptureSummary
	reasm := map[string]*Reassembler{}
	stats := map[string]*TypeStats{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			return s, fmt.Errorf("line %d: want 3 tab-separated fields", s.Lines+1)
		}
		raw, err := hex.DecodeString(parts[2])
		if err != nil {
			return s, fmt.Errorf("line %d: %v", s.Lines+1, err)
		}
		s.Lines++
		ch := parts[1]
		if reasm[ch] == nil {
			reasm[ch] = &Reassembler{}
		}
		for _, body := range reasm[ch].Write(raw) {
			s.Frames++
			key := fmt.Sprintf("%s/%02x", ch, body[0])
			if stats[key] == nil {
				stats[key] = &TypeStats{Channel: ch, Type: body[0], Example: hex.EncodeToString(body)}
			}
			stats[key].Count++
			if ch == "write" {
				continue
			}
			if smp, ok := DecodeRealtime(body); ok {
				rt := &s.Realtime
				if rt.Samples == 0 || smp.HR < rt.MinHR {
					rt.MinHR = smp.HR
				}
				if smp.HR > rt.MaxHR {
					rt.MaxHR = smp.HR
				}
				if rt.Samples == 0 {
					rt.FirstUnix = smp.Time.Unix()
				}
				rt.LastUnix = smp.Time.Unix()
				rt.Samples++
				rt.RRIntervals += len(smp.RR)
			}
			if _, _, ok := DecodeHistory(body); ok {
				s.History.Records++
			}
		}
	}
	if err := sc.Err(); err != nil {
		return s, err
	}
	for _, r := range reasm {
		s.Dropped += r.Dropped
	}
	for _, t := range stats {
		s.Types = append(s.Types, *t)
	}
	sort.Slice(s.Types, func(i, j int) bool {
		if s.Types[i].Channel != s.Types[j].Channel {
			return s.Types[i].Channel < s.Types[j].Channel
		}
		return s.Types[i].Type < s.Types[j].Type
	})
	return s, nil
}
