// Package ingest joins the pieces: it stores what the strap sends (live
// samples and downloaded history) in strapdb, and rolls stored samples up into
// per-day summaries and readiness with the science package.
package ingest

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/science"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/strapdb"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoopble"
)

// HistorySink stores history bursts in the database. It implements
// whoopble.Sink.
//
// Every raw record is stored, whatever its timestamp, so nothing the strap
// holds is lost when it is told it may free that memory. A burst containing a
// record that cannot even be stored raw is refused (an error), so it is never
// acknowledged.
//
// Dating. A strap whose clock was never set stamps records with its boot clock
// (1971-01-01 plus time since power-on). Those records can be placed in real
// time when the strap clock and the wall clock were read together: the offset
// between them is constant until the strap reboots. HaveOffset says that
// reading exists. Records are shifted by it, but only if they are consistent
// with a single boot: not later than the strap's clock now (StrapNowSec), and
// not before an earlier record in the same burst (a drop means a reboot in
// between, so the older ones are on a different boot and stay undated).
type HistorySink struct {
	DB  *strapdb.DB
	Now func() time.Time
	// OffsetSec is wall-clock seconds minus strap-clock seconds, measured when
	// the strap's clock and the wall clock were read together. StrapNowSec is
	// the strap clock at that moment.
	OffsetSec   int64
	StrapNowSec uint32
	HaveOffset  bool
	// Logf, if set, receives one summary line per burst.
	Logf func(format string, args ...any)
	// Stats, if set, accumulates counts across the bursts of one sync.
	Stats *SinkStats
}

// SinkStats counts what a sync found, so the page can say plainly how many
// heart-rate records came back (a sync of nothing but console logs is not a
// sync of your day).
type SinkStats struct {
	HeartRecords int // type 0x2F packets
	Dated        int // of those, how many were placed on the real timeline
	Undated      int // kept raw but could not be dated
	Other        int // console logs, events and the like
}

// ErrUnstorable means a record was too short to keep, so the burst must not be acknowledged.
var ErrUnstorable = errors.New("ingest: a history record is too short to store")

// minValidUnix is the earliest strap timestamp treated as a real date (2020-01-01).
const minValidUnix = 1577836800

// maxRedatedAge bounds how far back a re-dated record may land: a mapping that
// would put data months in the past is a wrong mapping, not old data.
const maxRedatedAge = 60 * 24 * time.Hour

// SaveBurst stores the raw records, the samples decoded from them and the sync
// position in one transaction.
func (s HistorySink) SaveBurst(b whoopble.Burst) error {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	type cand struct {
		sample strapdb.Sample
		old    bool // stamped on the strap's unset clock
	}
	var recs []strapdb.Record
	var other [][]byte
	byType := map[byte]int{}
	var cands []cand
	var last, first uint32
	var undecoded, redated, direct, undated int
	var prevOld uint32
	for _, body := range b.Records {
		if len(body) > 0 && body[0] != whoopble.TypeHistory {
			// Console logs, motion and the like: not heart data, but part of the burst the strap
			// will free on the ack, so they are kept raw.
			other = append(other, body)
			byType[body[0]]++
			continue
		}
		if len(body) < 11 {
			return fmt.Errorf("%w (%d bytes)", ErrUnstorable, len(body))
		}
		number := binary.LittleEndian.Uint32(body[3:7])
		recs = append(recs, strapdb.Record{Number: number, Body: body})
		if first == 0 || number < first {
			first = number
		}
		if number > last {
			last = number
		}
		sample, _, ok := whoopble.DecodeHistory(body)
		if !ok || sample.HR == 0 {
			undecoded++
			continue
		}
		ts := binary.LittleEndian.Uint32(body[7:11])
		rr := make([]uint16, len(sample.RR))
		for i, v := range sample.RR {
			rr[i] = uint16(v)
		}
		smp := strapdb.Sample{Time: sample.Time, HR: uint8(sample.HR), RR: rr, Source: strapdb.History}
		if ts >= minValidUnix {
			cands = append(cands, cand{sample: smp})
			direct++
			continue
		}
		// The strap's unset clock.
		if !s.HaveOffset || ts > s.StrapNowSec {
			undated++
			continue
		}
		if prevOld != 0 && ts < prevOld {
			// The clock went backwards: a reboot. Everything dated so far on the old clock is from an earlier boot.
			kept := cands[:0]
			for _, c := range cands {
				if !c.old {
					kept = append(kept, c)
				} else {
					undated++
					redated--
				}
			}
			cands = kept
		}
		prevOld = ts
		t := time.Unix(int64(ts)+s.OffsetSec, 0)
		if t.After(now().Add(5*time.Minute)) || t.Before(now().Add(-maxRedatedAge)) {
			undated++
			continue
		}
		smp.Time = t
		cands = append(cands, cand{sample: smp, old: true})
		redated++
	}
	if s.Stats != nil {
		s.Stats.HeartRecords += len(recs)
		s.Stats.Dated += len(cands)
		s.Stats.Undated += undated
		s.Stats.Other += len(other)
	}
	samples := make([]strapdb.Sample, 0, len(cands))
	for _, c := range cands {
		samples = append(samples, c.sample)
	}
	if s.Logf != nil {
		var span string
		if len(samples) > 0 {
			lo, hi := samples[0].Time, samples[0].Time
			for _, x := range samples {
				if x.Time.Before(lo) {
					lo = x.Time
				}
				if x.Time.After(hi) {
					hi = x.Time
				}
			}
			span = fmt.Sprintf(", covering %s to %s", lo.Local().Format("15:04:05"), hi.Local().Format("15:04:05"))
		}
		var kinds []string
		for ty, n := range byType {
			kinds = append(kinds, fmt.Sprintf("%#02x x%d", ty, n))
		}
		sort.Strings(kinds)
		s.Logf("history burst: %d of %d packets were heart records (#%d-#%d): %d on a valid clock, %d re-dated from the strap's unset clock, %d could not be dated, %d undecoded; other packets stored raw: %s%s",
			len(recs), len(b.Records), first, last, direct, redated, undated, undecoded, strings.Join(kinds, ", "), span)
	}
	return s.DB.SaveBurst(recs, samples, other, strapdb.SyncState{LastRecord: last, LastToken: b.Token, SyncedAt: now()})
}

