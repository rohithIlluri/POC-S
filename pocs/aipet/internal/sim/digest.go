// Package sim is the deterministic Codelings pet simulation: DNA/hatching,
// the daily tick (XP, health, mood, evolution), and — per GAME_DESIGN.md
// §5.2 — the one architectural law: every function here is a pure function
// of (state, digest, seed). No wall-clock reads, no map-iteration-order
// dependence, no floats in anything that must replay identically.
package sim

import (
	"sort"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

// Digest is one calendar day's activity, derived purely from that day's
// events. It is the sole input the sim reads about the outside world — no
// event ever reaches evolve.go or tick.go directly.
type Digest struct {
	Day          string // YYYY-MM-DD, local time
	Turns        int
	Sessions     int // distinct Session values touched this day
	Projects     int // distinct Project values touched this day
	Models       int // distinct Model values touched this day
	NewModels    int // models seen today that were never seen on an earlier day
	NewProjects  int // projects touched today that were never seen on an earlier day
	TokensIn     int64
	TokensOut    int64
	CacheRead    int64
	CacheWrite   int64
	CostUSD      float64
	MaxGapMin    int  // largest gap between consecutive turns in the same session, minutes
	NightSession bool // any turn between 00:00 and 05:00 local
	Fragmented   int  // count of sessions with only 1-2 turns (cold-start-heavy)
}

// CacheRatio returns cache-read tokens as a fraction of (input+cacheRead),
// 0 if there's no denominator. This is the FOCUS-stat and diet signal.
func (d Digest) CacheRatio() float64 {
	total := d.TokensIn + d.CacheRead
	if total == 0 {
		return 0
	}
	return float64(d.CacheRead) / float64(total)
}

// Digests buckets a full event history into one Digest per active calendar
// day (local time), sorted ascending by day. NewModels is computed against
// the running set of models seen on any strictly earlier day, so a model
// first touched at 23:59 doesn't retroactively change an earlier day.
func Digests(events []store.Event) []Digest {
	byDay := map[string][]store.Event{}
	for _, e := range events {
		day := e.Timestamp.Local().Format("2006-01-02")
		byDay[day] = append(byDay[day], e)
	}
	days := make([]string, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	sort.Strings(days)

	seenModels := map[string]bool{}
	seenProjects := map[string]bool{}
	out := make([]Digest, 0, len(days))
	for _, day := range days {
		evs := byDay[day]
		out = append(out, digestOneDay(day, evs, seenModels, seenProjects))
		for _, e := range evs {
			seenModels[e.Model] = true
			seenProjects[e.Project] = true
		}
	}
	return out
}

func digestOneDay(day string, evs []store.Event, seenModels, seenProjects map[string]bool) Digest {
	d := Digest{Day: day, Turns: len(evs)}
	sessions := map[string][]store.Event{}
	projects := map[string]bool{}
	models := map[string]bool{}

	for _, e := range evs {
		sessions[e.Session] = append(sessions[e.Session], e)
		projects[e.Project] = true
		models[e.Model] = true
		d.TokensIn += e.Input
		d.TokensOut += e.Output
		d.CacheRead += e.CacheRead
		d.CacheWrite += e.CacheWrite
		d.CostUSD += e.CostUSD
		hour := e.Timestamp.Local().Hour()
		if hour >= 0 && hour < 5 {
			d.NightSession = true
		}
	}
	d.Projects = len(projects)
	d.Models = len(models)
	d.Sessions = len(sessions)
	// Count DISTINCT first-ever names, not events: three turns on one brand
	// new model is still one new model.
	for m := range models {
		if !seenModels[m] {
			d.NewModels++
		}
	}
	for p := range projects {
		if !seenProjects[p] {
			d.NewProjects++
		}
	}

	for _, se := range sessions {
		if len(se) <= 2 {
			d.Fragmented++
		}
		sort.Slice(se, func(i, j int) bool { return se[i].Timestamp.Before(se[j].Timestamp) })
		for i := 1; i < len(se); i++ {
			gap := int(se[i].Timestamp.Sub(se[i-1].Timestamp) / time.Minute)
			if gap > d.MaxGapMin {
				d.MaxGapMin = gap
			}
		}
	}
	return d
}
