package web

import (
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoopble"
)

func TestStrapClockCommand(t *testing.T) {
	c := newClient(t)
	w := c.do("GET", "/strap/clock-command", nil, nil)
	if w.Code != 200 {
		t.Fatalf("GET /strap/clock-command = %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q; a stale clock command must never be cached", cc)
	}
	frame, err := hex.DecodeString(strings.TrimSpace(w.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	body, err := whoopble.Parse(frame)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 12 || body[0] != whoopble.TypeCommand || body[2] != whoopble.CmdSetClock {
		t.Fatalf("body = %x", body)
	}
	if got, want := int64(binary.LittleEndian.Uint32(body[3:7])), now.Unix(); got != want {
		t.Errorf("seconds = %d, want the server's clock %d", got, want)
	}
}

func TestClockButtonIsOnThePage(t *testing.T) {
	page := newClient(t).page()
	if !strings.Contains(page, `id="strap-clock"`) {
		t.Error("dashboard has no Set strap clock button")
	}
}

func TestDashboardShowsTheStoredDataPanel(t *testing.T) {
	page := newClient(t).page()
	for _, id := range []string{`id="data"`, `id="data-hero"`, `id="data-metrics"`, `id="data-svg"`, `id="data-zones"`, `id="data-profile"`, `id="data-live"`, `id="strap-sync"`} {
		if !strings.Contains(page, id) {
			t.Errorf("dashboard is missing %s", id)
		}
	}
	// The panel script is served and reads only the local API.
	c := newClient(t)
	js := c.do("GET", "/static/app.js", nil, nil).Body.String()
	for _, want := range []string{"/api/status", "/api/day", "/api/samples", "/api/profile", "/api/history/status", "/api/health", "/api/trend"} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js never calls %s", want)
		}
	}
	if strings.Contains(js, "http://") && strings.Contains(js, `fetch("http`) {
		t.Error("the page must not fetch from another origin")
	}
}
