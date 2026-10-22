package web

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/ingest"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoopble"
)

// History download. The strap records heart data on its own memory whether or
// not anything is connected, so a night with the computer off is still there
// in the morning. The dashboard connects, asks for it, and this file stores
// each burst before telling the strap it may free that memory.
//
// The page does the Bluetooth; the server runs the tested protocol logic
// (whoopble.HistorySync) and the database writes, and answers each batch of
// frames with the replies to send back (with response).

type historySession struct {
	mu      sync.Mutex
	sync    *whoopble.HistorySync
	started time.Time
	nextSeq byte
	stats   ingest.SinkStats
	last    *lastSync
}

// lastSync is the outcome of the most recent complete download.
type lastSync struct {
	At           int64 `json:"at"` // unix seconds
	Packets      int   `json:"packets"`
	Bursts       int   `json:"bursts"`
	HeartRecords int   `json:"heart_records"`
	Dated        int   `json:"dated"`
	Undated      int   `json:"undated"`
	Other        int   `json:"other"`
}

const historySeqBase = 0x40 // keeps history command numbers apart from the page's own

func (s *Server) historyStart(w http.ResponseWriter, r *http.Request) {
	db, err := s.database()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	// The page reads the strap's clock (from a live packet) together with the wall
	// clock and sends both, so records stamped on an unset strap clock can be dated.
	var in struct {
		StrapSec uint32 `json:"strap_sec"`
		WallMs   int64  `json:"wall_ms"`
	}
	json.NewDecoder(r.Body).Decode(&in)
	s.hist.mu.Lock()
	s.hist.stats = ingest.SinkStats{}
	s.hist.mu.Unlock()
	sink := ingest.HistorySink{DB: db, Now: s.Now, Stats: &s.hist.stats, Logf: func(f string, a ...any) { s.Log.Printf("history: "+f, a...) }}
	if in.StrapSec > 0 && in.WallMs > 0 {
		wall := in.WallMs / 1000
		// Only trust a reading taken close to now (a stale page could send an old one).
		if d := s.Now().Unix() - wall; d > -120 && d < 120 {
			sink.OffsetSec, sink.StrapNowSec, sink.HaveOffset = wall-int64(in.StrapSec), in.StrapSec, true
		}
	}
	s.Log.Printf("history: sync started (strap clock reading: %v, strap_sec=%d, offset=%ds)", sink.HaveOffset, in.StrapSec, sink.OffsetSec)
	s.hist.mu.Lock()
	s.hist.sync = whoopble.NewHistorySync(sink, historySeqBase+1)
	s.hist.started = s.Now()
	s.hist.nextSeq = historySeqBase
	s.hist.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"frame": hex.EncodeToString(whoopble.StartHistory(historySeqBase))})
}

type historyIn struct {
	Bodies []string `json:"bodies"` // hex of validated frame bodies, in arrival order
}

type historyOut struct {
	Replies []string `json:"replies"` // hex frames to write to the strap, in order
	Done    bool     `json:"done"`
	Bursts  int      `json:"bursts"`
	Records int      `json:"records"`
	Dropped int      `json:"discarded"`
	// What the packets turned out to be: heart-rate records are what metrics need.
	HeartRecords int `json:"heart_records"`
	Dated        int `json:"dated"`
}

// historyFrames feeds a batch of history bodies to the sync and returns what
// to write back. A storage failure stops the sync and returns no ack for the
// failing burst, so the strap keeps its data.
func (s *Server) historyFrames(w http.ResponseWriter, r *http.Request) {
	var in historyIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		apiError(w, http.StatusBadRequest, "bad JSON: %v", err)
		return
	}
	if len(in.Bodies) == 0 || len(in.Bodies) > 20000 {
		apiError(w, http.StatusBadRequest, "send 1 to 20000 bodies")
		return
	}
	s.hist.mu.Lock()
	defer s.hist.mu.Unlock()
	if s.hist.sync == nil {
		apiError(w, http.StatusConflict, "no history sync in progress: start one first")
		return
	}
	out := historyOut{Replies: []string{}}
	for _, h := range in.Bodies {
		body, err := hex.DecodeString(h)
		if err != nil {
			apiError(w, http.StatusBadRequest, "bad hex: %v", err)
			return
		}
		reply, err := s.hist.sync.Feed(body)
		if err != nil {
			s.hist.sync = nil
			status := http.StatusInternalServerError
			if !errors.Is(err, whoopble.ErrStore) {
				status = http.StatusUnprocessableEntity
			}
			apiError(w, status, "history sync stopped, nothing acknowledged for the failing burst: %v", err)
			return
		}
		if reply != nil {
			out.Replies = append(out.Replies, hex.EncodeToString(reply))
		}
	}
	out.Done = s.hist.sync.Done
	out.Bursts, out.Records, out.Dropped = s.hist.sync.Bursts, s.hist.sync.Stored, s.hist.sync.Discarded
	out.HeartRecords, out.Dated = s.hist.stats.HeartRecords, s.hist.stats.Dated
	if out.Done {
		s.hist.last = &lastSync{At: s.Now().Unix(), Packets: out.Records, Bursts: out.Bursts, HeartRecords: s.hist.stats.HeartRecords,
			Dated: s.hist.stats.Dated, Undated: s.hist.stats.Undated, Other: s.hist.stats.Other}
		s.Log.Printf("history: sync complete: %d packets in %d bursts, %d heart records (%d dated, %d undated), %d other", out.Records, out.Bursts,
			s.hist.stats.HeartRecords, s.hist.stats.Dated, s.hist.stats.Undated, s.hist.stats.Other)
		s.hist.sync = nil
	}
	writeJSON(w, http.StatusOK, out)
}

// historyAbort ends a sync and returns the abort command for the strap.
func (s *Server) historyAbort(w http.ResponseWriter, r *http.Request) {
	s.hist.mu.Lock()
	s.hist.sync = nil
	s.hist.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"frame": hex.EncodeToString(whoopble.AbortHistory(historySeqBase + 0x20))})
}

// historyStatus reports where the last sync stopped.
func (s *Server) historyStatus(w http.ResponseWriter, r *http.Request) {
	db, err := s.database()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	st, err := db.SyncState()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "%v", err)
		return
	}
	s.hist.mu.Lock()
	active := s.hist.sync != nil
	s.hist.mu.Unlock()
	resp := map[string]any{"active": active, "last_record": st.LastRecord}
	s.hist.mu.Lock()
	if s.hist.last != nil {
		resp["last"] = s.hist.last
	}
	s.hist.mu.Unlock()
	if !st.SyncedAt.IsZero() {
		resp["synced_at"] = st.SyncedAt.Unix()
	}
	writeJSON(w, http.StatusOK, resp)
}
