package science

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

// gravityAt builds a unit gravity vector whose z-angle is theta degrees.
func gravityAt(theta, phi float64) [3]float64 {
	t, p := theta*math.Pi/180, phi*math.Pi/180
	return [3]float64{math.Cos(t) * math.Cos(p), math.Cos(t) * math.Sin(p), math.Sin(t)}
}

// syntheticDay is 18:00 to 10:00 at one sample per second: an active evening,
// a still night from 23:00 to 07:00 with two turns in bed, an active morning.
func syntheticDay() (s []GravitySample, night0, night1 time.Time) {
	loc := time.UTC
	start := time.Date(2026, 11, 8, 18, 0, 0, 0, loc)
	night0 = time.Date(2026, 11, 8, 23, 0, 0, 0, loc)
	night1 = time.Date(2026, 11, 9, 7, 0, 0, 0, loc)
	turn1 := time.Date(2026, 11, 9, 2, 0, 0, 0, loc)
	turn2 := time.Date(2026, 11, 9, 5, 0, 0, 0, loc)
	rng := rand.New(rand.NewSource(1))
	theta, phi := 30.0, 10.0
	for t := start; t.Before(time.Date(2026, 11, 9, 10, 0, 0, 0, loc)); t = t.Add(time.Second) {
		switch {
		case t.Before(night0) || !t.Before(night1):
			if t.Unix()%5 == 0 { // restless: a new posture every few seconds
				theta += rng.Float64()*40 - 20
				phi += rng.Float64()*60 - 30
			}
		case t.Equal(turn1) || t.Equal(turn2):
			theta += 35 // rolling over
		default:
			theta += rng.NormFloat64() * 0.05 // breathing, pulse
		}
		theta = math.Max(-80, math.Min(80, theta))
		s = append(s, GravitySample{T: t, G: gravityAt(theta, phi)})
	}
	return s, night0, night1
}

func within(t, want time.Time, tol time.Duration) bool {
	d := t.Sub(want)
	return d < tol && d > -tol
}

func TestSleepPeriodFindsTheStillNight(t *testing.T) {
	s, n0, n1 := syntheticDay()
	w, ok := SleepPeriod(s)
	if !ok {
		t.Fatal("no sleep period found")
	}
	// The rolling median is centred on 5 minutes, so the edges are good to a few minutes.
	if !within(w.Start, n0, 10*time.Minute) || !within(w.End, n1, 10*time.Minute) {
		t.Errorf("window %v to %v, want about %v to %v", w.Start, w.End, n0, n1)
	}
}

func TestScoreSleepCountsBoutsAndTurnsInBed(t *testing.T) {
	s, n0, n1 := syntheticDay()
	w, _ := SleepPeriod(s)
	r := ScoreSleep(s, w)
	if want := n1.Sub(n0).Hours(); math.Abs(r.WindowHours-want) > 0.3 {
		t.Errorf("window %.2f h, want about %.2f", r.WindowHours, want)
	}
	if r.Efficiency < 0.95 || r.Efficiency > 1 {
		t.Errorf("a night with two turns should be about 99%% efficient, got %.3f", r.Efficiency)
	}
	if r.Awakenings != 2 {
		t.Errorf("awakenings %d, want the two turns", r.Awakenings)
	}
	if r.LongestBoutMin < 150 {
		t.Errorf("longest bout %.0f min, want at least 150", r.LongestBoutMin)
	}
	if math.Abs(r.AsleepHours*60+r.AwakeMinutes-r.WindowHours*60) > 0.01 {
		t.Errorf("asleep %.2f h + awake %.1f min does not add up to the %.2f h window", r.AsleepHours, r.AwakeMinutes, r.WindowHours)
	}
}

