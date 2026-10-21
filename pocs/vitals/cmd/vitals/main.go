// Command vitals is a private, local-first personal health tracker for the
// terminal: log sleep, steps, mood, symptoms, meds and more in one line, then
// see trends and plain-language insights. Data never leaves the machine.
//
//	vitals log sleep 7.5            log a measurement
//	vitals log symptom headache 4   log a labelled event (severity 1-5)
//	vitals today                    what you've logged today
//	vitals trends                   30-day summary with sparklines
//	vitals insights                 explainable observations from your data
//	vitals help                     everything else
package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/insights"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/metrics"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/stats"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "0.1.0"

const (
	maxTextLen = 200
	disclaimer = "vitals is a personal log, not medical advice. For health concerns, talk to a clinician."
)

type app struct {
	out, err io.Writer
	home     string
	now      func() time.Time
	loc      *time.Location
}

func main() {
	home, err := homeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vitals:", err)
		os.Exit(1)
	}
	a := &app{out: os.Stdout, err: os.Stderr, home: home, now: time.Now, loc: time.Local}
	os.Exit(a.run(os.Args[1:]))
}

// homeDir is $VITALS_HOME, or ~/.vitals.
func homeDir() (string, error) {
	if h := os.Getenv("VITALS_HOME"); h != "" {
		return h, nil
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find home directory (set VITALS_HOME): %w", err)
	}
	return filepath.Join(u, ".vitals"), nil
}

func (a *app) run(args []string) int {
	if len(args) == 0 {
		args = []string{"today"}
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "log", "add":
		err = a.cmdLog(rest)
	case "today":
		err = a.cmdToday(rest)
	case "list", "ls":
		err = a.cmdList(rest)
	case "trends":
		err = a.cmdTrends(rest)
	case "insights":
		err = a.cmdInsights(rest)
	case "goal", "goals":
		err = a.cmdGoal(rest)
	case "metrics":
		a.cmdMetrics()
	case "export":
		err = a.cmdExport(rest)
	case "undo":
		err = a.cmdUndo()
	case "delete", "rm":
		err = a.cmdDelete(rest)
	case "version", "--version", "-v":
		fmt.Fprintln(a.out, "vitals", version)
	case "help", "--help", "-h":
		a.usage(a.out)
	default:
		fmt.Fprintf(a.err, "vitals: unknown command %q\n\n", cmd)
		a.usage(a.err)
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(a.err, "vitals:", err)
		return 1
	}
	return 0
}

func (a *app) usage(w io.Writer) {
	fmt.Fprint(w, `vitals — a private, local-first health tracker

Usage:
  vitals log <metric> <value>          log a measurement      e.g. vitals log sleep 7.5
  vitals log symptom <name> [1-5]      log a symptom          e.g. vitals log symptom headache 4
  vitals log med <name> [doses]        log a medication       e.g. vitals log med vitamin-d
      --note "text"                    attach a note
      --at <when>                      backdate: yesterday | 07:30 | 2026-10-20 | 2026-10-20T07:30
  vitals today                         today's entries (default command)
  vitals list [--days 7] [--metric m]  recent entries with their ids
  vitals trends [--days 30]            per-metric summary with sparklines
  vitals insights                      observations from your own data
  vitals goal                          show goals
  vitals goal <metric> <target>        set a daily goal       e.g. vitals goal steps 8000, vitals goal weight <=75
  vitals goal <metric> --clear         remove a goal
  vitals metrics                       what you can track
  vitals export [--format csv|json]    dump everything to stdout
  vitals undo                          remove the most recent entry
  vitals delete <id>                   remove a specific entry
  vitals version

Data lives in $VITALS_HOME (default ~/.vitals) and never leaves this machine.
`+disclaimer+"\n")
}

// parseInterleaved lets flags appear anywhere among positionals
// (`vitals log sleep 7 --note x` and `vitals log --note x sleep 7`).
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func (a *app) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("vitals "+name, flag.ContinueOnError)
	fs.SetOutput(a.err)
	return fs
}

func (a *app) open() (*store.Store, []store.Entry, error) {
	s, err := store.Open(a.home)
	if err != nil {
		return nil, nil, err
	}
	entries, skipped, err := s.Load()
	if err != nil {
		return nil, nil, err
	}
	if skipped > 0 {
		fmt.Fprintf(a.err, "vitals: warning: skipped %d unreadable line(s) in %s\n", skipped, s.Path())
	}
	return s, entries, nil
}

// cleanText rejects control characters (they would garble the terminal on
// display) and over-long input.
func cleanText(field, s string) (string, error) {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > maxTextLen {
		return "", fmt.Errorf("%s is longer than %d characters", field, maxTextLen)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%s contains control characters", field)
		}
	}
	return s, nil
}