// ageOf returns the age in years from the exact birth date when there is one,
// else from the birth year alone.
func ageOf(p strapdb.Profile, now time.Time) (float64, bool) {
	if b, ok := p.BirthTime(); ok {
		return science.AgeAt(b, now), true
	}
	if p.BirthYear > 0 {
		return science.Age(p.BirthYear, now), true
	}
	return 0, false
}

// restTracker finds the lowest sustained heart rate: the lowest mean over a
// 5-minute bucket holding enough samples.
type restTracker struct {
	sum map[int64]float64
	n   map[int64]int
}

func newRestTracker() *restTracker {
	return &restTracker{sum: map[int64]float64{}, n: map[int64]int{}}
}

func (r *restTracker) add(t time.Time, hr float64) {
	k := t.UnixMilli() / restBucket.Milliseconds()
	r.sum[k] += hr
	r.n[k]++
}

// lowest returns the lowest bucket mean; ok is false with no full bucket.
func (r *restTracker) lowest() (float64, bool) {
	best, ok := 1e9, false
	for k, n := range r.n {
		if n >= minBucket {
			if m := r.sum[k] / float64(n); m < best {
				best, ok = m, true
			}
		}
	}
	return best, ok
}

// runBuilder collects RR intervals into runs of consecutive beats. The
// strap's realtime stream delivers RR in bursts with gaps, so a difference
// across a gap is not a heartbeat-to-heartbeat difference: each run is
// analysed on its own. It also counts beats seen against beats expected from
// the heart rate, which says how complete the RR series is.
type runBuilder struct {
	runs     [][]float64
	cur      []float64
	prev     time.Time
	beats    int
	expected float64
}

func (r *runBuilder) flush() {
	if len(r.cur) > 0 {
		r.runs = append(r.runs, r.cur)
		r.cur = nil
	}
}

func (r *runBuilder) add(s strapdb.Sample) {
	if len(s.RR) == 0 || (!r.prev.IsZero() && s.Time.Sub(r.prev) > 3*time.Second) {
		r.flush()
	}
	for _, v := range s.RR {
		r.cur = append(r.cur, float64(v))
	}
	r.beats += len(s.RR)
	r.expected += float64(s.HR) / 60 // one sample a second
	r.prev = s.Time
}

func (r *runBuilder) result() [][]float64 { r.flush(); return r.runs }

