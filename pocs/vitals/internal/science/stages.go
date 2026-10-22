package science

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Sleep stages (wake / light / deep / REM) estimated from what the strap
// records: heart rate (about 1 Hz), beat-to-beat RR intervals (with gaps), a
// gravity vector and a movement value per record. No EEG, no trained model.
//
// What this is, honestly: a transparent, rule-based classifier on 30-second
// epochs. Every threshold below is OURS and UNVALIDATED (no polysomnography
// was available); each rule follows a direction of change that published
// physiology reports, and the papers that report it are cited. It is NOT a
// reproduction of any published classifier: the published HR+motion stagers
// (Walch 2019, Beattie 2017, Fonseca 2017, Radha 2019) learned their weights
// from polysomnography and did not publish them, so they cannot be rebuilt
// here and no fake coefficients are shipped.
//
// What the literature says to expect from this class of method (wrist motion +
// optical heart rate, 4 stages, against polysomnography):
//   - Beattie et al. 2017, Physiol Meas 38:1968 (Fitbit-type wrist PPG + accel,
//     60 adults): 69 % epoch accuracy, kappa 0.52.
//   - Fonseca et al. 2017, Sleep 40(7) zsx097 (wrist PPG HRV + accel): 4-class
//     accuracy 59.3 %, kappa 0.42; 3-class (wake/NREM/REM) 72.9 %, kappa 0.46.
//   - Walch et al. 2019, Sleep 42(12) zsz180 (Apple Watch HR + accel): wake /
//     NREM / REM about 72 % with kappa 0.28 in their cohort.
//   - Miller et al. 2020, J Sports Sci 38:2631 (WHOOP's own algorithm): 4-stage
//     agreement 64 %, kappa 0.47; Miller et al. 2022, Sensors 22:6317: WHOOP
//     3.0 multi-state 60 %, kappa 0.44 (Oura 61 %, Apple 53 %, Garmin 50 %).
//   - Chinoy et al. 2021, Sleep 44(5) zsaa291: consumer devices detect sleep
//     well (sensitivity >= 0.93) but wake poorly (specificity 0.18-0.54), and
//     stage estimates were "inconsistent".
// A hand-tuned rule set should be expected to do no better than these trained
// models, and probably worse. Treat the output as a rough sketch of the night.
//
// The physiology the rules lean on (all from abstracts/papers read for this
// project; see docs/sleep-staging-research.md):
//   - Heart rate falls and parasympathetic (HF) activity rises in NREM sleep;
//     autonomic balance in REM is "in general similar to wakefulness"
//     (Trinder et al. 2001, J Sleep Res 10:253). Heart rate also drifts down
//     across the night independent of stage (same paper: a time effect), so
//     heart rate is compared with its own slow trend, not with the night mean.
//   - Total HRV power and LF/HF are highest in REM; HF is higher in stage 2 and
//     stage 4 NREM than in REM (Busek et al. 2005, Physiol Res 54:369).
//   - Breathing in REM is faster and shallower with the lowest ventilation
//     (Douglas et al. 1982, Thorax 37:840).
//   - Slow-wave activity peaks in the first NREM episode and declines across
//     consecutive episodes; NREM-REM cycles last about 80-120 minutes and REM is
//     about 18-25 % of sleep (Dijk 2009, J Clin Sleep Med 5(2 Suppl):S6).
//   - Subject-specific z-score normalisation of cardiorespiratory features
//     (Fonseca et al. 2015, Physiol Meas 36:2027) is used for the same reason
//     here: absolute HR and HRV differ a lot between people.

// Stage is one epoch's estimated sleep stage.
type Stage int8

const (
	StageNoData Stage = -1 // strap off or no optical lock: not scored
	StageWake   Stage = 0
	StageLight  Stage = 1 // N1 + N2
	StageDeep   Stage = 2 // N3, slow-wave sleep
	StageREM    Stage = 3
)

func (s Stage) String() string {
	switch s {
	case StageWake:
		return "wake"
	case StageLight:
		return "light"
	case StageDeep:
		return "deep"
	case StageREM:
		return "rem"
	}
	return "nodata"
}

// MarshalText makes stages read as words in JSON.
func (s Stage) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// StageSample is one strap record.
type StageSample struct {
	T        time.Time
	HR       int       // bpm; 0 = no optical lock
	RR       []float64 // beat-to-beat intervals in this record, ms
	G        [3]float64
	Movement float64 // the strap's movement value; NaN when unknown
	Device   int     // strap's own state 0 wake, 1 still, 2 sleep, 3 up; -1 unknown
}

// StageOptions tunes the classifier. The zero value is not useful; start from
// DefaultStageOptions.
type StageOptions struct {
	// TimePrior nudges deep sleep towards the start of the sleep period and REM
	// towards the end (Dijk 2009: slow-wave activity declines across the night).
	// It is a prior, so a sanity check that deep sleep is early is partly
	// circular while it is on; set 0 to switch it off.
	TimePrior float64
}

// DefaultStageOptions is what /api/sleep uses.
func DefaultStageOptions() StageOptions { return StageOptions{TimePrior: 0.25} }