func TestRestlessNightScoresLowerThanAStillOne(t *testing.T) {
	s, _, _ := syntheticDay()
	w, _ := SleepPeriod(s)
	still := ScoreSleep(s, w)
	rng := rand.New(rand.NewSource(2))
	// Shake the bed every ~12 minutes inside the window.
	theta := 0.0
	for i := range s {
		if s[i].T.After(w.Start) && s[i].T.Before(w.End) && rng.Intn(720) == 0 {
			theta += 40
		}
		x := s[i].G
		c, sn := math.Cos(theta*math.Pi/180), math.Sin(theta*math.Pi/180)
		s[i].G = [3]float64{x[0], x[1]*c - x[2]*sn, x[1]*sn + x[2]*c}
	}
	restless := ScoreSleep(s, w)
	if restless.Awakenings <= still.Awakenings || restless.Efficiency >= still.Efficiency {
		t.Errorf("restless %+v should score worse than still %+v", restless, still)
	}
}

func TestNoSleepWindowWhenAlwaysActiveOrTooShort(t *testing.T) {
	var s []GravitySample
	rng := rand.New(rand.NewSource(3))
	theta := 0.0
	for i := 0; i < 6*3600; i++ {
		if i%5 == 0 {
			theta += rng.Float64()*40 - 20
		}
		s = append(s, GravitySample{T: time.Unix(1_800_000_000+int64(i), 0), G: gravityAt(theta, 0)})
	}
	if w, ok := SleepPeriod(s); ok {
		// With uniform activity the 10th-percentile-times-15 threshold sits above
		// every value, so the whole span qualifies: that must not be reported as a short sleep.
		if w.End.Sub(w.Start) < 5*time.Hour {
			t.Errorf("found a spurious window %v", w)
		}
	}
	if _, ok := SleepPeriod(s[:100]); ok {
		t.Error("100 seconds of data cannot hold a sleep period")
	}
	if _, ok := SleepPeriod(nil); ok {
		t.Error("no data, no sleep period")
	}
}

func TestDataGapsBreakABout(t *testing.T) {
	// Twenty quiet minutes, a ten minute hole, twenty more quiet minutes.
	var s []GravitySample
	add := func(from, to int) {
		for i := from; i < to; i++ {
			s = append(s, GravitySample{T: time.Unix(1_800_000_000+int64(i), 0), G: gravityAt(10, 0)})
		}
	}
	add(0, 1200)
	add(1800, 3000)
	w := SleepWindow{Start: time.Unix(1_800_000_000, 0), End: time.Unix(1_800_003_000, 0)}
	r := ScoreSleep(s, w)
	if r.AsleepHours*60 > 40.5 || r.AsleepHours*60 < 38 {
		t.Errorf("asleep %.1f min: the hole must not count as sleep", r.AsleepHours*60)
	}
}

func TestDeviceSleepSummary(t *testing.T) {
	t0 := time.Unix(1_800_000_000, 0)
	var p []StatePoint
	add := func(state uint8, n int) {
		for i := 0; i < n; i++ {
			p = append(p, StatePoint{T: t0.Add(time.Duration(len(p)) * time.Second), State: state})
		}
	}
	add(0, 600) // wake 10 min
	add(1, 300) // still 5 min
	add(2, 3600)
	add(0, 60) // a minute awake
	add(2, 1200)
	add(3, 120)
	d, ok := DeviceSleepSummary(p)
	if !ok {
		t.Fatal("no summary")
	}
	if math.Abs(d.Minutes[2]-80) > 0.5 || math.Abs(d.SleepHours-80.0/60) > 0.01 {
		t.Errorf("sleep minutes %.1f", d.Minutes[2])
	}
	// The longest unbroken run of state sleep is the first hour.
	if !d.FirstSleep.Equal(t0.Add(900*time.Second)) || !d.LastSleep.Equal(t0.Add(4499*time.Second)) {
		t.Errorf("run %v to %v", d.FirstSleep.Sub(t0), d.LastSleep.Sub(t0))
	}
	if _, ok := DeviceSleepSummary(p[:1]); ok {
		t.Error("one point is not a summary")
	}
}
