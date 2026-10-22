package science

import (
	"math"
	"sort"
)

// Breathing leaves a mark on heart rate (respiratory sinus arrhythmia): beat
// intervals lengthen and shorten with each breath. RespiratoryRate finds that
// rhythm in an RR series.
const (
	respMinHz    = 0.10 // 6 breaths/min
	respMaxHz    = 0.50 // 30 breaths/min
	respStepHz   = 0.005
	respFs       = 4.0    // resampling rate, Hz
	respMinSec   = 60.0   // need at least a minute of beats
	respMinBeats = 30     // ...and at least this many
	respPeakMin  = 3.0    // the peak must stand this far above the median power
	respMaxGapMs = 3000.0 // a gap this long between beats breaks the series
)

// RespiratoryRate estimates breaths per minute from RR intervals (ms): beat
// times are rebuilt from the intervals, the interval series is resampled to
// 4 Hz, the mean is removed, a Hann window applied and the 0.10-0.50 Hz band
// scanned for its strongest component. ok is false when the series is too
// short, has gaps, or has no clear peak. Estimates from RR alone are less
// accurate than from ECG; a validation at rest found correlations of about
// 0.68-0.89 with a reference.
func RespiratoryRate(rr []float64) (bpm float64, ok bool) {
	if len(rr) < respMinBeats {
		return 0, false
	}
	t := make([]float64, len(rr)) // beat times in seconds
	var acc float64
	for i, v := range rr {
		if v <= 0 || v > respMaxGapMs {
			return 0, false
		}
		acc += v / 1000
		t[i] = acc
	}
	dur := t[len(t)-1] - t[0]
	if dur < respMinSec {
		return 0, false
	}
	n := int(dur * respFs)
	x := make([]float64, n)
	j := 0
	for i := range x {
		ti := t[0] + float64(i)/respFs
		for j+1 < len(t)-1 && t[j+1] < ti {
			j++
		}
		w := (ti - t[j]) / (t[j+1] - t[j])
		x[i] = rr[j] + clamp(w, 0, 1)*(rr[j+1]-rr[j])
	}
	var mean float64
	for _, v := range x {
		mean += v
	}
	mean /= float64(n)
	for i := range x {
		hann := 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(n-1)))
		x[i] = (x[i] - mean) * hann
	}
	var freqs, power []float64
	for f := respMinHz; f <= respMaxHz+1e-9; f += respStepHz {
		// Goertzel: the power of x at frequency f in one pass, with no
		// per-sample trig.
		w := 2 * math.Pi * f / respFs
		coeff := 2 * math.Cos(w)
		var s1, s2 float64
		for _, v := range x {
			s0 := v + coeff*s1 - s2
			s2, s1 = s1, s0
		}
		freqs = append(freqs, f)
		power = append(power, s1*s1+s2*s2-coeff*s1*s2)
	}
	best := 0
	for i, p := range power {
		if p > power[best] {
			best = i
		}
	}
	sorted := append([]float64(nil), power...)
	sort.Float64s(sorted)
	if med := sorted[len(sorted)/2]; med <= 0 || power[best] < respPeakMin*med {
		return 0, false
	}
	return freqs[best] * 60, true
}

const (
	respWindowSec = 120.0 // one estimate per two minutes of beats
	respMaxWindow = 120   // look at no more than this many windows
)

// RespiratoryRateWindows estimates breathing across a long RR series (for
// instance a whole night) by taking RespiratoryRate on consecutive two-minute
// windows, evenly spread if there are more than respMaxWindow of them, and
// returning the median. A window with gaps or no clear peak is skipped. ok is
// false when no window gave an estimate.
func RespiratoryRateWindows(rr []float64) (bpm float64, windows int, ok bool) {
	// Cut the series into windows of about two minutes of beats.
	var starts, ends []int
	i := 0
	for i < len(rr) {
		var dur float64
		j := i
		for j < len(rr) && dur < respWindowSec*1000 {
			dur += rr[j]
			j++
		}
		if dur < respWindowSec*1000 {
			break
		}
		starts, ends = append(starts, i), append(ends, j)
		i = j
	}
	step := 1
	if len(starts) > respMaxWindow {
		step = (len(starts) + respMaxWindow - 1) / respMaxWindow
	}
	var rates []float64
	for k := 0; k < len(starts); k += step {
		if v, ok := RespiratoryRate(rr[starts[k]:ends[k]]); ok {
			rates = append(rates, v)
		}
	}
	if len(rates) == 0 {
		return 0, 0, false
	}
	sort.Float64s(rates)
	return rates[len(rates)/2], len(rates), true
}
