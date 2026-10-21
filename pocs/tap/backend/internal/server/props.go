package server

import (
	"fmt"
	"html"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/store"
)

// Props: tbh-style compliment polls where only people you have tapped can
// pick you. Safety rules from the design, enforced here:
//   - prompts come only from the curated, positive list below; no free text;
//   - every pick is a real vote by a real connection; nothing is generated;
//   - only active connections appear as options or count toward a card,
//     so a removal or block drops someone out in both directions at once;
//   - Props unlocks at PropsMinConnections, for voting and for seeing picks,
//     so a pick can't be traced by elimination; no response carries the voter.

type prompt struct{ ID, Text string }

var propsPrompts = []prompt{
	{"p_coffee", "Best person to get coffee with"},
	{"p_founder", "Most likely to start a company"},
	{"p_story", "Best storyteller"},
	{"p_listener", "Best listener"},
	{"p_laugh", "Made me laugh the most"},
	{"p_mentor", "Would ask for career advice"},
	{"p_roadtrip", "Best road-trip partner"},
	{"p_questions", "Asks the best questions"},
	{"p_energy", "Brings the best energy to a room"},
	{"p_explain", "Explains things best"},
	{"p_kind", "Kindest person I met lately"},
	{"p_idea", "Had the most interesting idea"},
	{"p_host", "Would throw the best dinner party"},
	{"p_calm", "Calmest in a crisis"},
	{"p_ship", "Most likely to ship something cool"},
	{"p_music", "Best taste in music"},
}

func promptText(id string) string {
	for _, p := range propsPrompts {
		if p.ID == id {
			return p.Text
		}
	}
	return ""
}

func isoWeek(t time.Time) string {
	y, w := t.UTC().ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// connections returns the user's active connections, each with the event of
// the tap where they first met.
func connections(tx store.Tx, userID string) map[string]string {
	out := map[string]string{}
	for _, e := range tx.EdgesOf(userID) {
		if e.Status != store.EdgeActive {
			continue
		}
		first, _ := tx.Tap(e.FirstTapID)
		out[otherOf(e, userID)] = first.EventID
	}
	return out
}

func (s *Server) newRound(userID, week string, conns map[string]string, now time.Time) store.PropsRound {
	ids := make([]string, 0, len(conns))
	for id := range conns {
		ids = append(ids, id)
	}
	slices.Sort(ids) // map order is random; sort before shuffling so only rand decides
	r := store.PropsRound{UserID: userID, Week: week, CreatedAt: now}
	for i, pi := range rand.Perm(len(propsPrompts)) {
		if i == s.cfg.PropsPollsPerWeek {
			break
		}
		rand.Shuffle(len(ids), func(a, b int) { ids[a], ids[b] = ids[b], ids[a] })
		n := min(s.cfg.PropsOptions, len(ids))
		r.Polls = append(r.Polls, store.PropsPoll{PromptID: propsPrompts[pi].ID, Options: slices.Clone(ids[:n])})
	}
	return r
}

type propsOption struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
}

type propsPollView struct {
	Index    int           `json:"index"`
	Prompt   string        `json:"prompt"`
	Options  []propsOption `json:"options"`
	Answered bool          `json:"answered"`
}

