// Command tapd runs the Tap API and serves the browser demo at /.
//
//	go run ./backend/cmd/tapd -dev        # http://localhost:8080
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/server"
	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/store"
	"github.com/rohithIlluri/POC-S/pocs/tap/web"
)

func main() {
	addr := flag.String("addr", envOr("ADDR", ":"+envOr("PORT", "8080")), "listen address")
	dev := flag.Bool("dev", false, "dev mode: return OTP codes in responses instead of sending SMS")
	flag.Parse()

	cfg, err := server.DefaultConfig()
	if err != nil {
		log.Fatal(err)
	}
	cfg.DevOTP = *dev
	cfg.Static = web.FS
	srv, err := server.New(cfg, store.NewMemory())
	if err != nil {
		log.Fatal(err)
	}
	if !*dev {
		log.Print("warning: no SMS provider is wired up yet; run with -dev to sign in")
	}
	log.Printf("tapd listening on %s (in-memory store; receipt key is regenerated on restart)", *addr)
	hs := &http.Server{Addr: *addr, Handler: srv, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(hs.ListenAndServe())
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
