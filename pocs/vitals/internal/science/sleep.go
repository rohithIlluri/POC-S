package science

import (
	"math"
	"sort"
	"time"
)

// Sleep from the strap's accelerometer, using two published, open methods.
//
//	[1] van Hees VT, Sabia S, Anderson KN, et al. A novel, open access method to
//	    assess sleep duration using a wrist-worn accelerometer. PLoS ONE 2015;
//	    10(11):e0142533. doi:10.1371/journal.pone.0142533
//	    Arm angle = atan(az / sqrt(ax^2 + ay^2)) * 180/pi, averaged per 5-second
//	    epoch. Sleep is a "sustained inactivity bout": no change larger than 5
//	    degrees for at least 5 minutes. Against polysomnography (n = 28) the
//	    5 degree / 5 minute setting had sensitivity 91 +/- 13 %, specificity
//	    45 +/- 23 %, and overestimated the sleep window by 31 +/- 38 minutes.
//	[2] van Hees VT, Sabia S, Jones SE, et al. Estimating sleep parameters using an
//	    accelerometer without sleep diary. Sci Rep 2018; 8:12975.
//	    doi:10.1038/s41598-018-31266-z   (the "HDCZA" sleep period time window)
//	    Absolute difference between successive 5-second z-angles, a 5-minute
//	    rolling median of that, a threshold of the 10th percentile times 15
//	    (per noon-to-noon day), blocks below it lasting more than 30 minutes,
//	    gaps shorter than 60 minutes merged, longest block = sleep period.
//	    Against polysomnography the window's C-statistic was 0.86 (clinic) and
//	    0.83 (good sleepers).
//
// Differences from the papers, all forced by the strap's data:
//   - The strap gives one gravity vector per record (about 1 per second), not raw
//     30-100 Hz acceleration, so the per-epoch angle is the mean of the per-record
//     angles instead of the median of raw axes over a rolling 5 seconds.
//   - Which strap axis is perpendicular to the skin is not known. On the recorded
//     night the sleep window was the same to within 2 minutes whichever of the
//     three axes was used as "z".
//   - The papers do not say whether the 5-minute rolling median is centred; this
//     code centres it on 60 epochs.
//   - The percentile uses linear interpolation (the papers do not say).
//
// These are estimates from motion alone. The 5 degree / 5 minute rule is known to
// overestimate sleep, and nothing here separates light, deep or REM sleep: no
// validated coefficient set for that is published for this sensor, so none is
// shipped.

// GravitySample is one gravity-vector reading (in g).
type GravitySample struct {
	T time.Time
	G [3]float64
}

const (
	sleepEpoch      = 5 * time.Second
	sleepRollEpochs = 60 // 5 minutes of 5-second epochs
	hdczaPercentile = 10.0
	hdczaMultiplier = 15.0
	hdczaMinBlock   = 30 * time.Minute
	hdczaMergeGap   = 60 * time.Minute
	boutAngleDeg    = 5.0
	boutMinDuration = 5 * time.Minute
)

// SleepWindow is the sleep period: from the start of the longest low-motion
// block to its end.
type SleepWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// SleepReport is one night scored from motion.
type SleepReport struct {
	Window         SleepWindow `json:"window"`
	WindowHours    float64     `json:"window_hours"`     // sleep period (a proxy for time in bed: lights-out is not known)
	AsleepHours    float64     `json:"asleep_hours"`     // total of the sustained inactivity bouts inside the window
	AwakeMinutes   float64     `json:"awake_minutes"`    // window minus asleep (wake after sleep onset)
	Efficiency     float64     `json:"efficiency"`       // asleep / window, 0-1
	Awakenings     int         `json:"awakenings"`       // stretches of activity between bouts inside the window
	LongestBoutMin float64     `json:"longest_bout_min"` // longest unbroken inactivity bout
	Method         string      `json:"method"`
}

