package ingest

import (
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/strapdb"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoopble"
)

// Battery is the strap's own state-of-charge reading, taken from the "SOC
// report" lines in the firmware console it sends with every history download
// (about one per ten minutes). It is as fresh as the last sync, not live.
type Battery struct {
	Have      bool      `json:"have"`
	SOC       float64   `json:"soc,omitempty"`            // percent at At
	At        time.Time `json:"at,omitempty"`             // when the strap logged it
	Points    int       `json:"points"`                   // readings found
	PerHour   float64   `json:"drain_per_hour,omitempty"` // percentage points per hour, over the last few hours
	HoursLeft float64   `json:"hours_left,omitempty"`     // from At, at that rate; an estimate
	Charging  bool      `json:"charging,omitempty"`       // the level was rising
	Note      string    `json:"note"`
}

const (
	batteryWindow  = 6 * time.Hour // how far back the drain rate looks
	batteryMinSpan = time.Hour     // less than this and a rate means nothing
	batteryNote    = "From the strap's own log, so it is only as fresh as the last history sync. Hours left is an estimate at the recent drain rate."
)

// ReadBattery reads the battery level and recent drain from the stored console
// logs.
func ReadBattery(db *strapdb.DB) (Battery, error) {
	var bodies [][]byte
	if err := db.RawBodies(func(b []byte) bool {
		if len(b) > 0 && b[0] == whoopble.TypeConsoleLog {
			bodies = append(bodies, append([]byte(nil), b...))
		}
		return true
	}); err != nil {
		return Battery{Note: batteryNote}, err
	}
	type pt struct {
		t   time.Time
		soc float64
	}
	var pts []pt
	for _, l := range whoopble.ConsoleLines(bodies) {
		if soc, ok := whoopble.BatterySOC(l.Text); ok && l.Time.Year() >= 2020 { // a log on the unset clock has no real time
			pts = append(pts, pt{l.Time, soc})
		}
	}
	out := Battery{Points: len(pts), Note: batteryNote}
	if len(pts) == 0 {
		return out, nil
	}
	// Lines can repeat when a burst is resent, and arrive in packet order: take the newest by time.
	last := pts[0]
	for _, p := range pts {
		if p.t.After(last.t) {
			last = p
		}
	}
	out.Have, out.SOC, out.At = true, last.soc, last.t

	// Least-squares slope of the level over the last few hours.
	var xs, ys []float64
	var first time.Time
	for _, p := range pts {
		if last.t.Sub(p.t) > batteryWindow {
			continue
		}
		if first.IsZero() || p.t.Before(first) {
			first = p.t
		}
		xs, ys = append(xs, p.t.Sub(last.t).Hours()), append(ys, p.soc)
	}
	if len(xs) < 3 || last.t.Sub(first) < batteryMinSpan {
		return out, nil
	}
	var sx, sy, sxx, sxy float64
	n := float64(len(xs))
	for i := range xs {
		sx, sy, sxx, sxy = sx+xs[i], sy+ys[i], sxx+xs[i]*xs[i], sxy+xs[i]*ys[i]
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return out, nil
	}
	slope := (n*sxy - sx*sy) / den // percentage points per hour
	switch {
	case slope > 0.05:
		out.Charging = true
	case slope < -0.05:
		out.PerHour = -slope
		out.HoursLeft = out.SOC / out.PerHour
	}
	return out, nil
}
