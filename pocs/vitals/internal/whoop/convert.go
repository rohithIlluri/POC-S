package whoop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/metrics"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
)

// Source tags every entry that came from WHOOP.
const Source = "whoop"

// ScoreScored is the score_state of a record WHOOP has finished scoring;
// PENDING_SCORE and UNSCORABLE records carry no score yet.
const ScoreScored = "SCORED"

// ID accepts WHOOP identifiers whether they arrive as JSON numbers (cycles)
// or strings (UUIDs for sleeps and workouts in v2).
type ID string

func (id *ID) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		*id = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*id = ID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*id = ID(n.String())
	return nil
}

// Cycle is one WHOOP physiological day.
type Cycle struct {
	ID         ID         `json:"id"`
	Start      time.Time  `json:"start"`
	End        *time.Time `json:"end"`
	ScoreState string     `json:"score_state"`
	Score      *struct {
		Strain           float64 `json:"strain"`
		Kilojoule        float64 `json:"kilojoule"`
		AverageHeartRate float64 `json:"average_heart_rate"`
		MaxHeartRate     float64 `json:"max_heart_rate"`
	} `json:"score"`
}

// Recovery is the morning readiness score attached to a cycle.
type Recovery struct {
	CycleID    ID        `json:"cycle_id"`
	SleepID    ID        `json:"sleep_id"`
	CreatedAt  time.Time `json:"created_at"`
	ScoreState string    `json:"score_state"`
	Score      *struct {
		UserCalibrating  bool     `json:"user_calibrating"`
		RecoveryScore    float64  `json:"recovery_score"`
		RestingHeartRate float64  `json:"resting_heart_rate"`
		HRVRMSSDMilli    float64  `json:"hrv_rmssd_milli"`
		SpO2Percentage   *float64 `json:"spo2_percentage"`
		SkinTempCelsius  *float64 `json:"skin_temp_celsius"`
	} `json:"score"`
}

// Sleep is one sleep or nap.
type Sleep struct {
	ID         ID        `json:"id"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Nap        bool      `json:"nap"`
	ScoreState string    `json:"score_state"`
	Score      *struct {
		StageSummary struct {
			TotalLightSleepMilli    float64 `json:"total_light_sleep_time_milli"`
			TotalSlowWaveSleepMilli float64 `json:"total_slow_wave_sleep_time_milli"`
			TotalREMSleepMilli      float64 `json:"total_rem_sleep_time_milli"`
			TotalAwakeMilli         float64 `json:"total_awake_time_milli"`
			TotalInBedMilli         float64 `json:"total_in_bed_time_milli"`
			DisturbanceCount        int     `json:"disturbance_count"`
			SleepCycleCount         int     `json:"sleep_cycle_count"`
			TotalNoDataMilli        float64 `json:"total_no_data_time_milli"`
		} `json:"stage_summary"`
		RespiratoryRate            *float64 `json:"respiratory_rate"`
		SleepPerformancePercentage *float64 `json:"sleep_performance_percentage"`
		SleepEfficiencyPercentage  *float64 `json:"sleep_efficiency_percentage"`
	} `json:"score"`
}

// Workout is one recorded activity.
type Workout struct {
	ID         ID        `json:"id"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	SportName  string    `json:"sport_name"`
	ScoreState string    `json:"score_state"`
	Score      *struct {
		Strain           float64 `json:"strain"`
		AverageHeartRate float64 `json:"average_heart_rate"`
		Kilojoule        float64 `json:"kilojoule"`
	} `json:"score"`
}

// Data is one sync window's worth of WHOOP records.
type Data struct {
	Cycles     []Cycle
	Recoveries []Recovery
	Sleeps     []Sleep
	Workouts   []Workout
}

// Fetch pulls every collection for [start, end).
func (cl *Client) Fetch(ctx context.Context, start, end time.Time) (Data, error) {
	var d Data
	var err error
	if d.Cycles, err = cl.Cycles(ctx, start, end); err != nil {
		return d, err
	}
	if d.Recoveries, err = cl.Recoveries(ctx, start, end); err != nil {
		return d, err
	}
	if d.Sleeps, err = cl.Sleeps(ctx, start, end); err != nil {
		return d, err
	}
	if d.Workouts, err = cl.Workouts(ctx, start, end); err != nil {
		return d, err
	}
	return d, nil
}

