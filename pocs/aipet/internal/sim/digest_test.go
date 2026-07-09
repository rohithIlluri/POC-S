package sim

import (
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

func ev(session, project, model string, ts time.Time, in, out, cr int64, cost float64) store.Event {
	return store.Event{
		Key: session + model + ts.String(), Source: "claude-code",
		Session: session, Project: project, Model: model, Timestamp: ts,
		Input: in, Output: out, CacheRead: cr, CostUSD: cost,
	}
}

func TestDigestsBucketsByLocalDay(t *testing.T) {
	day1 := time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local)
	day2 := time.Date(2026, 7, 2, 10, 0, 0, 0, time.Local)
	events := []store.Event{
		ev("s1", "p1", "opus", day1, 100, 50, 0, 1.0),
		ev("s1", "p1", "opus", day1.Add(time.Minute), 100, 50, 0, 1.0),
		ev("s2", "p1", "sonnet", day2, 100, 50, 0, 0.5),
	}
	digests := Digests(events)
	if len(digests) != 2 {
		t.Fatalf("expected 2 digests, got %d", len(digests))
	}
	if digests[0].Turns != 2 {
		t.Errorf("day1 turns = %d, want 2", digests[0].Turns)
	}
	if digests[1].Turns != 1 {
		t.Errorf("day2 turns = %d, want 1", digests[1].Turns)
	}
	// Ascending order.
	if digests[0].Day >= digests[1].Day {
		t.Errorf("digests not ascending: %s, %s", digests[0].Day, digests[1].Day)
	}
}

func TestDigestNewModelsOnlyCountsFirstAppearance(t *testing.T) {
	day1 := time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local)
	day2 := time.Date(2026, 7, 2, 10, 0, 0, 0, time.Local)
	events := []store.Event{
		ev("s1", "p1", "opus", day1, 100, 50, 0, 1.0),
		ev("s2", "p1", "opus", day2, 100, 50, 0, 1.0),   // same model, second day
		ev("s2", "p1", "sonnet", day2, 100, 50, 0, 0.5), // new model, second day
	}
	digests := Digests(events)
	if digests[0].NewModels != 1 {
		t.Errorf("day1 NewModels = %d, want 1 (opus, first time)", digests[0].NewModels)
	}
	if digests[1].NewModels != 1 {
		t.Errorf("day2 NewModels = %d, want 1 (only sonnet is new)", digests[1].NewModels)
	}
}

func TestDigestCacheRatio(t *testing.T) {
	d := Digest{TokensIn: 40, CacheRead: 60}
	if got := d.CacheRatio(); got != 0.6 {
		t.Errorf("CacheRatio = %v, want 0.6", got)
	}
	empty := Digest{}
	if got := empty.CacheRatio(); got != 0 {
		t.Errorf("CacheRatio with no tokens = %v, want 0", got)
	}
}

func TestDigestFragmentedCountsShortSessions(t *testing.T) {
	base := time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local)
	events := []store.Event{
		ev("s1", "p1", "opus", base, 10, 10, 0, 0.1), // 1-turn session: fragmented
		ev("s2", "p1", "opus", base, 10, 10, 0, 0.1), // 1-turn session: fragmented
		ev("s3", "p1", "opus", base, 10, 10, 0, 0.1), // part of a longer session
		ev("s3", "p1", "opus", base.Add(time.Minute), 10, 10, 0, 0.1),
		ev("s3", "p1", "opus", base.Add(2*time.Minute), 10, 10, 0, 0.1),
	}
	d := Digests(events)[0]
	if d.Fragmented != 2 {
		t.Errorf("Fragmented = %d, want 2", d.Fragmented)
	}
	if d.Sessions != 3 {
		t.Errorf("Sessions = %d, want 3", d.Sessions)
	}
}

func TestDigestMaxGapAndNightSession(t *testing.T) {
	base := time.Date(2026, 7, 1, 2, 0, 0, 0, time.Local) // 2am local
	events := []store.Event{
		ev("s1", "p1", "opus", base, 10, 10, 0, 0.1),
		ev("s1", "p1", "opus", base.Add(20*time.Minute), 10, 10, 0, 0.1),
	}
	d := Digests(events)[0]
	if !d.NightSession {
		t.Error("expected NightSession=true for a 2am event")
	}
	if d.MaxGapMin != 20 {
		t.Errorf("MaxGapMin = %d, want 20", d.MaxGapMin)
	}
}

func TestDigestsEmptyInput(t *testing.T) {
	if got := Digests(nil); len(got) != 0 {
		t.Errorf("expected no digests for no events, got %d", len(got))
	}
}
