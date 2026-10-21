package hrv

import (
	"math"
	"testing"
)

func TestCleanDropsArtifacts(t *testing.T) {
	got := Clean([]float64{1000, 1010, 250, 1005, 2500, 600, 990, math.NaN(), 1000})
	want := []float64{1000, 1010, 1005, 990, 1000}
	if len(got) != len(want) {
		t.Fatalf("Clean = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Clean = %v", got)
		}
	}
}

func TestCleanSurvivesABadFirstBeat(t *testing.T) {
	// A start-of-stream glitch (seen in real WHOOP captures) must not become
	// the reference that every later, correct beat is compared against.
	rr := []float64{1639}
	for i := 0; i < 40; i++ {
		rr = append(rr, 900+float64(i%2)*20)
	}
	if got := Clean(rr); len(got) != 40 || got[0] != 900 {
		t.Fatalf("Clean kept %d beats starting %v", len(got), got[:1])
	}
}

func TestRMSSD(t *testing.T) {
	// Alternating 1000/1040 ms: every successive difference is 40 ms.
	rr := make([]float64, 40)
	for i := range rr {
		rr[i] = 1000 + float64(i%2)*40
	}
	got, ok := RMSSD(rr)
	if !ok || math.Abs(got-40) > 1e-9 {
		t.Fatalf("RMSSD = %v %v", got, ok)
	}
	if _, ok := RMSSD(rr[:10]); ok {
		t.Fatal("too few intervals should not produce HRV")
	}
}

func TestRestingHR(t *testing.T) {
	rr := make([]float64, 40)
	for i := range rr {
		rr[i] = 1000 // 60 bpm
	}
	if got, err := RestingHR(nil, rr); err != nil || math.Abs(got-60) > 1e-9 {
		t.Fatalf("from RR: %v %v", got, err)
	}
	hr := []float64{0, 0, 58, 59, 60, 61, 120, 59, 60, 58, 61, 60} // a motion spike and lock-on zeros
	if got, err := RestingHR(hr, nil); err != nil || got != 60 {
		t.Fatalf("from bpm median: %v %v", got, err)
	}
	if _, err := RestingHR([]float64{60, 61}, nil); err != ErrTooShort {
		t.Fatalf("short: %v", err)
	}
}

func TestSummarize(t *testing.T) {
	hr := []float64{0, 100, 120, 140, 160, 180, 140, 120, 100, 120, 120}
	w, err := Summarize(hr)
	if err != nil || w.Max != 180 || math.Abs(w.Avg-130) > 1e-9 {
		t.Fatalf("Summarize = %+v %v", w, err)
	}
	if _, err := Summarize([]float64{90}); err != ErrTooShort {
		t.Fatal("short workout accepted")
	}
}
