package science

import (
	"math"
	"strings"
	"testing"
	"time"
)

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.4f, want %.4f ± %.4f", name, got, want, tol)
	}
}

func TestMaxHRAndVO2(t *testing.T) {
	near(t, "Tanaka(40)", MaxHRTanaka(40), 180, 1e-9)
	near(t, "Tanaka(20)", MaxHRTanaka(20), 194, 1e-9)
	if got := MaxHR(40, 188); got != 188 {
		t.Errorf("an observed max above the estimate must win, got %v", got)
	}
	if got := MaxHR(40, 170); got != 180 {
		t.Errorf("the estimate must win when higher, got %v", got)
	}
	v, ok := VO2maxUth(190, 50)
	if !ok {
		t.Fatal("not ok")
	}
	near(t, "Uth", v, 15.3*190/50, 1e-9)
	if _, ok := VO2maxUth(60, 60); ok {
		t.Error("max not above rest must not estimate")
	}
	if age := Age(1990, time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)); age != 36 {
		t.Errorf("age = %v", age)
	}
}

func TestIntervalsCapGapsAndDropGarbage(t *testing.T) {
	t0 := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	pts := []HRPoint{
		{t0, 100},
		{t0.Add(time.Second), 101},
		{t0.Add(time.Hour), 102},               // the strap went quiet for ~an hour before this
		{t0.Add(time.Hour + time.Second), 300}, // implausible
		{t0.Add(time.Hour + 2*time.Second), 103},
	}
	iv := Intervals(pts, 5*time.Second)
	var total float64
	for _, i := range iv {
		total += i.Seconds
	}
	if len(iv) != 4 {
		t.Fatalf("got %d intervals: %+v", len(iv), iv)
	}
	// 1 s + 5 s (gap capped) + 1 s (to the garbage reading, still held) + 1 s (last) = 8
	near(t, "held seconds", total, 8, 1e-9)
}

func TestEdwardsTRIMP(t *testing.T) {
	// An hour at 150 bpm with a 190 bpm max is 79% of max: zone 3, weight 3.
	near(t, "edwards 60 min zone 3", EdwardsTRIMP([]Interval{{150, 3600}}, 190), 180, 1e-9)
	// Zone edges: 50% counts, 49% does not, 90% is the top weight.
	near(t, "50%", EdwardsTRIMP([]Interval{{95, 60}}, 190), 1, 1e-9)
	near(t, "49%", EdwardsTRIMP([]Interval{{93, 60}}, 190), 0, 1e-9)
	near(t, "90%", EdwardsTRIMP([]Interval{{171, 60}}, 190), 5, 1e-9)
	near(t, "rest of day", EdwardsTRIMP([]Interval{{60, 86400}}, 190), 0, 1e-9)
}

func TestBanisterTRIMP(t *testing.T) {
	// 45 min at HR 160, rest 40, max 190: x = 0.8.
	want := 45 * 0.8 * 0.64 * math.Exp(1.92*0.8)
	near(t, "men", BanisterTRIMP([]Interval{{160, 45 * 60}}, 40, 190, Male), want, 1e-6)
	wantF := 45 * 0.8 * 0.86 * math.Exp(1.67*0.8)
	near(t, "women", BanisterTRIMP([]Interval{{160, 45 * 60}}, 40, 190, Female), wantF, 1e-6)
	near(t, "below rest", BanisterTRIMP([]Interval{{30, 600}}, 40, 190, Male), 0, 1e-9)
}

func TestStrainIsMonotonicAndBounded(t *testing.T) {
	prev := -1.0
	for _, trimp := range []float64{0, 10, 60, 180, 400, 1000, 10000} {
		s := Strain(trimp)
		if s < prev || s < 0 || s > 21 {
			t.Fatalf("Strain(%v) = %v after %v", trimp, s, prev)
		}
		prev = s
	}
	near(t, "hour of zone-3 work", Strain(180), 21*(1-math.Exp(-0.9)), 1e-9)
}

