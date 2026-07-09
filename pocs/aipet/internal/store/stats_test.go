package store

import (
	"testing"
	"time"
)

func TestAggregateBasics(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	events := []Event{
		{Key: "a", Source: "claude-code", Project: "repo1", Model: "opus",
			Timestamp: now, Input: 100, Output: 50, CacheRead: 200, CacheWrite: 30, CostUSD: 2},
		{Key: "b", Source: "claude-code", Project: "repo1", Model: "sonnet",
			Timestamp: now, Input: 10, Output: 5, CostUSD: 0.5},
		{Key: "c", Source: "codex", Project: "repo2", Model: "gpt-5",
			Timestamp: yesterday, Input: 1000, Output: 500, CostUSD: 3},
	}
	s := Aggregate(events)

	if s.Turns != 3 {
		t.Errorf("Turns = %d, want 3", s.Turns)
	}
	if s.TotalCost != 5.5 {
		t.Errorf("TotalCost = %v, want 5.5", s.TotalCost)
	}
	// Only the two `now` events count toward today.
	if s.TodayCost != 2.5 {
		t.Errorf("TodayCost = %v, want 2.5", s.TodayCost)
	}
	if s.TokensIn != 1110 || s.TokensOut != 555 {
		t.Errorf("tokens = %d/%d, want 1110/555", s.TokensIn, s.TokensOut)
	}
	if s.ByModel["opus"] != 2 || s.ByProject["repo2"] != 3 || s.BySource["claude-code"] != 2.5 {
		t.Errorf("group-bys wrong: %+v", s)
	}
	if !s.LastEventAt.Equal(now) {
		t.Errorf("LastEventAt = %v, want %v", s.LastEventAt, now)
	}
}

func TestAggregateEmpty(t *testing.T) {
	s := Aggregate(nil)
	if s.Turns != 0 || s.TotalCost != 0 {
		t.Errorf("empty aggregate should be zero: %+v", s)
	}
	// Maps must be non-nil so callers can index without panicking.
	if s.ByModel == nil || s.ByProject == nil || s.BySource == nil || s.DailyCost == nil {
		t.Error("aggregate maps must be initialized")
	}
}

// TestAggregateTimezoneBoundary pins the "today" definition to local time: a
// UTC-stored event from late yesterday UTC that is today locally must count.
func TestAggregateTimezoneBoundary(t *testing.T) {
	// Construct an event exactly at local midnight today; it must be "today".
	localMidnight := time.Now().Truncate(24 * time.Hour) // approx; good enough with the explicit format below
	y, m, d := time.Now().Date()
	localMidnight = time.Date(y, m, d, 0, 0, 1, 0, time.Local)

	s := Aggregate([]Event{{Key: "x", Timestamp: localMidnight.UTC(), CostUSD: 1}})
	if s.TodayCost != 1 {
		t.Errorf("event at local midnight should count as today, TodayCost = %v", s.TodayCost)
	}
}

func TestTopN(t *testing.T) {
	m := map[string]float64{"a": 1, "b": 5, "c": 3, "d": 2}
	top := TopN(m, 2)
	if len(top) != 2 || top[0].Key != "b" || top[1].Key != "c" {
		t.Errorf("TopN wrong: %+v", top)
	}
	// n larger than the map is fine.
	if got := TopN(m, 10); len(got) != 4 {
		t.Errorf("TopN(10) = %d entries, want 4", len(got))
	}
	if got := TopN(nil, 3); len(got) != 0 {
		t.Errorf("TopN(nil) should be empty, got %+v", got)
	}
}
