package web

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/ingest"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/strapdb"
)

// database opens the strap database on first use. Only one vitals process can
// hold it at a time (the file is locked), so a second server on the same home
// gets a clear error instead of corrupting anything.
func (s *Server) database() (*strapdb.DB, error) {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()
	if s.db != nil {
		return s.db, nil
	}
	db, err := strapdb.Open(s.Home, s.Loc)
	if err != nil {
		return nil, fmt.Errorf("%w (another vitals using %s?)", err, s.Home)
	}
	s.db = db
	return db, nil
}

// Close releases the database, if it was opened.
func (s *Server) Close() error {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func apiError(w http.ResponseWriter, status int, format string, a ...any) {
	writeJSON(w, status, map[string]string{"error": fmt.Sprintf(format, a...)})
}

// apiPost guards a JSON write: same origin, a JSON body, the CSRF token in a
// header, and a bounded size.
func (s *Server) apiPost(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" {
			if u, err := url.Parse(o); err != nil || u.Host != r.Host {
				apiError(w, http.StatusForbidden, "cross-origin request refused")
				return
			}
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			apiError(w, http.StatusUnsupportedMediaType, "send application/json")
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.csrf)) != 1 {
			apiError(w, http.StatusForbidden, "stale or missing token: reload the page")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		fn(w, r)
	}
}

// Limits on a live-sample upload.
const (
	maxUploadSamples = 5000
	maxSampleAge     = 24 * time.Hour  // the page may hold samples while offline
	maxSampleAhead   = 5 * time.Minute // allowance for clock skew
)

type sampleIn struct {
	T   int64    `json:"t"` // unix milliseconds on the computer's clock
	HR  int      `json:"hr"`
	RR  []uint16 `json:"rr"`
	Off bool     `json:"off"` // the strap said it was off the wrist
}

