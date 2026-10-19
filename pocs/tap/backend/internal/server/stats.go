package server

import (
	"cmp"
	"crypto/subtle"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/store"
)

// Pilot metrics, as the build plan defines them:
//   - north star: weekly taps per active user;
//   - pilot gate: 40% of users tap again within 14 days of their first tap;
//   - tap success by failure code and channel, which Phase 0 and the pilot
//     are judged on;
//   - share of taps between an iPhone and an Android phone.
//
// They are computed from raw rows on request. At pilot scale (thousands of
// taps) that is milliseconds; at launch scale this moves to BigQuery.

const (
	statsWindow   = 7 * 24 * time.Hour
	retapWithin   = 14 * 24 * time.Hour
	retapLookback = 60 * 24 * time.Hour // first taps older than this fall out of the cohort
	pilotGate     = 0.40
)

type countRow struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

type transportRow struct {
	Transport   string  `json:"transport"`
	Attempts    int     `json:"attempts"`
	Matched     int     `json:"matched"`
	SuccessRate float64 `json:"success_rate"`
}

type Stats struct {
	GeneratedAt time.Time `json:"generated_at"`
	WindowDays  int       `json:"window_days"`

	Attempts    int            `json:"attempts"` // claim requests, retries excluded
	Matched     int            `json:"matched"`
	SuccessRate float64        `json:"success_rate"`
	Failures    []countRow     `json:"failures"` // rejected codes plus "expired", most common first
	ByTransport []transportRow `json:"by_transport"`
	WaitP50MS   int64          `json:"match_wait_p50_ms"`
	WaitP95MS   int64          `json:"match_wait_p95_ms"`

	Taps               int     `json:"taps"`
	ActiveUsers        int     `json:"active_users"`         // users in at least one tap this window
	TapsPerActiveUser  float64 `json:"taps_per_active_user"` // the north star
	CrossPlatformShare float64 `json:"cross_platform_share"` // of taps where both phones are iOS or Android
	CrossPlatformBase  int     `json:"cross_platform_base"`  // taps that share is computed over

	RetapCohort int     `json:"retap_cohort"` // users whose first tap is 14+ days old
	Retapped    int     `json:"retapped"`
	RetapRate   float64 `json:"retap_rate"`
	PilotGate   float64 `json:"pilot_gate"`
	GateMet     bool    `json:"gate_met"`

	PropsVotes int `json:"props_votes"`
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func computeStats(tx store.Tx, now time.Time) Stats {
	since := now.Add(-statsWindow)
	st := Stats{GeneratedAt: now, WindowDays: int(statsWindow / (24 * time.Hour)), PilotGate: pilotGate,
		Failures: []countRow{}, ByTransport: []transportRow{}}

	failures := map[string]int{}
	byT := map[string]*transportRow{}
	var waits []int64
	for _, a := range tx.AttemptsSince(since) {
		if a.Outcome == "retry" {
			continue // a resent request for a claim already counted
		}
		st.Attempts++
		tr := byT[a.Transport]
		if tr == nil {
			tr = &transportRow{Transport: a.Transport}
			byT[a.Transport] = tr
		}
		tr.Attempts++
		switch a.Outcome {
		case "matched":
			st.Matched++
			tr.Matched++
			waits = append(waits, a.WaitMS)
		case "expired":
			failures["expired"]++
		default:
			failures[a.Code]++
		}
	}
	st.SuccessRate = ratio(st.Matched, st.Attempts)
	for code, n := range failures {
		st.Failures = append(st.Failures, countRow{code, n})
	}
	slices.SortFunc(st.Failures, func(a, b countRow) int { return cmp.Or(b.Count-a.Count, strings.Compare(a.Code, b.Code)) })
	for _, tr := range byT {
		tr.SuccessRate = ratio(tr.Matched, tr.Attempts)
		st.ByTransport = append(st.ByTransport, *tr)
	}
	slices.SortFunc(st.ByTransport, func(a, b transportRow) int { return strings.Compare(a.Transport, b.Transport) })
	if len(waits) > 0 {
		slices.Sort(waits)
		st.WaitP50MS = waits[len(waits)/2]
		st.WaitP95MS = waits[(len(waits)*95)/100]
	}

	// Taps this window, active users and cross-platform share.
	active := map[string]int{}
	platform := map[string]string{}
	plat := func(id string) string {
		if p, ok := platform[id]; ok {
			return p
		}
		d, _ := tx.Device(id)
		platform[id] = d.Platform
		return d.Platform
	}
	cross := 0
	for _, tp := range tx.TapsSince(since) {
		st.Taps++
		for _, u := range []string{tp.UserA, tp.UserB} {
			if u != "deleted" {
				active[u]++
			}
		}
		pa, pb := plat(tp.DeviceA), plat(tp.DeviceB)
		if isPhone(pa) && isPhone(pb) {
			st.CrossPlatformBase++
			if pa != pb {
				cross++
			}
		}
	}
	st.ActiveUsers = len(active)
	taps := 0
	for _, n := range active {
		taps += n
	}
	st.TapsPerActiveUser = ratio(taps, st.ActiveUsers)
	st.CrossPlatformShare = ratio(cross, st.CrossPlatformBase)

	// Re-tap: among users whose first tap is at least 14 days old, the share
	// who tapped anyone again within 14 days of it.
	byUser := map[string][]time.Time{}
	for _, tp := range tx.TapsSince(now.Add(-retapLookback)) {
		for _, u := range []string{tp.UserA, tp.UserB} {
			if u != "deleted" {
				byUser[u] = append(byUser[u], tp.CreatedAt)
			}
		}
	}
	for _, times := range byUser {
		slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
		first := times[0]
		if now.Sub(first) < retapWithin {
			continue // too new to judge
		}
		st.RetapCohort++
		if len(times) > 1 && times[1].Sub(first) <= retapWithin {
			st.Retapped++
		}
	}
	st.RetapRate = ratio(st.Retapped, st.RetapCohort)
	st.GateMet = st.RetapCohort > 0 && st.RetapRate >= pilotGate
	st.PropsVotes = tx.PropsVotesSince(since)
	return st
}

func isPhone(platform string) bool { return platform == "ios" || platform == "android" }

// handleStats serves pilot metrics to holders of the admin token. With no
// token configured the endpoint does not exist.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if s.cfg.AdminToken == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.AdminToken)) != 1 {
		writeErr(w, fail(404, "not_found", "not found"))
		return
	}
	var st Stats
	if err := s.st.Tx(func(tx store.Tx) error { st = computeStats(tx, s.cfg.Now()); return nil }); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, st)
}
