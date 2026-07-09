// Command aipet is a local, terminal-native AI-pet companion: it watches how
// you use Claude Code and Codex and helps you spend fewer tokens and work more
// efficiently — all on-device, with no data ever leaving the machine.
//
// Subcommands:
//
//	aipet            launch the interactive pet (TUI)
//	aipet daemon     run the background collector loop (foreground process)
//	aipet status     run one collection cycle and print a summary
//	aipet config     view or set local configuration
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
	"github.com/enterprise/aipet/internal/tui"
	"github.com/enterprise/aipet/internal/version"
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
	case "version", "-v", "--version":
		fmt.Printf("aipet %s\n", version.Version)
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
	fmt.Printf("aipet daemon starting (collect every %dm)…\n", cfg.CollectIntervalMin)
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
	for _, ce := range snap.CollectErrors {
		fmt.Printf("  ! collect: %s\n", ce)
	}
	if len(snap.Suggestions) > 0 {
		fmt.Println("\n  suggestions:")
		shown := 0
		for _, sg := range snap.Suggestions {
			fmt.Printf("   [%s] %s\n", sg.Severity, sg.Title)
			shown++
			if shown >= 5 {
				break
			}
		}
	}
}

func runConfig(cfg config.Config, args []string) {
	if len(args) == 0 {
		p, _ := config.Path()
		fmt.Printf("config file: %s\n", p)
		fmt.Printf("  daily_budget_usd:     %.2f\n", cfg.DailyBudgetUSD)
		fmt.Printf("  collect_interval_min: %d\n", cfg.CollectIntervalMin)
		fmt.Println("\nset values with: aipet config <key> <value>")
		return
	}
	if len(args) != 2 {
		fatalf("usage: aipet config <key> <value>")
	}
	key, val := args[0], args[1]
	switch key {
	case "daily_budget_usd":
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			fatalf("invalid number: %v", err)
		}
		cfg.DailyBudgetUSD = f
	case "collect_interval_min":
		n, err := strconv.Atoi(val)
		if err != nil {
			fatalf("invalid integer: %v", err)
		}
		cfg.CollectIntervalMin = n
	default:
		fatalf("unknown key %q", key)
	}
	if err := cfg.Save(); err != nil {
		fatalf("save config: %v", err)
	}
	fmt.Printf("set %s = %s\n", key, val)
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

func usage() {
	fmt.Print(`aipet — local AI-pet companion (zero data leakage, zero token cost)

usage:
  aipet            launch the interactive pet (TUI)
  aipet daemon     run the background collector loop
  aipet status     collect once and print a summary
  aipet config     show config, or: aipet config <key> <value>
  aipet version    print version

config keys: daily_budget_usd, collect_interval_min
`)
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "aipet: "+format+"\n", a...)
	os.Exit(1)
}
