package strapdb

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 21, 22, 0, 0, 0, time.UTC)

func open(t *testing.T) *DB {
	t.Helper()
	d, err := Open(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestSamplesRoundTripInOrder(t *testing.T) {
	d := open(t)
	var in []Sample
	for i := 0; i < 100; i++ {
		s := Sample{Time: t0.Add(time.Duration(i) * time.Second), HR: uint8(60 + i%40), Source: Realtime}
		if i%7 == 0 {
			s.RR = []uint16{800, 810}
		}
		in = append(in, s)
	}
	if err := d.PutSamples(in); err != nil {
		t.Fatal(err)
	}
	var got []Sample
	d.Samples(t0, t0.Add(time.Hour), func(s Sample) bool { got = append(got, s); return true })
	if len(got) != len(in) {
		t.Fatalf("got %d samples, want %d", len(got), len(in))
	}
	for i := range in {
		if !got[i].Time.Equal(in[i].Time) || got[i].HR != in[i].HR || len(got[i].RR) != len(in[i].RR) {
			t.Fatalf("sample %d: got %+v want %+v", i, got[i], in[i])
		}
	}
	// A range is half-open: [from, to).
	n, _ := d.CountSamples(t0.Add(10*time.Second), t0.Add(20*time.Second))
	if n != 10 {
		t.Errorf("range count = %d, want 10", n)
	}
}

func TestWritesAreIdempotent(t *testing.T) {
	d := open(t)
	s := []Sample{{Time: t0, HR: 70}, {Time: t0.Add(time.Second), HR: 71}}
	for i := 0; i < 3; i++ {
		if err := d.PutSamples(s); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := d.CountSamples(t0.Add(-time.Hour), t0.Add(time.Hour)); n != 2 {
		t.Errorf("after 3 identical writes there are %d samples, want 2", n)
	}
	r := []Record{{Number: 7, Body: []byte{1, 2, 3}}}
	d.PutRecords(r)
	d.PutRecords(r)
	if b, ok, _ := d.Record(7); !ok || !bytes.Equal(b, []byte{1, 2, 3}) {
		t.Errorf("record 7 = %v %v", b, ok)
	}
	if _, ok, _ := d.Record(8); ok {
		t.Error("record 8 should not exist")
	}
}

func TestRefusesTimesFromAStrapWithoutAClock(t *testing.T) {
	d := open(t)
	bad := time.Date(1971, 1, 1, 0, 43, 26, 0, time.UTC) // what an unset strap reports
	if err := d.PutSamples([]Sample{{Time: t0, HR: 60}, {Time: bad, HR: 61}}); !errors.Is(err, ErrClockUnset) {
		t.Fatalf("err = %v", err)
	}
	if n, _ := d.CountSamples(time.Unix(0, 0), t0.Add(time.Hour)); n != 0 {
		t.Errorf("a refused batch must write nothing, found %d samples", n)
	}
	if err := d.PutEvent(Event{Time: bad, Number: 13}); !errors.Is(err, ErrClockUnset) {
		t.Errorf("event err = %v", err)
	}
	if _, ok, _ := d.DayZero(); ok {
		t.Error("a refused write fixed day zero")
	}
}

func TestDayZeroIsTheFirstSamplesLocalDate(t *testing.T) {
	loc := time.FixedZone("EST", -5*3600)
	d, err := Open(t.TempDir(), loc)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	first := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC) // 21:00 on the 21st in EST
	d.PutSamples([]Sample{{Time: first, HR: 60}})
	d.PutSamples([]Sample{{Time: first.Add(48 * time.Hour), HR: 60}})
	if day, ok, _ := d.DayZero(); !ok || day != "2026-10-21" {
		t.Errorf("day zero = %q %v", day, ok)
	}
}

func TestSaveBurstIsOneTransaction(t *testing.T) {
	d := open(t)
	recs := []Record{{Number: 10, Body: []byte{9}}, {Number: 11, Body: []byte{8}}}
	samples := []Sample{{Time: t0, HR: 66, Source: History}, {Time: t0.Add(time.Second), HR: 67, Source: History}}
	st := SyncState{LastRecord: 11, LastToken: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}, SyncedAt: t0}
	if err := d.SaveBurst(recs, samples, [][]byte{{0x32, 1, 2}, {0x32, 3, 4}}, st); err != nil {
		t.Fatal(err)
	}
	if n, _ := d.RawPackets(); n != 2 {
		t.Errorf("raw packets stored = %d, want the 2 console-log packets", n)
	}
	got, _ := d.SyncState()
	if got.LastRecord != 11 || got.LastToken != st.LastToken || !got.SyncedAt.Equal(t0) {
		t.Fatalf("sync state = %+v", got)
	}
	if n, _ := d.CountSamples(t0, t0.Add(time.Minute)); n != 2 {
		t.Errorf("samples = %d", n)
	}
	// A burst with an unset-clock sample is refused whole: no records, no state.
	bad := []Sample{{Time: time.Date(1971, 1, 1, 0, 0, 5, 0, time.UTC), HR: 60}}
	if err := d.SaveBurst([]Record{{Number: 99, Body: []byte{1}}}, bad, nil, SyncState{LastRecord: 99}); !errors.Is(err, ErrClockUnset) {
		t.Fatalf("err = %v", err)
	}
	if _, ok, _ := d.Record(99); ok {
		t.Error("a refused burst stored a record")
	}
	if got, _ := d.SyncState(); got.LastRecord != 11 {
		t.Errorf("a refused burst moved sync state to %d", got.LastRecord)
	}
}

func TestEventsProfileAndDays(t *testing.T) {
	d := open(t)
	d.PutEvent(Event{Time: t0, Number: 13, Data: []byte{1}})
	d.PutEvent(Event{Time: t0, Number: 28})
	d.PutEvent(Event{Time: t0.Add(time.Minute), Number: 3, Data: []byte{9, 9}})
	var nums []uint16
	d.Events(t0, t0.Add(time.Hour), func(e Event) bool { nums = append(nums, e.Number); return true })
	if len(nums) != 3 || nums[0] != 13 || nums[1] != 28 || nums[2] != 3 {
		t.Errorf("events = %v (two events in the same millisecond must both survive)", nums)
	}

	if err := d.SetProfile(Profile{BirthYear: 1990, Sex: "female", WeightKg: 60}); err != nil {
		t.Fatal(err)
	}
	if p, _ := d.Profile(); p.BirthYear != 1990 || p.Sex != "female" {
		t.Errorf("profile = %+v", p)
	}

	type sum struct{ Strain float64 }
	d.PutDay("2026-10-21", sum{Strain: 9.5})
	var got sum
	if ok, _ := d.GetDay("2026-10-21", &got); !ok || got.Strain != 9.5 {
		t.Errorf("day = %+v %v", got, ok)
	}
	if ok, _ := d.GetDay("2026-10-22", &got); ok {
		t.Error("an uncached day was reported cached")
	}
}

func TestDaysWithDataCountsOnlyFullEnoughDays(t *testing.T) {
	d := open(t)
	day1 := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	var s []Sample
	for i := 0; i < 50; i++ {
		s = append(s, Sample{Time: day1.Add(time.Duration(i) * time.Second), HR: 60})
	}
	for i := 0; i < 5; i++ {
		s = append(s, Sample{Time: day2.Add(time.Duration(i) * time.Second), HR: 60})
	}
	d.PutSamples(s)
	days, err := d.DaysWithData(10)
	if err != nil || len(days) != 1 || days[0] != "2026-10-21" {
		t.Errorf("days = %v %v", days, err)
	}
}

func TestFileIsPrivateAndReopens(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	d.PutSamples([]Sample{{Time: t0, HR: 70}})
	d.Close()
	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("db file mode = %v %v", info.Mode(), err)
	}
	d, err = Open(dir, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if n, _ := d.CountSamples(t0, t0.Add(time.Second)); n != 1 {
		t.Errorf("after reopening there are %d samples", n)
	}
}

// Latency numbers for the access patterns vitals uses. Run with
//
//	go test ./internal/strapdb -bench . -benchmem
func BenchmarkWriteBatch5s(b *testing.B) {
	d, _ := Open(b.TempDir(), time.UTC)
	defer d.Close()
	batch := make([]Sample, 5)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := range batch {
			batch[j] = Sample{Time: t0.Add(time.Duration(i*5+j) * time.Second), HR: 70, RR: []uint16{800}}
		}
		if err := d.PutSamples(batch); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReadNight reads a full 8-hour night (28,800 samples).
func BenchmarkReadNight(b *testing.B) {
	d, _ := Open(b.TempDir(), time.UTC)
	defer d.Close()
	const n = 8 * 3600
	all := make([]Sample, n)
	for i := range all {
		all[i] = Sample{Time: t0.Add(time.Duration(i) * time.Second), HR: 55, RR: []uint16{1000}}
	}
	if err := d.PutSamples(all); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		count := 0
		d.Samples(t0, t0.Add(n*time.Second), func(Sample) bool { count++; return true })
		if count != n {
			b.Fatalf("read %d", count)
		}
	}
}

func TestSavingASexNeverErasesTheDateOfBirth(t *testing.T) {
	d := open(t)
	d.MergeProfile(Profile{BirthDate: "2000-01-15", BirthYear: 2000})
	got, err := d.MergeProfile(Profile{Sex: "male"})
	if err != nil || got.BirthDate != "2000-01-15" || got.Sex != "male" || got.BirthYear != 2000 {
		t.Fatalf("merged = %+v %v", got, err)
	}
	if p, _ := d.Profile(); p != got {
		t.Errorf("stored %+v, returned %+v", p, got)
	}
}

func TestProfileSurvivesALostDatabase(t *testing.T) {
	dir := t.TempDir()
	d, _ := Open(dir, time.UTC)
	d.MergeProfile(Profile{BirthDate: "2000-01-15", BirthYear: 2000, Sex: "male"})
	d.Close()

	info, err := os.Stat(filepath.Join(dir, ProfileFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup %v %v: it must exist and be private", info, err)
	}
	// Lose the database file entirely.
	os.Remove(filepath.Join(dir, FileName))
	d, err = Open(dir, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if p, _ := d.Profile(); p.BirthDate != "2000-01-15" || p.Sex != "male" {
		t.Errorf("profile after losing the database = %+v", p)
	}
	// A database that already has a profile is not overwritten by the backup.
	d.MergeProfile(Profile{Sex: "female"})
	d.Close()
	d, _ = Open(dir, time.UTC)
	defer d.Close()
	if p, _ := d.Profile(); p.Sex != "female" {
		t.Errorf("backup overrode the database: %+v", p)
	}
}

func TestOffWristFlagRoundTrips(t *testing.T) {
	d := open(t)
	d.PutSamples([]Sample{{Time: t0, HR: 70, OffWrist: true}, {Time: t0.Add(time.Second), HR: 71, Source: History}})
	var got []Sample
	d.Samples(t0, t0.Add(time.Minute), func(s Sample) bool { got = append(got, s); return true })
	if len(got) != 2 || !got[0].OffWrist || got[0].Source != Realtime || got[1].OffWrist || got[1].Source != History {
		t.Fatalf("round trip lost the flag or the source: %+v", got)
	}
}

// bbolt's integrity check reports false "reachable freed / unreachable unfreed"
// pages if a write commits while it runs. Check must never overlap a write.
func TestCheckNeverFalselyFailsDuringWrites(t *testing.T) {
	d := open(t)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			d.PutDay(fmt.Sprintf("2026-10-%02d", 1+i%28), map[string]int{"n": i})
			d.PutSamples([]Sample{{Time: t0.Add(time.Duration(i) * time.Second), HR: 60, RR: []uint16{900}}})
			d.SetSyncState(SyncState{LastRecord: uint32(i)})
		}
	}()
	for i := 0; i < 80; i++ {
		if err := d.Check(); err != nil {
			close(stop)
			<-done
			t.Fatalf("Check #%d reported a problem during writes: %v", i, err)
		}
	}
	close(stop)
	<-done
	if err := d.Check(); err != nil {
		t.Fatalf("idle Check: %v", err)
	}
}
