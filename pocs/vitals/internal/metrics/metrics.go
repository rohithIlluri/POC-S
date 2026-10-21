// Package metrics is the registry of things vitals can track: their units,
// valid ranges, how a day's entries roll up, and which direction is "better".
package metrics

import (
	"fmt"
	"math"
	"strings"
)

// Agg is how multiple entries on the same day combine into one daily value.
type Agg int

const (
	Sum  Agg = iota // steps, water, sleep (naps add up)
	Mean            // mood, energy, heart rate
	Last            // weight: the latest reading of the day wins
)

// Kind separates numeric measurements from labelled events.
type Kind int

const (
	Numeric Kind = iota // `vitals log sleep 7.5`
	Event               // `vitals log symptom headache 4` — needs a label
)

// Direction says which way a metric should move to count as an improvement.
type Direction int

const (
	Neutral Direction = iota
	Higher
	Lower
)

// Metric describes one trackable quantity.
type Metric struct {
	Name    string
	Unit    string
	Desc    string
	Kind    Kind
	Agg     Agg
	Min     float64
	Max     float64
	Default float64 // value used for an Event logged without one
	Integer bool
	Better  Direction
	Aliases []string
}

var registry = []Metric{
	{Name: "sleep", Unit: "h", Desc: "hours slept", Agg: Sum, Min: 0, Max: 24, Better: Higher},
	{Name: "steps", Unit: "steps", Desc: "step count", Agg: Sum, Min: 0, Max: 200000, Integer: true, Better: Higher},
	{Name: "exercise", Unit: "min", Desc: "minutes of exercise", Agg: Sum, Min: 0, Max: 1440, Integer: true, Better: Higher, Aliases: []string{"workout"}},
	{Name: "water", Unit: "L", Desc: "water intake in litres", Agg: Sum, Min: 0, Max: 20, Better: Higher},
	{Name: "mood", Unit: "/5", Desc: "mood, 1 (low) to 5 (great)", Agg: Mean, Min: 1, Max: 5, Integer: true, Better: Higher},
	{Name: "energy", Unit: "/5", Desc: "energy, 1 (drained) to 5 (energised)", Agg: Mean, Min: 1, Max: 5, Integer: true, Better: Higher},
	{Name: "heartrate", Unit: "bpm", Desc: "resting heart rate", Agg: Mean, Min: 20, Max: 250, Integer: true, Better: Lower, Aliases: []string{"hr", "rhr"}},
	{Name: "weight", Unit: "kg", Desc: "body weight", Agg: Last, Min: 1, Max: 500, Better: Neutral},
	{Name: "symptom", Unit: "/5", Desc: "a symptom with severity 1-5 (default 3)", Kind: Event, Agg: Mean, Min: 1, Max: 5, Default: 3, Integer: true, Better: Lower},
	{Name: "med", Unit: "dose", Desc: "a medication or supplement taken (default 1 dose)", Kind: Event, Agg: Sum, Min: 0, Max: 100, Default: 1, Better: Neutral, Aliases: []string{"meds", "medication"}},
}

// All returns every registered metric in display order.
func All() []Metric {
	out := make([]Metric, len(registry))
	copy(out, registry)
	return out
}

// Lookup finds a metric by name or alias, case-insensitively.
func Lookup(name string) (Metric, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, m := range registry {
		if m.Name == name {
			return m, true
		}
		for _, a := range m.Aliases {
			if a == name {
				return m, true
			}
		}
	}
	return Metric{}, false
}

// Validate checks v is a finite value inside the metric's range.
func (m Metric) Validate(v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("%s: value must be a finite number", m.Name)
	}
	if v < m.Min || v > m.Max {
		return fmt.Errorf("%s: %s is out of range (%s–%s %s)", m.Name, m.Format(v), m.Format(m.Min), m.Format(m.Max), m.Unit)
	}
	if m.Integer && v != math.Trunc(v) {
		return fmt.Errorf("%s: value must be a whole number", m.Name)
	}
	return nil
}

// Format renders a bare value with sensible precision for the metric.
func (m Metric) Format(v float64) string {
	if m.Integer || v == math.Trunc(v) {
		return fmt.Sprintf("%.0f", v)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}

// FormatStat renders a derived value (an average or a difference), which
// needs a decimal even for whole-number metrics: a mood average of 3.8 must
// not print as 4. Large magnitudes like step counts stay whole.
func (m Metric) FormatStat(v float64) string {
	if math.Abs(v) >= 100 {
		return fmt.Sprintf("%.0f", v)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0")
}

// FormatWithUnit renders a value plus unit ("7.5 h", "4/5", "8000 steps").
func (m Metric) FormatWithUnit(v float64) string {
	return m.withUnit(m.Format(v))
}

// FormatStatWithUnit is FormatStat plus unit.
func (m Metric) FormatStatWithUnit(v float64) string {
	return m.withUnit(m.FormatStat(v))
}

func (m Metric) withUnit(s string) string {
	if strings.HasPrefix(m.Unit, "/") {
		return s + m.Unit
	}
	return s + " " + m.Unit
}
