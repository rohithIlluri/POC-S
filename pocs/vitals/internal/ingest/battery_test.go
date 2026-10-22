package ingest

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/strapdb"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoopble"
)

// consolePacket builds a console-log body the way the strap sends it: a
// 13-byte header with the packet time at [4:8), then the text.
func consolePacket(at time.Time, text string) []byte {
	b := make([]byte, 13, 13+len(text))
	b[0] = whoopble.TypeConsoleLog
	binary.LittleEndian.PutUint32(b[4:], uint32(at.Unix()))
	return append(b, text...)
}

func storeSOC(t *testing.T, db *strapdb.DB, start time.Time, every time.Duration, levels ...float64) {
	t.Helper()
	var bodies [][]byte
	for i, soc := range levels {
		at := start.Add(time.Duration(i) * every)
		bodies = append(bodies, consolePacket(at, fmt.Sprintf("10, %d: SUPERVISOR: SOC report: %.2f\n", 1000+i, soc)))
		bodies = append(bodies, consolePacket(at, "10, 1: BLE: History burst success.\n")) // unrelated line
	}
	if err := db.SaveBurst(nil, nil, bodies, strapdb.SyncState{}); err != nil {
		t.Fatal(err)
	}
}

func TestBatteryDrainAndHoursLeft(t *testing.T) {
	db := open(t)
	start := time.Date(2026, 10, 21, 22, 0, 0, 0, utc)
	// 1.2 percentage points an hour, one reading every 30 minutes for 9 hours.
	var levels []float64
	for i := 0; i < 19; i++ {
		levels = append(levels, 45-0.6*float64(i))
	}
	storeSOC(t, db, start, 30*time.Minute, levels...)
	b, err := ReadBattery(db)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Have || b.Points != 19 || math.Abs(b.SOC-34.2) > 0.01 {
		t.Fatalf("battery %+v", b)
	}
	if !b.At.Equal(start.Add(9 * time.Hour)) {
		t.Errorf("At = %v, want the newest reading", b.At)
	}
	if math.Abs(b.PerHour-1.2) > 0.02 || math.Abs(b.HoursLeft-34.2/1.2) > 0.6 {
		t.Errorf("drain %.3f/h, %.1f h left; want 1.2/h, about 28.5 h", b.PerHour, b.HoursLeft)
	}
	if b.Charging {
		t.Error("a falling level is not charging")
	}
}

func TestBatteryWithoutEnoughHistoryGivesALevelButNoPrediction(t *testing.T) {
	db := open(t)
	storeSOC(t, db, time.Date(2026, 10, 21, 22, 0, 0, 0, utc), 10*time.Minute, 40, 39.9) // 10 minutes of data
	b, _ := ReadBattery(db)
	if !b.Have || b.SOC != 39.9 || b.HoursLeft != 0 || b.PerHour != 0 {
		t.Errorf("battery %+v: want the level only", b)
	}
}

func TestBatteryRisingMeansCharging(t *testing.T) {
	db := open(t)
	storeSOC(t, db, time.Date(2026, 10, 21, 22, 0, 0, 0, utc), 20*time.Minute, 30, 33, 36, 39, 42, 45, 48)
	b, _ := ReadBattery(db)
	if !b.Charging || b.HoursLeft != 0 {
		t.Errorf("battery %+v: want charging and no hours-left", b)
	}
}

func TestBatteryUnknownWhenNoConsoleWasEverStored(t *testing.T) {
	b, err := ReadBattery(open(t))
	if err != nil || b.Have || b.Points != 0 || b.Note == "" {
		t.Errorf("battery %+v, err %v", b, err)
	}
}