// parseWhen understands the --at forms listed in usage().
func parseWhen(s string, now time.Time, loc *time.Location) (time.Time, error) {
	now = now.In(loc)
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "yesterday") {
		return now.AddDate(0, 0, -1), nil
	}
	if t, err := time.ParseInLocation("15:04", s, loc); err == nil {
		return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, loc), nil
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	if t, err := time.ParseInLocation(stats.DayLayout, s, loc); err == nil {
		return t.Add(12 * time.Hour), nil
	}
	return time.Time{}, fmt.Errorf("can't read --at %q (try yesterday, 07:30, 2026-10-20 or 2026-10-20T07:30)", s)
}

func (a *app) cmdLog(args []string) error {
	fs := a.flags("log")
	note := fs.String("note", "", "attach a note")
	when := fs.String("at", "", "when it happened")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return errors.New("usage: vitals log <metric> <value>  (see `vitals metrics`)")
	}
	m, ok := metrics.Lookup(pos[0])
	if !ok {
		return fmt.Errorf("unknown metric %q (see `vitals metrics`)", pos[0])
	}

	e := store.Entry{ID: store.NewID(), Time: a.now().In(a.loc), Metric: m.Name}
	var raw string
	if m.Kind == metrics.Event {
		if len(pos) < 2 || len(pos) > 3 {
			return fmt.Errorf("usage: vitals log %s <name> [value]", m.Name)
		}
		if e.Label, err = cleanText("name", pos[1]); err != nil {
			return err
		}
		if e.Label == "" {
			return errors.New("name must not be empty")
		}
		e.Value = m.Default
		if len(pos) == 3 {
			raw = pos[2]
		}
	} else {
		if len(pos) != 2 {
			return fmt.Errorf("usage: vitals log %s <value>", m.Name)
		}
		raw = pos[1]
	}
	if raw != "" {
		if e.Value, err = strconv.ParseFloat(raw, 64); err != nil {
			return fmt.Errorf("%s: %q is not a number", m.Name, raw)
		}
	}
	if err := m.Validate(e.Value); err != nil {
		return err
	}
	if e.Note, err = cleanText("note", *note); err != nil {
		return err
	}
	if *when != "" {
		t, err := parseWhen(*when, a.now(), a.loc)
		if err != nil {
			return err
		}
		if t.After(a.now().Add(time.Minute)) {
			return errors.New("--at is in the future")
		}
		e.Time = t
	}

	s, err := store.Open(a.home)
	if err != nil {
		return err
	}
	if err := s.Append(e); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "logged %s  (%s, id %s)\n", describe(m, e), e.Time.In(a.loc).Format("Mon 2 Jan 15:04"), e.ID)
	return nil
}