// epochAngles averages the z-angle of every sample into 5-second epochs from
// the first to the last, leaving NaN where an epoch has no sample. axis is the
// index treated as "z".
func epochAngles(s []GravitySample, axis int) (start time.Time, ang []float64) {
	if len(s) == 0 {
		return time.Time{}, nil
	}
	e := func(t time.Time) int64 { return t.Unix() / int64(sleepEpoch/time.Second) }
	first, last := e(s[0].T), e(s[len(s)-1].T)
	n := int(last-first) + 1
	sum := make([]float64, n)
	cnt := make([]int, n)
	o1, o2 := (axis+1)%3, (axis+2)%3
	for _, x := range s {
		a := math.Atan2(x.G[axis], math.Hypot(x.G[o1], x.G[o2])) * 180 / math.Pi
		if math.IsNaN(a) {
			continue
		}
		i := int(e(x.T) - first)
		sum[i] += a
		cnt[i]++
	}
	ang = make([]float64, n)
	for i := range ang {
		if cnt[i] == 0 {
			ang[i] = math.NaN()
		} else {
			ang[i] = sum[i] / float64(cnt[i])
		}
	}
	return time.Unix(first*int64(sleepEpoch/time.Second), 0), ang
}

// SleepPeriod finds the sleep period of the supplied samples, which should cover
// one noon-to-noon day (the threshold is a percentile of that span). It uses the
// strap's third gravity axis as "z"; see the package comment.
func SleepPeriod(s []GravitySample) (SleepWindow, bool) {
	t0, ang := epochAngles(s, 2)
	if len(ang) < 2*sleepRollEpochs {
		return SleepWindow{}, false
	}
	diff := make([]float64, len(ang)-1)
	for i := range diff {
		diff[i] = math.Abs(ang[i+1] - ang[i]) // NaN when either epoch is missing
	}
	rm := rollingMedian(diff, sleepRollEpochs)
	var valid []float64
	for _, v := range rm {
		if !math.IsNaN(v) {
			valid = append(valid, v)
		}
	}
	if len(valid) == 0 {
		return SleepWindow{}, false
	}
	thr := percentile(valid, hdczaPercentile) * hdczaMultiplier

	type run struct{ a, b int } // epoch index range, inclusive
	var blocks []run
	start := -1
	for i, v := range rm {
		below := !math.IsNaN(v) && v < thr
		if below && start < 0 {
			start = i
		}
		if start >= 0 && (!below || i == len(rm)-1) {
			end := i
			if !below {
				end = i - 1
			}
			blocks = append(blocks, run{start, end})
			start = -1
		}
	}
	epochs := func(d time.Duration) int { return int(d / sleepEpoch) }
	var kept []run
	for _, b := range blocks {
		if b.b-b.a+1 > epochs(hdczaMinBlock) {
			kept = append(kept, b)
		}
	}
	var merged []run
	for _, b := range kept {
		if n := len(merged); n > 0 && b.a-merged[n-1].b-1 < epochs(hdczaMergeGap) {
			merged[n-1].b = b.b
		} else {
			merged = append(merged, b)
		}
	}
	if len(merged) == 0 {
		return SleepWindow{}, false
	}
	best := merged[0]
	for _, b := range merged[1:] {
		if b.b-b.a > best.b-best.a {
			best = b
		}
	}
	at := func(i int) time.Time { return t0.Add(time.Duration(i+1) * sleepEpoch) } // diff[i] sits between epochs i and i+1
	return SleepWindow{Start: at(best.a), End: at(best.b)}, true
}

