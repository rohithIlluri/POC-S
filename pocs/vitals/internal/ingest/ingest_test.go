package ingest

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/strapdb"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoopble"
)

var (
	utc = time.UTC
	now = time.Date(2026, 11, 20, 12, 0, 0, 0, utc)
)

func open(t *testing.T) *strapdb.DB {
	t.Helper()
	db, err := strapdb.Open(t.TempDir(), utc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// night writes 22:00 the evening before day to 08:00, one sample a second,
// each carrying one RR interval. RR alternates by 40 ms (RMSSD 40) around a
// 14 breaths/min swing, and heart rate sits at hr.
func writeNight(t *testing.T, db *strapdb.DB, day time.Time, hr uint8) {
	t.Helper()
	start := time.Date(day.Year(), day.Month(), day.Day()-1, 22, 0, 0, 0, utc)
	var batch []strapdb.Sample
	flush := func() {
		if err := db.PutSamples(batch); err != nil {
			t.Fatal(err)
		}
		batch = batch[:0]
	}
	var clock float64
	for i := 0; i < 10*3600; i++ {
		rr := 900 + 30*math.Sin(2*math.Pi*(14.0/60)*clock) + float64(i%2)*20 // alternate by 20: RMSSD ~ 20
		clock += rr / 1000
		batch = append(batch, strapdb.Sample{
			Time: start.Add(time.Duration(i) * time.Second), HR: hr, RR: []uint16{uint16(rr)}, Source: strapdb.Realtime,
		})
		if len(batch) == 36000 {
			flush()
		}
	}
	flush()
}

func TestNightGivesRestingHRHRVAndBreathing(t *testing.T) {
	db := open(t)
	day := time.Date(2026, 11, 2, 0, 0, 0, 0, utc)
	writeNight(t, db, day, 54)
	s, err := Summarize(db, "2026-11-02", strapdb.Profile{}, now)
	if err != nil {
		t.Fatal(err)
	}
	n := s.Night
	if !n.HaveRestingHR || math.Abs(n.RestingHR-54) > 0.01 {
		t.Errorf("resting HR = %v (%v)", n.RestingHR, n.HaveRestingHR)
	}
	if !n.HaveHRV || n.RMSSD < 15 || n.RMSSD > 45 || math.Abs(n.LnRMSSD-math.Log(n.RMSSD)) > 1e-9 {
		t.Errorf("HRV = %+v", n)
	}
	if !n.HaveResp || math.Abs(n.RespRate-14) > 1.5 {
		t.Errorf("respiratory rate = %v (%v), want about 14", n.RespRate, n.HaveResp)
	}
	if n.Hours < 9.9 {
		t.Errorf("hours = %v", n.Hours)
	}
	// No profile, so no max heart rate and no strain.
	if s.HaveStrain {
		t.Error("strain without a profile")
	}
}

func TestSparseRRDoesNotCreateFalseHRV(t *testing.T) {
	// RR present only in tiny scattered bursts, separated by gaps. Differencing
	// across the gaps would invent variability; runs of fewer than MinHRVPairs
	// in total must give no HRV at all.
	db := open(t)
	start := time.Date(2026, 11, 1, 22, 0, 0, 0, utc)
	var s []strapdb.Sample
	for i := 0; i < 3*3600; i++ {
		sm := strapdb.Sample{Time: start.Add(time.Duration(i) * time.Second), HR: 60}
		if i%600 < 2 { // two RR-carrying seconds every ten minutes
			sm.RR = []uint16{uint16(800 + (i/600)*40)}
		}
		s = append(s, sm)
	}
	db.PutSamples(s)
	sum, _ := Summarize(db, "2026-11-02", strapdb.Profile{}, now)
	if sum.Night.HaveHRV {
		t.Errorf("HRV from isolated beats: %+v", sum.Night)
	}
	if !sum.Night.HaveRestingHR {
		t.Error("resting HR should still be found from the heart-rate samples")
	}
}

func TestStrainFromAnHourOfExercise(t *testing.T) {
	db := open(t)
	p := strapdb.Profile{BirthYear: 1990, Sex: "male"}
	start := time.Date(2026, 11, 3, 17, 0, 0, 0, utc)
	var s []strapdb.Sample
	// Half an hour of quiet first, so the day has a resting level to measure against.
	for i := 0; i < 1800; i++ {
		s = append(s, strapdb.Sample{Time: start.Add(-30*time.Minute + time.Duration(i)*time.Second), HR: 60})
	}
	for i := 0; i < 3600; i++ {
		s = append(s, strapdb.Sample{Time: start.Add(time.Duration(i) * time.Second), HR: 150})
	}
	db.PutSamples(s)
	sum, err := Summarize(db, "2026-11-03", p, now)
	if err != nil {
		t.Fatal(err)
	}
	// Age 36: Tanaka max 182.8, so 150 bpm is 82% of max: zone 4, weight 4.
	if !sum.HaveStrain || math.Abs(sum.Edwards-240) > 0.5 {
		t.Fatalf("edwards = %v (%+v)", sum.Edwards, sum)
	}
	want := 21 * (1 - math.Exp(-240.0/200))
	if math.Abs(sum.Strain-want) > 0.05 {
		t.Errorf("strain = %v, want %v", sum.Strain, want)
	}
	if sum.Banister <= 0 {
		t.Error("Banister load missing with a sex on the profile")
	}
	// Exercise alone, with nothing to measure the reserve against, gives no Banister value.
	other := open(t)
	var only []strapdb.Sample
	for i := 0; i < 3600; i++ {
		only = append(only, strapdb.Sample{Time: start.Add(time.Duration(i) * time.Second), HR: 150})
	}
	other.PutSamples(only)
	if o, _ := Summarize(other, "2026-11-03", p, now); o.Banister != 0 || !o.HaveStrain {
		t.Errorf("an exercise-only day: banister %v strain %v", o.Banister, o.HaveStrain)
	}
	if sum.Minutes < 89.9 || sum.Minutes > 90.1 {
		t.Errorf("minutes = %v", sum.Minutes)
	}
}

func TestStatusCountsDaysAndNightsAndSaysWhatIsBlocked(t *testing.T) {
	db := open(t)
	for d := 2; d <= 4; d++ {
		writeNight(t, db, time.Date(2026, 11, d, 0, 0, 0, 0, utc), 55)
	}
	o, err := Status(db, now)
	if err != nil {
		t.Fatal(err)
	}
	if o.DayZero != "2026-11-01" {
		t.Errorf("day zero = %q (the first sample was at 22:00 on Nov 1)", o.DayZero)
	}
	if o.Nights != 3 {
		t.Errorf("nights = %d, want 3", o.Nights)
	}
	if o.Days < 3 {
		t.Errorf("days = %d", o.Days)
	}
	state := map[string]string{}
	for _, s := range o.Readiness {
		state[s.Metric] = s.State
	}
	if state["strain"] == "blocked" {
		t.Error("strain needs nothing but heart rate and must not be blocked")
	}
	if state["recovery"] == "blocked" {
		t.Errorf("recovery = %q: three live nights are stored, so it is calibrating, not blocked", state["recovery"])
	}
	if state["hrv"] == "blocked" || state["resting_hr"] == "blocked" {
		t.Error("hrv and resting heart rate work from the day's own samples")
	}
	if state["sleep"] != "blocked" {
		t.Errorf("sleep = %q: it still needs history and verified motion", state["sleep"])
	}
	if o.DBBytes <= 0 {
		t.Error("db size missing")
	}
}

func TestRecoveryAgainstAPersonalBaseline(t *testing.T) {
	db := open(t)
	// Nine nights at the same level, then a tenth with much higher HRV: it must
	// beat the baseline. (The baseline nights share a pattern, so SD floors at MinSD.)
	for d := 1; d <= 9; d++ {
		writeNight(t, db, time.Date(2026, 11, d, 0, 0, 0, 0, utc), 55)
	}
	target := time.Date(2026, 11, 10, 0, 0, 0, 0, utc)
	start := time.Date(2026, 11, 9, 22, 0, 0, 0, utc)
	var s []strapdb.Sample
	for i := 0; i < 10*3600; i++ {
		rr := 900 + float64(i%2)*120 // alternate by 120: far more variability
		s = append(s, strapdb.Sample{Time: start.Add(time.Duration(i) * time.Second), HR: 50, RR: []uint16{uint16(rr)}})
	}
	db.PutSamples(s)
	_ = target
	r, ok, err := RecoveryFor(db, "2026-11-10", strapdb.Profile{}, now)
	if err != nil || !ok {
		t.Fatalf("recovery: %+v %v %v", r, ok, err)
	}
	if r.Score <= 67 || r.Tier != "high" || r.Full {
		t.Errorf("a clearly better night scored %+v (9 nights of baseline is still calibrating)", r)
	}
	// Too few earlier nights: no score.
	db2 := open(t)
	writeNight(t, db2, time.Date(2026, 11, 3, 0, 0, 0, 0, utc), 55)
	if _, ok, _ := RecoveryFor(db2, "2026-11-03", strapdb.Profile{}, now); ok {
		t.Error("a score with no baseline")
	}
}

func TestSummaryCacheIsDroppedWhenDataArrives(t *testing.T) {
	db := open(t)
	day := time.Date(2026, 11, 5, 0, 0, 0, 0, utc)
	db.PutSamples([]strapdb.Sample{{Time: day.Add(10 * time.Hour), HR: 80}})
	a, _ := Day(db, "2026-11-05", strapdb.Profile{}, now)
	if a.Samples != 1 {
		t.Fatalf("samples = %d", a.Samples)
	}
	db.PutSamples([]strapdb.Sample{{Time: day.Add(10*time.Hour + time.Second), HR: 81}})
	b, _ := Day(db, "2026-11-05", strapdb.Profile{}, now)
	if b.Samples != 2 {
		t.Errorf("a new sample did not refresh the cached day (%d)", b.Samples)
	}
	// A profile change must refresh strain too.
	db.SetProfile(strapdb.Profile{BirthYear: 1990, Sex: "male"})
	c, _ := Day(db, "2026-11-05", strapdb.Profile{BirthYear: 1990, Sex: "male"}, now)
	if !c.HaveStrain {
		t.Error("the cached day ignored the new profile")
	}
}

// historyRecord builds a 0x2F body with the fields DecodeHistory reads.
func historyRecord(number uint32, unix uint32, hr byte, rr ...uint16) []byte {
	b := make([]byte, 88)
	b[0], b[1], b[2] = whoopble.TypeHistory, 12, 7
	binary.LittleEndian.PutUint32(b[3:], number)
	binary.LittleEndian.PutUint32(b[7:], unix)
	b[17], b[18] = hr, byte(len(rr))
	for i, v := range rr {
		binary.LittleEndian.PutUint16(b[19+2*i:], v)
	}
	return b
}

func TestHistorySinkKeepsEveryRecordButOnlyDatesTheValidOnes(t *testing.T) {
	db := open(t)
	t0 := uint32(time.Date(2026, 11, 8, 3, 0, 0, 0, utc).Unix())
	burst := whoopble.Burst{
		Records: [][]byte{
			historyRecord(100, 31536020, 61, 980), // the strap's unset 1971 clock
			historyRecord(101, t0, 58, 1040),
			historyRecord(102, t0+1, 59),
		},
		Expected: 3,
		Token:    [8]byte{9, 9, 9, 9, 9, 9, 9, 9},
	}
	if err := (HistorySink{DB: db, Now: func() time.Time { return now }}).SaveBurst(burst); err != nil {
		t.Fatal(err)
	}
	for _, n := range []uint32{100, 101, 102} {
		if _, ok, _ := db.Record(n); !ok {
			t.Errorf("raw record %d was not kept", n)
		}
	}
	st, _ := db.SyncState()
	if st.LastRecord != 102 || st.LastToken != burst.Token || !st.SyncedAt.Equal(now) {
		t.Errorf("sync state %+v", st)
	}
	var got []strapdb.Sample
	db.Samples(time.Unix(0, 0), now, func(s strapdb.Sample) bool { got = append(got, s); return true })
	if len(got) != 2 || got[0].HR != 58 || got[0].Source != strapdb.History || len(got[0].RR) != 1 {
		t.Fatalf("samples %+v: only the two valid-clock records should be dated", got)
	}
	// Re-running the same burst changes nothing.
	(HistorySink{DB: db, Now: func() time.Time { return now }}).SaveBurst(burst)
	if n, _ := db.CountSamples(time.Unix(0, 0), now); n != 2 {
		t.Errorf("a repeated burst left %d samples", n)
	}
}

func TestSyncEngineIntoTheDatabase(t *testing.T) {
	db := open(t)
	sink := HistorySink{DB: db, Now: func() time.Time { return now }}
	h := whoopble.NewHistorySync(sink, 1)
	meta := func(sub byte, data ...byte) []byte {
		b := []byte{whoopble.TypeMetadata, 1, sub, 0, 0, 0, 0, 0, 0}
		d := append([]byte(nil), data...)
		for len(d) < 15 {
			d = append(d, 0)
		}
		return append(b, d...)
	}
	t0 := uint32(time.Date(2026, 11, 8, 3, 0, 0, 0, utc).Unix())
	h.Feed(meta(whoopble.MetaHistoryStart))
	h.Feed(historyRecord(1, t0, 60, 1000))
	h.Feed(historyRecord(2, t0+1, 61))
	reply, err := h.Feed(meta(whoopble.MetaHistoryEnd, 2, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8))
	if err != nil || reply == nil {
		t.Fatalf("%x %v", reply, err)
	}
	// The ack went out only after the rows were in the database.
	if n, _ := db.CountSamples(time.Unix(0, 0), now); n != 2 {
		t.Errorf("samples in the database when the ack was produced: %d", n)
	}
	if st, _ := db.SyncState(); st.LastRecord != 2 || st.LastToken != [8]byte{1, 2, 3, 4, 5, 6, 7, 8} {
		t.Errorf("sync state %+v", st)
	}
}

// BenchmarkSummarizeDay is the cost of summarising one full local day (86,400
// samples of heart rate, a night of RR, strain, resting HR, HRV and breathing).
func BenchmarkSummarizeDay(b *testing.B) {
	db, _ := strapdb.Open(b.TempDir(), utc)
	defer db.Close()
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, utc)
	s := make([]strapdb.Sample, 0, 86400)
	var clock float64
	for i := 0; i < 86400; i++ {
		rr := 900 + 30*math.Sin(2*math.Pi*(14.0/60)*clock) + float64(i%2)*20
		clock += rr / 1000
		s = append(s, strapdb.Sample{Time: start.Add(time.Duration(i) * time.Second), HR: uint8(55 + i%60), RR: []uint16{uint16(rr)}})
	}
	db.PutSamples(s)
	p := strapdb.Profile{BirthYear: 1990, Sex: "male"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Summarize(db, "2026-11-02", p, now); err != nil {
			b.Fatal(err)
		}
	}
}

func TestStrainUsesTheExactAgeFromTheBirthDate(t *testing.T) {
	db := open(t)
	start := time.Date(2026, 11, 3, 17, 0, 0, 0, utc)
	var s []strapdb.Sample
	for i := 0; i < 3600; i++ {
		s = append(s, strapdb.Sample{Time: start.Add(time.Duration(i) * time.Second), HR: 150})
	}
	db.PutSamples(s)
	// Born 2000-01-15: 26.8 years old on `now`, Tanaka max 189.2, so 150 bpm is
	// 79% of max (zone 3, weight 3): an hour is 180, not the 240 a 36-year-old gets.
	sum, err := Summarize(db, "2026-11-03", strapdb.Profile{BirthDate: "2000-01-15"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !sum.HaveStrain || math.Abs(sum.Edwards-180) > 0.5 || math.Abs(sum.HRMaxUsed-(208-0.7*science27(now))) > 0.1 {
		t.Fatalf("summary %+v", sum)
	}
	// A birth date alone (no sex) is enough for strain but not a "complete" profile.
	if _, haveAge := ageOf(strapdb.Profile{BirthDate: "2000-01-15"}, now); !haveAge {
		t.Error("a birth date was not read as an age")
	}
	if !profileCaps(strapdb.Profile{BirthDate: "2000-01-15"}).Profile {
		t.Error("a birth date is a complete profile: sex is optional")
	}
	if profileCaps(strapdb.Profile{Sex: "male"}).Profile {
		t.Error("a sex alone gives no age, so it is not a complete profile")
	}
}

func science27(now time.Time) float64 {
	b := time.Date(2000, 1, 15, 0, 0, 0, 0, utc)
	return now.Sub(b).Hours() / 24 / 365.25
}

// unsetBurst builds a burst of records stamped on the strap's unset clock.
func unsetBurst(first uint32, stamps ...uint32) whoopble.Burst {
	var recs [][]byte
	for i, ts := range stamps {
		recs = append(recs, historyRecord(first+uint32(i), ts, byte(58+i%5), 1000))
	}
	return whoopble.Burst{Records: recs, Expected: uint32(len(recs)), Token: [8]byte{7, 7, 7, 7, 7, 7, 7, 7}}
}

func samplesIn(db *strapdb.DB) []strapdb.Sample {
	var out []strapdb.Sample
	db.Samples(time.Unix(0, 0), now.Add(24*time.Hour), func(s strapdb.Sample) bool { out = append(out, s); return true })
	return out
}

func TestUnsetClockHistoryIsPlacedInRealTimeUsingTheClockOffset(t *testing.T) {
	db := open(t)
	// The strap was powered on an hour ago: its clock reads 1971-01-01 + 3600 s now.
	const strapNow = 31536000 + 3600
	sink := HistorySink{DB: db, Now: func() time.Time { return now }, HaveOffset: true, StrapNowSec: strapNow, OffsetSec: now.Unix() - strapNow}
	var logged []string
	sink.Logf = func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }
	// Records written 30, 29 and 28 minutes ago (on the strap's own clock).
	if err := sink.SaveBurst(unsetBurst(10, strapNow-1800, strapNow-1740, strapNow-1680)); err != nil {
		t.Fatal(err)
	}
	got := samplesIn(db)
	if len(got) != 3 {
		t.Fatalf("got %d samples, want 3 re-dated", len(got))
	}
	for i, want := range []time.Duration{-1800, -1740, -1680} {
		if !got[i].Time.Equal(now.Add(want * time.Second)) {
			t.Errorf("sample %d at %v, want %v", i, got[i].Time, now.Add(want*time.Second))
		}
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "3 re-dated") {
		t.Errorf("log = %q", logged)
	}
	if _, ok, _ := db.Record(11); !ok {
		t.Error("raw record not stored")
	}
}

func TestRedatingRefusesWhatCannotBeRight(t *testing.T) {
	const strapNow = 31536000 + 3600
	mk := func() (*strapdb.DB, HistorySink) {
		db := open(t)
		return db, HistorySink{DB: db, Now: func() time.Time { return now }, HaveOffset: true, StrapNowSec: strapNow, OffsetSec: now.Unix() - strapNow}
	}
	t.Run("a record later than the strap's own clock is from another boot", func(t *testing.T) {
		db, sink := mk()
		sink.SaveBurst(unsetBurst(1, strapNow-100, strapNow+500))
		if got := samplesIn(db); len(got) != 1 {
			t.Errorf("%d samples; the future-stamped record must stay undated", len(got))
		}
		if _, ok, _ := db.Record(2); !ok {
			t.Error("but it must still be kept raw")
		}
	})
	t.Run("a clock that goes backwards means a reboot: the older records are dropped", func(t *testing.T) {
		db, sink := mk()
		// Stamps 3000, 3010, 3020, then the clock restarts: 100, 110. Only the last two share the current boot.
		sink.SaveBurst(unsetBurst(1, 31536000+3000, 31536000+3010, 31536000+3020, 31536000+100, 31536000+110))
		got := samplesIn(db)
		if len(got) != 2 {
			t.Fatalf("%d samples, want the 2 after the reboot", len(got))
		}
		if !got[0].Time.Equal(time.Unix(now.Unix()-3500, 0)) {
			t.Errorf("first kept sample at %v", got[0].Time)
		}
	})
	t.Run("without a clock reading nothing on the unset clock is dated", func(t *testing.T) {
		db := open(t)
		sink := HistorySink{DB: db, Now: func() time.Time { return now }}
		sink.SaveBurst(unsetBurst(1, strapNow-100, strapNow-90))
		if len(samplesIn(db)) != 0 {
			t.Error("records were dated with no offset")
		}
		if _, ok, _ := db.Record(1); !ok {
			t.Error("not kept raw")
		}
	})
	t.Run("a mapping that lands months in the past is a wrong mapping", func(t *testing.T) {
		db := open(t)
		wrong := HistorySink{DB: db, Now: func() time.Time { return now }, HaveOffset: true, StrapNowSec: strapNow, OffsetSec: now.Unix() - strapNow - 90*86400}
		wrong.SaveBurst(unsetBurst(1, strapNow-100))
		if len(samplesIn(db)) != 0 {
			t.Error("a record was placed 90 days ago")
		}
	})
	t.Run("records already on a valid clock ignore the offset", func(t *testing.T) {
		db, sink := mk()
		valid := uint32(now.Add(-time.Hour).Unix())
		sink.SaveBurst(unsetBurst(1, valid))
		got := samplesIn(db)
		if len(got) != 1 || !got[0].Time.Equal(time.Unix(int64(valid), 0)) {
			t.Errorf("%+v", got)
		}
	})
}

func TestABurstWithAnUnstorableRecordIsRefusedSoItIsNeverAcknowledged(t *testing.T) {
	db := open(t)
	sink := HistorySink{DB: db, Now: func() time.Time { return now }}
	burst := unsetBurst(1, 31536100)
	burst.Records = append(burst.Records, []byte{whoopble.TypeHistory, 12, 7, 9}) // 4 bytes: nothing to key it by
	err := sink.SaveBurst(burst)
	if !errors.Is(err, ErrUnstorable) {
		t.Fatalf("err = %v", err)
	}
	if _, ok, _ := db.Record(1); ok {
		t.Error("a refused burst stored a record")
	}
	// Through the sync engine: no ack is produced, so the strap keeps its data.
	h := whoopble.NewHistorySync(sink, 1)
	m := func(sub byte, d ...byte) []byte {
		b := []byte{whoopble.TypeMetadata, 1, sub, 0, 0, 0, 0, 0, 0}
		for len(d) < 15 {
			d = append(d, 0)
		}
		return append(b, d...)
	}
	h.Feed(m(whoopble.MetaHistoryStart))
	h.Feed(historyRecord(1, 31536100, 60, 900))
	h.Feed([]byte{whoopble.TypeHistory, 12, 7, 9})
	reply, err := h.Feed(m(whoopble.MetaHistoryEnd, 2, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8))
	if reply != nil || err == nil {
		t.Fatalf("the engine acknowledged a burst it could not store: reply=%x err=%v", reply, err)
	}
}

func TestAMixedBurstKeepsConsoleLogsRawAndStillDatesTheHeartRecords(t *testing.T) {
	db := open(t)
	const strapNow = 31536000 + 3600
	sink := HistorySink{DB: db, Now: func() time.Time { return now }, HaveOffset: true, StrapNowSec: strapNow, OffsetSec: now.Unix() - strapNow}
	var logged []string
	sink.Logf = func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }
	burst := unsetBurst(100, strapNow-300, strapNow-299)
	for i := 0; i < 5; i++ {
		burst.Records = append(burst.Records, []byte{whoopble.TypeConsoleLog, 0x4b, 2, 0, byte(i), 'x'})
	}
	burst.Expected = 7
	if err := sink.SaveBurst(burst); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.RawPackets(); n != 5 {
		t.Errorf("raw packets = %d, want the 5 console logs", n)
	}
	if got := samplesIn(db); len(got) != 2 {
		t.Errorf("%d heart samples, want 2", len(got))
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "2 of 7 packets were heart records") || !strings.Contains(logged[0], "0x32 x5") {
		t.Errorf("log = %q", logged)
	}
	// A burst of nothing but console logs (what a real strap sent first) stores fine and dates nothing.
	db2 := open(t)
	only := whoopble.Burst{Expected: 3, Token: [8]byte{1}}
	for i := 0; i < 3; i++ {
		only.Records = append(only.Records, []byte{whoopble.TypeConsoleLog, 0x4b, 2, 0, byte(i)})
	}
	if err := (HistorySink{DB: db2, Now: func() time.Time { return now }}).SaveBurst(only); err != nil {
		t.Fatal(err)
	}
	if n, _ := db2.RawPackets(); n != 3 || len(samplesIn(db2)) != 0 {
		t.Errorf("raw %d, samples %d", n, len(samplesIn(db2)))
	}
	if st, _ := db2.SyncState(); st.LastToken != [8]byte{1} {
		t.Errorf("sync position not recorded for a console-log-only burst: %+v", st)
	}
}
