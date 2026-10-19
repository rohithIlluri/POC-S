// Command tapd runs the Tap API and serves the browser demo at /.
//
//	go run ./backend/cmd/tapd -dev                       # in-memory, http://localhost:8080
//	eval "$(go run ./backend/cmd/tapd -gen-secrets)"      # once, for a persistent database
//	DATABASE_URL=postgres://… go run ./backend/cmd/tapd -dev
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/server"
	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/store"
	"github.com/rohithIlluri/POC-S/pocs/tap/protocol"
	"github.com/rohithIlluri/POC-S/pocs/tap/web"
)

func main() {
	addr := flag.String("addr", envOr("ADDR", ":"+envOr("PORT", "8080")), "listen address")
	dev := flag.Bool("dev", false, "dev mode: return OTP codes in responses instead of sending SMS")
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "Postgres URL (empty: in-memory store)")
	gen := flag.Bool("gen-secrets", false, "print a fresh TAP_PHONE_SALT and TAP_RECEIPT_KEY as shell exports, then exit")
	flag.Parse()

	if *gen {
		if err := genSecrets(); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg, err := server.DefaultConfig()
	if err != nil {
		log.Fatal(err)
	}
	cfg.DevOTP = *dev
	cfg.Static = web.FS

	var st store.Store = store.NewMemory()
	if *dbURL != "" {
		// A persistent store needs stable secrets: the salt finds returning
		// users by phone, the key keeps old receipts verifiable.
		if cfg.PhoneSalt, cfg.ReceiptKey, err = loadSecrets(); err != nil {
			log.Fatalf("%v\nrun `tapd -gen-secrets` once and keep its output with your database credentials", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		pg, err := store.OpenPostgres(ctx, *dbURL)
		cancel()
		if err != nil {
			log.Fatalf("postgres: %v", err)
		}
		defer pg.Close()
		st = pg
		log.Print("store: postgres (migrations applied)")
	} else {
		log.Print("store: in-memory (data and receipt key are lost on restart)")
	}

	srv, err := server.New(cfg, st)
	if err != nil {
		log.Fatal(err)
	}
	if !*dev {
		log.Print("warning: no SMS provider is wired up yet; run with -dev to sign in")
	}
	log.Printf("tapd listening on %s", *addr)
	hs := &http.Server{Addr: *addr, Handler: srv, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := hs.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	// Cloud Run sends SIGTERM and allows 10 s: finish in-flight claims (they
	// wait up to 10 s for a partner), then drain the event worker's queue.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	<-stop
	log.Print("shutting down: finishing requests, draining event queue")
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
	defer cancel()
	if err := hs.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	srv.Close()
	log.Print("stopped")
}

func genSecrets() error {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	key, err := protocol.GenerateDeviceKey()
	if err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	fmt.Printf("export TAP_PHONE_SALT=%s\nexport TAP_RECEIPT_KEY=%s\n", hex.EncodeToString(salt), protocol.B64.EncodeToString(der))
	return nil
}

func loadSecrets() ([]byte, *ecdsa.PrivateKey, error) {
	saltHex, keyB64 := os.Getenv("TAP_PHONE_SALT"), os.Getenv("TAP_RECEIPT_KEY")
	if saltHex == "" || keyB64 == "" {
		return nil, nil, errors.New("DATABASE_URL is set but TAP_PHONE_SALT or TAP_RECEIPT_KEY is missing")
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil || len(salt) < 16 {
		return nil, nil, errors.New("TAP_PHONE_SALT must be at least 16 bytes of hex")
	}
	der, err := protocol.B64.DecodeString(keyB64)
	if err != nil {
		return nil, nil, fmt.Errorf("TAP_RECEIPT_KEY: %w", err)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, nil, fmt.Errorf("TAP_RECEIPT_KEY: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("TAP_RECEIPT_KEY must be a P-256 ECDSA key")
	}
	return salt, key, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
