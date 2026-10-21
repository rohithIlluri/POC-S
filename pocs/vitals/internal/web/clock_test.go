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
	if len(body) != 11 || body[0] != whoopble.TypeCommand || body[2] != whoopble.CmdSetClock {
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
