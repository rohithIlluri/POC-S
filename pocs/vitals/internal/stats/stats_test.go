package stats

import (
	"math"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/metrics"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
)

var (
	utc = time.UTC
	now = time.Date(2026, 10, 21, 20, 0, 0, 0, utc)
)

func at(daysAgo, hour int) time.Time {
	return time.Date(2026, 10, 21-daysAgo, hour, 0, 0, 0, utc)
}

func must(name string) metrics.Metric {
	m, ok := metrics.Lookup(name)
	if !ok {
		panic(name)
	}
	return m
}

func TestWindow(t *testing.T) {
	w := Window(now, 3, utc)
	want := []string{"2026-10-19", "2026-10-20", "2026-10-21"}
	for i := range want {
		if w[i] != want[i] {
			t.Fatalf("Window = %v", w)
		}
	}
}

func TestWindowRespectsLocation(t *testing.T) {
	// 02:00 UTC on the 21st is still the 20th in New York.
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	w := Window(time.Date(2026, 10, 21, 2, 0, 0, 0, utc), 1, ny)
	if w[0] != "2026-10-20" {
		t.Fatalf("got %v", w)
	}
}

func TestDailyAggregation(t *testing.T) {
	entries := []store.Entry{
		{Metric: "steps", Time: at(0, 9), Value: 3000},
		{Metric: "steps", Time: at(0, 18), Value: 5000},
		{Metric: "mood", Time: at(0, 9), Value: 2},
		{Metric: "mood", Time: at(0, 18), Value: 4},
		{Metric: "weight", Time: at(0, 7), Value: 80},
		{Metric: "weight", Time: at(0, 21), Value: 81},
	}
	day := "2026-10-21"
	if v := Daily(entries, must("steps"), utc)[day]; v != 8000 {
		t.Errorf("steps sum = %v", v)
	}
	if v := Daily(entries, must("mood"), utc)[day]; v != 3 {
		t.Errorf("mood mean = %v", v)
	}
	if v := Daily(entries, must("weight"), utc)[day]; v != 81 {
		t.Errorf("weight last = %v", v)
	}
}

func TestPearson(t *testing.T) {
	r, ok := Pearson([]float64{1, 2, 3, 4}, []float64{2, 4, 6, 8})
	if !ok || math.Abs(r-1) > 1e-9 {
		t.Errorf("perfect positive: %v %v", r, ok)
	}
	r, ok = Pearson([]float64{1, 2, 3, 4}, []float64{8, 6, 4, 2})
	if !ok || math.Abs(r+1) > 1e-9 {
		t.Errorf("perfect negative: %v %v", r, ok)
	}
	if _, ok := Pearson([]float64{1, 1, 1}, []float64{1, 2, 3}); ok {
		t.Error("zero variance should not be ok")
	}
	if _, ok := Pearson([]float64{1, 2}, []float64{1, 2}); ok {
		t.Error("n<3 should not be ok")
	}
}

func TestSparkline(t *testing.T) {
	got := Sparkline([]float64{0, 5, 10, 0}, []bool{true, true, true, false})
	if got != "▁▅█·" {
		t.Errorf("Sparkline = %q", got)
	}
	if got := Sparkline([]float64{3, 3}, []bool{true, true}); got != "▅▅" {
		t.Errorf("flat = %q", got)
	}
}

func TestSummarizeWithWeekDelta(t *testing.T) {
	var entries []store.Entry
	for d := 13; d >= 7; d-- {
		entries = append(entries, store.Entry{Metric: "sleep", Time: at(d, 8), Value: 6})
	}
	for d := 6; d >= 0; d-- {
		entries = append(entries, store.Entry{Metric: "sleep", Time: at(d, 8), Value: 8})
	}
	s, ok := Summarize(entries, must("sleep"), now, 14, utc)
	if !ok || s.Days != 14 || s.Mean != 7 || s.Min != 6 || s.Max != 8 {
		t.Fatalf("summary = %+v", s)
	}
	if !s.HasDelta || s.Recent != 8 || s.Prior != 6 {
		t.Fatalf("delta = %+v", s)
	}
	if _, ok := Summarize(entries, must("steps"), now, 14, utc); ok {
		t.Error("no steps data should be !ok")
	}
}

func TestEventCounts(t *testing.T) {
	entries := []store.Entry{
		{Metric: "symptom", Label: "Headache", Time: at(1, 9), Value: 2},
		{Metric: "symptom", Label: "headache", Time: at(0, 9), Value: 4},
		{Metric: "symptom", Label: "nausea", Time: at(0, 10), Value: 3},
		{Metric: "symptom", Label: "cough", Time: at(40, 10), Value: 3}, // outside window
	}
	got := EventCounts(entries, must("symptom"), now, 14, utc)
	if len(got) != 2 || got[0].Label != "headache" || got[0].Count != 2 || got[0].Mean != 3 {
		t.Fatalf("EventCounts = %+v", got)
	}
}

func TestStreak(t *testing.T) {
	entries := []store.Entry{
		{Metric: "mood", Time: at(3, 9)},
		{Metric: "mood", Time: at(2, 9)},
		{Metric: "mood", Time: at(1, 9)},
	}
	if got := Streak(entries, now, utc); got != 3 {
		t.Errorf("streak ending yesterday = %d", got)
	}
	entries = append(entries, store.Entry{Metric: "mood", Time: at(0, 9)})
	if got := Streak(entries, now, utc); got != 4 {
		t.Errorf("streak incl today = %d", got)
	}
	if got := Streak(entries[:1], now, utc); got != 0 {
		t.Errorf("broken streak = %d", got)
	}
}