// describe renders an entry's metric and value in one phrase.
func describe(m metrics.Metric, e store.Entry) string {
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

func (a *app) printEntries(entries []store.Entry, withDate bool) {
	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	for _, e := range entries {
		m, ok := metrics.Lookup(e.Metric)
		if !ok {
			continue
		}
		layout := "15:04"
		if withDate {
			layout = "Mon 02 Jan 15:04"
		}
		value := m.FormatWithUnit(e.Value)
		if m.Kind == metrics.Event {
			value = strings.TrimPrefix(describe(m, e), m.Name+" ")
		}
		note := ""
		if e.Note != "" {
			note = strconv.Quote(e.Note)
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", e.Time.In(a.loc).Format(layout), m.Name, value, note, e.ID)
	}
	tw.Flush()
}

func (a *app) cmdToday(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: vitals today")
	}
	_, entries, err := a.open()
	if err != nil {
		return err
	}
	now := a.now()
	today := stats.DayKey(now, a.loc)
	var todays []store.Entry
	for _, e := range entries {
		if stats.DayKey(e.Time, a.loc) == today {
			todays = append(todays, e)
		}
	}
	fmt.Fprintf(a.out, "Today · %s\n", now.In(a.loc).Format("Mon 2 Jan 2006"))
	if len(todays) == 0 {
		fmt.Fprintln(a.out, "  nothing logged yet — try `vitals log sleep 7.5` or `vitals log mood 4`")
	} else {
		a.printEntries(todays, false)
	}
	if n := stats.Streak(entries, now, a.loc); n >= 2 {
		fmt.Fprintf(a.out, "\n%d-day streak\n", n)
	}
	return nil
}

func (a *app) cmdList(args []string) error {
	fs := a.flags("list")
	days := fs.Int("days", 7, "how many days back")
	only := fs.String("metric", "", "only this metric")
	if _, err := parseInterleaved(fs, args); err != nil {
		return err
	}
	if *days < 1 {
		return errors.New("--days must be at least 1")
	}
	var filter string
	if *only != "" {
		m, ok := metrics.Lookup(*only)
		if !ok {
			return fmt.Errorf("unknown metric %q", *only)
		}
		filter = m.Name
	}
	_, entries, err := a.open()
	if err != nil {
		return err
	}
	inWindow := map[string]bool{}
	for _, k := range stats.Window(a.now(), *days, a.loc) {
		inWindow[k] = true
	}
	var shown []store.Entry
	for _, e := range entries {
		if inWindow[stats.DayKey(e.Time, a.loc)] && (filter == "" || e.Metric == filter) {
			shown = append(shown, e)
		}
	}
	if len(shown) == 0 {
		fmt.Fprintf(a.out, "no entries in the last %d day(s)\n", *days)
		return nil
	}
	a.printEntries(shown, true)
	return nil
}

func (a *app) cmdTrends(args []string) error {
	fs := a.flags("trends")
	days := fs.Int("days", 30, "window size in days")
	if _, err := parseInterleaved(fs, args); err != nil {
		return err
	}
	if *days < 2 || *days > 366 {
		return errors.New("--days must be between 2 and 366")
	}
	_, entries, err := a.open()
	if err != nil {
		return err
	}
	now := a.now()
	keys := stats.Window(now, *days, a.loc)
	fmt.Fprintf(a.out, "Trends · last %d days (%s → %s)\n\n", *days, keys[0], keys[len(keys)-1])

	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  metric\tdays\tavg\tmin\tmax\tweek vs prior\ttrend")
	shown := false
	var events []metrics.Metric
	for _, m := range metrics.All() {
		if m.Kind == metrics.Event {
			events = append(events, m)
			continue
		}
		s, ok := stats.Summarize(entries, m, now, *days, a.loc)
		if !ok {
			continue
		}
		shown = true
		delta := "—"
		if s.HasDelta {
			delta = arrow(s.Recent-s.Prior, m) + " " + signed(m, s.Recent-s.Prior)
		}
		fmt.Fprintf(tw, "  %s\t%d\t%s\t%s\t%s\t%s\t%s\n", m.Name, s.Days,
			m.FormatStatWithUnit(s.Mean), m.Format(s.Min), m.Format(s.Max), delta, s.Spark)
	}
	if shown {
		tw.Flush()
	}
	for _, m := range events {
		counts := stats.EventCounts(entries, m, now, *days, a.loc)
		if len(counts) == 0 {
			continue
		}
		shown = true
		parts := make([]string, len(counts))
		for i, c := range counts {
			parts[i] = fmt.Sprintf("%s ×%d", c.Label, c.Count)
		}
		fmt.Fprintf(a.out, "\n  %s: %s\n", m.Name, strings.Join(parts, ", "))
	}
	if !shown {
		fmt.Fprintln(a.out, "  no data in this window yet")
	}
	return nil
}

// negligible reports whether a change rounds to zero at display precision.
func negligible(m metrics.Metric, d float64) bool {
	s := strings.TrimPrefix(m.FormatStat(d), "-")
	return s == "0"
}

func signed(m metrics.Metric, d float64) string {
	switch {
	case negligible(m, d):
		return "0"
	case d > 0:
		return "+" + m.FormatStat(d)
	default:
		return "-" + m.FormatStat(-d)
	}
}

// arrow shows direction, and whether the move is good (▲/▼) or bad (△/▽)
// for metrics that have a preferred direction.
func arrow(d float64, m metrics.Metric) string {
	switch {
	case negligible(m, d):
		return "="
	case d > 0 && m.Better == metrics.Lower, d < 0 && m.Better == metrics.Higher:
		if d > 0 {
			return "△"
		}
		return "▽"
	case d > 0:
		return "▲"
	default:
		return "▼"
	}
}

func (a *app) cmdInsights(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: vitals insights")
	}
	_, entries, err := a.open()
	if err != nil {
		return err
	}
	cfg, err := config.Load(a.home)
	if err != nil {
		return err
	}
	ins := insights.Generate(entries, cfg, a.now(), a.loc)
	fmt.Fprintln(a.out, "Insights")
	if len(ins) == 0 {
		fmt.Fprintln(a.out, "  not enough data yet — log a few days of sleep and mood and check back.")
	}
	marks := map[insights.Level]string{insights.Watch: "!", insights.Info: "·", insights.Good: "✓"}
	for _, i := range ins {
		fmt.Fprintf(a.out, "  %s %s\n", marks[i.Level], i.Text)
	}
	fmt.Fprintln(a.out, "\n"+disclaimer)
	return nil
}

