package ingest

import (
	"reflect"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/strapdb"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoopble"
)

// strapRecord is this strap's own heart-rate layout (version 24, kind 5).
func strapRecord(number, unix uint32, hr byte, rr ...uint16) []byte {
	b := historyRecord(number, unix, hr, rr...)
	b[1], b[2] = 24, 5
	return b
}

// sensorRecord is the strap's other 0x2F record (version 25, kind 0, an 80-byte
// body seen every ~20 minutes). Its byte 17 is sensor data: here a 3, as in the
// real capture, where it overwrote a real heart rate.
func sensorRecord(number, unix uint32, byte17 byte) []byte {
	b := historyRecord(number, unix, byte17)
	b[1], b[2] = 25, 0
	return b[:80]
}

func TestSensorRecordsAreKeptRawButNeverBecomeHeartRate(t *testing.T) {
	db := open(t)
	t0 := uint32(time.Date(2026, 11, 8, 3, 0, 0, 0, utc).Unix())
	burst := whoopble.Burst{
		Records: [][]byte{
			strapRecord(200, t0, 58, 1040),
			sensorRecord(201, t0, 3),     // same second as a real record, later in the burst
			sensorRecord(202, t0+1, 255), // a second with no heart record at all
			strapRecord(203, t0+2, 60),
		},
		Expected: 4,
	}
	if err := (HistorySink{DB: db, Now: func() time.Time { return now }}).SaveBurst(burst); err != nil {
		t.Fatal(err)
	}
	for _, n := range []uint32{200, 201, 202, 203} {
		if _, ok, _ := db.Record(n); !ok {
			t.Errorf("raw record %d was not kept", n)
		}
	}
	got := samplesIn(db)
	if len(got) != 2 || got[0].HR != 58 || got[1].HR != 60 {
		t.Fatalf("samples %+v: want only the two real heart rates, 58 and 60", got)
	}
}

func TestRepairHistoryRestoresWhatTheOldDecoderOverwrote(t *testing.T) {
	db := open(t)
	t0 := uint32(time.Date(2026, 11, 8, 3, 0, 0, 0, utc).Unix())
	at := func(s uint32) time.Time { return time.Unix(int64(s), 0) }
	if err := db.PutRecords([]strapdb.Record{
		{Number: 300, Body: strapRecord(300, t0, 58, 1040)},
		{Number: 301, Body: sensorRecord(301, t0, 3)},
		{Number: 302, Body: strapRecord(302, t0+1, 59)},
		{Number: 303, Body: sensorRecord(303, t0+2, 255)},
		{Number: 304, Body: historyRecord(304, 31536020, 61)}, // unset clock: left alone
	}); err != nil {
		t.Fatal(err)
	}
	// What the old decoder left in the database.
	if err := db.PutSamples([]strapdb.Sample{
		{Time: at(t0), HR: 3, Source: strapdb.History},       // real 58 overwritten
		{Time: at(t0 + 1), HR: 59, Source: strapdb.History},  // fine
		{Time: at(t0 + 2), HR: 255, Source: strapdb.History}, // no real record exists here
		{Time: at(t0 + 3), HR: 70, Source: strapdb.Realtime}, // live sample: not ours to touch
	}); err != nil {
		t.Fatal(err)
	}

	rep, err := RepairHistory(db)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Records != 5 || rep.Heart != 2 || rep.Skipped != 2 || rep.Undated != 1 {
		t.Errorf("report %+v", rep)
	}
	var hrs []uint8
	var srcs []strapdb.Source
	for _, s := range samplesIn(db) {
		hrs, srcs = append(hrs, s.HR), append(srcs, s.Source)
	}
	if !reflect.DeepEqual(hrs, []uint8{58, 59, 70}) {
		t.Fatalf("heart rates after repair %v, want [58 59 70]", hrs)
	}
	if srcs[2] != strapdb.Realtime {
		t.Errorf("the live sample changed source: %v", srcs)
	}
	if got := samplesIn(db)[0]; len(got.RR) != 1 || got.RR[0] != 1040 {
		t.Errorf("the restored sample lost its RR interval: %+v", got)
	}

	// A second run leaves the same samples.
	before := samplesIn(db)
	if _, err := RepairHistory(db); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, samplesIn(db)) {
		t.Errorf("a second repair changed the samples")
	}
}