// coverage is the share of expected heartbeats that arrived as RR intervals.
func (r *runBuilder) coverage() float64 {
	if r.expected <= 0 {
		return 0
	}
	return math.Min(1, float64(r.beats)/r.expected)
}

// hrvReading is what a set of RR runs says about HRV and breathing.
type hrvReading struct {
	RMSSD, LnRMSSD, Resp float64
	Pairs                int
	HaveHRV, HaveResp    bool
}

func analyseRuns(runs [][]float64) hrvReading {
	var h hrvReading
	if rm, pairs, ok := science.RMSSDRuns(runs); ok {
		h.RMSSD, h.Pairs, h.HaveHRV = rm, pairs, true
		h.LnRMSSD = math.Log(rm)
	}
	var rates []float64
	for _, r := range runs {
		if bpm, w, ok := science.RespiratoryRateWindows(r); ok {
			for i := 0; i < w; i++ { // weight each run by how many windows backed it
				rates = append(rates, bpm)
			}
		}
	}
	if len(rates) > 0 {
		sort.Float64s(rates)
		h.Resp, h.HaveResp = rates[len(rates)/2], true
	}
	return h
}

// Night is the overnight reading for a day: the window from 22:00 the evening
// before to 08:00. It is a stand-in until sleep detection exists, so it
// includes time awake in bed. Off-wrist time is left out.
type Night struct {
	Samples       int     `json:"samples"`
	Hours         float64 `json:"hours"`
	RRPairs       int     `json:"rr_pairs"`
	RestingHR     float64 `json:"resting_hr"`
	RMSSD         float64 `json:"rmssd"`
	LnRMSSD       float64 `json:"ln_rmssd"`
	RespRate      float64 `json:"resp_rate"`
	HaveRestingHR bool    `json:"have_resting_hr"`
	HaveHRV       bool    `json:"have_hrv"`
	HaveResp      bool    `json:"have_resp"`
}

// Live is what can be read from the day's own samples right now, without an
// overnight recording. It is real but looser: resting HR is the lowest 5-minute
// average while awake, and HRV comes from whatever RR bursts the strap sent.
type Live struct {
	RestingHR     float64 `json:"resting_hr"`
	HaveRestingHR bool    `json:"have_resting_hr"`
	RMSSD         float64 `json:"rmssd"`
	LnRMSSD       float64 `json:"ln_rmssd"`
	RRBeats       int     `json:"rr_beats"`
	RRCoverage    float64 `json:"rr_coverage"` // 0-1, share of beats that came with an RR interval
	HaveHRV       bool    `json:"have_hrv"`
	RespRate      float64 `json:"resp_rate"`
	HaveResp      bool    `json:"have_resp"`
	VO2max        float64 `json:"vo2max"` // Uth estimate from max HR / resting HR
	HaveVO2       bool    `json:"have_vo2"`
}

// DaySummary is everything derived from one local calendar day of samples.
type DaySummary struct {
	Day             string     `json:"day"`
	Samples         int        `json:"samples"`
	FirstSampleMs   int64      `json:"first_sample_ms"`   // unix ms of the day's first sample; 0 when none
	Minutes         float64    `json:"minutes"`           // time worn with heart data
	OffWristMinutes float64    `json:"off_wrist_minutes"` // time the strap said it was off the body
	AvgHR           float64    `json:"avg_hr"`
	MinHR           float64    `json:"min_hr"`
	MaxHR           float64    `json:"max_hr"`
	HRMaxUsed       float64    `json:"hr_max_used"`
	Zones           [6]float64 `json:"zones_minutes"` // minutes below 50%, then 50-60 ... 90+ of max HR
	Edwards         float64    `json:"edwards"`
	Banister        float64    `json:"banister"`
	Strain          float64    `json:"strain"`
	HaveStrain      bool       `json:"have_strain"`
	Live            Live       `json:"live"`
	Night           Night      `json:"night"`
}

const (
	maxGap          = 5 * time.Second // longest a reading is held
	minNightSamples = 2 * 3600        // two hours of overnight data
	restBucket      = 5 * time.Minute
	minBucket       = 120 // samples a 5-minute bucket needs to count
)

