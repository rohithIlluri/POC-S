package leaderboard

import (
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

// day builds a timestamp at noon local time on the given date, so local-day
// grouping in Compute is unambiguous regardless of the test machine's zone.
func day(t *testing.T, d string) time.Time {
	t.Helper()
	ts, err := time.ParseInLocation("2006-01-02 15:04", d+" 12:00", time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func ev(ts time.Time, project, model string, cost float64, in, cacheRead int64) store.Event {
	return store.Event{
		Timestamp: ts, Project: project, Model: model,
		CostUSD: cost, Input: in, CacheRead: cacheRead,
	}
}

func TestComputeEmpty(t *testing.T) {
	b := Compute(nil, time.Now())
	if len(b.TopProjects) != 0 || b.Records.TotalTurns != 0 {
		t.Errorf("empty events should yield zero board, got %+v", b)
	}
}

func TestRankingsAndRecords(t *testing.T) {
	now := day(t, "2026-07-08")
	events := []store.Event{
		ev(day(t, "2026-07-06"), "webapp", "claude-opus-4-8", 5.00, 10_000, 0),
		ev(day(t, "2026-07-07"), "webapp", "claude-sonnet-5", 1.00, 10_000, 30_000),
		ev(day(t, "2026-07-07"), "cli", "claude-sonnet-5", 2.00, 5_000, 20_000),
		ev(day(t, "2026-07-08"), "cli", "claude-haiku", 0.10, 1_000, 0),
	}
	b := Compute(events, now)

	if b.TopProjects[0].Name != "webapp" || b.TopProjects[0].Value != 6.00 {
		t.Errorf("top project should be webapp at $6, got %+v", b.TopProjects[0])
	}
	if b.TopModels[0].Name != "claude-opus-4-8" {
		t.Errorf("top model should be opus, got %+v", b.TopModels[0])
	}
	// 2026-07-07 processed 65k tokens with 50k cache reads → ~76.9%, and is
	// the only day above the volume gate (07-06 has 10k, 07-08 has 1k).
	if len(b.BestCacheDays) != 1 || b.BestCacheDays[0].Name != "2026-07-07" {
		t.Fatalf("expected exactly one qualifying cache day, got %+v", b.BestCacheDays)
	}
	if got := b.BestCacheDays[0].Value; got < 76 || got > 78 {
		t.Errorf("cache ratio should be ~76.9%%, got %.1f", got)
	}
	if b.Records.BiggestDayUSD.Name != "2026-07-06" {
		t.Errorf("biggest day should be 07-06 ($5), got %+v", b.Records.BiggestDayUSD)
	}
	if b.Records.BusiestDay.Name != "2026-07-07" || b.Records.BusiestDay.Value != 2 {
		t.Errorf("busiest day should be 07-07 with 2 turns, got %+v", b.Records.BusiestDay)
	}
	if b.Records.ActiveDays != 3 || b.Records.FirstSeen != "2026-07-06" {
		t.Errorf("active days/first seen wrong: %+v", b.Records)
	}
	if b.Records.TotalTurns != 4 {
		t.Errorf("total turns = %d, want 4", b.Records.TotalTurns)
	}
}

func TestStreaks(t *testing.T) {
	now := day(t, "2026-07-08")

	// Three consecutive days ending today → current == longest == 3.
	b := Compute([]store.Event{
		ev(day(t, "2026-07-06"), "p", "m", 1, 100, 0),
		ev(day(t, "2026-07-07"), "p", "m", 1, 100, 0),
		ev(day(t, "2026-07-08"), "p", "m", 1, 100, 0),
	}, now)
	if b.Records.LongestStreak != 3 || b.Records.CurrentStreak != 3 {
		t.Errorf("want streaks 3/3, got %d/%d", b.Records.LongestStreak, b.Records.CurrentStreak)
	}

	// A longer historical streak, but the trailing activity ended 3 days ago:
	// current must be 0, longest preserved.
	b = Compute([]store.Event{
		ev(day(t, "2026-06-01"), "p", "m", 1, 100, 0),
		ev(day(t, "2026-06-02"), "p", "m", 1, 100, 0),
		ev(day(t, "2026-06-03"), "p", "m", 1, 100, 0),
		ev(day(t, "2026-06-04"), "p", "m", 1, 100, 0),
		ev(day(t, "2026-07-05"), "p", "m", 1, 100, 0),
	}, now)
	if b.Records.LongestStreak != 4 {
		t.Errorf("longest streak = %d, want 4", b.Records.LongestStreak)
	}
	if b.Records.CurrentStreak != 0 {
		t.Errorf("stale trailing activity must not count as current, got %d", b.Records.CurrentStreak)
	}

	// Activity ending yesterday still counts as a live streak.
	b = Compute([]store.Event{
		ev(day(t, "2026-07-06"), "p", "m", 1, 100, 0),
		ev(day(t, "2026-07-07"), "p", "m", 1, 100, 0),
	}, now)
	if b.Records.CurrentStreak != 2 {
		t.Errorf("streak ending yesterday should be current, got %d", b.Records.CurrentStreak)
	}
}

func TestTopNCapAndTieBreak(t *testing.T) {
	now := day(t, "2026-07-08")
	var events []store.Event
	// 7 projects with distinct costs, plus two models tied on cost.
	names := []string{"a", "b", "c", "d", "e", "f", "g"}
	for i, n := range names {
		events = append(events, ev(day(t, "2026-07-08"), n, "model-"+n, float64(i+1), 100, 0))
	}
	b := Compute(events, now)
	if len(b.TopProjects) != TopN {
		t.Fatalf("rankings must cap at %d, got %d", TopN, len(b.TopProjects))
	}
	if b.TopProjects[0].Name != "g" {
		t.Errorf("highest spender first, got %+v", b.TopProjects[0])
	}

	// Deterministic tie-break: equal values order by name ascending.
	tie := Compute([]store.Event{
		ev(day(t, "2026-07-08"), "zeta", "m1", 1, 100, 0),
		ev(day(t, "2026-07-08"), "alpha", "m2", 1, 100, 0),
	}, now)
	if tie.TopProjects[0].Name != "alpha" {
		t.Errorf("ties must break by name, got %+v", tie.TopProjects)
	}
}

func TestEmptyProjectNamed(t *testing.T) {
	b := Compute([]store.Event{
		ev(day(t, "2026-07-08"), "", "m", 1, 100, 0),
	}, day(t, "2026-07-08"))
	if b.TopProjects[0].Name != "(unknown)" {
		t.Errorf("empty project should rank as (unknown), got %q", b.TopProjects[0].Name)
	}
}
