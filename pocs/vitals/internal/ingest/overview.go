package ingest

import (
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/science"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/strapdb"
)

// minDayHours is how much heart data a local day needs to count as a day of
// data (one hour at one sample a second).
const minDaySamples = 3600

// Capabilities are the data paths that are verified end to end today. Each is
// switched on only when it has been checked against a real strap; until then
// the metrics that depend on it report themselves as blocked rather than
// guessing.
var Capabilities = science.Capabilities{}

func profileCaps(p strapdb.Profile) science.Capabilities {
	c := Capabilities
	_, haveAge := ageOf(p, time.Now())
	c.Profile = haveAge // sex is optional: it only adds the Banister load
	return c
}

// Day returns a day's summary, from the cache when it is there. Writes drop
// the cached days they change, so a cached day is always current.
func Day(db *strapdb.DB, day string, p strapdb.Profile, now time.Time) (DaySummary, error) {
	var cached DaySummary
	if ok, err := db.GetDay(day, &cached); err == nil && ok {
		return cached, nil
	}
	s, err := Summarize(db, day, p, now)
	if err != nil {
		return s, err
	}
	return s, db.PutDay(day, s)
}

// Overview is the state of the whole data set.
type Overview struct {
	DayZero    string           `json:"day_zero,omitempty"`
	Days       int              `json:"days"`   // local days with at least an hour of heart data
	Nights     int              `json:"nights"` // days with enough overnight data
	DBBytes    int64            `json:"db_bytes"`
	ProfileSet bool             `json:"profile_set"`
	Readiness  []science.Status `json:"readiness"`
}

// Status counts the days and nights of data stored and reports every
// metric's readiness.
func Status(db *strapdb.DB, now time.Time) (Overview, error) {
	var o Overview
	p, err := db.Profile()
	if err != nil {
		return o, err
	}
	o.DayZero, _, err = db.DayZero()
	if err != nil {
		return o, err
	}
	o.DBBytes, _ = db.Size()
	days, err := db.DaysWithData(minDaySamples)
	if err != nil {
		return o, err
	}
	o.Days = len(days)
	all, err := db.DaysWithData(1)
	if err != nil {
		return o, err
	}
	for _, day := range nextDays(all) {
		s, err := Day(db, day, p, now)
		if err != nil {
			return o, err
		}
		if s.Night.Samples >= minNightSamples {
			o.Nights++
		}
	}
	caps := profileCaps(p)
	o.ProfileSet = caps.Profile
	o.Readiness = science.Assess(o.Days, o.Nights, caps)
	return o, nil
}

// nextDays lists the days whose overnight window could hold data: every day
// that has samples, plus the morning after the last one.
func nextDays(days []string) []string {
	if len(days) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	for _, d := range days {
		add(d)
		if t, err := time.Parse("2006-01-02", d); err == nil {
			add(t.AddDate(0, 0, 1).Format("2006-01-02"))
		}
	}
	return out
}

// baselineDays is how many previous nights make up a baseline.
const baselineDays = 28

// RecoveryFor scores the night ending on day against the nights before it.
// ok is false when there is no reading for that night or too few earlier
// nights for a baseline.
func RecoveryFor(db *strapdb.DB, day string, p strapdb.Profile, now time.Time) (science.Recovery, bool, error) {
	target, err := Day(db, day, p, now)
	if err != nil {
		return science.Recovery{}, false, err
	}
	if !target.Night.HaveHRV || !target.Night.HaveRestingHR {
		return science.Recovery{}, false, nil
	}
	start, err := time.ParseInLocation("2006-01-02", day, now.Location())
	if err != nil {
		return science.Recovery{}, false, err
	}
	var hrvVals, rhrVals []float64
	for i := baselineDays; i >= 1; i-- {
		prior, err := Day(db, start.AddDate(0, 0, -i).Format("2006-01-02"), p, now)
		if err != nil {
			return science.Recovery{}, false, err
		}
		if prior.Night.HaveHRV && prior.Night.HaveRestingHR {
			hrvVals = append(hrvVals, prior.Night.LnRMSSD)
			rhrVals = append(rhrVals, prior.Night.RestingHR)
		}
	}
	r, ok := science.RecoveryScore(
		science.RecoveryInput{LnRMSSD: target.Night.LnRMSSD, RestingHR: target.Night.RestingHR},
		science.RecoveryBaselines{HRV: science.NewBaseline(hrvVals), RHR: science.NewBaseline(rhrVals)},
	)
	return r, ok, nil
}

// TrendPoint is one day in a trend: the few numbers worth drawing as a line.
type TrendPoint struct {
	Day           string  `json:"day"`
	Samples       int     `json:"samples"`
	WornMinutes   float64 `json:"worn_minutes"`
	Strain        float64 `json:"strain"`
	HaveStrain    bool    `json:"have_strain"`
	RestingHR     float64 `json:"resting_hr"`
	HaveRestingHR bool    `json:"have_resting_hr"`
	RMSSD         float64 `json:"rmssd"`
	HaveHRV       bool    `json:"have_hrv"`
}

// Trend returns the last n days up to and including endDay, oldest first. For
// each day the overnight reading is used when there is one, else the day's own.
func Trend(db *strapdb.DB, endDay string, n int, p strapdb.Profile, now time.Time) ([]TrendPoint, error) {
	end, err := time.ParseInLocation("2006-01-02", endDay, now.Location())
	if err != nil {
		return nil, err
	}
	out := make([]TrendPoint, 0, n)
	for i := n - 1; i >= 0; i-- {
		day := end.AddDate(0, 0, -i).Format("2006-01-02")
		s, err := Day(db, day, p, now)
		if err != nil {
			return nil, err
		}
		pt := TrendPoint{Day: day, Samples: s.Samples, WornMinutes: s.Minutes, Strain: s.Strain, HaveStrain: s.HaveStrain}
		switch {
		case s.Night.HaveRestingHR:
			pt.RestingHR, pt.HaveRestingHR = s.Night.RestingHR, true
		case s.Live.HaveRestingHR:
			pt.RestingHR, pt.HaveRestingHR = s.Live.RestingHR, true
		}
		switch {
		case s.Night.HaveHRV:
			pt.RMSSD, pt.HaveHRV = s.Night.RMSSD, true
		case s.Live.HaveHRV:
			pt.RMSSD, pt.HaveHRV = s.Live.RMSSD, true
		}
		out = append(out, pt)
	}
	return out, nil
}
