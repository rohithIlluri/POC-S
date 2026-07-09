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

// TestPetTicksThroughHatchWindow drives three active days of real (collected)
// Claude Code activity through the daemon and expects the pet to hatch.
func TestPetTicksThroughHatchWindow(t *testing.T) {
	home := isolateHome(t)
	cfg := config.Default()

	base := time.Date(2026, 7, 1, 10, 0, 0, 0, time.Local)
	for day := 0; day < 3; day++ {
		ts := base.AddDate(0, 0, day)
		writeClaudeTurn(t, home, "proj", "s1", "claude-opus-4-8", ts, 2000, 800)
		writeClaudeTurn(t, home, "proj", "s1", "claude-opus-4-8", ts.Add(10*time.Minute), 2000, 800)
	}

	var snap *Snapshot
	var err error
	for day := 0; day < 3; day++ {
		snap, err = RunCycleAt(cfg, base.AddDate(0, 0, day).Add(time.Hour))
		if err != nil {
			t.Fatalf("cycle day %d: %v", day, err)
		}
	}
	if snap.PetError != "" {
		t.Fatalf("unexpected pet error: %s", snap.PetError)
	}
	if snap.Pet.IsEgg() {
		t.Fatal("pet should have hatched after 3 active days")
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

// TestHatchWindowHelper is a narrow unit test of the pure helper functions
// in pet.go, independent of the filesystem.
func TestPendingDaysHelper(t *testing.T) {
	byDay := map[string]sim.Digest{"2026-07-01": {}, "2026-07-03": {}}
	got := pendingDays("", time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local), "2026-07-03", byDay)
	want := []string{"2026-07-01", "2026-07-02", "2026-07-03"}
	if len(got) != len(want) {
		t.Fatalf("pendingDays = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pendingDays[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestPendingDaysAlreadyCaughtUpToday(t *testing.T) {
	got := pendingDays("2026-07-05", time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local), "2026-07-05", nil)
	if len(got) != 0 {
		t.Errorf("expected no pending days when already ticked today, got %v", got)
	}
}
