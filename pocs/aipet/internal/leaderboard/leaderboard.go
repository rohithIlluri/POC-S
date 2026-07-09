// Package leaderboard turns the local event log into rankings and personal
// records the developer can check at a glance — top projects and models by
// spend, best cache-reuse days, activity streaks, and all-time bests. It is a
// pure function of collected events: no network, no model calls, no state.
package leaderboard

import (
	"fmt"
	"sort"
	"time"

	"github.com/enterprise/aipet/internal/store"
)

// minDayTokens is the volume floor for the best-cache-day ranking: a day must
// process at least this many tokens (fresh input + cache reads) to qualify,
// so a 3-turn day with one lucky cache hit can't top the board.
const minDayTokens = 20_000

// Entry is one ranked row: a name, its score, and a human-ready detail.
type Entry struct {
	Name   string  `json:"name"`
	Value  float64 `json:"value"`
	Detail string  `json:"detail,omitempty"`
}

// Records are all-time personal bests derived from the event log.
type Records struct {
	BiggestDayUSD Entry   `json:"biggest_day_usd"` // most spent in one local day
	BusiestDay    Entry   `json:"busiest_day"`     // most turns in one local day
	BestCacheDay  Entry   `json:"best_cache_day"`  // highest reuse ratio (volume-gated)
	FirstSeen     string  `json:"first_seen"`      // YYYY-MM-DD of the earliest event
	ActiveDays    int     `json:"active_days"`     // distinct local days with activity
	CurrentStreak int     `json:"current_streak"`  // consecutive active days ending today/yesterday
	LongestStreak int     `json:"longest_streak"`  // best consecutive-day run ever
	TotalTurns    int     `json:"total_turns"`
	LifetimeSpend float64 `json:"lifetime_spend_usd"`
}

// Board is the full leaderboard, computed from every collected event.
type Board struct {
	TopProjects   []Entry `json:"top_projects"`    // by lifetime USD
	TopModels     []Entry `json:"top_models"`      // by lifetime USD
	BestCacheDays []Entry `json:"best_cache_days"` // by reuse ratio, volume-gated
	Records       Records `json:"records"`
}

// TopN caps every ranking so the boards stay readable.
const TopN = 5

// Compute builds the Board from events. "now" anchors streak math (pass
// time.Now(); it is a parameter so tests are deterministic).
func Compute(events []store.Event, now time.Time) Board {
	var b Board
	if len(events) == 0 {
		return b
	}

	type dayAgg struct {
		cost      float64
		turns     int
		cacheRead int64
		freshIn   int64
	}
	byProject := map[string]float64{}
	byModel := map[string]float64{}
	byDay := map[string]*dayAgg{}

	first := events[0].Timestamp
	for _, e := range events {
		byProject[e.Project] += e.CostUSD
		byModel[e.Model] += e.CostUSD
		day := e.Timestamp.Local().Format("2006-01-02")
		d := byDay[day]
		if d == nil {
			d = &dayAgg{}
			byDay[day] = d
		}
		d.cost += e.CostUSD
		d.turns++
		d.cacheRead += e.CacheRead
		d.freshIn += e.Input
		if e.Timestamp.Before(first) {
			first = e.Timestamp
		}
		b.Records.TotalTurns++
		b.Records.LifetimeSpend += e.CostUSD
	}

	b.TopProjects = rankUSD(byProject)
	b.TopModels = rankUSD(byModel)

	// Cache-reuse ranking and record: ratio of cache reads to all prompt
	// tokens the day processed, gated on volume.
	for day, d := range byDay {
		total := d.cacheRead + d.freshIn
		if total < minDayTokens {
			continue
		}
		ratio := float64(d.cacheRead) / float64(total)
		b.BestCacheDays = append(b.BestCacheDays, Entry{
			Name:   day,
			Value:  ratio * 100,
			Detail: humanTokens(total) + " tokens processed",
		})
	}
	sort.Slice(b.BestCacheDays, func(i, j int) bool {
		if b.BestCacheDays[i].Value != b.BestCacheDays[j].Value {
			return b.BestCacheDays[i].Value > b.BestCacheDays[j].Value
		}
		return b.BestCacheDays[i].Name > b.BestCacheDays[j].Name
	})
	if len(b.BestCacheDays) > TopN {
		b.BestCacheDays = b.BestCacheDays[:TopN]
	}
	if len(b.BestCacheDays) > 0 {
		b.Records.BestCacheDay = b.BestCacheDays[0]
	}

	// Day records.
	for day, d := range byDay {
		if d.cost > b.Records.BiggestDayUSD.Value {
			b.Records.BiggestDayUSD = Entry{Name: day, Value: d.cost}
		}
		if float64(d.turns) > b.Records.BusiestDay.Value {
			b.Records.BusiestDay = Entry{Name: day, Value: float64(d.turns)}
		}
	}

	// Streaks over sorted distinct active days.
	days := make([]string, 0, len(byDay))
	for day := range byDay {
		days = append(days, day)
	}
	sort.Strings(days)
	b.Records.ActiveDays = len(days)
	b.Records.FirstSeen = first.Local().Format("2006-01-02")
	b.Records.LongestStreak, b.Records.CurrentStreak = streaks(days, now)

	return b
}

// rankUSD sorts a cost map descending and returns the top entries. Ties break
// by name so output is deterministic.
func rankUSD(m map[string]float64) []Entry {
	out := make([]Entry, 0, len(m))
	for k, v := range m {
		name := k
		if name == "" {
			name = "(unknown)"
		}
		out = append(out, Entry{Name: name, Value: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Value != out[j].Value {
			return out[i].Value > out[j].Value
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > TopN {
		out = out[:TopN]
	}
	return out
}

// streaks returns the longest run of consecutive days and the current run.
// The current streak counts only if it reaches today or yesterday — a streak
// broken two days ago is history, not "current". days must be sorted,
// distinct, formatted 2006-01-02.
func streaks(days []string, now time.Time) (longest, current int) {
	if len(days) == 0 {
		return 0, 0
	}
	run := 1
	longest = 1
	for i := 1; i < len(days); i++ {
		if consecutive(days[i-1], days[i]) {
			run++
		} else {
			run = 1
		}
		if run > longest {
			longest = run
		}
	}
	// run now holds the length of the trailing streak.
	last := days[len(days)-1]
	today := now.Local().Format("2006-01-02")
	yesterday := now.Local().AddDate(0, 0, -1).Format("2006-01-02")
	if last == today || last == yesterday {
		current = run
	}
	return longest, current
}

// consecutive reports whether day b is the calendar day after a. AddDate is
// used instead of a 24h duration so DST-shifted days still count.
func consecutive(a, b string) bool {
	ta, err := time.ParseInLocation("2006-01-02", a, time.Local)
	if err != nil {
		return false
	}
	return ta.AddDate(0, 0, 1).Format("2006-01-02") == b
}

func humanTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}