// ScoreSleep scores the sustained inactivity bouts of rule [1] inside w.
func ScoreSleep(s []GravitySample, w SleepWindow) SleepReport {
	rep := SleepReport{
		Window: w,
		Method: "van Hees 2015 sustained inactivity (5 deg / 5 min) inside the van Hees 2018 HDCZA sleep period; motion only, an estimate",
	}
	t0, ang := epochAngles(s, 2)
	if len(ang) < 2 {
		return rep
	}
	rep.WindowHours = w.End.Sub(w.Start).Hours()
	type bout struct{ a, b time.Time }
	var bouts []bout
	open := -1
	flush := func(endIdx int) {
		if open < 0 {
			return
		}
		a := t0.Add(time.Duration(open) * sleepEpoch)
		b := t0.Add(time.Duration(endIdx) * sleepEpoch)
		if b.Sub(a) >= boutMinDuration {
			bouts = append(bouts, bout{a, b})
		}
		open = -1
	}
	for i := 0; i+1 < len(ang); i++ {
		quiet := math.Abs(ang[i+1]-ang[i]) <= boutAngleDeg // false when NaN
		if quiet && open < 0 {
			open = i
		}
		if !quiet {
			flush(i)
		}
	}
	flush(len(ang) - 1)

	var asleep time.Duration
	var longest time.Duration
	var inside []bout
	for _, b := range bouts {
		a, e := b.a, b.b
		if a.Before(w.Start) {
			a = w.Start
		}
		if e.After(w.End) {
			e = w.End
		}
		if !e.After(a) {
			continue
		}
		asleep += e.Sub(a)
		if d := e.Sub(a); d > longest {
			longest = d
		}
		inside = append(inside, bout{a, e})
	}
	rep.AsleepHours = asleep.Hours()
	rep.AwakeMinutes = (w.End.Sub(w.Start) - asleep).Minutes()
	if rep.WindowHours > 0 {
		rep.Efficiency = rep.AsleepHours / rep.WindowHours
	}
	rep.LongestBoutMin = longest.Minutes()
	for i := 1; i < len(inside); i++ {
		if inside[i].a.After(inside[i-1].b) {
			rep.Awakenings++
		}
	}
	return rep
}

// StatePoint is one reading of the strap's own sleep-state code.
type StatePoint struct {
	T     time.Time
	State uint8 // 0 wake, 1 still, 2 sleep, 3 up
}

// DeviceSleep summarises the strap's own state labels. It is what the strap
// reported, not something scored here.
type DeviceSleep struct {
	FirstSleep time.Time  `json:"first_sleep"` // first record in the longest run of state "sleep"
	LastSleep  time.Time  `json:"last_sleep"`  // last record of that run
	SleepHours float64    `json:"sleep_hours"` // time in the state, all runs
	Minutes    [4]float64 `json:"minutes"`     // wake, still, sleep, up
}

// DeviceSleepSummary reads state codes in time order. Each reading counts for
// the time to the next one, at most five seconds.
func DeviceSleepSummary(p []StatePoint) (DeviceSleep, bool) {
	var out DeviceSleep
	if len(p) < 2 {
		return out, false
	}
	var runStart, bestStart, bestEnd time.Time
	var runLen, bestLen time.Duration
	prevSleep := false
	for i, x := range p {
		d := 5 * time.Second
		if i+1 < len(p) {
			if gap := p[i+1].T.Sub(x.T); gap < d {
				d = gap
			}
		}
		if x.State < 4 {
			out.Minutes[x.State] += d.Minutes()
		}
		isSleep := x.State == 2
		if isSleep && !prevSleep {
			runStart, runLen = x.T, 0
		}
		if isSleep {
			runLen += d
			if runLen > bestLen {
				bestLen, bestStart, bestEnd = runLen, runStart, x.T
			}
		}
		prevSleep = isSleep
	}
	out.SleepHours = out.Minutes[2] / 60
	out.FirstSleep, out.LastSleep = bestStart, bestEnd
	return out, bestLen > 0
}

func rollingMedian(x []float64, window int) []float64 {
	out := make([]float64, len(x))
	half := window / 2
	buf := make([]float64, 0, window+1)
	for i := range x {
		lo, hi := i-half, i+half
		if lo < 0 {
			lo = 0
		}
		if hi > len(x) {
			hi = len(x)
		}
		buf = buf[:0]
		for _, v := range x[lo:hi] {
			if !math.IsNaN(v) {
				buf = append(buf, v)
			}
		}
		// Too little data in the window to call it a median.
		if len(buf) < window/2 {
			out[i] = math.NaN()
			continue
		}
		sort.Float64s(buf)
		out[i] = buf[len(buf)/2]
		if len(buf)%2 == 0 {
			out[i] = (buf[len(buf)/2-1] + buf[len(buf)/2]) / 2
		}
	}
	return out
}

// percentile is the p-th percentile (0-100) with linear interpolation.
func percentile(x []float64, p float64) float64 {
	s := append([]float64(nil), x...)
	sort.Float64s(s)
	pos := p / 100 * float64(len(s)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return s[lo] + (s[hi]-s[lo])*(pos-float64(lo))
}
