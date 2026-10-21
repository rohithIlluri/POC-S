// Package insights turns the log into short, explainable observations. Every
// rule is a plain threshold or correlation over the user's own data — no
// model, no network — and each one says what it looked at.
package insights

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/metrics"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/stats"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
)

// Level orders insights: things to watch first, then neutral facts, then wins.
type Level int

const (
	Watch Level = iota
	Info
	Good
)

func (l Level) String() string {
	return [...]string{"watch", "info", "good"}[l]
}

// Insight is one observation.
type Insight struct {
	Level Level
	Text  string
}

const (
	shortWindow     = 14 // days for goals and symptom counts
	longWindow      = 30 // days for correlations and weight trend
	minPairs        = 7  // days both metrics must share before correlating
	minCorrelation  = 0.4
	recommendedLow  = 7.0 // adult sleep recommendation (h)
	symptomRepeat   = 3
	sleepContrastH  = 0.75
	heartRateRiseBP = 5.0
	recoveryRed     = 34.0 // WHOOP's red zone is 0–33%
	recoveryGreen   = 67.0 // and green is 67–100%
	hrvDropShare    = 0.15
)

// correlated pairs worth reporting, phrased as "more X → Y tends to be ...".
var pairs = [][2]string{
	{"sleep", "mood"},
	{"sleep", "energy"},
	{"exercise", "mood"},
	{"steps", "mood"},
	{"water", "energy"},
	{"sleep", "recovery"},
	{"recovery", "mood"},
	{"recovery", "energy"},
}

func metric(name string) metrics.Metric {
	m, _ := metrics.Lookup(name)
	return m
}

// Generate runs every rule over the entries as of now.
func Generate(entries []store.Entry, cfg config.Config, now time.Time, loc *time.Location) []Insight {
	var out []Insight
	add := func(l Level, format string, a ...any) {
		out = append(out, Insight{Level: l, Text: fmt.Sprintf(format, a...)})
	}

	if n := stats.Streak(entries, now, loc); n >= 3 {
		add(Good, "%d-day logging streak — consistent data makes every other insight sharper.", n)
	}

	sleep := metric("sleep")
	if s, ok := stats.Summarize(entries, sleep, now, 7, loc); ok && s.Days >= 3 {
		if s.Mean < recommendedLow {
			add(Watch, "Averaging %.1f h of sleep over the last 7 days (%d nights logged). Most adults need 7–9 h.", s.Mean, s.Days)
		} else {
			add(Good, "Averaging %.1f h of sleep over the last 7 days — inside the 7–9 h range.", s.Mean)
		}
	}

	goalNames := make([]string, 0, len(cfg.Goals))
	for name := range cfg.Goals {
		goalNames = append(goalNames, name)
	}
	sort.Strings(goalNames)
	for _, name := range goalNames {
		m, ok := metrics.Lookup(name)
		if !ok {
			continue
		}
		g := cfg.Goals[name]
		vals, has := stats.Series(stats.Daily(entries, m, loc), stats.Window(now, shortWindow, loc))
		logged := stats.Present(vals, has)
		if len(logged) == 0 {
			continue
		}
		met := 0
		for _, v := range logged {
			if g.Met(v) {
				met++
			}
		}
		level := Info
		switch rate := float64(met) / float64(len(logged)); {
		case rate >= 0.8:
			level = Good
		case rate < 0.5:
			level = Watch
		}
		add(level, "%s goal (%s %s) met on %d of %d logged days in the last %d days.",
			m.Name, g.Op, m.FormatWithUnit(g.Target), met, len(logged), shortWindow)
	}

	window := map[string]bool{}
	for _, k := range stats.Window(now, longWindow, loc) {
		window[k] = true
	}
	inWindow := func(d map[string]float64) map[string]float64 {
		out := map[string]float64{}
		for k, v := range d {
			if window[k] {
				out[k] = v
			}
		}
		return out
	}
	for _, p := range pairs {
		a, b := metric(p[0]), metric(p[1])
		xs, ys := stats.Paired(inWindow(stats.Daily(entries, a, loc)), inWindow(stats.Daily(entries, b, loc)))
		if len(xs) < minPairs {
			continue
		}
		r, ok := stats.Pearson(xs, ys)
		if !ok || math.Abs(r) < minCorrelation {
			continue
		}
		dir := "higher"
		if r < 0 {
			dir = "lower"
		}
		add(Info, "On days with more %s, your %s tends to be %s (r = %+.2f over %d days). Correlation, not causation.",
			a.Name, b.Name, dir, r, len(xs))
	}

	symptom := metric("symptom")
	sleepDaily := inWindow(stats.Daily(entries, sleep, loc))
	for _, c := range stats.EventCounts(entries, symptom, now, shortWindow, loc) {
		if c.Count < symptomRepeat {
			continue
		}
		add(Watch, "%s logged %d× in the last %d days (avg severity %.1f/5). If it persists or worsens, talk to a clinician.",
			c.Label, c.Count, shortWindow, c.Mean)

		days := stats.EventDays(entries, symptom.Name, c.Label, loc)
		var on, off []float64
		for k, v := range sleepDaily {
			if days[k] {
				on = append(on, v)
			} else {
				off = append(off, v)
			}
		}
		if len(on) >= 3 && len(off) >= 3 {
			if diff := stats.Mean(off) - stats.Mean(on); diff >= sleepContrastH {
				add(Info, "On %s days you slept %.1f h on average vs %.1f h on other days.", c.Label, stats.Mean(on), stats.Mean(off))
			}
		}
	}

	if s, ok := stats.Summarize(entries, metric("heartrate"), now, shortWindow, loc); ok && s.HasDelta {
		if rise := s.Recent - s.Prior; rise >= heartRateRiseBP {
			add(Watch, "Resting heart rate is up %.0f bpm vs the previous week (%.0f → %.0f). Stress, illness, poor sleep or overtraining can all do this.",
				rise, s.Prior, s.Recent)
		}
	}

	if s, ok := stats.Summarize(entries, metric("recovery"), now, 7, loc); ok && s.Days >= 3 {
		switch {
		case s.Mean < recoveryRed:
			add(Watch, "Recovery has averaged %.0f%% over the last 7 days — in the red. Prioritise sleep and go easy on strain.", s.Mean)
		case s.Mean >= recoveryGreen:
			add(Good, "Recovery has averaged %.0f%% over the last 7 days — in the green.", s.Mean)
		}
	}

	if s, ok := stats.Summarize(entries, metric("hrv"), now, shortWindow, loc); ok && s.HasDelta && s.Prior > 0 {
		if drop := (s.Prior - s.Recent) / s.Prior; drop >= hrvDropShare {
			add(Watch, "HRV is down %.0f%% vs the previous week (%.0f → %.0f ms). Often an early sign of stress, illness or under-recovery.",
				drop*100, s.Prior, s.Recent)
		}
	}

	weight := metric("weight")
	vals, has := stats.Series(stats.Daily(entries, weight, loc), stats.Window(now, longWindow, loc))
	if w := stats.Present(vals, has); len(w) >= 2 {
		add(Info, "Weight %+.1f kg over the last %d days (%s → %s).",
			w[len(w)-1]-w[0], longWindow, weight.FormatWithUnit(w[0]), weight.FormatWithUnit(w[len(w)-1]))
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Level < out[j].Level })
	return out
}