func TestBaselineAndRollingMean(t *testing.T) {
	b := NewBaseline([]float64{4.0, 4.2, 3.8, 4.0})
	near(t, "mean", b.Mean, 4.0, 1e-9)
	near(t, "sd", b.SD, math.Sqrt(0.08/3), 1e-9)
	if b.N != 4 {
		t.Errorf("N = %d", b.N)
	}
	// A baseline with no spread must not blow a tiny wobble up to a huge z.
	flat := NewBaseline([]float64{4, 4, 4, 4, 4, 4, 4})
	near(t, "z on flat baseline", flat.Z(4.03), 1.0, 1e-9) // floored at MinSD
	lo, hi := b.SWCBand()
	if !(lo < b.Mean && b.Mean < hi) {
		t.Errorf("band %v..%v does not bracket the mean", lo, hi)
	}
	near(t, "rolling 3", RollingMean([]float64{1, 2, 3, 4, 5}, 3), 4, 1e-9)
	near(t, "rolling more than exist", RollingMean([]float64{2, 4}, 7), 3, 1e-9)
	if NewBaseline(nil).N != 0 {
		t.Error("empty baseline")
	}
}

func TestLnRMSSD(t *testing.T) {
	// Alternating 1000/1040 ms: RMSSD 40 ms.
	rr := make([]float64, 100)
	for i := range rr {
		rr[i] = 1000 + float64(i%2)*40
	}
	v, ok := LnRMSSD(rr)
	if !ok {
		t.Fatal("not ok")
	}
	near(t, "ln rmssd", v, math.Log(40), 0.02)
	if _, ok := LnRMSSD(rr[:3]); ok {
		t.Error("three beats must not give an RMSSD")
	}
}

func TestRecoveryScore(t *testing.T) {
	steady := func(mean, sd float64, n int) Baseline { return Baseline{Mean: mean, SD: sd, N: n} }
	base := RecoveryBaselines{HRV: steady(4.0, 0.1, 30), RHR: steady(55, 2, 30)}

	r, ok := RecoveryScore(RecoveryInput{LnRMSSD: 4.0, RestingHR: 55}, base)
	if !ok || !r.Full {
		t.Fatalf("typical night: %+v %v", r, ok)
	}
	near(t, "typical night scores 50", r.Score, 50, 1e-9)

	good, _ := RecoveryScore(RecoveryInput{LnRMSSD: 4.15, RestingHR: 53}, base)
	bad, _ := RecoveryScore(RecoveryInput{LnRMSSD: 3.8, RestingHR: 59}, base)
	if !(bad.Score < r.Score && r.Score < good.Score) {
		t.Errorf("scores %v < %v < %v expected", bad.Score, r.Score, good.Score)
	}
	if good.Tier != "high" || bad.Tier != "low" || r.Tier != "moderate" {
		t.Errorf("tiers %q %q %q", bad.Tier, r.Tier, good.Tier)
	}

	// HRV up with the resting heart rate up too is worth less than HRV up alone.
	up, _ := RecoveryScore(RecoveryInput{LnRMSSD: 4.1, RestingHR: 55}, base)
	upHR, _ := RecoveryScore(RecoveryInput{LnRMSSD: 4.1, RestingHR: 59}, base)
	if upHR.Score >= up.Score {
		t.Errorf("a higher resting HR must lower the score: %v vs %v", upHR.Score, up.Score)
	}

	// Too little baseline: no number at all. Between 7 and 28 nights: a number, flagged early.
	if _, ok := RecoveryScore(RecoveryInput{4, 55}, RecoveryBaselines{HRV: steady(4, 0.1, 6), RHR: steady(55, 2, 6)}); ok {
		t.Error("6 nights must not produce a score")
	}
	early, ok := RecoveryScore(RecoveryInput{4, 55}, RecoveryBaselines{HRV: steady(4, 0.1, 10), RHR: steady(55, 2, 10)})
	if !ok || early.Full {
		t.Errorf("10 nights: %+v %v (should be a calibrating score)", early, ok)
	}
}

