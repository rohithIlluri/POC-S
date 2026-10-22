package science

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

// synthNight builds a night of strap records (0.9614 s apart) with a known
// stage per 30-second epoch, using the directions of change the classifier
// relies on: deep = HR below trend and steady with strong respiratory sinus
// arrhythmia; REM = HR above trend, variable, weak RSA; light in between;
// wake = movement, arm-angle changes and a faster heart.
type synthOpt struct {
	noRR       bool
	offWristAt time.Duration // start of a 1-hour stretch with no optical lock (0 = none)
	seed       int64
}

var synthT0 = time.Date(2026, 10, 21, 23, 0, 0, 0, time.UTC)

func synthPlan(sleepMin int) []Stage {
	// 30 min wake, then cycles of 90 min, then 30 min wake.
	var plan []Stage
	add := func(s Stage, min int) {
		for i := 0; i < 2*min; i++ {
			plan = append(plan, s)
		}
	}
	add(StageWake, 30)
	left := sleepMin
	for c := 0; left > 0; c++ {
		deep := []int{35, 25, 10, 0, 0, 0}[min(c, 5)]
		rem := []int{10, 18, 25, 30, 30, 30}[min(c, 5)]
		light := 90 - deep - rem
		for _, seg := range []struct {
			s Stage
			m int
		}{{StageLight, light / 2}, {StageDeep, deep}, {StageLight, light - light/2}, {StageREM, rem}} {
			m := min(seg.m, left)
			add(seg.s, m)
			left -= m
		}
	}
	add(StageWake, 30)
	return plan
}

