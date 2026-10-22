package science

import (
	"math"
	"sort"
)

// BaevskyBinMs is the histogram bin width for the stress index. The index
// depends on it, so it is fixed here; published normal ranges are
// inconsistent, which is why vitals only reports stress relative to your own
// baseline (StressLevel), never as an absolute band.
const BaevskyBinMs = 50

// BaevskyIndex is the stress index of an RR series (ms): AMo / (2 · Mo ·
// MxDMn), with Mo the modal interval in seconds, AMo the percentage of
// intervals in the modal bin, and MxDMn the range in seconds. ok is false for
// fewer than 30 intervals or no spread.
func BaevskyIndex(rr []float64) (float64, bool) {
	if len(rr) < 30 {
		return 0, false
	}
	bins := map[int]int{}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range rr {
		bins[int(v/BaevskyBinMs)]++
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	if hi-lo <= 0 {
		return 0, false
	}
	keys := make([]int, 0, len(bins))
	for k := range bins {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	modeBin, best := keys[0], 0
	for _, k := range keys {
		if bins[k] > best {
			modeBin, best = k, bins[k]
		}
	}
	mo := (float64(modeBin) + 0.5) * BaevskyBinMs / 1000
	amo := 100 * float64(best) / float64(len(rr))
	return amo / (2 * mo * (hi - lo) / 1000), true
}

// StressReading is a period of stillness to judge.
type StressReading struct {
	LnRMSSD float64
	HR      float64
}

// StressLevel is 0-100: how far this period sits toward "stressed" against
// your own baselines. It rises when RMSSD is below your usual and heart rate
// above it. The caller must pass only periods where you were still (motion
// low), otherwise exercise reads as stress; that gating needs the strap's
// motion data. Equal weights and the normal-curve mapping are vitals' own
// design.
func StressLevel(r StressReading, hrv, hr Baseline) (float64, bool) {
	if hrv.N < MinProvisionalNights || hr.N < MinProvisionalNights {
		return 0, false
	}
	z := 0.5*(-hrv.Z(r.LnRMSSD)) + 0.5*hr.Z(r.HR)
	return 100 * Phi(z), true
}
