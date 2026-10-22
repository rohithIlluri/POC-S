// Package science turns heart data into the numbers vitals shows: max heart
// rate, cardiovascular load and strain, recovery against a personal baseline,
// stress, respiratory rate and fitness. Pure functions, no I/O.
//
// These are vitals' own calculations from published research (see
// docs/metrics-plan.md for the evidence and which parts are verified). They
// are not WHOOP's algorithms and will not reproduce WHOOP's scores. Choices
// that are this project's own design say so, and are meant to be tuned on real
// data.
package science

import (
	"math"
	"time"
)

// Sex selects sex-specific constants where a method has them.
type Sex int

const (
	Male Sex = iota + 1
	Female
)

// ParseSex reads "male" / "female" (any case); ok is false otherwise.
func ParseSex(s string) (Sex, bool) {
	switch s {
	case "male", "Male", "MALE", "m", "M":
		return Male, true
	case "female", "Female", "FEMALE", "f", "F":
		return Female, true
	}
	return 0, false
}

// Age is the age in years of someone born in birthYear, at now.
func Age(birthYear int, now time.Time) float64 {
	return float64(now.Year() - birthYear)
}

// AgeAt is the exact age in years of someone born on birth, at now (for
// example 27.2), counting whole years since the last birthday plus the
// fraction of the year that has passed.
func AgeAt(birth, now time.Time) float64 {
	by, bm, bd := birth.Date()
	years := now.Year() - by
	last := time.Date(now.Year(), bm, bd, 0, 0, 0, 0, now.Location())
	if last.After(now) {
		years--
		last = time.Date(now.Year()-1, bm, bd, 0, 0, 0, 0, now.Location())
	}
	next := time.Date(last.Year()+1, bm, bd, 0, 0, 0, 0, now.Location())
	return float64(years) + now.Sub(last).Seconds()/next.Sub(last).Seconds()
}

// MaxHRTanaka is the age-predicted maximal heart rate, 208 − 0.7 × age
// (Tanaka, Monahan & Seals, J Am Coll Cardiol 2001;37:153-6). It does not
// differ between men and women or with habitual activity. Individual scatter
// is large (SEE about 11-12 bpm), so see MaxHR.
func MaxHRTanaka(age float64) float64 { return 208 - 0.7*age }

// MaxHR is the larger of the age-based estimate and the highest heart rate
// you have actually recorded, which beats the estimate whenever it is higher.
func MaxHR(age, observedMax float64) float64 {
	return math.Max(MaxHRTanaka(age), observedMax)
}

// VO2maxUth estimates VO2max in ml/kg/min as 15.3 × HRmax / HRrest (Uth et al.,
// Eur J Appl Physiol 2004). The error was 2.7 ml/kg/min with a measured HRmax
// and 4.7 with an age-predicted one, in well-trained men aged 21-51; treat it
// as a rough estimate elsewhere.
func VO2maxUth(hrMax, hrRest float64) (float64, bool) {
	if hrRest <= 0 || hrMax <= hrRest {
		return 0, false
	}
	return 15.3 * hrMax / hrRest, true
}

// Phi is the standard normal cumulative distribution function.
func Phi(z float64) float64 { return 0.5 * (1 + math.Erf(z/math.Sqrt2)) }

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
