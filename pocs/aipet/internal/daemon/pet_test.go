package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/save"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
)

func writeClaudeTurn(t *testing.T, home, project, session, model string, ts time.Time, in, out int64) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","uuid":"` + session + `-` + ts.Format(time.RFC3339Nano) + `","sessionId":"` + session +
		`","cwd":"/home/dev/` + project + `","timestamp":"` + ts.Format(time.RFC3339) + `","message":{"model":"` + model +
		`","usage":{"input_tokens":` + itoa(in) + `,"output_tokens":` + itoa(out) + `}}}` + "\n"
	f, err := os.OpenFile(filepath.Join(dir, session+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// TestPetHatchesSameDayFromEnthusiasticSession drives a single day of
// real, multi-session Claude Code activity through the daemon and expects
// the pet to hatch immediately — activity-based hatching (not calendar
// days) is the whole point of the collection gap / hatch pacing fix.
func TestPetHatchesSameDayFromEnthusiasticSession(t *testing.T) {
	home := isolateHome(t)
	cfg := config.Default()
	ts := time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local)

	// sim.HatchSessionThreshold well-formed sessions (>=2 turns each), all
	// on the same real day.
	for s := 0; s < 5; s++ {
		session := "s" + itoa(int64(s))
		writeClaudeTurn(t, home, "proj", session, "claude-opus-4-8", ts, 2000, 800)
		writeClaudeTurn(t, home, "proj", session, "claude-opus-4-8", ts.Add(2*time.Minute), 2000, 800)
	}

	snap, err := RunCycleAt(cfg, ts.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if snap.PetError != "" {
		t.Fatalf("unexpected pet error: %s", snap.PetError)
	}
	if snap.Pet.IsEgg() {
		t.Fatal("pet should have hatched same-day from 5 qualifying sessions")
	}
	if snap.Pet.SpeciesID == "" {
		t.Fatal("hatched pet must carry a species id")
	}
}

// TestPetHatchesViaCalendarSafetyValve exercises the fallback path: a
// casual user who never has 5 sessions in one egg's life, but is active
// across HatchWindowDays real calendar days, must still hatch.
func TestPetHatchesViaCalendarSafetyValve(t *testing.T) {
	home := isolateHome(t)
	cfg := config.Default()

	base := time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local)
	for day := 0; day < 5; day++ {
		ts := base.AddDate(0, 0, day)
		writeClaudeTurn(t, home, "proj", "s1", "claude-opus-4-8", ts, 2000, 800)
		writeClaudeTurn(t, home, "proj", "s1", "claude-opus-4-8", ts.Add(10*time.Minute), 2000, 800)
	}

	var snap *Snapshot
	var err error
	for day := 0; day < 5; day++ {
		snap, err = RunCycleAt(cfg, base.AddDate(0, 0, day).Add(time.Hour))
		if err != nil {
			t.Fatalf("cycle day %d: %v", day, err)
		}
	}
	if snap.PetError != "" {
		t.Fatalf("unexpected pet error: %s", snap.PetError)
	}
	if snap.Pet.IsEgg() {
		t.Fatal("pet should have hatched via the calendar-day safety valve")
	}
	if snap.Pet.SpeciesID == "" {
		t.Fatal("hatched pet must carry a species id")
	}
}

// TestPetTickPersistsAcrossCycles ensures the pet's state (egg -> counted
// active days) survives being reloaded between daemon cycles, not just
// within one process's memory.
func TestPetTickPersistsAcrossCycles(t *testing.T) {
	home := isolateHome(t)
	cfg := config.Default()
	base := time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local)

	writeClaudeTurn(t, home, "proj", "s1", "claude-opus-4-8", base, 1000, 500)
	if _, err := RunCycleAt(cfg, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	pet, ok, err := save.TryLoadPet()
	if err != nil || !ok {
		t.Fatalf("expected a saved pet after first cycle: ok=%v err=%v", ok, err)
	}
	if pet.ActiveDayCount != 1 {
		t.Errorf("expected 1 active day recorded, got %d", pet.ActiveDayCount)
	}
}

// TestPetTickCatchesUpMissedDays simulates the machine being off for a few
// days between daemon cycles: the next cycle must catch up every skipped
// calendar day rather than silently losing them.
func TestPetTickCatchesUpMissedDays(t *testing.T) {
	home := isolateHome(t)
	cfg := config.Default()
	base := time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local)

	writeClaudeTurn(t, home, "proj", "s1", "claude-opus-4-8", base, 1000, 500)
	if _, err := RunCycleAt(cfg, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	// Machine "off" for 4 days, no events logged; next cycle runs on day 5.
	future := base.AddDate(0, 0, 4).Add(time.Hour)
	writeClaudeTurn(t, home, "proj", "s2", "claude-opus-4-8", future.Add(-time.Hour), 1000, 500)
	snap, err := RunCycleAt(cfg, future)
	if err != nil {
		t.Fatal(err)
	}
	if snap.PetError != "" {
		t.Fatalf("unexpected pet error: %s", snap.PetError)
	}
	// 2 active days (day 0 and day 4) out of the 5-day span: idle days in
	// between must not be double-counted as active or as multiple hatches.
	if snap.Pet.ActiveDayCount != 2 {
		t.Errorf("expected 2 active days after catch-up, got %d", snap.Pet.ActiveDayCount)
	}
}

// TestPetTickNoActivityStillPublishesSnapshot: a day with zero collected
// events must not error the whole cycle — the pet just goes idle.
func TestPetTickNoActivityStillPublishesSnapshot(t *testing.T) {
	isolateHome(t)
	cfg := config.Default()
	snap, err := RunCycleAt(cfg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if snap.PetError != "" {
		t.Fatalf("unexpected pet error on a quiet day: %s", snap.PetError)
	}
	if !snap.Pet.IsEgg() {
		t.Error("with zero activity ever, the pet should still be an unhatched egg")
	}
}

// TestPastPendingDaysHelper is a narrow unit test of the pure helper
// functions in pet.go, independent of the filesystem. pastPendingDays now
// deliberately EXCLUDES "today" — today is handled by the separate
// replay-from-baseline step in runPetTick, which is safe to re-run.
func TestPastPendingDaysHelper(t *testing.T) {
	byDay := map[string]sim.Digest{"2026-07-01": {}, "2026-07-03": {}}
	got := pastPendingDays("", time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local), "2026-07-03", byDay)
	want := []string{"2026-07-01", "2026-07-02"}
	if len(got) != len(want) {
		t.Fatalf("pastPendingDays = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pastPendingDays[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestPastPendingDaysAlreadyCaughtUp(t *testing.T) {
	got := pastPendingDays("2026-07-05", time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local), "2026-07-05", nil)
	if len(got) != 0 {
		t.Errorf("expected no past pending days when already caught up through yesterday, got %v", got)
	}
}

func TestPastPendingDaysExcludesTodayEvenOnFirstTick(t *testing.T) {
	// Egg started today, never ticked: there must be zero PAST days to
	// seal — today itself is out of scope for this helper.
	got := pastPendingDays("", time.Date(2026, 7, 5, 0, 0, 0, 0, time.Local), "2026-07-05", nil)
	if len(got) != 0 {
		t.Errorf("expected no past pending days on day one, got %v", got)
	}
}

// TestEncountersRollForCompletedDays drives several active days through the
// daemon and expects wild encounters recorded in the dex for the completed
// (pre-today) days — and none double-rolled on re-runs.
func TestEncountersRollForCompletedDays(t *testing.T) {
	home := isolateHome(t)
	cfg := config.Default()
	base := time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local)

	// Realistic flow: the daemon runs a cycle every day. Day 0 has 5
	// well-formed sessions, hatching the egg same-day (activity-based
	// threshold); day 3 introduces a brand-new project and model, so when
	// day 3 completes (i.e. during day 4's cycle) its encounter triggers roll.
	var snap *Snapshot
	var err error
	for day := 0; day < 8; day++ {
		ts := base.AddDate(0, 0, day)
		if day == 0 {
			for s := 0; s < 5; s++ {
				session := "hatch" + itoa(int64(s))
				writeClaudeTurn(t, home, "proj", session, "claude-opus-4-8", ts, 2000, 800)
				writeClaudeTurn(t, home, "proj", session, "claude-opus-4-8", ts.Add(2*time.Minute), 2000, 800)
			}
		} else {
			writeClaudeTurn(t, home, "proj", "s1", "claude-opus-4-8", ts, 2000, 800)
		}
		if day == 3 {
			writeClaudeTurn(t, home, "newproj", "s2", "claude-haiku-4-5", ts.Add(time.Hour), 500, 200)
		}
		snap, err = RunCycleAt(cfg, ts.Add(2*time.Hour))
		if err != nil {
			t.Fatalf("cycle day %d: %v", day, err)
		}
	}
	if snap.PetError != "" {
		t.Fatalf("pet error: %s", snap.PetError)
	}
	if snap.Pet.IsEgg() {
		t.Fatal("pet should have hatched same-day on day 0")
	}
	if len(snap.Dex.Seen) == 0 {
		t.Fatal("expected at least one wild encounter recorded after hatch (new project + new model days)")
	}

	// Re-running the same cycle must not re-roll the same days.
	before := len(snap.Dex.Seen)
	essenceBefore := snap.Dex.EchoEssence
	snap2, err := RunCycleAt(cfg, base.AddDate(0, 0, 5).Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(snap2.Dex.Seen) != before || snap2.Dex.EchoEssence != essenceBefore {
		t.Errorf("re-running a cycle must not re-roll encounters: seen %d->%d essence %d->%d",
			before, len(snap2.Dex.Seen), essenceBefore, snap2.Dex.EchoEssence)
	}
}

// TestSameDayReCollectionReflectsNewActivity is the regression test for the
// bug found in hands-on sandbox testing: once a calendar day had been
// ticked once, every SUBSEQUENT collection cycle that same day silently
// did nothing — new real activity collected later in the day never moved
// the pet (egg progress, XP, stats) until the NEXT calendar day. This
// directly undercut the in-TUI background collector and the switch to
// activity-based (same-day) egg hatching. Verifies: (1) new same-day
// activity is reflected on the very next cycle, and (2) "once per day"
// counters (ActiveDayCount, GritStreak) are NOT double-incremented despite
// many re-ticks of the same day.
func TestSameDayReCollectionReflectsNewActivity(t *testing.T) {
	home := isolateHome(t)
	cfg := config.Default()
	ts := time.Date(2026, 7, 1, 9, 0, 0, 0, time.Local)

	// Cycle 1: one qualifying session (2 turns) — not enough to hatch yet.
	writeClaudeTurn(t, home, "proj", "s0", "claude-opus-4-8", ts, 2000, 800)
	writeClaudeTurn(t, home, "proj", "s0", "claude-opus-4-8", ts.Add(time.Minute), 2000, 800)
	snap1, err := RunCycleAt(cfg, ts.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !snap1.Pet.IsEgg() {
		t.Fatal("should still be an egg after only 1 qualifying session")
	}
	if snap1.Pet.EggSessionCount != 1 {
		t.Fatalf("expected EggSessionCount=1 after cycle 1, got %d", snap1.Pet.EggSessionCount)
	}
	if snap1.Pet.ActiveDayCount != 1 {
		t.Fatalf("expected ActiveDayCount=1 after cycle 1, got %d", snap1.Pet.ActiveDayCount)
	}

	// Cycle 2: SAME calendar day, more real activity arrives (this is what
	// the TUI's background ticker or a second `aipet status` run looks
	// like). Before the fix, this would silently do nothing.
	for s := 1; s < 5; s++ {
		session := "s" + itoa(int64(s))
		st := ts.Add(time.Duration(s) * 10 * time.Minute)
		writeClaudeTurn(t, home, "proj", session, "claude-opus-4-8", st, 2000, 800)
		writeClaudeTurn(t, home, "proj", session, "claude-opus-4-8", st.Add(time.Minute), 2000, 800)
	}
	snap2, err := RunCycleAt(cfg, ts.Add(50*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if snap2.Pet.IsEgg() {
		t.Fatal("expected the egg to hatch same-day once 5 qualifying sessions accumulated across two cycles")
	}
	// Critical: ActiveDayCount/GritStreak must reflect ONE day, not two
	// (one per cycle) — the whole point of the baseline-replay fix.
	if snap2.Pet.ActiveDayCount != 1 {
		t.Fatalf("re-ticking the same day must not double-count ActiveDayCount: got %d, want 1", snap2.Pet.ActiveDayCount)
	}
	if snap2.Pet.GritStreak != 1 {
		t.Fatalf("re-ticking the same day must not double-count GritStreak: got %d, want 1", snap2.Pet.GritStreak)
	}

	// Cycle 3: re-run again with NO new activity — must be a stable no-op,
	// not a further mutation.
	snap3, err := RunCycleAt(cfg, ts.Add(55*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if snap3.Pet.ActiveDayCount != 1 || snap3.Pet.GritStreak != 1 {
		t.Fatalf("a same-day cycle with no new activity must not change counters: got ActiveDayCount=%d GritStreak=%d",
			snap3.Pet.ActiveDayCount, snap3.Pet.GritStreak)
	}
	if snap3.Pet.SpeciesID != snap2.Pet.SpeciesID {
		t.Error("re-running with no new activity must not change the hatched species")
	}
}

// TestSameDayJournalNotSpammed ensures the hatch/evolve/diet journal lines
// for "today" are written once, not once per same-day collection cycle.
func TestSameDayJournalNotSpammed(t *testing.T) {
	home := isolateHome(t)
	cfg := config.Default()
	ts := time.Date(2026, 7, 1, 9, 0, 0, 0, time.Local)
	for s := 0; s < 5; s++ {
		session := "s" + itoa(int64(s))
		st := ts.Add(time.Duration(s) * time.Minute)
		writeClaudeTurn(t, home, "proj", session, "claude-opus-4-8", st, 2000, 800)
		writeClaudeTurn(t, home, "proj", session, "claude-opus-4-8", st.Add(30*time.Second), 2000, 800)
	}

	// Three cycles the same day, same fully-collected activity each time.
	for i := 0; i < 3; i++ {
		if _, err := RunCycleAt(cfg, ts.Add(time.Duration(i+1)*10*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := save.ReadJournal()
	if err != nil {
		t.Fatal(err)
	}
	hatchedCount := 0
	for _, e := range entries {
		if e.Kind == "hatched" {
			hatchedCount++
		}
	}
	if hatchedCount != 1 {
		t.Fatalf("expected exactly 1 'hatched' journal entry across 3 same-day cycles, got %d", hatchedCount)
	}
}