// Heuristic constants. All OURS, unvalidated; see the comments for the reason
// behind each one.
const (
	StageEpoch = 30 * time.Second // the AASM scoring epoch length

	stageMargin       = 30 * time.Minute // scored before and after the sleep period
	stageWin          = 5                // half-width in epochs of the 5.5-minute feature window
	stageMinHRPerEp   = 10               // an epoch needs this many records with HR to be scored
	stageMinHRWindow  = 60               // HR values needed for the windowed HR SD
	stageMinRRPairs   = 60               // valid successive RR pairs needed for RMSSD (about 1 minute of beats in 5.5)
	stageMoveFactor   = 4.0              // "active": movement above 4x the night's median...
	stageAngleActive  = 10.0             // ...or the arm angle swinging more than 10 degrees inside the epoch
	stageWakeHRRise   = 4.0              // bpm above the local trend that, with movement, marks an arousal to wake
	stageWakeSustain  = 3                // or this many active epochs in a row (1.5 min of movement)
	stageScoreSmooth  = 2                // half-width of the moving average on stage scores (2.5 min)
	stageThreshold    = 0.6              // z-score a deep/REM score must reach
	stageMinBout      = 10               // deep and REM bouts shorter than 5 min become light
	stageREMGapFill   = 6                // light gaps <= 3 min inside a REM period are REM
	stageNoREMFirst   = 60               // no REM in the first 30 min after sleep onset (sleep-onset REM is atypical in healthy adults)
	stagePersistent   = 20               // sleep onset = first 10 minutes of unbroken sleep
	stageCycleMerge   = 40               // REM bouts closer than 20 min belong to one REM period
	stageLowConf      = 0.6              // epochs below this confidence are flagged
	stageRRPairJump   = 0.2              // a successive RR difference over 20 % is treated as an artefact
	stageMinRR        = 300.0
	stageMaxRR        = 2000.0
	stageMinSleepSpan = 20 * time.Minute
)

// StageEpochOut is one scored epoch with the features behind it (NaN when a
// feature could not be computed).
type StageEpochOut struct {
	Start      time.Time
	Stage      Stage
	Confidence float64 // a heuristic 0-1 margin, NOT a calibrated probability
	HR         float64 // mean bpm in the epoch
	HRDev      float64 // HR minus its 90-minute running median
	HRSD       float64 // SD of 1 Hz HR in the 5.5-min window
	RMSSD      float64 // ms, 5.5-min window, NaN when RR is insufficient
	Resp       float64 // breaths/min from RR (respiratory sinus arrhythmia), NaN when no clean run
	Move       float64 // mean strap movement value
	Angle      float64 // range of the arm angle in the epoch, degrees
	RRCoverage float64 // share of the epoch covered by RR intervals
	Device     int     // strap's own state (majority), -1 unknown
	DeepScore  float64
	REMScore   float64
}

// StageRun is a run of consecutive epochs with the same stage.
type StageRun struct {
	Stage         Stage     `json:"stage"`
	Start         time.Time `json:"start"`
	End           time.Time `json:"end"`
	Epochs        int       `json:"epochs"`
	Confidence    float64   `json:"confidence"`     // mean epoch confidence
	LowConfidence int       `json:"low_confidence"` // epochs in the run below the flag level
}

// StageCycle is one NREM-REM cycle: from sleep onset (or the end of the
// previous REM period) to the end of the next REM period.
type StageCycle struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Minutes     float64   `json:"minutes"`
	DeepMinutes float64   `json:"deep_minutes"`
	REMMinutes  float64   `json:"rem_minutes"`
	Complete    bool      `json:"complete"` // false for the tail after the last REM period
}

// StageTotals are the night's summary numbers.
type StageTotals struct {
	WakeMin   float64 `json:"wake_min"` // whole scored span, including before onset and after final wake
	LightMin  float64 `json:"light_min"`
	DeepMin   float64 `json:"deep_min"`
	REMMin    float64 `json:"rem_min"`
	NoDataMin float64 `json:"nodata_min"`
	TSTMin    float64 `json:"tst_min"`   // light + deep + REM
	LightPct  float64 `json:"light_pct"` // of total sleep time
	DeepPct   float64 `json:"deep_pct"`
	REMPct    float64 `json:"rem_pct"`
	// Rest onset is the start of the motion sleep period (van Hees 2018), a
	// proxy for lights-out, which the strap cannot know.
	RestOnset     time.Time `json:"rest_onset"`
	SleepOnset    time.Time `json:"sleep_onset"` // first 10 min of unbroken sleep
	FinalWake     time.Time `json:"final_wake"`  // end of the last sleep epoch
	SOLMin        float64   `json:"sol_min"`     // rest onset to sleep onset (proxy for sleep onset latency)
	REMLatencyMin float64   `json:"rem_latency_min"`
	HaveREM       bool      `json:"have_rem"`
	WASOMin       float64   `json:"waso_min"` // wake between sleep onset and final wake
	Cycles        int       `json:"cycles"`   // complete NREM-REM cycles
}

// StageCheck is a physiology sanity check on the result. Independent is false
// when the rule set itself pushes towards a pass (the check is then circular
// and says little about accuracy).
type StageCheck struct {
	Name        string `json:"name"`
	Pass        bool   `json:"pass"`
	Independent bool   `json:"independent"`
	Detail      string `json:"detail"`
}