// apiSamples stores live samples from the dashboard. Times are the computer's
// clock when each sample arrived, which is correct even while the strap's own
// clock is not set.
func (s *Server) apiSamples(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Samples []sampleIn `json:"samples"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		apiError(w, http.StatusBadRequest, "bad JSON: %v", err)
		return
	}
	if len(in.Samples) == 0 || len(in.Samples) > maxUploadSamples {
		apiError(w, http.StatusBadRequest, "send 1 to %d samples", maxUploadSamples)
		return
	}
	now := s.Now()
	out := make([]strapdb.Sample, 0, len(in.Samples))
	for _, x := range in.Samples {
		t := time.UnixMilli(x.T)
		switch {
		case x.HR > 255 || (x.HR < 1 && !x.Off): // an off-wrist packet may carry no heart rate
			apiError(w, http.StatusBadRequest, "heart rate %d out of range", x.HR)
			return
		case len(x.RR) > 8:
			apiError(w, http.StatusBadRequest, "too many RR intervals in one sample")
			return
		case t.Before(now.Add(-maxSampleAge)) || t.After(now.Add(maxSampleAhead)):
			apiError(w, http.StatusBadRequest, "sample time %s is outside the last 24 hours", t.Format(time.RFC3339))
			return
		}
		out = append(out, strapdb.Sample{Time: t, HR: uint8(x.HR), RR: x.RR, Source: strapdb.Realtime, OffWrist: x.Off})
	}
	db, err := s.database()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if err := db.PutSamples(out); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, strapdb.ErrClockUnset) {
			status = http.StatusUnprocessableEntity
		}
		apiError(w, status, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"stored": len(out)})
}

// apiStatus reports day zero, how many days and nights are stored and every
// metric's readiness.
func (s *Server) apiStatus(w http.ResponseWriter, r *http.Request) {
	db, err := s.database()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	o, err := ingest.Status(db, s.Now().In(s.Loc))
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

// apiSleep scores the night ending on ?date= (default today) from the raw
// history records: motion-based sleep from published methods, plus what the
// strap itself labelled. Read-only; nothing is stored.
//
//	GET /api/sleep?date=YYYY-MM-DD
func (s *Server) apiSleep(w http.ResponseWriter, r *http.Request) {
	day := r.URL.Query().Get("date")
	if day == "" {
		day = s.Now().In(s.Loc).Format("2006-01-02")
	}
	db, err := s.database()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	night, err := ingest.Sleep(db, day, s.Loc)
	if err != nil {
		apiError(w, http.StatusBadRequest, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, night)
}

// apiBattery reports the strap's battery from its own console log: the level
// at the last sync and the recent drain. Read-only.
//
//	GET /api/battery
func (s *Server) apiBattery(w http.ResponseWriter, r *http.Request) {
	db, err := s.database()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	b, err := ingest.ReadBattery(db)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// apiDay returns one day's summary and, when there is a baseline, the
// recovery for the night ending that day.
func (s *Server) apiDay(w http.ResponseWriter, r *http.Request) {
	now := s.Now().In(s.Loc)
	day := r.URL.Query().Get("date")
	if day == "" {
		day = now.Format("2006-01-02")
	}
	db, err := s.database()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	p, err := db.Profile()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	sum, err := ingest.Day(db, day, p, now)
	if err != nil {
		apiError(w, http.StatusBadRequest, "%v", err)
		return
	}
	resp := map[string]any{"summary": sum}
	if rec, ok, err := ingest.RecoveryFor(db, day, p, now); err == nil && ok {
		resp["recovery"] = rec
	}
	writeJSON(w, http.StatusOK, resp)
}

const (
	defaultPoints = 600
	maxPoints     = 5000
)

// apiSeries returns heart rate over a time range, averaged into at most
// `points` buckets so a chart of a whole day stays small.
//
//	GET /api/samples?from=<unix ms>&to=<unix ms>&points=600
func (s *Server) apiSeries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err1 := strconv.ParseInt(q.Get("from"), 10, 64)
	to, err2 := strconv.ParseInt(q.Get("to"), 10, 64)
	if err1 != nil || err2 != nil || to <= from {
		apiError(w, http.StatusBadRequest, "give from and to as unix milliseconds, from < to")
		return
	}
	points := defaultPoints
	if v := q.Get("points"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPoints {
			apiError(w, http.StatusBadRequest, "points must be 1-%d", maxPoints)
			return
		}
		points = n
	}
	db, err := s.database()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	width := math.Ceil(float64(to-from) / float64(points))
	sums := make([]float64, points)
	counts := make([]int, points)
	err = db.Samples(time.UnixMilli(from), time.UnixMilli(to), func(sm strapdb.Sample) bool {
		if sm.OffWrist || sm.HR == 0 {
			return true // an off-wrist reading is not a heart rate: leave a gap, not a dip to zero
		}
		i := int(float64(sm.Time.UnixMilli()-from) / width)
		if i >= points {
			i = points - 1
		}
		sums[i] += float64(sm.HR)
		counts[i]++
		return true
	})
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	t := make([]int64, 0, points)
	hr := make([]float64, 0, points)
	for i, n := range counts {
		if n == 0 {
			continue
		}
		t = append(t, from+int64(float64(i)*width))
		hr = append(hr, math.Round(sums[i]/float64(n)*10)/10)
	}
	writeJSON(w, http.StatusOK, map[string]any{"t": t, "hr": hr})
}

// apiProfile reads or sets the profile (birth year, sex, weight). It is kept
// in the local database and used for max heart rate and calories.
func (s *Server) apiProfile(w http.ResponseWriter, r *http.Request) {
	db, err := s.database()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if r.Method == http.MethodPost {
		var p strapdb.Profile
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			apiError(w, http.StatusBadRequest, "bad JSON: %v", err)
			return
		}
		year := s.Now().Year()
		if p.BirthDate != "" {
			b, ok := p.BirthTime()
			if !ok || b.Year() < year-110 || b.After(s.Now().AddDate(-5, 0, 0)) {
				apiError(w, http.StatusBadRequest, "birth_date must be a real date (YYYY-MM-DD) between 5 and 110 years ago")
				return
			}
			p.BirthYear = b.Year()
		}
		switch {
		case p.BirthYear != 0 && (p.BirthYear < year-110 || p.BirthYear > year-5):
			apiError(w, http.StatusBadRequest, "birth_year out of range")
			return
		case p.Sex != "" && p.Sex != "male" && p.Sex != "female":
			apiError(w, http.StatusBadRequest, `sex must be "male" or "female"`)
			return
		case p.WeightKg != 0 && (p.WeightKg < 20 || p.WeightKg > 400):
			apiError(w, http.StatusBadRequest, "weight_kg out of range")
			return
		}
		// Merge: only the fields sent change, so saving a sex keeps the date of birth.
		if _, err := db.MergeProfile(p); err != nil {
			apiError(w, http.StatusInternalServerError, "%v", err)
			return
		}
	}
	p, err := db.Profile()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// apiHealth is a quick self-check: the database passes bbolt's integrity
// check, how old the newest sample is, and where the history sync stands.
func (s *Server) apiHealth(w http.ResponseWriter, r *http.Request) {
	db, err := s.database()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "database": err.Error()})
		return
	}
	resp := map[string]any{"ok": true}
	if err := db.Check(); err != nil {
		resp["ok"], resp["database"] = false, err.Error()
	} else {
		resp["database"] = "ok"
	}
	if sm, ok, err := db.LatestSample(); err == nil && ok {
		resp["latest_sample"] = sm.Time.Unix()
		resp["latest_sample_age_seconds"] = int(s.Now().Sub(sm.Time).Seconds())
		resp["latest_hr"] = sm.HR
		resp["on_wrist"] = !sm.OffWrist
	}
	if st, err := db.SyncState(); err == nil {
		resp["last_history_record"] = st.LastRecord
	}
	if size, err := db.Size(); err == nil {
		resp["db_bytes"] = size
	}
	code := http.StatusOK
	if resp["ok"] == false {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, resp)
}

// apiCSRF hands the current token to a same-origin page. A page keeps working
// after the server restarts (which issues a new token) by asking for it again
// when a write is refused. A cross-site request is refused, and a cross-origin
// page cannot read the response anyway.
func (s *Server) apiCSRF(w http.ResponseWriter, r *http.Request) {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		apiError(w, http.StatusForbidden, "same-origin only")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": s.csrf})
}

// apiTrend returns the last N days of the few headline numbers, oldest first.
//
//	GET /api/trend?days=14
func (s *Server) apiTrend(w http.ResponseWriter, r *http.Request) {
	n := 14
	if v := r.URL.Query().Get("days"); v != "" {
		k, err := strconv.Atoi(v)
		if err != nil || k < 1 || k > 90 {
			apiError(w, http.StatusBadRequest, "days must be 1-90")
			return
		}
		n = k
	}
	db, err := s.database()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	p, err := db.Profile()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	now := s.Now().In(s.Loc)
	pts, err := ingest.Trend(db, now.Format("2006-01-02"), n, p, now)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": pts})
}

// statusRecorder remembers the status a handler wrote.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// accessLog writes one line to the server log for each distinct failing
// request (status 400 or more on /api, /strap or a POST), at most once every
// 30 seconds per method, path and status. It never logs bodies, query strings
// or tokens. Its purpose is to make a misbehaving page visible: a tab with a
// stale token shows up as a stream of 403s here.
func (s *Server) accessLog(next http.Handler) http.Handler {
	var mu sync.Mutex
	last := map[string]time.Time{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.status < 400 || !(strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/strap/") || r.Method == http.MethodPost) {
			return
		}
		key := fmt.Sprintf("%s %s %d", r.Method, r.URL.Path, rec.status)
		mu.Lock()
		defer mu.Unlock()
		if t, ok := last[key]; ok && s.Now().Sub(t) < 30*time.Second {
			return
		}
		last[key] = s.Now()
		s.Log.Printf("refused: %s", key)
	})
}

// apiClientLog lets the page tell the server log what happened when a button
// was used or a script failed, so a problem in a browser can be read without
// the browser. Short text only; at most 60 lines a minute; local log only.
func (s *Server) apiClientLog(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Event   string `json:"event"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		apiError(w, http.StatusBadRequest, "bad JSON")
		return
	}
	s.clientLogMu.Lock()
	now := s.Now()
	if now.Sub(s.clientLogWindow) > time.Minute {
		s.clientLogWindow, s.clientLogCount = now, 0
	}
	s.clientLogCount++
	over := s.clientLogCount > 60
	s.clientLogMu.Unlock()
	if over {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "slow down"})
		return
	}
	clip := func(v string, n int) string {
		v = strings.Map(func(r rune) rune {
			if r < 32 || r == 127 {
				return ' '
			}
			return r
		}, v)
		if len(v) > n {
			v = v[:n]
		}
		return v
	}
	s.Log.Printf("page: %s: %s", clip(in.Event, 40), clip(in.Message, 300))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
