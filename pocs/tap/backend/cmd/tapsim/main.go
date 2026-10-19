// Command tapsim runs Tap's end-to-end scenarios with simulated phones,
// against a server at -url or an in-process one when -url is empty.
package main

import (
	"flag"
	"fmt"
	"net/http/httptest"
	"os"

	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/server"
	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/store"
	"github.com/rohithIlluri/POC-S/pocs/tap/sim"
)

func main() {
	url := flag.String("url", "", "Tap server base URL (empty: start one in-process)")
	pairs := flag.Int("burst", 200, "simultaneous taps in the burst scenario")
	flag.Parse()

	base := *url
	if base == "" {
		cfg, err := server.DefaultConfig()
		if err != nil {
			fail(err)
		}
		cfg.DevOTP = true
		cfg.Logf = nil
		srv, err := server.New(cfg, store.NewMemory())
		if err != nil {
			fail(err)
		}
		ts := httptest.NewServer(srv)
		defer ts.Close()
		base = ts.URL
	}
	fmt.Printf("tapsim against %s\n\n", base)
	if err := sim.RunAll(base, *pairs, os.Stdout); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "tapsim:", err)
	os.Exit(1)
}
