// Command aipet is the enterprise AI-spend companion: a local, terminal-native
// "pet" that watches how developers use Claude Code and Codex and proactively
// helps them spend fewer tokens, work more efficiently, and stay current — all
// on-device, with no data leaving the machine.
//
// Subcommands:
//
//	aipet            launch the interactive pet (TUI)
//	aipet daemon     run the background collector loop (foreground process)
//	aipet status     run one collection cycle and print a summary
//	aipet config     view or set local configuration
//	aipet update     check the enterprise feed for a newer version
//	aipet version    print version
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/enterprise/aipet/internal/config"
	"github.com/enterprise/aipet/internal/daemon"
	"github.com/enterprise/aipet/internal/feed"
	"github.com/enterprise/aipet/internal/tui"
)

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	cfg, err := config.Load()
	if err != nil {
		fatalf("load config: %v", err)
	}

	switch cmd {
	case "", "tui", "pet":
		runTUI(cfg)
	case "daemon":
		runDaemon(cfg)
	case "status":
		runStatus(cfg)
	case "config":
		runConfig(cfg, os.Args[2:])
	case "update":
		runUpdate(cfg)
	case "version", "-v", "--version":
		fmt.Printf("aipet %s\n", feed.Version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func runTUI(cfg config.Config) {
	// Kick one cycle so the pet has fresh data even if no daemon is running.
	_, _ = daemon.Run(cfg)
	p := tea.NewProgram(tui.New(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fatalf("tui: %v", err)
	}
}

func runDaemon(cfg config.Config) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	fmt.Printf("aipet daemon starting (poll every %dm)…\n", cfg.PollIntervalMin)
	if err := daemon.Serve(ctx, cfg); err != nil {
		fatalf("daemon: %v", err)
	}
	fmt.Println("aipet daemon stopped.")
}

func runStatus(cfg config.Config) {
	snap, err := daemon.Run(cfg)
	if err != nil {
		fatalf("status: %v", err)
	}
	s := snap.Stats
	fmt.Printf("aipet status — %s\n", snap.UpdatedAt.Format("2006-01-02 15:04"))
	fmt.Printf("  sources:   %s\n", sourcesLine(snap.Sources))
	fmt.Printf("  today:     $%.2f", s.TodayCost)
	if cfg.DailyBudgetUSD > 0 {
		fmt.Printf("  (budget $%.2f)", cfg.DailyBudgetUSD)
	}
	fmt.Printf("\n  all-time:  $%.2f over %d turns\n", s.TotalCost, s.Turns)
	fmt.Printf("  new this run: %d events\n", snap.NewEvents)
	if snap.FeedOK {
		fmt.Printf("  feed:      ok (%d tips)\n", len(snap.Tips))
	} else {
		fmt.Printf("  feed:      unavailable (%s)\n", snap.FeedError)
	}
	if len(snap.Suggestions) > 0 {
		fmt.Println("\n  suggestions:")
		shown := 0
		for _, sg := range snap.Suggestions {
			if sg.Source == "feed" {
				continue
			}
			fmt.Printf("   [%s] %s\n", sg.Severity, sg.Title)
			shown++
			if shown >= 5 {
				break
			}
		}
	}
	if snap.UpdateAvailable && snap.UpdateInfo != nil {
		fmt.Printf("\n  ⬆ update available: v%s — run `aipet update`\n", snap.UpdateInfo.LatestVersion)
	}
}

func runConfig(cfg config.Config, args []string) {
	if len(args) == 0 {
		p, _ := config.Path()
		fmt.Printf("config file: %s\n", p)
		fmt.Printf("  feed_url:          %q\n", cfg.FeedURL)
		fmt.Printf("  feed_public_key:   %s\n", maskKey(cfg.FeedPublicKey))
		fmt.Printf("  daily_budget_usd:  %.2f\n", cfg.DailyBudgetUSD)
		fmt.Printf("  poll_interval_min: %d\n", cfg.PollIntervalMin)
		fmt.Println("\nset values with: aipet config <key> <value>")
		return
	}
	if len(args) != 2 {
		fatalf("usage: aipet config <key> <value>")
	}
	key, val := args[0], args[1]
	switch key {
	case "feed_url":
		cfg.FeedURL = val
	case "feed_public_key":
		cfg.FeedPublicKey = val
	case "daily_budget_usd":
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			fatalf("invalid number: %v", err)
		}
		cfg.DailyBudgetUSD = f
	case "poll_interval_min":
		n, err := strconv.Atoi(val)
		if err != nil {
			fatalf("invalid integer: %v", err)
		}
		cfg.PollIntervalMin = n
	default:
		fatalf("unknown key %q", key)
	}
	if err := cfg.Save(); err != nil {
		fatalf("save config: %v", err)
	}
	fmt.Printf("set %s = %s\n", key, val)
}

func runUpdate(cfg config.Config) {
	snap, err := daemon.Run(cfg)
	if err != nil {
		fatalf("update check: %v", err)
	}
	if !snap.FeedOK {
		fatalf("feed unavailable: %s", snap.FeedError)
	}
	if !snap.UpdateAvailable || snap.UpdateInfo == nil {
		fmt.Printf("aipet is up to date (v%s).\n", feed.Version)
		return
	}
	u := snap.UpdateInfo
	fmt.Printf("Update available: v%s (current v%s)\n", u.LatestVersion, feed.Version)
	if u.Notes != "" {
		fmt.Printf("  notes: %s\n", u.Notes)
	}
	fmt.Printf("  download: %s\n", u.DownloadURL)
	fmt.Println("\nThis POC reports updates but does not self-replace the binary.")
	fmt.Println("In production the daemon would download, verify the SHA-256, and swap atomically.")
}

func sourcesLine(m map[string]bool) string {
	if len(m) == 0 {
		return "none detected"
	}
	out := ""
	for k := range m {
		if out != "" {
			out += ", "
		}
		out += k
	}
	return out
}

func maskKey(k string) string {
	if k == "" {
		return "(none — feed verification disabled)"
	}
	if len(k) <= 8 {
		return "****"
	}
	return k[:4] + "…" + k[len(k)-4:]
}

func usage() {
	fmt.Print(`aipet — enterprise AI-spend companion (local, zero data leakage)

usage:
  aipet            launch the interactive pet (TUI)
  aipet daemon     run the background collector loop
  aipet status     collect once and print a summary
  aipet config     show config, or: aipet config <key> <value>
  aipet update     check the enterprise feed for a newer version
  aipet version    print version

config keys: feed_url, feed_public_key, daily_budget_usd, poll_interval_min
`)
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "aipet: "+format+"\n", a...)
	os.Exit(1)
}