func (a *app) cmdGoal(args []string) error {
	fs := a.flags("goal")
	clearGoal := fs.Bool("clear", false, "remove the goal")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	cfg, err := config.Load(a.home)
	if err != nil {
		return err
	}
	if len(pos) == 0 && !*clearGoal {
		if len(cfg.Goals) == 0 {
			fmt.Fprintln(a.out, "no goals set — try `vitals goal steps 8000` or `vitals goal sleep 7.5`")
			return nil
		}
		for _, m := range metrics.All() {
			if g, ok := cfg.Goals[m.Name]; ok {
				fmt.Fprintf(a.out, "  %-10s %s %s\n", m.Name, g.Op, m.FormatWithUnit(g.Target))
			}
		}
		return nil
	}
	if len(pos) == 0 {
		return errors.New("usage: vitals goal <metric> --clear")
	}
	m, ok := metrics.Lookup(pos[0])
	if !ok {
		return fmt.Errorf("unknown metric %q", pos[0])
	}
	if m.Kind == metrics.Event {
		return fmt.Errorf("%s is an event; goals apply to daily measurements", m.Name)
	}
	if *clearGoal {
		if len(pos) != 1 {
			return errors.New("usage: vitals goal <metric> --clear")
		}
		delete(cfg.Goals, m.Name)
		if err := config.Save(a.home, cfg); err != nil {
			return err
		}
		fmt.Fprintf(a.out, "cleared %s goal\n", m.Name)
		return nil
	}
	if len(pos) != 2 {
		return errors.New("usage: vitals goal <metric> <target>")
	}
	defaultOp := map[metrics.Direction]string{metrics.Higher: ">=", metrics.Lower: "<="}[m.Better]
	g, err := config.ParseGoal(pos[1], defaultOp)
	if err != nil {
		return fmt.Errorf("%s: %w", m.Name, err)
	}
	if err := m.Validate(g.Target); err != nil {
		return err
	}
	if cfg.Goals == nil {
		cfg.Goals = map[string]config.Goal{}
	}
	cfg.Goals[m.Name] = g
	if err := config.Save(a.home, cfg); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "goal set: %s %s %s per day\n", m.Name, g.Op, m.FormatWithUnit(g.Target))
	return nil
}

func (a *app) cmdMetrics() {
	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  metric\tunit\tdescription")
	for _, m := range metrics.All() {
		name := m.Name
		if len(m.Aliases) > 0 {
			name += " (" + strings.Join(m.Aliases, ", ") + ")"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", name, m.Unit, m.Desc)
	}
	tw.Flush()
}

func (a *app) cmdExport(args []string) error {
	fs := a.flags("export")
	format := fs.String("format", "csv", "csv or json")
	if _, err := parseInterleaved(fs, args); err != nil {
		return err
	}
	_, entries, err := a.open()
	if err != nil {
		return err
	}
	switch *format {
	case "json":
		if entries == nil {
			entries = []store.Entry{}
		}
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	case "csv":
		w := csv.NewWriter(a.out)
		_ = w.Write([]string{"id", "time", "metric", "value", "unit", "label", "note"})
		for _, e := range entries {
			unit := ""
			if m, ok := metrics.Lookup(e.Metric); ok {
				unit = m.Unit
			}
			_ = w.Write([]string{e.ID, e.Time.Format(time.RFC3339), e.Metric,
				strconv.FormatFloat(e.Value, 'f', -1, 64), unit, csvSafe(e.Label), csvSafe(e.Note)})
		}
		w.Flush()
		return w.Error()
	default:
		return fmt.Errorf("unknown --format %q (csv or json)", *format)
	}
}

// csvSafe defuses spreadsheet formula injection: a cell starting with one of
// these characters is executed as a formula by Excel/Sheets.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@", rune(s[0])) {
		return "'" + s
	}
	return s
}

func (a *app) cmdUndo() error {
	s, err := store.Open(a.home)
	if err != nil {
		return err
	}
	// Most recently written, not latest timestamp, so undoing a backdated
	// entry removes that entry.
	last, err := s.LastWritten()
	if errors.Is(err, store.ErrNotFound) {
		return errors.New("nothing to undo")
	}
	if err != nil {
		return err
	}
	return a.remove(s, last.ID)
}

func (a *app) cmdDelete(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: vitals delete <id>  (ids are shown by `vitals list`)")
	}
	s, err := store.Open(a.home)
	if err != nil {
		return err
	}
	return a.remove(s, args[0])
}

func (a *app) remove(s *store.Store, id string) error {
	e, err := s.Delete(id)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("no entry with id %q", id)
	}
	if err != nil {
		return err
	}
	desc := e.Metric
	if m, ok := metrics.Lookup(e.Metric); ok {
		desc = describe(m, e)
	}
	fmt.Fprintf(a.out, "removed %s  (%s)\n", desc, e.Time.In(a.loc).Format("Mon 2 Jan 15:04"))
	return nil
}
