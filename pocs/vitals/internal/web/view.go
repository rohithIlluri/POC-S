package web

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/entry"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/insights"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/metrics"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/stats"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoop"
)

// Disclaimer is shown under the insights.
const Disclaimer = "vitals is a personal log, not medical advice. For health concerns, talk to a clinician."

type view struct {
	Today      string
	Days       int
	DayOptions []int
	CSRF       string
	Flash      []flash
	Whoop      whoopView
	Tiles      []tile
	Insights   []insightView
	Charts     []chart
	Events     []eventView
	Recent     []entryView
	Metrics    []metricOption
	Disclaimer string
	Empty      bool
}

type whoopView struct {
	Configured bool
	Connected  bool
	Member     string
	LastSync   string
	Redirect   string
}

type tile struct {
	Label  string
	Value  string
	When   string
	Status string // "", "good", "warning", "critical"
	Badge  string // text label that always accompanies Status
}

type insightView struct {
	Level string
	Icon  string
	Label string
	Text  string
}

type eventView struct {
	Metric string
	Items  []string
}

type entryView struct {
	ID     string
	When   string
	Metric string
	Value  string
	Note   string
	Source string
}

type metricOption struct {
	Name  string
	Desc  string
	Event bool
}

// Chart geometry, in SVG user units (the SVG scales to its container).
const (
	chartW   = 320.0
	chartH   = 96.0
	padTop   = 8.0
	padBot   = 8.0
	padLeft  = 4.0
	padRight = 4.0
)

type chart struct {
	Name     string
	Desc     string
	Avg      string
	Range    string
	Delta    string
	DeltaDir string // "good" | "bad" | "flat" | "" — always paired with an arrow glyph
	Path     string
	Points   []point
	Single   bool // only one point: draw a marker instead of a line
	AvgY     float64
	First    string
	Last     string
	W, H     float64
}

type point struct {
	X, Y float64
	Tip  string
}

// keyTiles are the headline numbers, in order.
var keyTiles = []string{"recovery", "strain", "sleep", "hrv", "heartrate", "steps", "mood"}

