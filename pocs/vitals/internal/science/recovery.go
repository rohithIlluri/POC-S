package science

import (
	"math"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/hrv"
)

// MinSD stops a very steady baseline from turning a tiny wobble into a huge
// z-score. In natural-log RMSSD units a typical day-to-day SD is about 0.1.
const MinSD = 0.03

// SWCFactor sets the smallest-worthwhile-change band, baseline mean ± factor ×
// SD. 0.5 is the usual convention but was not confirmed against Plews et al.,
// so it is a tunable design choice.
const SWCFactor = 0.5

// LnRMSSD is the natural log of RMSSD of cleaned RR intervals (ms); ok is
// false when there are too few intervals.
func LnRMSSD(rr []float64) (float64, bool) {
	v, ok := hrv.RMSSD(hrv.Clean(rr))
	if !ok || v <= 0 {
		return 0, false
	}
	return math.Log(v), true
}

// MinHRVPairs is how many successive-beat differences an HRV figure needs
// (about 30 s of beats).
const MinHRVPairs = 30

// RMSSDRuns is RMSSD over several separate runs of consecutive beats. The
// strap's realtime stream delivers RR in bursts with gaps between them, and a
// difference taken across a gap is not a heartbeat-to-heartbeat difference, so
// each run is cleaned and differenced on its own and the squared differences
// are pooled. ok is false with fewer than MinHRVPairs differences.
func RMSSDRuns(runs [][]float64) (rmssd float64, pairs int, ok bool) {
	var sum float64
	for _, run := range runs {
		c := hrv.Clean(run)
		for i := 1; i < len(c); i++ {
			d := c[i] - c[i-1]
			sum += d * d
			pairs++
		}
	}
	if pairs < MinHRVPairs {
		return 0, pairs, false
	}
	return math.Sqrt(sum / float64(pairs)), pairs, true
}

// Baseline is a personal mean and spread over recent days.
type Baseline struct {
	Mean, SD float64
	N        int
}

// NewBaseline computes the mean and sample standard deviation of values.
func NewBaseline(values []float64) Baseline {
	b := Baseline{N: len(values)}
	if b.N == 0 {
		return b
	}
	for _, v := range values {
		b.Mean += v
	}
	b.Mean /= float64(b.N)
	if b.N > 1 {
		var ss float64
		for _, v := range values {
			ss += (v - b.Mean) * (v - b.Mean)
		}
		b.SD = math.Sqrt(ss / float64(b.N-1))
	}
	return b
}

// Z is how many (floored) standard deviations v sits from the baseline mean.
func (b Baseline) Z(v float64) float64 {
	return (v - b.Mean) / math.Max(b.SD, MinSD)
}

// SWCBand is the range of ordinary variation around the baseline mean.
func (b Baseline) SWCBand() (lo, hi float64) {
	d := SWCFactor * math.Max(b.SD, MinSD)
	return b.Mean - d, b.Mean + d
}

// RollingMean is the mean of the last n values (fewer if fewer exist). The
// HRV-guided-training literature uses a 7-day rolling mean of ln RMSSD rather
// than a single morning, because it follows accumulated fatigue and ignores
// one noisy night.
func RollingMean(values []float64, n int) float64 {
	if len(values) == 0 || n <= 0 {
		return 0
	}
	if len(values) > n {
		values = values[len(values)-n:]
	}
	var s float64
	for _, v := range values {
		s += v
	}
	return s / float64(len(values))
}

// Nights is the number of nights a baseline needs before it is trusted.
const (
	MinProvisionalNights = 7  // shown, labelled "calibrating"
	MinFullNights        = 28 // four baseline weeks
)

// RecoveryInput is last night's reading.
type RecoveryInput struct {
	LnRMSSD   float64 // ln RMSSD over the sleep window
	RestingHR float64 // lowest sustained heart rate asleep
}

// RecoveryBaselines are the personal baselines the reading is compared with.
type RecoveryBaselines struct {
	HRV Baseline // of nightly ln RMSSD
	RHR Baseline // of nightly resting heart rate
}

// Recovery is the readiness result.
type Recovery struct {
	Score float64 // 0-100: where last night sits in your own distribution
	Tier  string  // "low" | "moderate" | "high"
	ZHRV  float64
	ZRHR  float64
	Full  bool // false while still calibrating (fewer than MinFullNights)
}

// Recovery scores last night against your own baseline. Higher HRV and lower
// resting heart rate than usual raise it. The weights (0.7 HRV, 0.3 resting
// HR), the normal-curve mapping and the tier cut-offs (34, 67) are vitals' own
// design. ok is false until there are MinProvisionalNights nights in both
// baselines.
func RecoveryScore(in RecoveryInput, b RecoveryBaselines) (Recovery, bool) {
	if b.HRV.N < MinProvisionalNights || b.RHR.N < MinProvisionalNights {
		return Recovery{}, false
	}
	r := Recovery{
		ZHRV: b.HRV.Z(in.LnRMSSD),
		ZRHR: b.RHR.Z(in.RestingHR),
		Full: b.HRV.N >= MinFullNights && b.RHR.N >= MinFullNights,
	}
	z := 0.7*r.ZHRV - 0.3*r.ZRHR
	r.Score = 100 * Phi(z)
	switch {
	case r.Score < 34:
		r.Tier = "low"
	case r.Score < 67:
		r.Tier = "moderate"
	default:
		r.Tier = "high"
	}
	return r, true
}