func TestBaevskyIndex(t *testing.T) {
	// Intervals bunched tightly: a high index. Spread out: a low one.
	tight := make([]float64, 100)
	for i := range tight {
		tight[i] = 800 + float64(i%10) // 800-809: one bin, range 9 ms
	}
	wide := make([]float64, 100)
	for i := range wide {
		wide[i] = 700 + float64(i%10)*20 // 700-880 across several bins
	}
	a, ok1 := BaevskyIndex(tight)
	b, ok2 := BaevskyIndex(wide)
	if !ok1 || !ok2 || !(a > b) {
		t.Errorf("tight %v (%v) should exceed wide %v (%v)", a, ok1, b, ok2)
	}
	if _, ok := BaevskyIndex(tight[:10]); ok {
		t.Error("10 intervals must not give an index")
	}
	if _, ok := BaevskyIndex(make([]float64, 40)); ok {
		t.Error("no spread must not give an index")
	}
}

func TestStressLevel(t *testing.T) {
	hrv := Baseline{Mean: 4.0, SD: 0.1, N: 20}
	hr := Baseline{Mean: 60, SD: 3, N: 20}
	calm, _ := StressLevel(StressReading{LnRMSSD: 4.0, HR: 60}, hrv, hr)
	near(t, "at baseline", calm, 50, 1e-9)
	tense, _ := StressLevel(StressReading{LnRMSSD: 3.8, HR: 68}, hrv, hr)
	relaxed, _ := StressLevel(StressReading{LnRMSSD: 4.2, HR: 56}, hrv, hr)
	if !(relaxed < calm && calm < tense) {
		t.Errorf("%v < %v < %v expected", relaxed, calm, tense)
	}
	if _, ok := StressLevel(StressReading{4, 60}, Baseline{N: 3}, hr); ok {
		t.Error("no baseline, no stress score")
	}
}

// rsaSeries builds beat intervals whose length swings with a breath at bpm.
func rsaSeries(bpm, seconds float64) []float64 {
	var rr []float64
	var t float64
	for t < seconds {
		v := 850 + 45*math.Sin(2*math.Pi*(bpm/60)*t)
		rr = append(rr, v)
		t += v / 1000
	}
	return rr
}

func TestRespiratoryRate(t *testing.T) {
	for _, bpm := range []float64{9, 12, 15, 20, 26} {
		got, ok := RespiratoryRate(rsaSeries(bpm, 120))
		if !ok {
			t.Errorf("%v br/min: not ok", bpm)
			continue
		}
		near(t, "breaths/min", got, bpm, 1.0)
	}
	if _, ok := RespiratoryRate(rsaSeries(15, 20)); ok {
		t.Error("20 seconds is too short")
	}
	flat := make([]float64, 120)
	for i := range flat {
		flat[i] = 800
	}
	if _, ok := RespiratoryRate(flat); ok {
		t.Error("a perfectly flat series has no breathing signal")
	}
	gappy := rsaSeries(15, 120)
	gappy[50] = 9000
	if _, ok := RespiratoryRate(gappy); ok {
		t.Error("a long gap must reject the window")
	}
}

func TestAssessReadiness(t *testing.T) {
	by := func(sts []Status) map[string]Status {
		m := map[string]Status{}
		for _, s := range sts {
			m[s.Metric] = s
		}
		return m
	}
	// Day zero with a profile: everything the live stream can feed is available;
	// what needs a night, motion or a table is blocked, and says why.
	live := Capabilities{Profile: true}
	day0 := by(Assess(1, 0, live))
	for _, m := range []string{"strain", "hrv", "resting_hr", "respiratory_rate", "fitness"} {
		if day0[m].State == StateBlocked {
			t.Errorf("%s must work from live data, got blocked (%q)", m, day0[m].Note)
		}
	}
	for _, m := range []string{"recovery", "sleep", "stress", "fitness_age"} {
		if day0[m].State != StateBlocked || day0[m].Note == "" {
			t.Errorf("%s on day 0 = %q (%q)", m, day0[m].State, day0[m].Note)
		}
	}
	// No profile: fitness asks for the date of birth.
	if st := by(Assess(1, 0, Capabilities{}))["fitness"]; st.State != StateBlocked || !strings.Contains(st.Note, "date of birth") {
		t.Errorf("fitness without a profile = %+v", st)
	}
	// One overnight stream is enough to unblock recovery (it still calibrates).
	if st := by(Assess(2, 1, live))["recovery"]; st.State != StateCalibrating {
		t.Errorf("recovery after one live night = %q", st.State)
	}
	// With history working, recovery calibrates, then is provisional, then full.
	caps := Capabilities{HistorySync: true, Motion: true, Profile: true, Norms: true}
	for _, tc := range []struct {
		nights int
		want   string
	}{{0, StateCalibrating}, {6, StateCalibrating}, {7, StateProvisional}, {27, StateProvisional}, {28, StateFull}} {
		if got := by(Assess(tc.nights, tc.nights, caps))["recovery"].State; got != tc.want {
			t.Errorf("recovery with %d nights = %q, want %q", tc.nights, got, tc.want)
		}
	}
	if st := by(Assess(30, 30, caps))["recovery"]; st.Have != 30 || st.Provisional != 7 || st.Full != 28 {
		t.Errorf("recovery status %+v", st)
	}

}

