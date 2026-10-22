package ingest

import (
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/strapdb"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoopble"
)

// motionRecord is a version-24 heart record with a gravity vector and a sleep-state code.
func motionRecord(number uint32, t time.Time, hr byte, theta, phi float64, state byte) []byte {
	b := make([]byte, 96)
	b[0], b[1], b[2] = whoopble.TypeHistory, 24, 5
	binary.LittleEndian.PutUint32(b[3:], number)
	binary.LittleEndian.PutUint32(b[7:], uint32(t.Unix()))
	b[17] = hr
	th, ph := theta*math.Pi/180, phi*math.Pi/180
	g := [3]float64{math.Cos(th) * math.Cos(ph), math.Cos(th) * math.Sin(ph), math.Sin(th)}
	for i, v := range g {
		binary.LittleEndian.PutUint32(b[36+4*i:], math.Float32bits(float32(v)))
	}
	b[80] = state << 4
	return b
}

func TestSleepNightFromRawRecords(t *testing.T) {
	db := open(t)
	loc := utc
	start := time.Date(2026, 11, 8, 18, 0, 0, 0, loc)
	night0 := time.Date(2026, 11, 8, 23, 30, 0, 0, loc)
	night1 := time.Date(2026, 11, 9, 6, 30, 0, 0, loc)
	end := time.Date(2026, 11, 9, 9, 0, 0, 0, loc)
	rng := rand.New(rand.NewSource(7))
	theta, phi := 20.0, 0.0
	var recs []strapdb.Record
	n := uint32(1000)
	for ts := start; ts.Before(end); ts = ts.Add(time.Second) {
		asleep := !ts.Before(night0) && ts.Before(night1)
		hr, state := byte(78), byte(0)
		if asleep {
			theta += rng.NormFloat64() * 0.05
			hr, state = 58+byte(rng.Intn(4)), 2
		} else if ts.Unix()%5 == 0 {
			theta = math.Max(-80, math.Min(80, theta+rng.Float64()*40-20))
			phi += rng.Float64()*60 - 30
		}
		recs = append(recs, strapdb.Record{Number: n, Body: motionRecord(n, ts, hr, theta, phi, state)})
		n++
	}
	if err := db.PutRecords(recs); err != nil {
		t.Fatal(err)
	}

	s, err := Sleep(db, "2026-11-09", loc)
	if err != nil {
		t.Fatal(err)
	}
	if s.Records != len(recs) {
		t.Errorf("records %d, want %d", s.Records, len(recs))
	}
	if s.Motion == nil || s.Device == nil {
		t.Fatalf("missing blocks: %+v", s)
	}
	if d := s.Motion.Window.Start.Sub(night0); d < -15*time.Minute || d > 15*time.Minute {
		t.Errorf("motion window starts %v, want about %v", s.Motion.Window.Start, night0)
	}
	if d := s.Motion.Window.End.Sub(night1); d < -15*time.Minute || d > 15*time.Minute {
		t.Errorf("motion window ends %v, want about %v", s.Motion.Window.End, night1)
	}
	if want := night1.Sub(night0).Hours(); math.Abs(s.Device.SleepHours-want) > 0.01 {
		t.Errorf("device sleep %.3f h, want %.3f", s.Device.SleepHours, want)
	}
	if s.LowestHR < 58 || s.LowestHR > 61 || s.AverageHR < 58 || s.AverageHR > 62 {
		t.Errorf("heart rate in the window: lowest %.1f average %.1f", s.LowestHR, s.AverageHR)
	}
	if s.Note == "" {
		t.Error("the estimate caveat must travel with the numbers")
	}
	// Stages ride along additively. These records carry no RR, so the
	// estimate must say RR was insufficient and be marked very low confidence.
	if s.Stages == nil {
		t.Fatal("a scored night should carry a stage estimate")
	}
	if !s.Stages.Estimate || s.Stages.RRSufficient || s.Stages.Confidence != "very low" || len(s.Stages.Caveats) == 0 || len(s.Stages.Runs) == 0 {
		t.Errorf("stages block: estimate %v rr %v confidence %q caveats %d runs %d", s.Stages.Estimate, s.Stages.RRSufficient, s.Stages.Confidence, len(s.Stages.Caveats), len(s.Stages.Runs))
	}
	if tst := s.Stages.Totals.TSTMin; tst < 5*60 || tst > 7.5*60 {
		t.Errorf("staged sleep %.0f min, the night is 7 h", tst)
	}

	// The day before has no data, and an empty span reports nothing rather than an error.
	empty, err := Sleep(db, "2026-11-05", loc)
	if err != nil || empty.Motion != nil || empty.Device != nil || empty.Records != 0 {
		t.Errorf("empty night: %+v %v", empty, err)
	}
	if _, err := Sleep(db, "not-a-day", loc); err == nil {
		t.Error("a bad date must be an error")
	}
}

func TestSleepIgnoresRecordsThatAreNotTheStrapsHeartLayout(t *testing.T) {
	db := open(t)
	loc := utc
	ts := time.Date(2026, 11, 9, 2, 0, 0, 0, loc)
	sensor := motionRecord(1, ts, 3, 10, 0, 2)
	sensor[1], sensor[2] = 25, 0
	v12 := motionRecord(2, ts.Add(time.Second), 70, 10, 0, 2)
	v12[1], v12[2] = 12, 7
	if err := db.PutRecords([]strapdb.Record{{Number: 1, Body: sensor}, {Number: 2, Body: v12}}); err != nil {
		t.Fatal(err)
	}
	s, err := Sleep(db, "2026-11-09", loc)
	if err != nil || s.Records != 0 {
		t.Errorf("records %d err %v: only version 24 / kind 5 carries these fields", s.Records, err)
	}
}
