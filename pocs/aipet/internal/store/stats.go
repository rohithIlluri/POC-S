package store

import (
	"sort"
	"time"
)

// Stats is a rollup of events over a window, used by the TUI and advisor.
type Stats struct {
	TotalCost   float64
	TodayCost   float64
	Turns       int
	TokensIn    int64
	TokensOut   int64
	CacheRead   int64
	CacheWrite  int64
	ByModel     map[string]float64 // model -> cost
	ByProject   map[string]float64 // project -> cost
	BySource    map[string]float64 // tool -> cost
	DailyCost   map[string]float64 // YYYY-MM-DD -> cost
	LastEventAt time.Time
}

// Aggregate rolls a slice of events into Stats. "today" is computed in local time.
func Aggregate(events []Event) Stats {
	s := Stats{
		ByModel:   map[string]float64{},
		ByProject: map[string]float64{},
		BySource:  map[string]float64{},
		DailyCost: map[string]float64{},
	}
	today := time.Now().Format("2006-01-02")
	for _, e := range events {
		s.TotalCost += e.CostUSD
		s.Turns++
		s.TokensIn += e.Input
		s.TokensOut += e.Output
		s.CacheRead += e.CacheRead
		s.CacheWrite += e.CacheWrite
		s.ByModel[e.Model] += e.CostUSD
		s.ByProject[e.Project] += e.CostUSD
		s.BySource[e.Source] += e.CostUSD
		day := e.Timestamp.Local().Format("2006-01-02")
		s.DailyCost[day] += e.CostUSD
		if day == today {
			s.TodayCost += e.CostUSD
		}
		if e.Timestamp.After(s.LastEventAt) {
			s.LastEventAt = e.Timestamp
		}
	}
	return s
}

// TopN returns the n highest-cost entries of a cost map, descending.
func TopN(m map[string]float64, n int) []KV {
	kvs := make([]KV, 0, len(m))
	for k, v := range m {
		kvs = append(kvs, KV{k, v})
	}
	sort.Slice(kvs, func(i, j int) bool { return kvs[i].Value > kvs[j].Value })
	if len(kvs) > n {
		kvs = kvs[:n]
	}
	return kvs
}

// KV is a labeled cost value.
type KV struct {
	Key   string
	Value float64
}