// Hypnogram is one night's stage estimate.
type Hypnogram struct {
	Method              string             `json:"method"`
	Estimate            bool               `json:"estimate"` // always true
	EpochSeconds        int                `json:"epoch_seconds"`
	Start               time.Time          `json:"start"`
	End                 time.Time          `json:"end"`
	Runs                []StageRun         `json:"runs"`
	Totals              StageTotals        `json:"totals"`
	Cycles              []StageCycle       `json:"cycles"`
	Confidence          string             `json:"confidence"` // "low" or "very low": this method is unvalidated
	MeanConfidence      float64            `json:"mean_confidence"`
	LowConfidenceEpochs int                `json:"low_confidence_epochs"`
	RRCoverage          float64            `json:"rr_coverage"`   // in the sleep period
	RRSufficient        bool               `json:"rr_sufficient"` // RMSSD available for most sleep epochs
	DeviceAgreement     float64            `json:"device_agreement"`
	DeviceKappa         float64            `json:"device_kappa"`
	StageHR             map[string]float64 `json:"stage_hr"` // mean bpm per stage
	Checks              []StageCheck       `json:"checks"`
	Caveats             []string           `json:"caveats"`
	Epochs              []StageEpochOut    `json:"-"`
}

// StageNight scores 30-second epochs from window.Start-30min to
// window.End+30min (clipped to the data). Sleep is only allowed inside the
// motion sleep period (SleepPeriod). Samples must be in time order. ok is false
// when there is too little to score.
func StageNight(s []StageSample, w SleepWindow, opt StageOptions) (Hypnogram, bool) {
	h := Hypnogram{
		Method: "rule-based 30 s epochs (ours, unvalidated): wake from movement + heart-rate rise inside the van Hees 2018 sleep period; " +
			"deep = heart rate below its own trend with steady HR, REM = heart rate above trend with variable HR and lower RMSSD, " +
			"light otherwise; features z-scored per night (Fonseca 2015); minimum 5-min deep/REM bouts",
		Estimate:     true,
		EpochSeconds: int(StageEpoch / time.Second),
		Confidence:   "low",
		StageHR:      map[string]float64{},
	}
	if len(s) == 0 || w.End.Sub(w.Start) < stageMinSleepSpan {
		return h, false
	}
	start := w.Start.Add(-stageMargin).Truncate(StageEpoch)
	if first := s[0].T.Truncate(StageEpoch); first.After(start) {
		start = first
	}
	end := w.End.Add(stageMargin)
	if last := s[len(s)-1].T.Add(StageEpoch).Truncate(StageEpoch); last.Before(end) {
		end = last
	}
	n := int(end.Sub(start) / StageEpoch)
	if n < 2*stageMinBout {
		return h, false
	}
	h.Start, h.End = start, start.Add(time.Duration(n)*StageEpoch)
	idx := func(t time.Time) int { return int(t.Sub(start) / StageEpoch) }
	inWin := func(k int) bool {
		t := start.Add(time.Duration(k) * StageEpoch)
		return !t.Before(w.Start) && t.Before(w.End)
	}

	// ---- per-epoch raw features
	ep := make([]StageEpochOut, n)
	type acc struct {
		hrSum          float64
		hrN, recN      int
		mvSum          float64
		mvN            int
		angMin, angMax float64
		rrMs           float64
		dev            [4]int
		hrVals         []float64
	}
	a := make([]acc, n)
	for i := range a {
		a[i].angMin, a[i].angMax = math.Inf(1), math.Inf(-1)
	}
	// Beats in record order. A record with no RR breaks the chain, so a
	// successive difference is only taken between beats known to be adjacent.
	type pair struct {
		k int
		d float64
	}
	var pairs []pair
	type beat struct {
		k  int
		v  float64
		ok bool
	}
	var beats [][]beat // contiguous runs
	var run []beat
	prevRR := math.NaN()
	for _, x := range s {
		k := idx(x.T)
		if k < 0 || k >= n {
			prevRR = math.NaN()
			if len(run) > 0 {
				beats, run = append(beats, run), nil
			}
			continue
		}
		e := &a[k]
		e.recN++
		if x.HR > 0 {
			e.hrSum += float64(x.HR)
			e.hrN++
			e.hrVals = append(e.hrVals, float64(x.HR))
		}
		if !math.IsNaN(x.Movement) && !math.IsInf(x.Movement, 0) {
			e.mvSum += x.Movement
			e.mvN++
		}
		if ang := math.Atan2(x.G[2], math.Hypot(x.G[0], x.G[1])) * 180 / math.Pi; !math.IsNaN(ang) {
			e.angMin, e.angMax = math.Min(e.angMin, ang), math.Max(e.angMax, ang)
		}
		if x.Device >= 0 && x.Device < 4 {
			e.dev[x.Device]++
		}
		if len(x.RR) == 0 {
			prevRR = math.NaN()
			if len(run) > 0 {
				beats, run = append(beats, run), nil
			}
			continue
		}
		for _, v := range x.RR {
			good := v >= stageMinRR && v <= stageMaxRR
			if good {
				e.rrMs += v
			}
			if good && !math.IsNaN(prevRR) && math.Abs(v-prevRR) <= stageRRPairJump*prevRR {
				pairs = append(pairs, pair{k, v - prevRR})
			}
			if good {
				prevRR = v
				run = append(run, beat{k, v, true})
			} else {
				prevRR = math.NaN()
				if len(run) > 0 {
					beats, run = append(beats, run), nil
				}
			}
		}
	}
	if len(run) > 0 {
		beats = append(beats, run)
	}
	for k := range ep {
		e := &ep[k]
		e.Start = start.Add(time.Duration(k) * StageEpoch)
		e.HR, e.Move, e.Angle = math.NaN(), math.NaN(), math.NaN()
		e.HRDev, e.HRSD, e.RMSSD, e.Resp = math.NaN(), math.NaN(), math.NaN(), math.NaN()
		e.DeepScore, e.REMScore = math.NaN(), math.NaN()
		e.Device = -1
		if a[k].hrN >= stageMinHRPerEp {
			e.HR = a[k].hrSum / float64(a[k].hrN)
		}
		if a[k].mvN > 0 {
			e.Move = a[k].mvSum / float64(a[k].mvN)
		}
		if a[k].angMax >= a[k].angMin {
			e.Angle = a[k].angMax - a[k].angMin
		}
		e.RRCoverage = math.Min(1, a[k].rrMs/float64(StageEpoch/time.Millisecond))
		best, bestN := -1, 0
		for st, c := range a[k].dev {
			if c > bestN {
				best, bestN = st, c
			}
		}
		e.Device = best
	}

	// ---- movement: "active" epochs. Body movement corrupts optical heart rate
	// and RR, so active epochs are left out of the heart-rate windows below.
	var winMoves []float64
	for k := range ep {
		if inWin(k) && !math.IsNaN(ep[k].Move) {
			winMoves = append(winMoves, ep[k].Move)
		}
	}
	moveThr := math.Inf(1)
	if len(winMoves) > 0 {
		moveThr = stageMoveFactor * median(winMoves)
	}
	active := make([]bool, n)
	for k := range ep {
		active[k] = (!math.IsNaN(ep[k].Move) && ep[k].Move > moveThr) || (!math.IsNaN(ep[k].Angle) && ep[k].Angle > stageAngleActive)
	}

	// ---- windowed features (5.5 minutes centred on the epoch, still epochs only)
	pairsBy := make([][]float64, n)
	for _, p := range pairs {
		pairsBy[p.k] = append(pairsBy[p.k], p.d)
	}
	for k := range ep {
		lo, hi := max(0, k-stageWin), min(n-1, k+stageWin)
		var hrs []float64
		var sq float64
		var np int
		for j := lo; j <= hi; j++ {
			if active[j] {
				continue
			}
			hrs = append(hrs, a[j].hrVals...)
			for _, d := range pairsBy[j] {
				sq += d * d
				np++
			}
		}
		if len(hrs) >= stageMinHRWindow {
			ep[k].HRSD = sd(hrs)
		}
		if np >= stageMinRRPairs {
			ep[k].RMSSD = math.Sqrt(sq / float64(np))
		}
	}
	// HR trend: a robust straight line (Theil-Sen) through the still epochs of
	// the sleep period, so the slow overnight fall of heart rate (Trinder 2001:
	// a time effect independent of stage) does not read as "deeper and
	// deeper". Outside the sleep period the same is done separately.
	for _, side := range []bool{true, false} {
		var xs, ys []float64
		for k := range ep {
			if inWin(k) == side && !active[k] && !math.IsNaN(ep[k].HR) {
				xs, ys = append(xs, float64(k)), append(ys, ep[k].HR)
			}
		}
		if len(xs) < 2*stageMinBout {
			continue
		}
		icpt, slope := theilSen(xs, ys)
		for k := range ep {
			if inWin(k) == side && !math.IsNaN(ep[k].HR) {
				ep[k].HRDev = ep[k].HR - (icpt + slope*float64(k))
			}
		}
	}
	// Breathing rate per epoch from a 2.5-minute contiguous beat run centred on
	// it (RespiratoryRate needs an unbroken series). Not used for staging; it is
	// an independent cross-check (REM breathing differs, Douglas 1982).
	byEpoch := make([][]int, n) // run index list per epoch
	for ri, r := range beats {
		seen := -1
		for _, b := range r {
			if b.k != seen {
				byEpoch[b.k] = append(byEpoch[b.k], ri)
				seen = b.k
			}
		}
	}
	for k := range ep {
		for _, ri := range byEpoch[k] {
			var rr []float64
			for _, b := range beats[ri] {
				if b.k >= k-2 && b.k <= k+2 {
					rr = append(rr, b.v)
				}
			}
			if v, ok := RespiratoryRate(rr); ok {
				ep[k].Resp = v
				break
			}
		}
	}

	// ---- wake vs sleep
	conf := make([]float64, n)
	stage := make([]Stage, n)
	for k := range ep {
		switch {
		case a[k].recN == 0 || math.IsNaN(ep[k].HR):
			stage[k] = StageNoData
		case !inWin(k):
			stage[k], conf[k] = StageWake, 0.85
		default:
			stage[k] = StageLight // decided below
		}
	}
	for k := range ep {
		if stage[k] != StageLight || !active[k] {
			continue
		}
		sustained := 1
		for j := k - 1; j >= 0 && active[j]; j-- {
			sustained++
		}
		for j := k + 1; j < n && active[j]; j++ {
			sustained++
		}
		hrUp := !math.IsNaN(ep[k].HRDev) && ep[k].HRDev >= stageWakeHRRise
		switch {
		case hrUp && sustained >= stageWakeSustain:
			stage[k], conf[k] = StageWake, 0.8
		case hrUp || sustained >= stageWakeSustain:
			stage[k], conf[k] = StageWake, 0.65
		}
	}

	// ---- sleep onset (needed for the REM latency guard and the time prior)
	onset, final := -1, -1
	for k, c := 0, 0; k < n; k++ {
		if isSleep(stage[k]) {
			c++
			if c == stagePersistent {
				onset = k - stagePersistent + 1
				break
			}
		} else {
			c = 0
		}
	}
	if onset < 0 {
		h.Epochs = ep
		h.Caveats = append(h.Caveats, "no 10-minute stretch of sleep was found, so no stages were scored")
		return h, false
	}
	for k := n - 1; k >= onset; k-- {
		if isSleep(stage[k]) {
			final = k
			break
		}
	}

	// ---- deep / REM scores on the sleep epochs, z-scored per night
	var sl []int
	for k := onset; k <= final; k++ {
		if isSleep(stage[k]) {
			sl = append(sl, k)
		}
	}
	// Robust per-night z-scores (median and MAD, clipped to +/-3) so a few
	// movement artefacts cannot set the scale.
	z := func(get func(StageEpochOut) float64) []float64 {
		var v []float64
		for _, k := range sl {
			if x := get(ep[k]); !math.IsNaN(x) && !active[k] {
				v = append(v, x)
			}
		}
		out := make([]float64, n)
		for i := range out {
			out[i] = math.NaN()
		}
		if len(v) < 2*stageMinBout {
			return out
		}
		m := median(v)
		dev := make([]float64, len(v))
		for i, x := range v {
			dev[i] = math.Abs(x - m)
		}
		scale := 1.4826 * median(dev)
		if scale == 0 {
			return out
		}
		for _, k := range sl {
			if x := get(ep[k]); !math.IsNaN(x) {
				out[k] = clamp((x-m)/scale, -3, 3)
			}
		}
		return out
	}
	zDev := z(func(e StageEpochOut) float64 { return e.HRDev })
	zSD := z(func(e StageEpochOut) float64 { return e.HRSD })
	// RMSSD rises across the night on its own (Busek 2005 saw RR lengthen
	// cycle by cycle), so log RMSSD is detrended like heart rate.
	lrm := make([]float64, n)
	for k := range lrm {
		lrm[k] = math.NaN()
		if ep[k].RMSSD > 0 {
			lrm[k] = math.Log(ep[k].RMSSD)
		}
	}
	{
		var xs, ys []float64
		for _, k := range sl {
			if !math.IsNaN(lrm[k]) && !active[k] {
				xs, ys = append(xs, float64(k)), append(ys, lrm[k])
			}
		}
		if len(xs) >= 2*stageMinBout {
			icpt, slope := theilSen(xs, ys)
			for k := range lrm {
				lrm[k] -= icpt + slope*float64(k)
			}
		}
	}
	zRM := z(func(e StageEpochOut) float64 {
		k := int(e.Start.Sub(start) / StageEpoch)
		return lrm[k]
	})
	rawDeep := make([]float64, n)
	rawREM := make([]float64, n)
	for k := range rawDeep {
		rawDeep[k], rawREM[k] = math.NaN(), math.NaN()
	}
	span := float64(final - onset + 1)
	for _, k := range sl {
		if math.IsNaN(zDev[k]) || math.IsNaN(zSD[k]) {
			continue
		}
		// Deep: heart rate below its trend and steady (parasympathetic, lowest
		// HRV power in NREM: Trinder 2001, Busek 2005).
		d := (-zDev[k] - zSD[k]) / 2
		// REM: heart rate above trend and variable, HF (RMSSD) lower than NREM
		// (Trinder 2001, Busek 2005). Without RR, HR alone decides.
		r := (zDev[k] + zSD[k]) / 2
		if !math.IsNaN(zRM[k]) {
			r = (zDev[k] + zSD[k] - zRM[k]) / 3
		}
		f := float64(k-onset) / span // 0 at sleep onset, 1 at final wake
		d += opt.TimePrior * (1 - 2*f)
		r += opt.TimePrior * (2*f - 1)
		rawDeep[k], rawREM[k] = d, r
	}
	deep := smooth(rawDeep, stageScoreSmooth)
	rem := smooth(rawREM, stageScoreSmooth)
	for _, k := range sl {
		ep[k].DeepScore, ep[k].REMScore = deep[k], rem[k]
		still := !active[k]
		rrPenalty := 1.0
		if math.IsNaN(ep[k].RMSSD) {
			rrPenalty = 0.85
		}
		switch {
		case math.IsNaN(deep[k]) || math.IsNaN(rem[k]):
			stage[k], conf[k] = StageLight, 0.5
		case still && deep[k] >= stageThreshold && deep[k] >= rem[k]:
			stage[k], conf[k] = StageDeep, rrPenalty*(0.55+0.3*clamp(deep[k]-stageThreshold, 0, 1))
		case still && rem[k] >= stageThreshold && k-onset >= stageNoREMFirst:
			stage[k], conf[k] = StageREM, rrPenalty*(0.55+0.3*clamp(rem[k]-stageThreshold, 0, 1))
		default:
			margin := stageThreshold - math.Max(deep[k], rem[k])
			stage[k], conf[k] = StageLight, rrPenalty*(0.5+0.3*clamp(margin, 0, 1))
		}
	}
	// ---- temporal clean-up: fill short light gaps inside REM periods, then
	// demote deep/REM bouts shorter than 5 minutes to light.
	fillGaps(stage, conf, StageREM, stageREMGapFill)
	for _, st := range []Stage{StageDeep, StageREM} {
		for _, r := range runsOf(stage) {
			if stage[r[0]] == st && r[1]-r[0]+1 < stageMinBout {
				for k := r[0]; k <= r[1]; k++ {
					stage[k], conf[k] = StageLight, 0.5
				}
			}
		}
	}
	for k := range ep {
		ep[k].Stage, ep[k].Confidence = stage[k], conf[k]
	}
	h.Epochs = ep

	// ---- outputs
	epMin := StageEpoch.Minutes()
	var confSum float64
	var confN int
	for _, r := range runsOf(stage) {
		sr := StageRun{Stage: stage[r[0]], Start: ep[r[0]].Start, End: ep[r[1]].Start.Add(StageEpoch), Epochs: r[1] - r[0] + 1}
		var c float64
		for k := r[0]; k <= r[1]; k++ {
			c += conf[k]
			if stage[k] != StageNoData {
				confSum += conf[k]
				confN++
				if conf[k] < stageLowConf {
					sr.LowConfidence++
					h.LowConfidenceEpochs++
				}
			}
		}
		sr.Confidence = round2(c / float64(sr.Epochs))
		h.Runs = append(h.Runs, sr)
	}
	if confN > 0 {
		h.MeanConfidence = round2(confSum / float64(confN))
	}
	t := &h.Totals
	hrBy := map[Stage][]float64{}
	for k, st := range stage {
		switch st {
		case StageWake:
			t.WakeMin += epMin
		case StageLight:
			t.LightMin += epMin
		case StageDeep:
			t.DeepMin += epMin
		case StageREM:
			t.REMMin += epMin
		default:
			t.NoDataMin += epMin
		}
		if !math.IsNaN(ep[k].HR) && st != StageNoData {
			hrBy[st] = append(hrBy[st], ep[k].HR)
		}
	}
	for st, v := range hrBy {
		h.StageHR[st.String()] = round2(mean(v))
	}
	t.TSTMin = t.LightMin + t.DeepMin + t.REMMin
	if t.TSTMin > 0 {
		t.LightPct, t.DeepPct, t.REMPct = round2(100*t.LightMin/t.TSTMin), round2(100*t.DeepMin/t.TSTMin), round2(100*t.REMMin/t.TSTMin)
	}
	t.RestOnset = w.Start
	t.SleepOnset = ep[onset].Start
	t.FinalWake = ep[final].Start.Add(StageEpoch)
	t.SOLMin = math.Max(0, t.SleepOnset.Sub(w.Start).Minutes())
	for k := onset; k <= final; k++ {
		if stage[k] == StageWake {
			t.WASOMin += epMin
		}
		if stage[k] == StageREM && !t.HaveREM {
			t.HaveREM = true
			t.REMLatencyMin = float64(k-onset) * epMin
		}
	}
	h.Cycles = cyclesOf(stage, ep, onset, final)
	for _, c := range h.Cycles {
		if c.Complete {
			t.Cycles++
		}
	}

	// RR coverage and device agreement inside the sleep period.
	var cov float64
	var covN, rmN, slN int
	var agree, tot int
	var both, oursS, devS int
	for k := range ep {
		if !inWin(k) || stage[k] == StageNoData {
			continue
		}
		cov += ep[k].RRCoverage
		covN++
		if isSleep(stage[k]) {
			slN++
			if !math.IsNaN(ep[k].RMSSD) {
				rmN++
			}
		}
	}
	for k := range ep {
		if stage[k] == StageNoData || ep[k].Device < 0 {
			continue
		}
		o, d := isSleep(stage[k]), ep[k].Device == 2
		tot++
		if o == d {
			agree++
		}
		if o && d {
			both++
		}
		if o {
			oursS++
		}
		if d {
			devS++
		}
	}
	if covN > 0 {
		h.RRCoverage = round2(cov / float64(covN))
	}
	h.RRSufficient = slN > 0 && float64(rmN) >= 0.6*float64(slN)
	if tot > 0 {
		po := float64(agree) / float64(tot)
		pS := float64(oursS) / float64(tot) * float64(devS) / float64(tot)
		pW := float64(tot-oursS) / float64(tot) * float64(tot-devS) / float64(tot)
		pe := pS + pW
		h.DeviceAgreement = round2(po)
		if pe < 1 {
			h.DeviceKappa = round2((po - pe) / (1 - pe))
		}
	}
	h.Checks = stageChecks(h, ep, onset, final, opt)
	h.Caveats = append(h.Caveats,
		"Estimate only: no polysomnography was available, so the accuracy of this method on this strap is unknown.",
		"Published wrist HR+motion stagers with trained weights reach about 60-70 % 4-stage agreement (kappa 0.4-0.5) against polysomnography; this hand-made rule set should be expected to do worse.",
		"Thresholds are this project's own heuristics; deep and REM are only called when the evidence is clear, otherwise the epoch stays light.",
		"Wake is hard for every wrist device (specificity 0.18-0.54 in Chinoy 2021): quiet wakefulness in bed is likely scored as sleep.",
	)
	if !h.RRSufficient {
		h.Caveats = append(h.Caveats, "Too few clean RR intervals for RMSSD in much of the night: REM was scored from heart rate alone, which is less reliable.")
		h.Confidence = "very low"
	}
	if w.End.Sub(w.Start) < 3*time.Hour {
		h.Caveats = append(h.Caveats, "Short sleep (under 3 h): night-level normalisation and the cycle checks are not meaningful for naps.")
		h.Confidence = "very low"
	}
	return h, true
}

