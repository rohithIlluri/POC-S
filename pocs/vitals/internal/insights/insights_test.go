package insights

import (
	"strings"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
)

var (
	utc = time.UTC
	now = time.Date(2026, 10, 21, 20, 0, 0, 0, utc)
)

func at(daysAgo int) time.Time {
	return time.Date(2026, 10, 21-daysAgo, 9, 0, 0, 0, utc)
}

func find(ins []Insight, substr string) (Insight, bool) {
	for _, i := range ins {
		if strings.Contains(i.Text, substr) {
			return i, true
		}
	}
	return Insight{}, false
}

func TestShortSleepIsFlagged(t *testing.T) {
	var e []store.Entry
	for d := 0; d < 5; d++ {
		e = append(e, store.Entry{Metric: "sleep", Time: at(d), Value: 5.5})
	}
	got := Generate(e, config.Config{}, now, utc)
	i, ok := find(got, "of sleep")
	if !ok || i.Level != Watch || !strings.Contains(i.Text, "5.5 h") {
		t.Fatalf("sleep insight missing or wrong: %+v", got)
	}
	if got[0].Level != Watch {
		t.Errorf("watch items should sort first: %+v", got)
	}
	if _, ok := find(got, "5-day logging streak"); !ok {
		t.Errorf("streak insight missing: %+v", got)
	}
}

func TestGoalProgress(t *testing.T) {
	var e []store.Entry
	for d, v := range []float64{9000, 9500, 4000, 10000, 8000} {
		e = append(e, store.Entry{Metric: "steps", Time: at(d), Value: v})
	}
	cfg := config.Config{Goals: map[string]config.Goal{"steps": {Op: ">=", Target: 8000}}}
	i, ok := find(Generate(e, cfg, now, utc), "steps goal")
	if !ok || !strings.Contains(i.Text, "met on 4 of 5") || i.Level != Good {
		t.Fatalf("goal insight = %+v ok=%v", i, ok)
	}
}

func TestSleepMoodCorrelation(t *testing.T) {
	var e []store.Entry
	sleep := []float64{5, 6, 7, 8, 6, 5, 8, 7}
	mood := []float64{2, 3, 4, 5, 3, 2, 5, 4}
	for d := range sleep {
		e = append(e,
			store.Entry{Metric: "sleep", Time: at(d), Value: sleep[d]},
			store.Entry{Metric: "mood", Time: at(d), Value: mood[d]})
	}
	i, ok := find(Generate(e, config.Config{}, now, utc), "more sleep, your mood tends to be higher")
	if !ok || !strings.Contains(i.Text, "over 8 days") {
		t.Fatalf("correlation insight = %+v ok=%v", i, ok)
	}
}

func TestTooFewPairsNoCorrelation(t *testing.T) {
	var e []store.Entry
	for d := 0; d < 4; d++ {
		e = append(e,
			store.Entry{Metric: "sleep", Time: at(d), Value: float64(5 + d)},
			store.Entry{Metric: "mood", Time: at(d), Value: float64(1 + d)})
	}
	if _, ok := find(Generate(e, config.Config{}, now, utc), "Correlation"); ok {
		t.Fatal("should not correlate on 4 days")
	}
}

func TestRepeatedSymptomAndSleepContrast(t *testing.T) {
	var e []store.Entry
	for d := 0; d < 8; d++ {
		v := 8.0
		if d%2 == 0 {
			v = 5.5
			e = append(e, store.Entry{Metric: "symptom", Label: "headache", Time: at(d), Value: 3})
		}
		e = append(e, store.Entry{Metric: "sleep", Time: at(d), Value: v})
	}
	got := Generate(e, config.Config{}, now, utc)
	if i, ok := find(got, "headache logged 4×"); !ok || i.Level != Watch {
		t.Fatalf("symptom insight: %+v", got)
	}
	if _, ok := find(got, "On headache days you slept 5.5 h"); !ok {
		t.Fatalf("sleep contrast missing: %+v", got)
	}
}

func TestHeartRateRise(t *testing.T) {
	var e []store.Entry
	for d := 0; d < 14; d++ {
		v := 58.0
		if d < 7 {
			v = 66
		}
		e = append(e, store.Entry{Metric: "heartrate", Time: at(d), Value: v})
	}
	if _, ok := find(Generate(e, config.Config{}, now, utc), "up 8 bpm"); !ok {
		t.Fatal("heart rate rise not flagged")
	}
}

func TestEmptyLogHasNoInsights(t *testing.T) {
	if got := Generate(nil, config.Config{}, now, utc); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}
