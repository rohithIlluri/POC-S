package science

import (
	"math"
	"time"
)

// HRPoint is one heart-rate reading.
type HRPoint struct {
	T  time.Time
	HR float64
}

// Interval is a heart rate held for some seconds.
type Interval struct {
	HR      float64
	Seconds float64
}

const (
	minPlausibleHR = 30
	maxPlausibleHR = 230
)

// Intervals turns readings (in time order) into held intervals. Each reading
// lasts until the next one, but never longer than maxGap, so a strap that
// went quiet doesn't count as an hour at the last heart rate. Implausible
// readings are dropped.
func Intervals(points []HRPoint, maxGap time.Duration) []Interval {
	var out []Interval
	for i, p := range points {
		if p.HR < minPlausibleHR || p.HR > maxPlausibleHR {
			continue
		}
		d := time.Second
		if i+1 < len(points) {
			d = points[i+1].T.Sub(p.T)
		}
		if d <= 0 {
			continue
		}
		if d > maxGap {
			d = maxGap
		}
		out = append(out, Interval{HR: p.HR, Seconds: d.Seconds()})
	}
	return out
}

// ZoneOf returns the heart-rate zone of hr as a share of max heart rate:
// 0 below 50%, then 1 (50-60%), 2 (60-70%), 3 (70-80%), 4 (80-90%), 5 (90%+).
// These are the same bands Edwards weights 1 to 5.
func ZoneOf(hr, hrMax float64) int {
	if hrMax <= 0 {
		return 0
	}
	switch x := hr / hrMax; {
	case x >= 0.9:
		return 5
	case x >= 0.8:
		return 4
	case x >= 0.7:
		return 3
	case x >= 0.6:
		return 2
	case x >= 0.5:
		return 1
	}
	return 0
}

// ZoneMinutes is the time spent in each zone (index as ZoneOf), in minutes.
func ZoneMinutes(iv []Interval, hrMax float64) [6]float64 {
	var z [6]float64
	for _, i := range iv {
		z[ZoneOf(i.HR, hrMax)] += i.Seconds / 60
	}
	return z
}

// EdwardsTRIMP is the zone-weighted training impulse (Edwards 1993): minutes
// spent at 50-60%, 60-70%, 70-80%, 80-90% and 90-100% of max heart rate,
// weighted 1 to 5, summed. Below 50% counts for nothing.
func EdwardsTRIMP(iv []Interval, hrMax float64) float64 {
	if hrMax <= 0 {
		return 0
	}
	var total float64
	for _, i := range iv {
		x := i.HR / hrMax
		w := 0.0
		switch {
		case x >= 0.9:
			w = 5
		case x >= 0.8:
			w = 4
		case x >= 0.7:
			w = 3
		case x >= 0.6:
			w = 2
		case x >= 0.5:
			w = 1
		}
		total += i.Seconds / 60 * w
	}
	return total
}

// BanisterTRIMP is the exponentially weighted training impulse: minutes ×
// x × a·e^(b·x) with x the fraction of heart-rate reserve, (HR − rest) /
// (max − rest). Men use a = 0.64, b = 1.92; women a = 0.86, b = 1.67.
//
// Status: the constants were found on several independent pages but not in
// the primary paper, and one worked example elsewhere disagreed with the
// formula, so treat the absolute value as approximate. Edwards is the
// primary load measure; this one is for cross-checking.
func BanisterTRIMP(iv []Interval, hrRest, hrMax float64, sex Sex) float64 {
	if hrMax <= hrRest {
		return 0
	}
	a, b := 0.64, 1.92
	if sex == Female {
		a, b = 0.86, 1.67
	}
	var total float64
	for _, i := range iv {
		x := clamp((i.HR-hrRest)/(hrMax-hrRest), 0, 1)
		total += i.Seconds / 60 * x * a * math.Exp(b*x)
	}
	return total
}

// StrainTau sets the strain scale: strain = 21 × (1 − e^(−TRIMP/StrainTau)).
// This mapping is vitals' own design: it rises steeply for the first hour of
// real work, saturates toward 21, and needs no history. 200 puts an hour of
// steady work at 70-80% of max heart rate (Edwards load 180) near 12. Tune it
// against your own days.
const StrainTau = 200.0

// Strain maps an Edwards training impulse to a 0-21 day strain.
func Strain(trimp float64) float64 {
	if trimp <= 0 {
		return 0
	}
	return 21 * (1 - math.Exp(-trimp/StrainTau))
}