// Convert maps WHOOP records onto vitals metrics. Entry IDs are derived from
// WHOOP's record IDs ("whoop:<metric>:<record>"), so converting the same
// record twice yields the same entry and store.Upsert can merge re-syncs.
// pending counts records skipped because WHOOP hasn't scored them yet; they
// are picked up by a later sync.
//
// Timestamps follow the vitals convention that a night's sleep belongs to
// the day you wake up: sleep and recovery are stamped at wake time, strain
// at the start of the cycle, a workout at its start.
func Convert(d Data) (entries []store.Entry, pending int) {
	add := func(metric string, record ID, t time.Time, v float64, note string) {
		m, ok := metrics.Lookup(metric)
		if !ok || record == "" || t.IsZero() {
			return
		}
		if m.Integer {
			v = math.Round(v)
		} else {
			v = math.Round(v*100) / 100
		}
		if m.Validate(v) != nil {
			return // out-of-range readings (sensor glitches) are dropped, not clamped
		}
		entries = append(entries, store.Entry{
			ID:     fmt.Sprintf("whoop:%s:%s", m.Name, record),
			Time:   t,
			Metric: m.Name,
			Value:  v,
			Note:   note,
			Source: Source,
		})
	}

	wake := map[ID]time.Time{}
	for _, s := range d.Sleeps {
		if !s.Nap {
			wake[s.ID] = s.End
		}
		if s.ScoreState != ScoreScored || s.Score == nil {
			pending++
			continue
		}
		st := s.Score.StageSummary
		asleep := (st.TotalLightSleepMilli + st.TotalSlowWaveSleepMilli + st.TotalREMSleepMilli) / float64(time.Hour/time.Millisecond)
		note := ""
		if s.Nap {
			note = "nap"
		}
		add("sleep", s.ID, s.End, asleep, note)
		if s.Nap {
			continue
		}
		if p := s.Score.SleepPerformancePercentage; p != nil {
			add("sleepperf", s.ID, s.End, *p, "")
		}
		if r := s.Score.RespiratoryRate; r != nil {
			add("resprate", s.ID, s.End, *r, "")
		}
	}

	for _, r := range d.Recoveries {
		if r.ScoreState != ScoreScored || r.Score == nil {
			pending++
			continue
		}
		t, ok := wake[r.SleepID]
		if !ok {
			t = r.CreatedAt
		}
		note := ""
		if r.Score.UserCalibrating {
			note = "calibrating"
		}
		add("recovery", r.CycleID, t, r.Score.RecoveryScore, note)
		add("hrv", r.CycleID, t, r.Score.HRVRMSSDMilli, note)
		add("heartrate", r.CycleID, t, r.Score.RestingHeartRate, note)
		if v := r.Score.SpO2Percentage; v != nil {
			add("spo2", r.CycleID, t, *v, note)
		}
		if v := r.Score.SkinTempCelsius; v != nil {
			add("skintemp", r.CycleID, t, *v, note)
		}
	}

	for _, c := range d.Cycles {
		if c.ScoreState != ScoreScored || c.Score == nil {
			pending++
			continue
		}
		note := ""
		if c.End == nil {
			note = "in progress"
		}
		add("strain", c.ID, c.Start, c.Score.Strain, note)
	}

	for _, w := range d.Workouts {
		if w.End.IsZero() || !w.End.After(w.Start) {
			continue
		}
		add("exercise", w.ID, w.Start, w.End.Sub(w.Start).Minutes(), sport(w.SportName))
	}
	return entries, pending
}

func sport(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "_", " "))
	if name == "" {
		return "workout"
	}
	// WHOOP-supplied text is untrusted: keep it printable and short.
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if len([]rune(name)) > 60 {
		name = string([]rune(name)[:60])
	}
	return name
}
