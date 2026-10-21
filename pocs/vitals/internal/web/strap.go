package web

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/hrv"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/metrics"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
)

// StrapSource tags entries recorded live from a Bluetooth heart-rate strap.
const StrapSource = "strap"

const (
	maxSamples = 50000         // ~14 h of 1 Hz bpm, or plenty of RR
	maxSession = 6 * time.Hour // longest workout we'll record
	minWorkout = 60 * time.Second
)

// strapSave records a session captured in the browser over Web Bluetooth.
// The page sends raw samples; all the maths happens here, in tested Go.
//
//	kind=resting  → heartrate (+ hrv when the strap sent enough RR intervals)
//	kind=workout  → exercise minutes, noted with avg / max bpm
func (s *Server) strapSave(w http.ResponseWriter, r *http.Request) {
	entries, err := s.strapEntries(r)
	if err == nil {
		var st *store.Store
		if st, err = store.Open(s.Home); err == nil {
			_, _, err = st.Upsert(entries)
		}
	}
	if err != nil {
		s.addFlash("watch", "Strap session not saved: %v", err)
	} else {
		var parts []string
		for _, e := range entries {
			m, _ := metrics.Lookup(e.Metric)
			parts = append(parts, m.Name+" "+m.FormatWithUnit(e.Value))
		}
		s.addFlash("good", "Saved from your strap: %s.", strings.Join(parts, ", "))
	}
	back(w, r)
}

func (s *Server) strapEntries(r *http.Request) ([]store.Entry, error) {
	hr, err := parseSamples(r.PostForm.Get("hr"))
	if err != nil {
		return nil, fmt.Errorf("heart-rate samples: %w", err)
	}
	rr, err := parseSamples(r.PostForm.Get("rr"))
	if err != nil {
		return nil, fmt.Errorf("RR intervals: %w", err)
	}
	secs, err := strconv.ParseFloat(r.PostForm.Get("seconds"), 64)
	if err != nil || secs <= 0 || math.IsInf(secs, 0) || time.Duration(secs*float64(time.Second)) > maxSession {
		return nil, errors.New("session length missing or out of range")
	}
	duration := time.Duration(secs * float64(time.Second))
	now := s.Now().In(s.Loc)
	newEntry := func(metric string, v float64, at time.Time, note string) (store.Entry, error) {
		m, _ := metrics.Lookup(metric)
		if m.Integer {
			v = math.Round(v)
		} else {
			v = math.Round(v*10) / 10
		}
		if err := m.Validate(v); err != nil {
			return store.Entry{}, err
		}
		return store.Entry{ID: store.NewID(), Time: at, Metric: m.Name, Value: v, Note: note, Source: StrapSource}, nil
	}

	switch r.PostForm.Get("kind") {
	case "resting":
		bpm, err := hrv.RestingHR(hr, rr)
		if err != nil {
			return nil, err
		}
		note := fmt.Sprintf("resting check, %s", roundDuration(duration))
		e, err := newEntry("heartrate", bpm, now, note)
		if err != nil {
			return nil, err
		}
		out := []store.Entry{e}
		if rmssd, ok := hrv.RMSSD(rr); ok {
			if e, err := newEntry("hrv", rmssd, now, note); err == nil {
				out = append(out, e)
			}
		}
		return out, nil
	case "workout":
		if duration < minWorkout {
			return nil, errors.New("workouts shorter than a minute aren't saved")
		}
		sum, err := hrv.Summarize(hr)
		if err != nil {
			return nil, err
		}
		note := fmt.Sprintf("avg %.0f bpm, max %.0f bpm", sum.Avg, sum.Max)
		if label := strings.TrimSpace(r.PostForm.Get("label")); label != "" && len(label) <= 40 && !strings.ContainsAny(label, "\x00\r\n\t") {
			note = label + " · " + note
		}
		e, err := newEntry("exercise", duration.Minutes(), now.Add(-duration), note)
		if err != nil {
			return nil, err
		}
		return []store.Entry{e}, nil
	default:
		return nil, errors.New("unknown session kind")
	}
}

// parseSamples reads a comma-separated list of numbers.
func parseSamples(s string) ([]float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	fields := strings.Split(s, ",")
	if len(fields) > maxSamples {
		return nil, fmt.Errorf("more than %d samples", maxSamples)
	}
	out := make([]float64, 0, len(fields))
	for _, f := range fields {
		v, err := strconv.ParseFloat(strings.TrimSpace(f), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("%q is not a number", f)
		}
		out = append(out, v)
	}
	return out, nil
}

func roundDuration(d time.Duration) string {
	if d < 90*time.Second {
		return fmt.Sprintf("%.0f s", d.Seconds())
	}
	return fmt.Sprintf("%.0f min", d.Minutes())
}
