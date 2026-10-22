package science

// How much data each metric needs before it can be shown, and what stops it.
//
// Days are local calendar days with enough samples; nights are days with
// enough overnight heart-rate data (a stand-in until sleep detection exists).
// The thresholds are vitals' own design, informed by the baseline lengths used
// in HRV-guided-training studies (a baseline of about four weeks, a 7-day
// rolling mean). Strain needs no history at all.

// Capabilities say which data paths work today. A metric that needs one that
// is off is reported as blocked, not silently missing.
type Capabilities struct {
	HistorySync bool // stored data can be downloaded from the strap
	Motion      bool // the strap's accelerometer fields are decoded and checked
	Norms       bool // an age/sex fitness reference table is in place
	Profile     bool // age and sex are set
}

// State of a metric.
const (
	StateBlocked     = "blocked"
	StateCalibrating = "calibrating" // not enough days yet to show a number
	StateProvisional = "provisional" // shown, labelled as early
	StateFull        = "full"
)

// Unit says what a requirement counts.
const (
	Days   = "days"
	Nights = "nights"
)

// Status is one metric's readiness.
type Status struct {
	Metric      string `json:"metric"`
	Label       string `json:"label"`
	State       string `json:"state"`
	Unit        string `json:"unit"`
	Have        int    `json:"have"`
	Provisional int    `json:"provisional"` // needed for a provisional number
	Full        int    `json:"full"`        // needed for a full number
	Note        string `json:"note,omitempty"`
}

type spec struct {
	metric, label, unit string
	provisional, full   int
	// needs reports why the metric can't run today ("" when it can). nights is
	// how many nights of overnight data are stored.
	needs func(c Capabilities, nights int) string
}

func always(Capabilities, int) string { return "" }

var specs = []spec{
	{"strain", "Strain", Days, 0, 14, always},
	// These four read the day's own samples, so they work from a live stream.
	{"hrv", "HRV", Days, 1, 14, always},
	{"resting_hr", "Resting heart rate", Days, 1, 14, always},
	{"respiratory_rate", "Respiratory rate", Days, 1, 14, always},
	{"fitness", "Fitness (VO2max est.)", Days, 1, 14, func(c Capabilities, _ int) string {
		if !c.Profile {
			return "needs your date of birth"
		}
		return ""
	}},
	// Recovery compares a night with your own nights, so it needs an overnight recording.
	{"recovery", "Recovery", Nights, MinProvisionalNights, MinFullNights, func(c Capabilities, nights int) string {
		if !c.HistorySync && nights == 0 {
			return "needs an overnight recording: sync the strap's history, or leave it streaming overnight"
		}
		return ""
	}},
	{"sleep", "Sleep", Nights, 1, 14, func(c Capabilities, _ int) string {
		if !c.HistorySync || !c.Motion {
			return "needs history sync and verified motion data"
		}
		return ""
	}},
	{"stress", "Stress", Days, MinProvisionalNights, MinFullNights, func(c Capabilities, _ int) string {
		if !c.HistorySync || !c.Motion {
			return "needs continuous RR and verified motion data"
		}
		return ""
	}},
	{"fitness_age", "Fitness age", Days, 7, 14, func(c Capabilities, _ int) string {
		if !c.Norms {
			return "needs an age/sex reference table that has not been added"
		}
		return ""
	}},
}

// Assess reports every metric's readiness given how many days and nights of
// data are stored and which data paths work.
func Assess(days, nights int, caps Capabilities) []Status {
	out := make([]Status, 0, len(specs))
	for _, s := range specs {
		have := days
		if s.unit == Nights {
			have = nights
		}
		st := Status{Metric: s.metric, Label: s.label, Unit: s.unit, Have: have, Provisional: s.provisional, Full: s.full}
		switch why := s.needs(caps, nights); {
		case why != "":
			st.State, st.Note = StateBlocked, why
		case have >= s.full:
			st.State = StateFull
		case have >= s.provisional && have > 0, s.provisional == 0:
			st.State = StateProvisional
		default:
			st.State = StateCalibrating
		}
		out = append(out, st)
	}
	return out
}