func buildView(entries []store.Entry, cfg config.Config, creds whoop.Credentials, now time.Time, loc *time.Location, days int) view {
	v := view{
		Today:      now.In(loc).Format("Monday 2 January 2006"),
		Days:       days,
		DayOptions: []int{7, 30, 90},
		Disclaimer: Disclaimer,
		Empty:      len(entries) == 0,
		Whoop: whoopView{
			Configured: creds.Configured(),
			Connected:  creds.Connected(),
			Member:     creds.MemberName,
			Redirect:   creds.RedirectURL,
		},
	}
	if !creds.LastSync.IsZero() {
		v.Whoop.LastSync = ago(now.Sub(creds.LastSync))
	}

	for _, name := range keyTiles {
		if t, ok := latestTile(entries, name, now, loc); ok {
			v.Tiles = append(v.Tiles, t)
		}
	}

	icons := map[insights.Level][2]string{
		insights.Watch: {"!", "Watch"},
		insights.Info:  {"i", "Note"},
		insights.Good:  {"✓", "Good"},
	}
	for _, i := range insights.Generate(entries, cfg, now, loc) {
		ic := icons[i.Level]
		v.Insights = append(v.Insights, insightView{Level: i.Level.String(), Icon: ic[0], Label: ic[1], Text: i.Text})
	}

	keys := stats.Window(now, days, loc)
	for _, m := range metrics.All() {
		if m.Kind == metrics.Event {
			counts := stats.EventCounts(entries, m, now, days, loc)
			if len(counts) == 0 {
				continue
			}
			ev := eventView{Metric: m.Name}
			for _, c := range counts {
				ev.Items = append(ev.Items, fmt.Sprintf("%s ×%d", c.Label, c.Count))
			}
			v.Events = append(v.Events, ev)
			continue
		}
		if c, ok := buildChart(entries, m, now, keys, loc); ok {
			v.Charts = append(v.Charts, c)
		}
	}

	recentKeys := map[string]bool{}
	for _, k := range stats.Window(now, 7, loc) {
		recentKeys[k] = true
	}
	for i := len(entries) - 1; i >= 0 && len(v.Recent) < 60; i-- {
		e := entries[i]
		m, ok := metrics.Lookup(e.Metric)
		if !ok || !recentKeys[stats.DayKey(e.Time, loc)] {
			continue
		}
		v.Recent = append(v.Recent, entryView{
			ID: e.ID, When: e.Time.In(loc).Format("Mon 2 Jan 15:04"), Metric: m.Name,
			Value: entry.Value(m, e), Note: e.Note, Source: e.Source,
		})
	}

	for _, m := range metrics.All() {
		v.Metrics = append(v.Metrics, metricOption{Name: m.Name, Desc: m.Desc, Event: m.Kind == metrics.Event})
	}
	return v
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

// latestTile shows today's (or else yesterday's) daily value for a metric.
func latestTile(entries []store.Entry, name string, now time.Time, loc *time.Location) (tile, bool) {
	m, ok := metrics.Lookup(name)
	if !ok {
		return tile{}, false
	}
	daily := stats.Daily(entries, m, loc)
	w := stats.Window(now, 2, loc)
	when, key := "today", w[1]
	if _, ok := daily[key]; !ok {
		when, key = "yesterday", w[0]
	}
	val, ok := daily[key]
	if !ok {
		return tile{}, false
	}
	t := tile{Label: m.Name, Value: m.FormatStatWithUnit(val), When: when}
	if m.Name == "recovery" {
		// WHOOP's own zones; the badge text means colour never carries it alone.
		switch {
		case val >= 67:
			t.Status, t.Badge = "good", "Green"
		case val >= 34:
			t.Status, t.Badge = "warning", "Yellow"
		default:
			t.Status, t.Badge = "critical", "Red"
		}
	}
	return t, true
}

func buildChart(entries []store.Entry, m metrics.Metric, now time.Time, keys []string, loc *time.Location) (chart, bool) {
	s, ok := stats.Summarize(entries, m, now, len(keys), loc)
	if !ok {
		return chart{}, false
	}
	vals, has := stats.Series(stats.Daily(entries, m, loc), keys)
	lo, hi := s.Min, s.Max
	if hi == lo {
		lo, hi = lo-1, hi+1
	}
	pad := (hi - lo) * 0.1
	lo, hi = lo-pad, hi+pad
	x := func(i int) float64 {
		if len(keys) == 1 {
			return chartW / 2
		}
		return padLeft + float64(i)*(chartW-padLeft-padRight)/float64(len(keys)-1)
	}
	y := func(v float64) float64 {
		return padTop + (hi-v)/(hi-lo)*(chartH-padTop-padBot)
	}

	c := chart{
		Name: m.Name, Desc: m.Desc, W: chartW, H: chartH,
		Avg:   "avg " + m.FormatStatWithUnit(s.Mean),
		Range: fmt.Sprintf("%s – %s", m.FormatStat(s.Min), m.FormatStatWithUnit(s.Max)),
		AvgY:  round1(y(s.Mean)),
		First: dayLabel(keys[0], loc),
		Last:  dayLabel(keys[len(keys)-1], loc),
	}
	var path strings.Builder
	pen := false
	for i, v := range vals {
		if !has[i] {
			pen = false // a gap in the data is a gap in the line
			continue
		}
		px, py := round1(x(i)), round1(y(v))
		if pen {
			fmt.Fprintf(&path, " L%.1f %.1f", px, py)
		} else {
			fmt.Fprintf(&path, " M%.1f %.1f", px, py)
		}
		pen = true
		c.Points = append(c.Points, point{X: px, Y: py, Tip: dayLabel(keys[i], loc) + " · " + m.FormatStatWithUnit(v)})
	}
	c.Path = strings.TrimSpace(path.String())
	c.Single = len(c.Points) == 1

	if s.HasDelta {
		d := s.Recent - s.Prior
		ds := m.FormatStat(math.Abs(d))
		switch {
		case ds == "0":
			c.Delta, c.DeltaDir = "→ no change vs prior week", "flat"
		default:
			arrow, sign := "↑", "+"
			if d < 0 {
				arrow, sign = "↓", "−"
			}
			c.Delta = fmt.Sprintf("%s %s%s vs prior week", arrow, sign, ds)
			switch {
			case m.Better == metrics.Higher && d > 0, m.Better == metrics.Lower && d < 0:
				c.DeltaDir = "good"
			case m.Better == metrics.Neutral:
				c.DeltaDir = "flat"
			default:
				c.DeltaDir = "bad"
			}
		}
	}
	return c, true
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

func dayLabel(key string, loc *time.Location) string {
	t, err := time.ParseInLocation(stats.DayLayout, key, loc)
	if err != nil {
		return key
	}
	return t.Format("2 Jan")
}
