package ingest

import (
	"math"
	"sort"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/science"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/strapdb"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoopble"
)

// SleepNight is the night ending on Day (noon the day before to noon that day),
// read straight from the raw history records, so nothing about it is stored
// separately and it can be recomputed whenever the science changes.
type SleepNight struct {
	Day     string `json:"day"`
	Records int    `json:"records"` // version-24 heart records in the span
	// Motion is scored here from the accelerometer with published methods
	// (see science/sleep.go). Nil when the span holds too little still time.
	Motion *science.SleepReport `json:"motion,omitempty"`
	// Device is what the strap itself labelled each second (its on-device
	// sleep state), kept separate because vitals did not compute it.
	Device *science.DeviceSleep `json:"device,omitempty"`
	// Stages is a rule-based estimate of wake / light / deep / REM from heart
	// rate, RR intervals and motion (science/stages.go). Unvalidated: no
	// polysomnography was available. Nil when the night cannot be staged.
	Stages *science.Hypnogram `json:"stages,omitempty"`
	// Heart rate inside the motion sleep window.
	LowestHR  float64 `json:"lowest_hr,omitempty"`  // lowest 5-minute median, bpm
	AverageHR float64 `json:"average_hr,omitempty"` // bpm
	Note      string  `json:"note"`
}

const sleepNote = "Estimates. Motion sleep uses van Hees 2015/2018 accelerometer methods (5 deg / 5 min; HDCZA window) and tends to overestimate sleep; the device block is the strap's own labelling. Stages are a rule-based estimate from heart rate, RR and motion (ours, not validated against polysomnography)."

// Sleep scores the night ending on day. loc decides where noon is.
func Sleep(db *strapdb.DB, day string, loc *time.Location) (SleepNight, error) {
	out := SleepNight{Day: day, Note: sleepNote}
	d, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return out, err
	}
	from := time.Date(d.Year(), d.Month(), d.Day()-1, 12, 0, 0, 0, loc)
	to := time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, loc)

	type rec struct {
		t  time.Time
		g  [3]float64
		hr int
		st uint8
		rr []float64
		mv float64
	}
	var recs []rec
	err = db.Records(func(r strapdb.Record) bool {
		h, ok := whoopble.DecodeHistoryFull(r.Body)
		if !ok || h.Time.Before(from) || !h.Time.Before(to) || h.Time.Unix() < minValidUnix {
			return true
		}
		g := [3]float64{float64(h.Gravity[0]), float64(h.Gravity[1]), float64(h.Gravity[2])}
		if math.IsNaN(g[0]+g[1]+g[2]) || math.IsInf(g[0]+g[1]+g[2], 0) {
			return true
		}
		rr := make([]float64, len(h.RR))
		for i, v := range h.RR {
			rr[i] = float64(v)
		}
		recs = append(recs, rec{h.Time, g, h.HR, uint8(h.Sleep), rr, float64(h.Movement)})
		return true
	})
	if err != nil {
		return out, err
	}
	out.Records = len(recs)
	if len(recs) == 0 {
		return out, nil
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].t.Before(recs[j].t) })

	grav := make([]science.GravitySample, len(recs))
	states := make([]science.StatePoint, len(recs))
	for i, r := range recs {
		grav[i] = science.GravitySample{T: r.t, G: r.g}
		states[i] = science.StatePoint{T: r.t, State: r.st}
	}
	if ds, ok := science.DeviceSleepSummary(states); ok {
		out.Device = &ds
	}
	w, ok := science.SleepPeriod(grav)
	if !ok {
		return out, nil
	}
	rep := science.ScoreSleep(grav, w)
	out.Motion = &rep

	ss := make([]science.StageSample, len(recs))
	for i, r := range recs {
		ss[i] = science.StageSample{T: r.t, HR: r.hr, RR: r.rr, G: r.g, Movement: r.mv, Device: int(r.st)}
	}
	if hyp, ok := science.StageNight(ss, w, science.DefaultStageOptions()); ok {
		out.Stages = &hyp
	}

	var hrs []float64
	buckets := map[int64][]float64{}
	for _, r := range recs {
		if r.hr > 0 && !r.t.Before(w.Start) && r.t.Before(w.End) {
			hrs = append(hrs, float64(r.hr))
			k := r.t.Unix() / 300
			buckets[k] = append(buckets[k], float64(r.hr))
		}
	}
	if len(hrs) > 0 {
		var sum float64
		for _, h := range hrs {
			sum += h
		}
		out.AverageHR = sum / float64(len(hrs))
		out.LowestHR = math.MaxFloat64
		for _, b := range buckets {
			if len(b) < 120 { // a bucket needs 2 minutes of beats to count, as for resting HR
				continue
			}
			sort.Float64s(b)
			if m := b[len(b)/2]; m < out.LowestHR {
				out.LowestHR = m
			}
		}
		if out.LowestHR == math.MaxFloat64 {
			out.LowestHR = 0
		}
	}
	return out, nil
}