// Summarize computes one day from stored samples. The profile gives the age
// (and optionally sex); without an age there is no max heart rate, so no
// strain or zones. Samples taken off the wrist are counted as wear time but
// never enter a heart-rate metric.
func Summarize(db *strapdb.DB, day string, p strapdb.Profile, now time.Time) (DaySummary, error) {
	from, to, err := db.DayBounds(day)
	if err != nil {
		return DaySummary{}, err
	}
	out := DaySummary{Day: day}

	var pts []science.HRPoint
	var sumHR float64
	var worn, off int
	out.MinHR = 1e9
	rest := newRestTracker()
	var runs runBuilder
	err = db.Samples(from, to, func(s strapdb.Sample) bool {
		if out.Samples == 0 {
			out.FirstSampleMs = s.Time.UnixMilli()
		}
		out.Samples++
		if s.OffWrist {
			off++
			return true
		}
		worn++
		hr := float64(s.HR)
		rest.add(s.Time, hr)
		runs.add(s)
		pts = append(pts, science.HRPoint{T: s.Time, HR: hr})
		sumHR += hr
		if hr > 0 && hr < out.MinHR {
			out.MinHR = hr
		}
		if hr > out.MaxHR {
			out.MaxHR = hr
		}
		return true
	})
	if err != nil {
		return out, err
	}
	if worn == 0 {
		out.MinHR = 0
	} else {
		out.AvgHR = sumHR / float64(worn)
	}
	out.OffWristMinutes = float64(off) / 60
	iv := science.Intervals(pts, maxGap)
	for _, i := range iv {
		out.Minutes += i.Seconds / 60
	}

	// What the day's own data says, with no overnight recording needed.
	if best, ok := rest.lowest(); ok {
		out.Live.RestingHR, out.Live.HaveRestingHR = best, true
	}
	h := analyseRuns(runs.result())
	out.Live.RRBeats, out.Live.RRCoverage = runs.beats, runs.coverage()
	out.Live.RMSSD, out.Live.LnRMSSD, out.Live.HaveHRV = h.RMSSD, h.LnRMSSD, h.HaveHRV
	out.Live.RespRate, out.Live.HaveResp = h.Resp, h.HaveResp

	out.Night, err = night(db, from)
	if err != nil {
		return out, err
	}

	if age, ok := ageOf(p, now); ok && worn > 0 {
		out.HRMaxUsed = science.MaxHR(age, out.MaxHR)
		out.Zones = science.ZoneMinutes(iv, out.HRMaxUsed)
		out.Edwards = science.EdwardsTRIMP(iv, out.HRMaxUsed)
		out.Strain, out.HaveStrain = science.Strain(out.Edwards), true
		// Uth VO2max needs a resting heart rate: last night's when there is
		// one, else the day's lowest 5-minute average.
		restHR, haveRest := out.Night.RestingHR, out.Night.HaveRestingHR
		if !haveRest {
			restHR, haveRest = out.Live.RestingHR, out.Live.HaveRestingHR
		}
		if haveRest {
			out.Live.VO2max, out.Live.HaveVO2 = science.VO2maxUth(out.HRMaxUsed, restHR)
			// Banister measures load against the heart-rate reserve, so it needs the same resting value and a sex.
			if sex, ok := science.ParseSex(p.Sex); ok {
				out.Banister = science.BanisterTRIMP(iv, restHR, out.HRMaxUsed, sex)
			}
		}
	}
	return out, nil
}

// night reads the overnight window ending at 08:00 on the day starting at dayStart.
func night(db *strapdb.DB, dayStart time.Time) (Night, error) {
	loc := dayStart.Location()
	y, m, d := dayStart.Date()
	from := time.Date(y, m, d-1, 22, 0, 0, 0, loc)
	to := time.Date(y, m, d, 8, 0, 0, 0, loc)

	var n Night
	rest := newRestTracker()
	var runs runBuilder
	err := db.Samples(from, to, func(s strapdb.Sample) bool {
		if s.OffWrist {
			return true
		}
		n.Samples++
		rest.add(s.Time, float64(s.HR))
		runs.add(s)
		return true
	})
	if err != nil {
		return n, err
	}
	n.Hours = float64(n.Samples) / 3600
	if n.Samples < minNightSamples {
		return n, nil
	}
	if best, ok := rest.lowest(); ok {
		n.RestingHR, n.HaveRestingHR = best, true
	}
	h := analyseRuns(runs.result())
	n.RMSSD, n.LnRMSSD, n.RRPairs, n.HaveHRV = h.RMSSD, h.LnRMSSD, h.Pairs, h.HaveHRV
	n.RespRate, n.HaveResp = h.Resp, h.HaveResp
	return n, nil
}
