package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/server"
	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/store/storetest"
	"github.com/rohithIlluri/POC-S/pocs/tap/sim"
)

// TestScenarios is CI's end-to-end job: every scenario against a real HTTP
// server, including a 100-pair burst through the matching engine.
func TestScenarios(t *testing.T) {
	cfg, err := server.DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DevOTP = true
	cfg.ClaimWait = 2 * time.Second // the relay scenario waits this out
	cfg.Logf = nil
	srv, err := server.New(cfg, storetest.New(t))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	var out strings.Builder
	if err := sim.RunAll(ts.URL, 100, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	t.Log("\n" + out.String())
}