func TestRespiratoryRateWindowsOverANight(t *testing.T) {
	// Eight hours with breathing that drifts from 12 to 16 breaths/min.
	var rr []float64
	var tsec, phase float64
	for tsec < 8*3600 {
		bpm := 12 + 4*tsec/(8*3600)
		v := 850 + 45*math.Sin(phase)
		rr = append(rr, v)
		dt := v / 1000
		phase += 2 * math.Pi * (bpm / 60) * dt
		tsec += dt
	}
	start := time.Now()
	got, windows, ok := RespiratoryRateWindows(rr)
	if !ok || windows < 20 {
		t.Fatalf("ok=%v windows=%d", ok, windows)
	}
	near(t, "median over the night", got, 14, 1.0)
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("a whole night took %v; it must stay fast", d)
	}
	if _, _, ok := RespiratoryRateWindows(rr[:50]); ok {
		t.Error("under two minutes of beats must not give a result")
	}
}

func TestAgeAtIsExactAroundABirthday(t *testing.T) {
	birth := time.Date(2000, 1, 15, 0, 0, 0, 0, time.UTC)
	at := func(y int, m time.Month, d, h int) float64 {
		return AgeAt(birth, time.Date(y, m, d, h, 0, 0, 0, time.UTC))
	}
	near(t, "on the 27th birthday", at(2027, 1, 15, 0), 27.0, 1e-9)
	if a := at(2027, 1, 14, 12); a >= 27 || a < 26.99 {
		t.Errorf("the day before the 27th birthday = %v, want just under 27", a)
	}
	// 71.5 days after the birthday, in a 365-day year.
	near(t, "2027-03-27 noon", at(2027, 3, 27, 12), 27+71.5/365, 1e-6)
	if a := at(2028, 1, 14, 23); a >= 28 || a < 27.99 {
		t.Errorf("just before the 28th = %v", a)
	}
	// A February 29 birthday is handled without panicking and stays monotonic.
	leap := time.Date(2000, 2, 29, 0, 0, 0, 0, time.UTC)
	prev := -1.0
	for d := 0; d < 800; d += 50 {
		a := AgeAt(leap, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, d))
		if a < prev {
			t.Fatalf("age went backwards: %v after %v", a, prev)
		}
		prev = a
	}
}

func TestZonesAgreeWithEdwards(t *testing.T) {
	iv := []Interval{{60, 3000}, {100, 600}, {120, 900}, {140, 1200}, {160, 300}, {180, 120}}
	z := ZoneMinutes(iv, 190)
	var total float64
	for _, m := range z {
		total += m
	}
	near(t, "minutes add up", total, (3000+600+900+1200+300+120)/60.0, 1e-9)
	// Edwards is the zone minutes times 1..5 (zone 0 counts for nothing).
	var want float64
	for i := 1; i <= 5; i++ {
		want += z[i] * float64(i)
	}
	near(t, "edwards = weighted zone minutes", EdwardsTRIMP(iv, 190), want, 1e-9)
	if ZoneOf(95, 190) != 1 || ZoneOf(94, 190) != 0 || ZoneOf(171, 190) != 5 || ZoneOf(100, 0) != 0 {
		t.Errorf("zone edges: %d %d %d %d", ZoneOf(95, 190), ZoneOf(94, 190), ZoneOf(171, 190), ZoneOf(100, 0))
	}
}
