package server

import (
	"fmt"
	"html"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/store"
)

func otherOf(e store.Edge, userID string) string {
	if e.UserA == userID {
		return e.UserB
	}
	return e.UserA
}

func activeOthers(tx store.Tx, userID string) []string {
	var out []string
	for _, e := range tx.EdgesOf(userID) {
		if e.Status == store.EdgeActive {
			out = append(out, otherOf(e, userID))
		}
	}
	return out
}

// eventTitle names an event until someone who was there names it.
func eventTitle(e store.Event) string {
	if e.Name != "" {
		return e.Name
	}
	return "Event on " + e.Start.Format("Jan 2")
}

type person struct {
	UserID      string    `json:"user_id"`
	DisplayName string    `json:"display_name"`
	PhotoURL    string    `json:"photo_url,omitempty"`
	MetAt       time.Time `json:"met_at"`
	LastTapAt   time.Time `json:"last_tap_at"`
	TapCount    int       `json:"tap_count"`
	Note        string    `json:"note,omitempty"`
	Place       string    `json:"place,omitempty"` // geohash 7; empty when either side hid it
	EventID     string    `json:"event_id,omitempty"`
}

type group struct {
	Title   string   `json:"title"`
	EventID string   `json:"event_id,omitempty"`
	People  []person `json:"people"`
}

// handlePeople returns the caller's connections grouped by the event or
// month where they first met — the graph as memories.
func (s *Server) handlePeople(w http.ResponseWriter, _ *http.Request, userID string) {
	var groups []*group
	byKey := map[string]*group{}
	places := []string{}
	if err := s.st.Tx(func(tx store.Tx) error {
		for _, e := range tx.EdgesOf(userID) {
			if e.Status != store.EdgeActive {
				continue
			}
			other := otherOf(e, userID)
			u, ok := tx.User(other)
			if !ok {
				continue
			}
			mine, theirs := tx.Meta(userID, other), tx.Meta(other, userID)
			first, _ := tx.Tap(e.FirstTapID)
			p := person{UserID: u.ID, DisplayName: u.DisplayName, PhotoURL: u.PhotoURL, MetAt: e.CreatedAt,
				LastTapAt: e.LastTapAt, TapCount: e.TapCount, Note: mine.Note, EventID: first.EventID}
			if !mine.HidePlace && !theirs.HidePlace {
				p.Place = first.Geohash
				if !slices.Contains(places, p.Place) {
					places = append(places, p.Place)
				}
			}
			key, title := "m:"+e.CreatedAt.Format("2006-01"), "Met in "+e.CreatedAt.Format("January 2006")
			if ev, ok := tx.Event(first.EventID); ok {
				key, title = "e:"+ev.ID, eventTitle(ev)
			}
			g := byKey[key]
			if g == nil {
				g = &group{Title: title, EventID: first.EventID, People: []person{}}
				if !strings.HasPrefix(key, "e:") {
					g.EventID = ""
				}
				byKey[key] = g
				groups = append(groups, g)
			}
			g.People = append(g.People, p)
		}
		return nil
	}); err != nil {
		writeErr(w, err)
		return
	}
	total := 0
	for _, g := range groups {
		total += len(g.People)
	}
	writeJSON(w, 200, map[string]any{"groups": groups, "places": places, "total": total})
}

// connected loads the edge between the caller and id, or fails with 404 so
// nobody can probe for accounts they haven't met.
func connected(tx store.Tx, userID, other string, activeOnly bool) (store.Edge, *apiError) {
	e, ok := tx.Edge(userID, other)
	if !ok || (activeOnly && e.Status != store.EdgeActive) {
		return store.Edge{}, fail(404, "not_connected", "you haven't tapped this person")
	}
	return e, nil
}

func (s *Server) handleNote(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		Text string `json:"text"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if len(req.Text) > 500 {
		writeErr(w, fail(400, "note_too_long", "notes are at most 500 characters"))
		return
	}
	other := r.PathValue("id")
	err := s.st.Tx(func(tx store.Tx) error {
		if _, err := connected(tx, userID, other, true); err != nil {
			return err
		}
		m := tx.Meta(userID, other)
		m.Note = strings.TrimSpace(req.Text)
		tx.PutMeta(m)
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"note": strings.TrimSpace(req.Text)})
}

func (s *Server) handleHidePlace(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		Hidden bool `json:"hidden"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	other := r.PathValue("id")
	err := s.st.Tx(func(tx store.Tx) error {
		if _, err := connected(tx, userID, other, true); err != nil {
			return err
		}
		m := tx.Meta(userID, other)
		m.HidePlace = req.Hidden
		tx.PutMeta(m)
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"hidden": req.Hidden})
}

// removeEdge marks the edge removed by userID's side. The other person is
// not told; the edge simply stops appearing for both.
func removeEdge(tx store.Tx, e store.Edge, userID string) {
	if e.UserA == userID {
		e.Status = store.EdgeRemovedByA
	} else {
		e.Status = store.EdgeRemovedByB
	}
	tx.PutEdge(e)
}

func (s *Server) handleRemove(w http.ResponseWriter, r *http.Request, userID string) {
	other := r.PathValue("id")
	err := s.st.Tx(func(tx store.Tx) error {
		e, err := connected(tx, userID, other, true)
		if err != nil {
			return err
		}
		removeEdge(tx, e, userID)
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"removed": true})
}

func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	err := s.st.Tx(func(tx store.Tx) error {
		e, err := connected(tx, userID, req.UserID, false)
		if err != nil {
			return err
		}
		tx.PutBlock(userID, req.UserID)
		if e.Status == store.EdgeActive {
			removeEdge(tx, e, userID)
		}
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"blocked": true})
}

