package web

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoopble"
)

func hexBody(b []byte) string { return `"` + hex.EncodeToString(b) + `"` }

func histRecord(number uint32, unix int64, hr byte) []byte {
	b := make([]byte, 88)
	b[0], b[1], b[2] = whoopble.TypeHistory, 12, 7
	binary.LittleEndian.PutUint32(b[3:], number)
	binary.LittleEndian.PutUint32(b[7:], uint32(unix))
	b[17] = hr
	return b
}

func histMeta(sub byte, data ...byte) []byte {
	b := []byte{whoopble.TypeMetadata, 1, sub, 0, 0, 0, 0, 0, 0}
	d := append([]byte(nil), data...)
	for len(d) < 15 {
		d = append(d, 0)
	}
	return append(b, d...)
}

type histReply struct {
	Replies []string `json:"replies"`
	Done    bool     `json:"done"`
	Bursts  int      `json:"bursts"`
	Records int      `json:"records"`
}

func TestHistoryDownloadStoresBeforeItAcknowledges(t *testing.T) {
	c := newClient(t)
	c.page()
	t.Cleanup(func() { c.srv.Close() })

	// Frames before a sync starts are refused: nothing to attach them to.
	if w := c.jsonPost("/api/history/frames", `{"bodies":["00"]}`, nil); w.Code != http.StatusConflict {
		t.Fatalf("frames before start = %d", w.Code)
	}

	w := c.jsonPost("/api/history/start", `{}`, nil)
	var start struct {
		Frame string `json:"frame"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &start) != nil {
		t.Fatalf("start = %d %s", w.Code, w.Body)
	}
	frame, _ := hex.DecodeString(start.Frame)
	body, err := whoopble.Parse(frame)
	if err != nil || len(body) != 4 || body[2] != whoopble.CmdSendHistory {
		t.Fatalf("start frame body = %x %v", body, err)
	}

	// One burst of three records from last night (clock valid), then the end marker.
	night := now.Add(-10 * time.Hour).Unix()
	token := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	bodies := []string{
		hexBody(histMeta(whoopble.MetaHistoryStart)),
		hexBody(histRecord(500, night, 52)),
		hexBody(histRecord(501, night+1, 53)),
		hexBody(histRecord(502, night+2, 54)),
	}
	w = c.jsonPost("/api/history/frames", `{"bodies":[`+strings.Join(bodies, ",")+`]}`, nil)
	var out histReply
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || len(out.Replies) != 0 || out.Bursts != 0 {
		t.Fatalf("mid-burst = %d %s: nothing may be acknowledged before the end marker", w.Code, w.Body)
	}

	end := histMeta(whoopble.MetaHistoryEnd, append([]byte{3, 0, 0, 0}, token...)...)
	w = c.jsonPost("/api/history/frames", `{"bodies":[`+hexBody(end)+`,`+hexBody(histMeta(whoopble.MetaHistoryComplete))+`]}`, nil)
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || len(out.Replies) != 1 || !out.Done || out.Bursts != 1 || out.Records != 3 {
		t.Fatalf("end = %d %s", w.Code, w.Body)
	}
	ack, _ := hex.DecodeString(out.Replies[0])
	ackBody, _ := whoopble.Parse(ack)
	if ackBody[2] != whoopble.CmdHistoryResult || ackBody[3] != 1 || hex.EncodeToString(ackBody[4:]) != hex.EncodeToString(token) {
		t.Errorf("ack = %x: it must echo the token back", ackBody)
	}

	// The data is in the database, and the sync position is recorded.
	r := c.do("GET", "/api/samples?from="+itoa(now.Add(-12*time.Hour).UnixMilli())+"&to="+itoa(now.UnixMilli())+"&points=100", nil, nil)
	if !strings.Contains(r.Body.String(), `"hr":[`) || strings.Contains(r.Body.String(), `"hr":[]`) {
		t.Errorf("stored series = %s", r.Body)
	}
	st := c.do("GET", "/api/history/status", nil, nil)
	if !strings.Contains(st.Body.String(), `"last_record":502`) || !strings.Contains(st.Body.String(), `"active":false`) {
		t.Errorf("status = %s", st.Body)
	}
}

func TestHistoryStorageFailureMeansNoAck(t *testing.T) {
	c := newClient(t)
	c.page()
	c.jsonPost("/api/history/start", `{}`, nil)
	// Close the database under the sync: the burst cannot be stored.
	c.srv.dbMu.Lock()
	c.srv.db.Close()
	c.srv.dbMu.Unlock()
	t.Cleanup(func() { c.srv.dbMu.Lock(); c.srv.db = nil; c.srv.dbMu.Unlock() })

	night := now.Add(-10 * time.Hour).Unix()
	bodies := []string{
		hexBody(histMeta(whoopble.MetaHistoryStart)),
		hexBody(histRecord(1, night, 52)),
		hexBody(histMeta(whoopble.MetaHistoryEnd, 1, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8)),
	}
	w := c.jsonPost("/api/history/frames", `{"bodies":[`+strings.Join(bodies, ",")+`]}`, nil)
	if w.Code == 200 || strings.Contains(w.Body.String(), `"replies":["`) {
		t.Fatalf("a burst that could not be stored was acknowledged: %d %s", w.Code, w.Body)
	}
	// The sync is over; later frames have nothing to attach to.
	if w := c.jsonPost("/api/history/frames", `{"bodies":["00"]}`, nil); w.Code != http.StatusConflict {
		t.Errorf("after a failure, frames = %d", w.Code)
	}
}

func TestHistoryEndpointsAreGuarded(t *testing.T) {
	c := newClient(t)
	c.page()
	t.Cleanup(func() { c.srv.Close() })
	for _, path := range []string{"/api/history/start", "/api/history/frames", "/api/history/abort"} {
		if w := c.jsonPost(path, `{"bodies":["00"]}`, func(r *http.Request) { r.Header.Set("X-CSRF-Token", "x") }); w.Code != http.StatusForbidden {
			t.Errorf("%s with a bad token = %d", path, w.Code)
		}
	}
	w := c.jsonPost("/api/history/abort", `{}`, nil)
	if w.Code != 200 {
		t.Fatalf("abort = %d", w.Code)
	}
	var a struct{ Frame string }
	json.Unmarshal(w.Body.Bytes(), &a)
	frame, _ := hex.DecodeString(a.Frame)
	if body, err := whoopble.Parse(frame); err != nil || body[2] != whoopble.CmdAbortHistory {
		t.Errorf("abort frame %x %v", body, err)
	}
}

func TestHistoryFromAStrapWithAnUnsetClockIsDatedFromThePagesClockReading(t *testing.T) {
	c := newClient(t)
	c.page()
	t.Cleanup(func() { c.srv.Close() })

	// The page read a live packet: the strap's clock said 1971-01-01 + 5000 s at the same moment the wall clock said `now`.
	const strapSec = 31536000 + 5000
	start := `{"strap_sec":` + itoa(strapSec) + `,"wall_ms":` + itoa(now.UnixMilli()) + `}`
	if w := c.jsonPost("/api/history/start", start, nil); w.Code != 200 {
		t.Fatalf("start = %d %s", w.Code, w.Body)
	}
	// Two hours earlier would be before the strap booted, so use records from 600 s to 540 s ago, on the strap's own clock.
	var bodies []string
	bodies = append(bodies, hexBody(histMeta(whoopble.MetaHistoryStart)))
	for i := 0; i < 60; i++ {
		bodies = append(bodies, hexBody(histRecord(uint32(200+i), strapSec-600+int64(i), byte(60+i%5))))
	}
	token := []byte{9, 8, 7, 6, 5, 4, 3, 2}
	bodies = append(bodies, hexBody(histMeta(whoopble.MetaHistoryEnd, append([]byte{60, 0, 0, 0}, token...)...)))
	w := c.jsonPost("/api/history/frames", `{"bodies":[`+strings.Join(bodies, ",")+`]}`, nil)
	var out histReply
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || out.Bursts != 1 || out.Records != 60 || len(out.Replies) != 1 {
		t.Fatalf("frames = %d %s", w.Code, w.Body)
	}
	// The data is on today's real timeline, not in 1971.
	from, to := now.Add(-11*time.Minute).UnixMilli(), now.Add(-9*time.Minute).UnixMilli()
	r := c.do("GET", "/api/samples?from="+itoa(from)+"&to="+itoa(to)+"&points=100", nil, nil)
	var series struct {
		T  []int64   `json:"t"`
		HR []float64 `json:"hr"`
	}
	json.Unmarshal(r.Body.Bytes(), &series)
	if len(series.HR) < 50 {
		t.Fatalf("only %d points landed 9-11 minutes ago: %s", len(series.HR), r.Body)
	}
	st := c.do("GET", "/api/status", nil, nil)
	if !strings.Contains(st.Body.String(), `"day_zero":"2026-10-21"`) {
		t.Errorf("day zero should be today, not 1971: %s", st.Body)
	}
	// A page that sends a stale clock reading (an hour old) must not be trusted.
	c2 := newClient(t)
	c2.page()
	t.Cleanup(func() { c2.srv.Close() })
	stale := `{"strap_sec":` + itoa(strapSec) + `,"wall_ms":` + itoa(now.Add(-time.Hour).UnixMilli()) + `}`
	c2.jsonPost("/api/history/start", stale, nil)
	c2.jsonPost("/api/history/frames", `{"bodies":[`+strings.Join(bodies, ",")+`]}`, nil)
	r2 := c2.do("GET", "/api/samples?from=0&to="+itoa(now.UnixMilli()+1)+"&points=100", nil, nil)
	if strings.Contains(r2.Body.String(), `"hr":[6`) {
		t.Errorf("a stale clock reading dated records anyway: %s", r2.Body)
	}
}

func TestAConsoleLogBurstThroughHTTPIsAcknowledgedAndKeptRaw(t *testing.T) {
	c := newClient(t)
	c.page()
	t.Cleanup(func() { c.srv.Close() })
	c.jsonPost("/api/history/start", `{}`, nil)
	token := []byte{0x05, 0xd9, 0, 0, 2, 0, 0, 0}
	bodies := []string{hexBody(histMeta(whoopble.MetaHistoryStart))}
	for i := 0; i < 54; i++ {
		bodies = append(bodies, hexBody([]byte{whoopble.TypeConsoleLog, 0x4b, 2, 0, byte(i), 'l', 'o', 'g'}))
	}
	bodies = append(bodies, hexBody(histMeta(whoopble.MetaHistoryEnd, append([]byte{54, 0, 0, 0}, token...)...)))
	w := c.jsonPost("/api/history/frames", `{"bodies":[`+strings.Join(bodies, ",")+`]}`, nil)
	var out histReply
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || out.Bursts != 1 || out.Records != 54 || len(out.Replies) != 1 {
		t.Fatalf("a console-log burst: %d %s", w.Code, w.Body)
	}
	ack, _ := hex.DecodeString(out.Replies[0])
	body, _ := whoopble.Parse(ack)
	if body[3] != 1 || hex.EncodeToString(body[4:12]) != hex.EncodeToString(token) {
		t.Errorf("ack = %x", body)
	}
	st := c.do("GET", "/api/history/status", nil, nil)
	if !strings.Contains(st.Body.String(), `"synced_at"`) {
		t.Errorf("status = %s", st.Body)
	}
}