func isSleep(s Stage) bool { return s == StageLight || s == StageDeep || s == StageREM }

// runsOf returns [first, last] index pairs of equal consecutive stages.
func runsOf(st []Stage) [][2]int {
	var out [][2]int
	for i := 0; i < len(st); {
		j := i
		for j+1 < len(st) && st[j+1] == st[i] {
			j++
		}
		out = append(out, [2]int{i, j})
		i = j + 1
	}
	return out
}

// fillGaps relabels light runs of at most maxGap epochs that sit between two
// runs of stage s.
func fillGaps(st []Stage, conf []float64, s Stage, maxGap int) {
	r := runsOf(st)
	for i := 1; i+1 < len(r); i++ {
		g := r[i]
		if st[g[0]] == StageLight && st[r[i-1][0]] == s && st[r[i+1][0]] == s && g[1]-g[0]+1 <= maxGap {
			for k := g[0]; k <= g[1]; k++ {
				st[k], conf[k] = s, 0.5
			}
		}
	}
}

// cyclesOf cuts the night at the end of each REM period (REM bouts less than
// 20 minutes apart count as one period). This mirrors the usual NREM-REM cycle
// idea; the merge distance is ours.
func cyclesOf(st []Stage, ep []StageEpochOut, onset, final int) []StageCycle {
	type period struct{ a, b int }
	var rems []period
	for _, r := range runsOf(st[onset : final+1]) {
		a, b := r[0]+onset, r[1]+onset
		if st[a] != StageREM {
			continue
		}
		if n := len(rems); n > 0 && a-rems[n-1].b-1 < stageCycleMerge {
			rems[n-1].b = b
		} else {
			rems = append(rems, period{a, b})
		}
	}
	var out []StageCycle
	from := onset
	mk := func(a, b int, complete bool) StageCycle {
		c := StageCycle{Start: ep[a].Start, End: ep[b].Start.Add(StageEpoch), Complete: complete}
		c.Minutes = c.End.Sub(c.Start).Minutes()
		for k := a; k <= b; k++ {
			switch st[k] {
			case StageDeep:
				c.DeepMinutes += StageEpoch.Minutes()
			case StageREM:
				c.REMMinutes += StageEpoch.Minutes()
			}
		}
		return c
	}
	for _, p := range rems {
		out = append(out, mk(from, p.b, true))
		from = p.b + 1
	}
	if from <= final && float64(final-from+1)*StageEpoch.Minutes() >= 30 {
		out = append(out, mk(from, final, false))
	}
	return out
}

