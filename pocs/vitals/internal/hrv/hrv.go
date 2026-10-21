// Package hrv turns a short recording from a Bluetooth heart-rate strap —
// heart-rate samples and, when the strap sends them, beat-to-beat (RR)
// intervals — into a resting heart rate, an HRV figure, or a workout
// summary. RR intervals come from the standard BLE Heart Rate Measurement
// characteristic (0x2A37), which many straps (WHOOP's "broadcast heart
// rate" mode, chest straps, watches) emit.
package hrv

import (
	"errors"
	"math"
	"sort"
)

const (
	minRR = 300.0  // ms; 200 bpm
	maxRR = 2000.0 // ms; 30 bpm
	// maxJump drops an interval that differs from the previous accepted one
	// by more than this share — the classic signature of a missed or extra
	// beat, which would otherwise dominate RMSSD.
	maxJump       = 0.20
	maxFromMedian = 0.30
	// MinRRForHRV is how many clean intervals an HRV figure needs (~30 s
	// of beats); fewer and RMSSD is too noisy to be worth saving.
	MinRRForHRV = 30
)

// Clean drops physiologically impossible intervals and ectopic beats. It
// first discards anything more than 30% from the median — so one bad value
// at the start can't become the reference that rejects every good beat
// after it — then drops >20% jumps from the previous accepted beat.
func Clean(rr []float64) []float64 {
	plausible := make([]float64, 0, len(rr))
	for _, v := range rr {
		if !math.IsNaN(v) && v >= minRR && v <= maxRR {
			plausible = append(plausible, v)
		}
	}
	if len(plausible) == 0 {
		return nil
	}
	sorted := append([]float64(nil), plausible...)
	sort.Float64s(sorted)
	med := sorted[len(sorted)/2]

	out := make([]float64, 0, len(plausible))
	for _, v := range plausible {
		if math.Abs(v-med)/med > maxFromMedian {
			continue
		}
		if n := len(out); n > 0 && math.Abs(v-out[n-1])/out[n-1] > maxJump {
			continue
		}
		out = append(out, v)
	}
	return out
}

// RMSSD is the root mean square of successive differences of clean RR
// intervals, in ms — the standard short-term HRV measure (and what WHOOP
// reports). ok is false with fewer than MinRRForHRV clean intervals.
func RMSSD(rr []float64) (rmssd float64, ok bool) {
	c := Clean(rr)
	if len(c) < MinRRForHRV {
		return 0, false
	}
	var sum float64
	for i := 1; i < len(c); i++ {
		d := c[i] - c[i-1]
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(c)-1)), true
}

// ErrTooShort means the recording doesn't hold enough data to summarise.
var ErrTooShort = errors.New("recording too short — keep the strap connected a little longer")

// RestingHR estimates resting heart rate. With enough clean RR intervals it
// uses them (more precise than the strap's rounded bpm); otherwise it takes
// the median of the bpm samples, which shrugs off motion spikes.
func RestingHR(hr []float64, rr []float64) (float64, error) {
	if c := Clean(rr); len(c) >= MinRRForHRV {
		var sum float64
		for _, v := range c {
			sum += v
		}
		return 60000 / (sum / float64(len(c))), nil
	}
	valid := validHR(hr)
	if len(valid) < 10 {
		return 0, ErrTooShort
	}
	sort.Float64s(valid)
	n := len(valid)
	if n%2 == 1 {
		return valid[n/2], nil
	}
	return (valid[n/2-1] + valid[n/2]) / 2, nil
}

// Workout is the summary of a recorded session.
type Workout struct {
	Avg, Max float64
}

// Summarize averages and peaks the bpm samples of a workout.
func Summarize(hr []float64) (Workout, error) {
	valid := validHR(hr)
	if len(valid) < 10 {
		return Workout{}, ErrTooShort
	}
	var w Workout
	for _, v := range valid {
		w.Avg += v
		w.Max = math.Max(w.Max, v)
	}
	w.Avg /= float64(len(valid))
	return w, nil
}

// validHR drops zeros (strap not yet locked on) and out-of-range readings.
func validHR(hr []float64) []float64 {
	out := make([]float64, 0, len(hr))
	for _, v := range hr {
		if v >= 25 && v <= 250 {
			out = append(out, v)
		}
	}
	return out
}
