// Package stats turns raw entries into daily series, window summaries,
// sparklines and correlations. Everything is a pure function of its inputs
// (including "now"), so it is deterministic and easy to test.
package stats

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/metrics"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
)

// DayLayout is the key format for a calendar day.
const DayLayout = "2006-01-02"

// DayKey is the local calendar day an instant falls on.
func DayKey(t time.Time, loc *time.Location) string {
	return t.In(loc).Format(DayLayout)
}

// Window returns the day keys of the `days` calendar days ending today,
// oldest first.
func Window(now time.Time, days int, loc *time.Location) []string {
	now = now.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, loc)
	keys := make([]string, days)
	for i := 0; i < days; i++ {
		keys[i] = today.AddDate(0, 0, i-days+1).Format(DayLayout)
	}
	return keys
}

// Daily rolls a metric's entries up to one value per local day using the
// metric's aggregation rule. Entries must be sorted by time (store.Load does).
func Daily(entries []store.Entry, m metrics.Metric, loc *time.Location) map[string]float64 {
	sums := map[string]float64{}
	counts := map[string]int{}
	for _, e := range entries {
		if e.Metric != m.Name {
			continue
		}
		k := DayKey(e.Time, loc)
		switch m.Agg {
		case metrics.Last:
			sums[k] = e.Value
		default:
			sums[k] += e.Value
		}
		counts[k]++
	}
	if m.Agg == metrics.Mean {
		for k := range sums {
			sums[k] /= float64(counts[k])
		}
	}
	return sums
}

// Series picks the values for the given keys out of a daily map, returning
// the values and whether each day had data.
func Series(daily map[string]float64, keys []string) ([]float64, []bool) {
	vals := make([]float64, len(keys))
	has := make([]bool, len(keys))
	for i, k := range keys {
		vals[i], has[i] = daily[k]
	}
	return vals, has
}

// Present filters a series down to the days that had data.
func Present(vals []float64, has []bool) []float64 {
	var out []float64
	for i, v := range vals {
		if has[i] {
			out = append(out, v)
		}
	}
	return out
}

// Mean of xs; 0 for an empty slice.
func Mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// Pearson is the correlation coefficient of two equal-length samples. ok is
// false when there are fewer than 3 points or either side has no variance.
func Pearson(xs, ys []float64) (r float64, ok bool) {
	n := len(xs)
	if n != len(ys) || n < 3 {
		return 0, false
	}
	mx, my := Mean(xs), Mean(ys)
	var sxy, sxx, syy float64
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 || syy == 0 {
		return 0, false
	}
	return sxy / math.Sqrt(sxx*syy), true
}

// Paired returns the values of a and b on the days both have data, in key
// order, for correlating two metrics.
func Paired(a, b map[string]float64) (xs, ys []float64) {
	keys := make([]string, 0, len(a))
	for k := range a {
		if _, ok := b[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		xs = append(xs, a[k])
		ys = append(ys, b[k])
	}
	return xs, ys
}

var bars = []rune("▁▂▃▄▅▆▇█")

// Sparkline draws one character per day, scaled between the series' min and
// max; days without data are a middle dot so gaps stay visible.
func Sparkline(vals []float64, has []bool) string {
	lo, hi := math.Inf(1), math.Inf(-1)
	for i, v := range vals {
		if has[i] {
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	var b strings.Builder
	for i, v := range vals {
		switch {
		case !has[i]:
			b.WriteRune('·')
		case hi == lo:
			b.WriteRune(bars[len(bars)/2])
		default:
			idx := int(math.Round((v - lo) / (hi - lo) * float64(len(bars)-1)))
			b.WriteRune(bars[idx])
		}
	}
	return b.String()
}

// Summary describes one numeric metric over a window.
type Summary struct {
	Metric   metrics.Metric
	Days     int // days in the window with data
	Mean     float64
	Min      float64
	Max      float64
	Recent   float64 // mean of the last 7 days
	Prior    float64 // mean of the 7 days before that
	HasDelta bool    // both weeks had enough data (≥3 days) to compare
	Spark    string
}

// Summarize builds a Summary for the window of `days` ending at now. ok is
// false when the metric has no data in the window.
func Summarize(entries []store.Entry, m metrics.Metric, now time.Time, days int, loc *time.Location) (s Summary, ok bool) {
	daily := Daily(entries, m, loc)
	vals, has := Series(daily, Window(now, days, loc))
	present := Present(vals, has)
	if len(present) == 0 {
		return Summary{}, false
	}
	s = Summary{Metric: m, Days: len(present), Mean: Mean(present), Min: present[0], Max: present[0], Spark: Sparkline(vals, has)}
	for _, v := range present {
		s.Min, s.Max = math.Min(s.Min, v), math.Max(s.Max, v)
	}
	weeks := Window(now, 14, loc)
	prior := Present(Series(daily, weeks[:7]))
	recent := Present(Series(daily, weeks[7:]))
	if len(recent) >= 3 && len(prior) >= 3 {
		s.Recent, s.Prior, s.HasDelta = Mean(recent), Mean(prior), true
	}
	return s, true
}

// LabelCount is how often one event label occurred.
type LabelCount struct {
	Label string
	Count int
	Mean  float64 // mean value (e.g. symptom severity)
}

// EventCounts tallies an event metric's labels within the window, most
// frequent first.
func EventCounts(entries []store.Entry, m metrics.Metric, now time.Time, days int, loc *time.Location) []LabelCount {
	inWindow := map[string]bool{}
	for _, k := range Window(now, days, loc) {
		inWindow[k] = true
	}
	idx := map[string]int{}
	var out []LabelCount
	for _, e := range entries {
		if e.Metric != m.Name || !inWindow[DayKey(e.Time, loc)] {
			continue
		}
		label := strings.ToLower(e.Label)
		i, seen := idx[label]
		if !seen {
			i = len(out)
			idx[label] = i
			out = append(out, LabelCount{Label: label})
		}
		out[i].Mean = (out[i].Mean*float64(out[i].Count) + e.Value) / float64(out[i].Count+1)
		out[i].Count++
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Count != out[b].Count {
			return out[a].Count > out[b].Count
		}
		return out[a].Label < out[b].Label
	})
	return out
}

// EventDays is the set of local days on which an event label was logged
// ("" matches any label).
func EventDays(entries []store.Entry, metric, label string, loc *time.Location) map[string]bool {
	days := map[string]bool{}
	for _, e := range entries {
		if e.Metric == metric && (label == "" || strings.EqualFold(e.Label, label)) {
			days[DayKey(e.Time, loc)] = true
		}
	}
	return days
}

// Streak counts consecutive days with at least one entry, ending today — or
// ending yesterday if nothing has been logged yet today, so a streak isn't
// shown as broken first thing in the morning.
func Streak(entries []store.Entry, now time.Time, loc *time.Location) int {
	logged := map[string]bool{}
	for _, e := range entries {
		logged[DayKey(e.Time, loc)] = true
	}
	now = now.In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, loc)
	if !logged[day.Format(DayLayout)] {
		day = day.AddDate(0, 0, -1)
	}
	n := 0
	for logged[day.Format(DayLayout)] {
		n++
		day = day.AddDate(0, 0, -1)
	}
	return n
}