func stageChecks(h Hypnogram, ep []StageEpochOut, onset, final int, opt StageOptions) []StageCheck {
	var out []StageCheck
	third := (final - onset + 1) / 3
	count := func(st Stage, a, b int) float64 {
		var c float64
		for k := a; k <= b; k++ {
			if ep[k].Stage == st {
				c += StageEpoch.Minutes()
			}
		}
		return c
	}
	priorNote := ""
	if opt.TimePrior > 0 {
		priorNote = fmt.Sprintf(" (a time prior of %.2f SD pushes this way, so the check is partly circular)", opt.TimePrior)
	}
	if third > 0 {
		d1, d3 := count(StageDeep, onset, onset+third-1), count(StageDeep, final-third+1, final)
		out = append(out, StageCheck{"deep sleep concentrated early", h.Totals.DeepMin > 0 && d1 > d3 && d1 >= h.Totals.DeepMin/3, opt.TimePrior == 0,
			fmt.Sprintf("deep: %.0f min in the first third, %.0f min in the last third, %.0f total%s", d1, d3, h.Totals.DeepMin, priorNote)})
		r1, r3 := count(StageREM, onset, onset+third-1), count(StageREM, final-third+1, final)
		out = append(out, StageCheck{"REM more frequent later", h.Totals.REMMin > 0 && r3 > r1, opt.TimePrior == 0,
			fmt.Sprintf("REM: %.0f min in the first third, %.0f min in the last third%s", r1, r3, priorNote)})
	}
	// REM periods lengthen across the night.
	var remLens []float64
	for _, c := range h.Cycles {
		if c.Complete {
			remLens = append(remLens, c.REMMinutes)
		}
	}
	if len(remLens) >= 2 {
		out = append(out, StageCheck{"REM periods lengthen", remLens[len(remLens)-1] > remLens[0], true,
			fmt.Sprintf("REM minutes per cycle: %v", fmtList(remLens))})
	}
	hd, hl, hr := h.StageHR["deep"], h.StageHR["light"], h.StageHR["rem"]
	if hd > 0 && hl > 0 && hr > 0 {
		out = append(out, StageCheck{"heart rate lowest in deep", hd < hl && hd < hr, false,
			fmt.Sprintf("mean HR deep %.1f, light %.1f, REM %.1f bpm (deep is defined partly by HR below trend, so this is circular)", hd, hl, hr)})
	}
	// Independent: breathing-rate variability (not an input to the rules).
	var remR, nremR []float64
	for k := onset; k <= final; k++ {
		if math.IsNaN(ep[k].Resp) {
			continue
		}
		switch ep[k].Stage {
		case StageREM:
			remR = append(remR, ep[k].Resp)
		case StageLight, StageDeep:
			nremR = append(nremR, ep[k].Resp)
		}
	}
	if len(remR) >= 10 && len(nremR) >= 10 {
		out = append(out, StageCheck{"breathing more variable in REM", sd(remR) > sd(nremR), true,
			fmt.Sprintf("SD of RR-derived breathing rate: REM %.2f vs NREM %.2f /min; mean REM %.1f vs NREM %.1f /min (n=%d/%d epochs)",
				sd(remR), sd(nremR), mean(remR), mean(nremR), len(remR), len(nremR))})
	}
	var sdREM, sdNREM []float64
	for k := onset; k <= final; k++ {
		if math.IsNaN(ep[k].HRSD) {
			continue
		}
		switch ep[k].Stage {
		case StageREM:
			sdREM = append(sdREM, ep[k].HRSD)
		case StageLight, StageDeep:
			sdNREM = append(sdNREM, ep[k].HRSD)
		}
	}
	if len(sdREM) > 0 && len(sdNREM) > 0 {
		out = append(out, StageCheck{"heart rate more variable in REM", mean(sdREM) > mean(sdNREM), false,
			fmt.Sprintf("mean 5-min HR SD: REM %.2f vs NREM %.2f bpm (an input to the REM rule, so circular)", mean(sdREM), mean(sdNREM))})
	}
	var lens []float64
	for _, c := range h.Cycles {
		if c.Complete {
			lens = append(lens, c.Minutes)
		}
	}
	if len(lens) > 0 {
		m := mean(lens)
		out = append(out, StageCheck{"cycles of about 80-120 minutes", m >= 70 && m <= 130, true,
			fmt.Sprintf("%d complete cycles, mean %.0f min: %v (Dijk 2009: about 80-120 min)", len(lens), m, fmtList(lens))})
	}
	if h.Totals.TSTMin > 0 {
		out = append(out, StageCheck{"REM share plausible", h.Totals.REMPct >= 10 && h.Totals.REMPct <= 30, true,
			fmt.Sprintf("REM %.0f %% of sleep (Dijk 2009: about 18-25 %% in adults)", h.Totals.REMPct)})
	}
	if h.DeviceAgreement > 0 {
		out = append(out, StageCheck{"agrees with the strap's own sleep/wake label", h.DeviceAgreement >= 0.85, true,
			fmt.Sprintf("epoch agreement %.0f %%, kappa %.2f (the strap's label is not ground truth either)", 100*h.DeviceAgreement, h.DeviceKappa)})
	}
	return out
}

