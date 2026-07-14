package save

import (
	"os"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
)

func isolateHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func TestLoadPetCreatesEggOnFirstRun(t *testing.T) {
	isolateHome(t)
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

	p, err := LoadPet(now)
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsEgg() {
		t.Fatal("first run should create a fresh egg")
	}
	if p.DNA == (sim.DNA{}) {
		t.Error("egg should have non-zero DNA")
	}
}

func TestLoadPetIsIdempotent(t *testing.T) {
	isolateHome(t)
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

	a, err := LoadPet(now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadPet(now)
	if err != nil {
		t.Fatal(err)
	}
	if a.DNA != b.DNA {
		t.Error("second LoadPet should return the same saved pet, not roll a new egg")
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	isolateHome(t)
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	want := sim.NewEgg(sim.NewDNA([]byte("roundtrip")), now)
	want.Level = 5
	want.Health = 77

	if err := SavePet(want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := TryLoadPet()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected a saved pet to be found")
	}
	if got.Level != 5 || got.Health != 77 || got.DNA != want.DNA {
		t.Errorf("roundtrip mismatch: got %+v", got)
	}
}

func TestTryLoadPetNoFileYet(t *testing.T) {
	isolateHome(t)
	_, ok, err := TryLoadPet()
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected ok=false when no pet has ever been saved")
	}
}

func TestSavePetAtomicity(t *testing.T) {
	isolateHome(t)
	p := sim.NewEgg(sim.NewDNA([]byte("atomic")), time.Now())
	if err := SavePet(p); err != nil {
		t.Fatal(err)
	}
	path, _ := PetPath()
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("tmp file should not linger after a successful save")
	}
}

func TestJournalAppendAndRead(t *testing.T) {
	isolateHome(t)
	e1 := Entry{Day: "2026-07-09", At: time.Now(), Kind: "hatched", VoiceID: "hatch_general_01", Text: "Hello."}
	e2 := Entry{Day: "2026-07-10", At: time.Now(), Kind: "diet", VoiceID: "journal_balanced_01", Text: "Good day."}

	if err := AppendJournal(e1); err != nil {
		t.Fatal(err)
	}
	if err := AppendJournal(e2); err != nil {
		t.Fatal(err)
	}
	got, err := ReadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 journal entries, got %d", len(got))
	}
	if got[0].Kind != "hatched" || got[1].Kind != "diet" {
		t.Errorf("journal not in append order: %+v", got)
	}
}

func TestReadJournalNoFileYet(t *testing.T) {
	isolateHome(t)
	got, err := ReadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty journal, got %d entries", len(got))
	}
}

func TestReadJournalSkipsCorruptLines(t *testing.T) {
	isolateHome(t)
	if err := AppendJournal(Entry{Day: "2026-07-09", Kind: "hatched"}); err != nil {
		t.Fatal(err)
	}
	path, _ := JournalPath()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("{not valid json\n")
	f.Close()
	if err := AppendJournal(Entry{Day: "2026-07-10", Kind: "evolved"}); err != nil {
		t.Fatal(err)
	}

	got, err := ReadJournal()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected the 2 valid entries despite a corrupt line in between, got %d", len(got))
	}
}

func TestPetFilePermissions(t *testing.T) {
	isolateHome(t)
	p := sim.NewEgg(sim.NewDNA([]byte("perm-test")), time.Now())
	if err := SavePet(p); err != nil {
		t.Fatal(err)
	}
	path, _ := PetPath()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("pet.json should be 0600, got %o", perm)
	}
}

// TestPreTodayPetRoundtrips confirms the nested PreTodayPet baseline
// pointer (used by the daemon's same-day replay fix) survives a save/load
// cycle intact — a regression guard since it's the one field in Pet that
// isn't a flat value type.
func TestPreTodayPetRoundtrips(t *testing.T) {
	isolateHome(t)
	baseline := sim.NewEgg(sim.NewDNA([]byte("baseline")), time.Now())
	baseline.ActiveDayCount = 2

	pet := baseline
	pet.ActiveDayCount = 3
	pet.PreTodayPet = &baseline
	pet.PreTodayDay = "2026-07-13"

	if err := SavePet(pet); err != nil {
		t.Fatal(err)
	}
	got, ok, err := TryLoadPet()
	if err != nil || !ok {
		t.Fatalf("expected a saved pet: ok=%v err=%v", ok, err)
	}
	if got.PreTodayDay != "2026-07-13" {
		t.Errorf("PreTodayDay lost: got %q", got.PreTodayDay)
	}
	if got.PreTodayPet == nil {
		t.Fatal("PreTodayPet baseline lost across save/load")
	}
	if got.PreTodayPet.ActiveDayCount != 2 {
		t.Errorf("PreTodayPet.ActiveDayCount = %d, want 2", got.PreTodayPet.ActiveDayCount)
	}
	if got.PreTodayPet.PreTodayPet != nil {
		t.Error("the baseline itself must never nest another baseline")
	}
}