var reportReasons = []string{"harassment", "fake_account", "spam", "unsafe", "other"}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		UserID string `json:"user_id"`
		Reason string `json:"reason"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if !slices.Contains(reportReasons, req.Reason) {
		writeErr(w, fail(400, "bad_reason", "reason must be one of %s", strings.Join(reportReasons, ", ")))
		return
	}
	err := s.st.Tx(func(tx store.Tx) error {
		if _, err := connected(tx, userID, req.UserID, false); err != nil {
			return err
		}
		tx.PutReport(store.Report{ID: newID("rpt_"), ReporterID: userID, SubjectID: req.UserID, Reason: req.Reason, CreatedAt: s.cfg.Now()})
		if tx.DistinctReporters(req.UserID) >= s.cfg.ReportsToFreeze {
			u, _ := tx.User(req.UserID)
			u.Frozen = true
			tx.PutUser(u)
		}
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"reported": true})
}

type eventView struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Named      bool      `json:"named"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	TapCount   int       `json:"tap_count"`
	PeopleMet  int       `json:"people_met"` // distinct people the caller tapped there
	RecapImage string    `json:"recap_image"`
}

func peopleMetAt(tx store.Tx, ev store.Event, userID string) int {
	seen := map[string]bool{}
	for _, t := range tx.TapsNearSince(ev.Geohash6, ev.Start) {
		if t.EventID != ev.ID {
			continue
		}
		if t.UserA == userID {
			seen[t.UserB] = true
		} else if t.UserB == userID {
			seen[t.UserA] = true
		}
	}
	return len(seen)
}

func view(tx store.Tx, ev store.Event, userID string) eventView {
	return eventView{ID: ev.ID, Name: eventTitle(ev), Named: ev.Name != "", Start: ev.Start, End: ev.End,
		TapCount: ev.TapCount, PeopleMet: peopleMetAt(tx, ev, userID), RecapImage: "/v1/events/" + ev.ID + "/recap.svg"}
}

func (s *Server) handleEvents(w http.ResponseWriter, _ *http.Request, userID string) {
	out := []eventView{}
	if err := s.st.Tx(func(tx store.Tx) error {
		for _, ev := range tx.EventsOf(userID) {
			out = append(out, view(tx, ev, userID))
		}
		return nil
	}); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"events": out})
}

func memberEvent(tx store.Tx, id, userID string) (store.Event, *apiError) {
	ev, ok := tx.Event(id)
	if !ok || !slices.Contains(ev.Members, userID) {
		return store.Event{}, fail(404, "no_event", "no such event")
	}
	return ev, nil
}

// handleNameEvent lets the first attendee who names an event set its name.
func (s *Server) handleNameEvent(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 60 {
		writeErr(w, fail(400, "bad_name", "event names are 1–60 characters"))
		return
	}
	var out eventView
	err := s.st.Tx(func(tx store.Tx) error {
		ev, err := memberEvent(tx, r.PathValue("id"), userID)
		if err != nil {
			return err
		}
		if ev.Name != "" {
			return fail(409, "already_named", "this event is already named %q", ev.Name)
		}
		ev.Name = name
		tx.PutEvent(ev)
		out = view(tx, ev, userID)
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func recapHeadline(ev eventView) string {
	noun := "humans"
	if ev.PeopleMet == 1 {
		noun = "human"
	}
	return fmt.Sprintf("Met %d %s at %s. Zero bots.", ev.PeopleMet, noun, ev.Name)
}

func (s *Server) handleRecap(w http.ResponseWriter, r *http.Request, userID string) {
	var out eventView
	err := s.st.Tx(func(tx store.Tx) error {
		ev, err := memberEvent(tx, r.PathValue("id"), userID)
		if err != nil {
			return err
		}
		out = view(tx, ev, userID)
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"event": out, "headline": recapHeadline(out)})
}

// handleRecapSVG renders the shareable story card at 1080×1920 (9:16). The
// apps rasterise it to PNG before handing it to Instagram or Snapchat.
func (s *Server) handleRecapSVG(w http.ResponseWriter, r *http.Request, userID string) {
	var ev eventView
	err := s.st.Tx(func(tx store.Tx) error {
		e, err := memberEvent(tx, r.PathValue("id"), userID)
		if err != nil {
			return err
		}
		ev = view(tx, e, userID)
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	esc := html.EscapeString
	w.Header().Set("Content-Type", "image/svg+xml")
	fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="1080" height="1920" viewBox="0 0 1080 1920">
<rect width="1080" height="1920" fill="#0f1115"/>
<circle cx="540" cy="760" r="300" fill="none" stroke="#4f8cff" stroke-width="10" opacity="0.35"/>
<circle cx="540" cy="760" r="220" fill="none" stroke="#4f8cff" stroke-width="10" opacity="0.6"/>
<text x="540" y="800" text-anchor="middle" font-family="Helvetica, Arial, sans-serif" font-size="200" font-weight="700" fill="#ffffff">%d</text>
<text x="540" y="1180" text-anchor="middle" font-family="Helvetica, Arial, sans-serif" font-size="64" font-weight="700" fill="#ffffff">humans met at</text>
<text x="540" y="1270" text-anchor="middle" font-family="Helvetica, Arial, sans-serif" font-size="64" font-weight="700" fill="#4f8cff">%s</text>
<text x="540" y="1380" text-anchor="middle" font-family="Helvetica, Arial, sans-serif" font-size="48" fill="#b8bec9">Zero bots. Every one tapped in person.</text>
<text x="540" y="1760" text-anchor="middle" font-family="Helvetica, Arial, sans-serif" font-size="44" fill="#b8bec9">%s · tap to connect</text>
</svg>`, ev.PeopleMet, esc(ev.Name), esc(ev.Start.Format("Jan 2, 2006")))
}