func fmtList(v []float64) string {
	s := "["
	for i, x := range v {
		if i > 0 {
			s += " "
		}
		s += fmt.Sprintf("%.0f", x)
	}
	return s + "]"
}

func smooth(x []float64, half int) []float64 {
	out := make([]float64, len(x))
	for i := range x {
		if math.IsNaN(x[i]) {
			out[i] = math.NaN()
			continue
		}
		var s float64
		var c int
		for j := max(0, i-half); j <= min(len(x)-1, i+half); j++ {
			if !math.IsNaN(x[j]) {
				s += x[j]
				c++
			}
		}
		out[i] = s / float64(c)
	}
	return out
}

func mean(v []float64) float64 {
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func sd(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	m := mean(v)
	var s float64
	for _, x := range v {
		s += (x - m) * (x - m)
	}
	return math.Sqrt(s / float64(len(v)-1))
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }

// theilSen fits y = a + b*x robustly: b is the median of all pairwise slopes
// (Theil 1950; Sen 1968), a the median of y - b*x. Large inputs are thinned to
// keep the pair count bounded.
func theilSen(x, y []float64) (a, b float64) {
	step := 1
	for len(x)/step > 600 {
		step++
	}
	var slopes []float64
	for i := 0; i < len(x); i += step {
		for j := i + step; j < len(x); j += step {
			if dx := x[j] - x[i]; dx != 0 {
				slopes = append(slopes, (y[j]-y[i])/dx)
			}
		}
	}
	if len(slopes) == 0 {
		return median(y), 0
	}
	b = median(slopes)
	r := make([]float64, len(x))
	for i := range x {
		r[i] = y[i] - b*x[i]
	}
	return median(r), b
}