func (s *Server) handlePropsRound(w http.ResponseWriter, _ *http.Request, userID string) {
	now := s.cfg.Now()
	week := isoWeek(now)
	resp := map[string]any{"week": week}
	err := s.st.Tx(func(tx store.Tx) error {
		conns := connections(tx, userID)
		resp["connections"] = len(conns)
		resp["unlocked"] = len(conns) >= s.cfg.PropsMinConnections
		resp["unlocks_at"] = s.cfg.PropsMinConnections
		if len(conns) < s.cfg.PropsMinConnections {
			resp["polls"] = []propsPollView{}
			return nil
		}
		r, ok := tx.PropsRound(userID, week)
		if !ok {
			r = s.newRound(userID, week, conns, now)
			tx.PutPropsRound(r)
		}
		polls := []propsPollView{}
		for i, p := range r.Polls {
			v := propsPollView{Index: i, Prompt: promptText(p.PromptID), Answered: p.Answered, Options: []propsOption{}}
			for _, id := range p.Options {
				if _, active := conns[id]; !active {
					continue // removed or blocked since the round was made
				}
				u, _ := tx.User(id)
				v.Options = append(v.Options, propsOption{u.ID, u.DisplayName})
			}
			if len(v.Options) > 0 {
				polls = append(polls, v)
			}
		}
		resp["polls"] = polls
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, resp)
}

func (s *Server) handlePropsVote(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		Index int    `json:"index"`
		Pick  string `json:"pick"` // a user id from the poll's options, or "" to skip
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	now := s.cfg.Now()
	week := isoWeek(now)
	err := s.st.Tx(func(tx store.Tx) error {
		round, ok := tx.PropsRound(userID, week)
		if !ok {
			return fail(409, "no_round", "open this week's Props first")
		}
		if req.Index < 0 || req.Index >= len(round.Polls) {
			return fail(400, "bad_index", "no poll %d this week", req.Index)
		}
		poll := &round.Polls[req.Index]
		if poll.Answered {
			return fail(409, "already_answered", "you already answered this poll")
		}
		if req.Pick != "" {
			if !slices.Contains(poll.Options, req.Pick) {
				return fail(400, "bad_pick", "pick one of the poll's options")
			}
			eventID, active := connections(tx, userID)[req.Pick]
			if !active {
				return fail(404, "not_connected", "you're no longer connected to this person")
			}
			tx.PutPropsVote(store.PropsVote{ID: newID("vote_"), VoterID: userID, PickedID: req.Pick,
				PromptID: poll.PromptID, Week: week, EventID: eventID, CreatedAt: now})
		}
		poll.Answered = true
		tx.PutPropsRound(round)
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// receivedPicks returns the votes for userID since a time, keeping only
// those from people still connected. Below the unlock threshold it returns
// none: with one or two connections, a single pick names its voter.
func (s *Server) receivedPicks(tx store.Tx, userID string, since time.Time) ([]store.PropsVote, map[string]string) {
	conns := connections(tx, userID)
	if len(conns) < s.cfg.PropsMinConnections {
		return nil, conns
	}
	var out []store.PropsVote
	for _, v := range tx.PropsVotesFor(userID, since) {
		if _, active := conns[v.VoterID]; active {
			out = append(out, v)
		}
	}
	return out, conns
}

type pickView struct {
	Prompt   string `json:"prompt"`
	PickedOn string `json:"picked_on"` // a date, never a time: a timestamp could point at who voted
	Hint     string `json:"hint,omitempty"`
}

func (s *Server) handlePropsInbox(w http.ResponseWriter, _ *http.Request, userID string) {
	picks := []pickView{}
	err := s.st.Tx(func(tx store.Tx) error {
		votes, conns := s.receivedPicks(tx, userID, s.cfg.Now().AddDate(0, 0, -30))
		// An event hint is shown only when the event holds enough of the
		// user's connections that it can't single anyone out.
		inEvent := map[string]int{}
		for _, ev := range conns {
			if ev != "" {
				inEvent[ev]++
			}
		}
		for _, v := range votes {
			p := pickView{Prompt: promptText(v.PromptID), PickedOn: v.CreatedAt.UTC().Format("2006-01-02")}
			if v.EventID != "" && inEvent[v.EventID] >= s.cfg.PropsHintMin {
				if ev, ok := tx.Event(v.EventID); ok {
					p.Hint = "someone from " + eventTitle(ev)
				}
			}
			picks = append(picks, p)
		}
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"picks": picks})
}

type propsCard struct {
	Week     string        `json:"week"`
	Name     string        `json:"name"`
	People   int           `json:"people"` // distinct connections who picked the user this week
	Picks    int           `json:"picks"`
	ByPrompt []promptCount `json:"by_prompt"`
	Headline string        `json:"headline"`
	Invite   string        `json:"invite"`
}

type promptCount struct {
	Prompt string `json:"prompt"`
	Count  int    `json:"count"`
}

func (s *Server) buildCard(userID string) (propsCard, error) {
	now := s.cfg.Now()
	card := propsCard{Week: isoWeek(now), ByPrompt: []promptCount{}, Invite: "You can't send Props online. Meet them."}
	err := s.st.Tx(func(tx store.Tx) error {
		u, _ := tx.User(userID)
		card.Name = u.DisplayName
		votes, _ := s.receivedPicks(tx, userID, now.AddDate(0, 0, -8))
		people := map[string]bool{}
		counts := map[string]int{}
		for _, v := range votes {
			if v.Week != card.Week {
				continue
			}
			people[v.VoterID] = true
			counts[v.PromptID]++
			card.Picks++
		}
		card.People = len(people)
		for id, n := range counts {
			card.ByPrompt = append(card.ByPrompt, promptCount{promptText(id), n})
		}
		slices.SortFunc(card.ByPrompt, func(a, b promptCount) int {
			if a.Count != b.Count {
				return b.Count - a.Count
			}
			return strings.Compare(a.Prompt, b.Prompt)
		})
		return nil
	})
	noun := "people"
	if card.People == 1 {
		noun = "person"
	}
	card.Headline = fmt.Sprintf("%d %s I've actually met picked me this week", card.People, noun)
	return card, err
}

func (s *Server) handlePropsCard(w http.ResponseWriter, _ *http.Request, userID string) {
	card, err := s.buildCard(userID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, card)
}

// handlePropsCardSVG renders the weekly card at story size (1080×1920).
func (s *Server) handlePropsCardSVG(w http.ResponseWriter, _ *http.Request, userID string) {
	card, err := s.buildCard(userID)
	if err != nil {
		writeErr(w, err)
		return
	}
	esc := html.EscapeString
	var rows strings.Builder
	for i, pc := range card.ByPrompt {
		if i == 4 {
			break
		}
		y := 1130 + i*110
		fmt.Fprintf(&rows, `<text x="140" y="%d" font-size="50" fill="#ffffff">%s</text><text x="940" y="%d" text-anchor="end" font-size="50" font-weight="700" fill="#4f8cff">×%d</text>`,
			y, esc(pc.Prompt), y, pc.Count)
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="1080" height="1920" viewBox="0 0 1080 1920" font-family="Helvetica, Arial, sans-serif">
<rect width="1080" height="1920" fill="#0f1115"/>
<text x="540" y="300" text-anchor="middle" font-size="44" fill="#b8bec9">%s · Props this week</text>
<circle cx="540" cy="620" r="230" fill="none" stroke="#4f8cff" stroke-width="10" opacity="0.6"/>
<text x="540" y="690" text-anchor="middle" font-size="220" font-weight="700" fill="#ffffff">%d</text>
<text x="540" y="960" text-anchor="middle" font-size="56" font-weight="700" fill="#ffffff">people I've actually met</text>
<text x="540" y="1030" text-anchor="middle" font-size="56" font-weight="700" fill="#ffffff">picked me this week</text>
%s
<text x="540" y="1720" text-anchor="middle" font-size="44" fill="#b8bec9">%s</text>
<text x="540" y="1790" text-anchor="middle" font-size="40" fill="#4f8cff">tap to connect</text>
</svg>`, esc(card.Name), card.People, rows.String(), esc(card.Invite))
}

// handlePropsInvite is where a shared card's link lands.
func (s *Server) handlePropsInvite(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Props on Tap</title>
<style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0f1115;color:#f2f4f8;font:17px/1.5 system-ui,sans-serif;text-align:center;padding:24px}h1{font-size:28px;margin:0 0 8px}p{color:#9aa3b2;max-width:28em}a{display:inline-block;margin-top:16px;padding:12px 22px;border-radius:12px;background:#2f6fed;color:#fff;text-decoration:none;font-weight:600}</style></head>
<body><main><h1>You can't send Props online.</h1><p>Meet them. Props come only from people who have tapped phones with you in person, so every one is from someone real.</p><a href="/">Get Tap</a></main></body></html>`)
}
