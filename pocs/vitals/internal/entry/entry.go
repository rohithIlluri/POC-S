// Package entry builds validated log entries from user input. The CLI and
// the web form both go through New, so they accept exactly the same things.
package entry

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/metrics"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
)

// MaxTextLen bounds labels and notes.
const MaxTextLen = 200

// New validates raw input for one entry. label is required for event
// metrics (symptom, med) and ignored otherwise; an empty rawValue means the
// event's default. The returned entry has a fresh ID.
func New(metric, label, rawValue, note string, at time.Time) (store.Entry, metrics.Metric, error) {
	m, ok := metrics.Lookup(metric)
	if !ok {
		return store.Entry{}, m, fmt.Errorf("unknown metric %q (see `vitals metrics`)", metric)
	}
	e := store.Entry{ID: store.NewID(), Time: at, Metric: m.Name}
	var err error
	rawValue = strings.TrimSpace(rawValue)
	if m.Kind == metrics.Event {
		if e.Label, err = CleanText("name", label); err != nil {
			return e, m, err
		}
		if e.Label == "" {
			return e, m, fmt.Errorf("%s needs a name, e.g. headache or ibuprofen", m.Name)
		}
		e.Value = m.Default
	} else if rawValue == "" {
		return e, m, fmt.Errorf("%s needs a value", m.Name)
	}
	if rawValue != "" {
		if e.Value, err = strconv.ParseFloat(rawValue, 64); err != nil {
			return e, m, fmt.Errorf("%s: %q is not a number", m.Name, rawValue)
		}
	}
	if err := m.Validate(e.Value); err != nil {
		return e, m, err
	}
	if e.Note, err = CleanText("note", note); err != nil {
		return e, m, err
	}
	return e, m, nil
}

// CleanText rejects control characters (they would garble a terminal on
// display) and over-long input.
func CleanText(field, s string) (string, error) {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > MaxTextLen {
		return "", fmt.Errorf("%s is longer than %d characters", field, MaxTextLen)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", errors.New(field + " contains control characters")
		}
	}
	return s, nil
}

// Describe renders an entry's metric and value in one phrase
// ("sleep 7.5 h", "symptom headache (4/5)", "med ibuprofen ×2").
func Describe(m metrics.Metric, e store.Entry) string {
	switch {
	case m.Name == "symptom":
		return fmt.Sprintf("symptom %s (%s)", e.Label, m.FormatWithUnit(e.Value))
	case m.Kind == metrics.Event:
		if e.Value == 1 {
			return fmt.Sprintf("%s %s", m.Name, e.Label)
		}
		return fmt.Sprintf("%s %s ×%s", m.Name, e.Label, m.Format(e.Value))
	default:
		return fmt.Sprintf("%s %s", m.Name, m.FormatWithUnit(e.Value))
	}
}

// Value renders just the value part of Describe.
func Value(m metrics.Metric, e store.Entry) string {
	return strings.TrimPrefix(Describe(m, e), m.Name+" ")
}