func synthNight(plan []Stage, o synthOpt) ([]StageSample, SleepWindow) {
	r := rand.New(rand.NewSource(o.seed + 1))
	dt := 961400 * time.Microsecond
	end := synthT0.Add(time.Duration(len(plan)) * StageEpoch)
	var out []StageSample
	nextBeat := 0.0 // seconds since t0
	hrWalk := 0.0
	ang := 30.0
	for t := synthT0; t.Before(end); t = t.Add(dt) {
		k := int(t.Sub(synthT0) / StageEpoch)
		st := plan[k]
		sec := t.Sub(synthT0).Seconds()
		trend := 64 - 4*sec/float64(len(plan)*30) // slow overnight fall
		var hr, rsa, noise, move float64
		switch st {
		case StageWake:
			hr, rsa, noise, move = trend+12, 15, 25, 0.05+0.04*r.Float64()
			ang += r.NormFloat64() * 8
		case StageLight:
			hr, rsa, noise, move = trend, 25, 8, 0.0068
			hrWalk = 0
		case StageDeep:
			hr, rsa, noise, move = trend-4, 45, 3, 0.0063
			hrWalk = 0
		case StageREM:
			hrWalk = 0.97*hrWalk + r.NormFloat64()*0.8
			hr, rsa, noise, move = trend+4+hrWalk, 8, 20, 0.0072
		}
		if st != StageWake && r.Float64() < 0.002 { // an occasional twitch
			move = 0.03
		}
		s := StageSample{T: t, Movement: move, Device: -1}
		rad := ang * math.Pi / 180
		s.G = [3]float64{math.Cos(rad), 0, math.Sin(rad)}
		hrNoise := map[Stage]float64{StageWake: 2, StageLight: 0.8, StageDeep: 0.3, StageREM: 1.5}[st]
		s.HR = int(math.Round(hr + r.NormFloat64()*hrNoise))
		offWrist := o.offWristAt > 0 && t.Sub(synthT0) >= o.offWristAt && t.Sub(synthT0) < o.offWristAt+time.Hour
		if offWrist {
			s.HR = 0
		}
		// beats that end inside this record
		for nextBeat < sec+dt.Seconds() {
			rr := 60000/hr + rsa*math.Sin(2*math.Pi*0.25*nextBeat) + r.NormFloat64()*noise
			nextBeat += rr / 1000
			if !o.noRR && !offWrist && r.Float64() < 0.9 {
				s.RR = append(s.RR, rr)
			}
		}
		if isSleep(st) {
			s.Device = 2
		} else {
			s.Device = 0
		}
		out = append(out, s)
	}
	first, last := -1, -1
	for i, s := range plan {
		if isSleep(s) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	w := SleepWindow{Start: synthT0.Add(time.Duration(first) * StageEpoch), End: synthT0.Add(time.Duration(last+1) * StageEpoch)}
	return out, w
}

func agreement(h Hypnogram, plan []Stage) (all float64, perStage map[Stage]float64) {
	perStage = map[Stage]float64{}
	n := map[Stage]int{}
	hit := map[Stage]int{}
	var ok, tot int
	for _, e := range h.Epochs {
		k := int(e.Start.Sub(synthT0) / StageEpoch)
		if k < 0 || k >= len(plan) || e.Stage == StageNoData {
			continue
		}
		tot++
		n[plan[k]]++
		if e.Stage == plan[k] {
			ok++
			hit[plan[k]]++
		}
	}
	for s, c := range n {
		perStage[s] = float64(hit[s]) / float64(c)
	}
	return float64(ok) / float64(tot), perStage
}

func TestStagesRecoverASyntheticNight(t *testing.T) {
	plan := synthPlan(450) // 7.5 h of sleep, five cycles
	s, w := synthNight(plan, synthOpt{})
	for _, prior := range []float64{0, DefaultStageOptions().TimePrior} {
		h, ok := StageNight(s, w, StageOptions{TimePrior: prior})
		if !ok {
			t.Fatal("a full synthetic night must be staged")
		}
		acc, per := agreement(h, plan)
		t.Logf("prior %.2f: agreement %.2f, per stage %v, totals %+v", prior, acc, per, h.Totals)
		if acc < 0.75 {
			t.Errorf("prior %.2f: epoch agreement %.2f on clean synthetic data, want >= 0.75", prior, acc)
		}
		for _, st := range []Stage{StageWake, StageLight, StageDeep, StageREM} {
			if per[st] < 0.5 {
				t.Errorf("prior %.2f: %s recall %.2f, want >= 0.5", prior, st, per[st])
			}
		}
		tt := h.Totals
		if sum := tt.WakeMin + tt.LightMin + tt.DeepMin + tt.REMMin + tt.NoDataMin; math.Abs(sum-h.End.Sub(h.Start).Minutes()) > 0.01 {
			t.Errorf("stage minutes %.1f do not add up to the scored span %.1f", sum, h.End.Sub(h.Start).Minutes())
		}
		if math.Abs(tt.TSTMin-(tt.LightMin+tt.DeepMin+tt.REMMin)) > 1e-9 || math.Abs(tt.LightPct+tt.DeepPct+tt.REMPct-100) > 0.1 {
			t.Errorf("TST / percentages inconsistent: %+v", tt)
		}
		if !tt.HaveREM || tt.REMLatencyMin < 30 {
			t.Errorf("REM latency %.0f (have %v): REM must not start in the first 30 min", tt.REMLatencyMin, tt.HaveREM)
		}
		if tt.Cycles < 3 || tt.Cycles > 6 {
			t.Errorf("cycles = %d, the night has 5", tt.Cycles)
		}
		if h.DeviceAgreement < 0.85 {
			t.Errorf("sleep/wake agreement with the planted labels %.2f", h.DeviceAgreement)
		}
		if !h.RRSufficient || h.Confidence != "low" {
			t.Errorf("clean RR should be sufficient (got %v, confidence %q)", h.RRSufficient, h.Confidence)
		}
		var sawEarlyDeep bool
		for _, c := range h.Checks {
			if c.Name == "deep sleep concentrated early" {
				sawEarlyDeep = c.Pass
			}
		}
		if !sawEarlyDeep {
			t.Errorf("deep is planted early, the check should pass: %+v", h.Checks)
		}
		// runs cover the span exactly, in order
		at := h.Start
		for _, r := range h.Runs {
			if !r.Start.Equal(at) || r.End.Sub(r.Start) != time.Duration(r.Epochs)*StageEpoch {
				t.Fatalf("runs are not contiguous at %v: %+v", at, r)
			}
			at = r.End
		}
		if !at.Equal(h.End) {
			t.Errorf("runs end at %v, span ends at %v", at, h.End)
		}
	}
}

func TestStagesWithoutRRFallBackToHeartRateAndSaySo(t *testing.T) {
	plan := synthPlan(420)
	s, w := synthNight(plan, synthOpt{noRR: true, seed: 2})
	h, ok := StageNight(s, w, DefaultStageOptions())
	if !ok {
		t.Fatal("no RR must still stage from heart rate and motion")
	}
	if h.RRSufficient || h.Confidence != "very low" || h.RRCoverage != 0 {
		t.Errorf("no RR: sufficient %v, confidence %q, coverage %v", h.RRSufficient, h.Confidence, h.RRCoverage)
	}
	var said bool
	for _, c := range h.Caveats {
		if len(c) > 0 && c[:7] == "Too few" {
			said = true
		}
	}
	if !said {
		t.Errorf("the caveats must say RR was insufficient: %v", h.Caveats)
	}
	for _, e := range h.Epochs {
		if !math.IsNaN(e.RMSSD) {
			t.Fatal("RMSSD must be NaN without RR")
		}
		if isSleep(e.Stage) && e.Confidence > 0.85*0.85+1e-9 && e.Stage != StageLight {
			t.Errorf("confidence %.2f without RR is too high", e.Confidence)
		}
	}
	acc, _ := agreement(h, plan)
	t.Logf("no-RR agreement %.2f", acc)
	if acc < 0.6 {
		t.Errorf("no-RR agreement %.2f", acc)
	}
}

func TestStagesOffWristIsNoDataNotSleep(t *testing.T) {
	plan := synthPlan(420)
	s, w := synthNight(plan, synthOpt{offWristAt: 3 * time.Hour, seed: 3})
	h, ok := StageNight(s, w, DefaultStageOptions())
	if !ok {
		t.Fatal("night with a gap should still be staged")
	}
	if h.Totals.NoDataMin < 59 || h.Totals.NoDataMin > 61 {
		t.Errorf("no-data minutes %.1f, want about 60", h.Totals.NoDataMin)
	}
	for _, e := range h.Epochs {
		off := e.Start.Sub(synthT0)
		if off >= 3*time.Hour+time.Minute && off < 4*time.Hour-time.Minute && e.Stage != StageNoData {
			t.Fatalf("epoch at +%v is %s while the strap had no optical lock", off, e.Stage)
		}
	}
}

func TestStagesShortNapIsFlaggedAndTinySpansAreRefused(t *testing.T) {
	plan := synthPlan(60)
	s, w := synthNight(plan, synthOpt{seed: 4})
	h, ok := StageNight(s, w, DefaultStageOptions())
	if !ok {
		t.Fatal("a one-hour nap should still be scored")
	}
	if h.Confidence != "very low" {
		t.Errorf("nap confidence %q, want very low", h.Confidence)
	}
	if h.Totals.TSTMin < 30 || h.Totals.TSTMin > 70 {
		t.Errorf("nap TST %.0f", h.Totals.TSTMin)
	}

	tiny := synthPlan(10)
	s, w = synthNight(tiny, synthOpt{seed: 5})
	if _, ok := StageNight(s, w, DefaultStageOptions()); ok {
		t.Error("a 10-minute window must not be staged")
	}
	if _, ok := StageNight(nil, SleepWindow{Start: synthT0, End: synthT0.Add(8 * time.Hour)}, DefaultStageOptions()); ok {
		t.Error("no samples must not be staged")
	}
}

func TestStagesAllAwakeFindsNoSleep(t *testing.T) {
	plan := make([]Stage, 2*240)
	for i := range plan {
		plan[i] = StageWake
	}
	s, _ := synthNight(plan, synthOpt{seed: 6})
	w := SleepWindow{Start: synthT0.Add(time.Hour), End: synthT0.Add(3 * time.Hour)}
	h, ok := StageNight(s, w, DefaultStageOptions())
	if ok {
		t.Errorf("restless wake should not produce a hypnogram: %+v", h.Totals)
	}
}

func TestStagesIsConservativeOnFlatData(t *testing.T) {
	// Perfectly featureless sleep: no evidence for deep or REM, so all light.
	plan := synthPlan(300)
	for i := range plan {
		if plan[i] == StageDeep || plan[i] == StageREM {
			plan[i] = StageLight
		}
	}
	s, w := synthNight(plan, synthOpt{seed: 7})
	h, ok := StageNight(s, w, StageOptions{})
	if !ok {
		t.Fatal("should stage")
	}
	if frac := (h.Totals.DeepMin + h.Totals.REMMin) / h.Totals.TSTMin; frac > 0.15 {
		t.Errorf("%.0f %% deep+REM invented from featureless light sleep", 100*frac)
	}
}

func TestTheilSenIgnoresOutliers(t *testing.T) {
	var x, y []float64
	for i := 0; i < 100; i++ {
		x = append(x, float64(i))
		v := 70 - 0.1*float64(i)
		if i%10 == 0 {
			v += 30
		}
		y = append(y, v)
	}
	a, b := theilSen(x, y)
	if math.Abs(b+0.1) > 1e-6 || math.Abs(a-70) > 1e-6 {
		t.Errorf("theilSen = %v, %v; want 70, -0.1", a, b)
	}
}
