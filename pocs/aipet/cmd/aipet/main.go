// Command aipet is a local, terminal-native AI-pet companion: it watches how
// you use Claude Code and Codex and helps you spend fewer tokens and work more
// efficiently — all on-device, with no data ever leaving the machine.
//
// The primary surface (see docs/design/HOST_INTEGRATION.md) is now `/aipet`
// inside Claude Code or Codex, installed once via the bare `aipet` command:
//
//	aipet              first run: setup wizard; later: pet card + a hint
//	aipet tui          the full interactive app
//	aipet setup        install/inspect/remove the host integration
//	aipet version      print version
//
// `aipet card`, `aipet statusline`, and `aipet collect` are plumbing the
// installed integration calls; a human rarely types them directly.
// `daemon`/`status`/`dex`/`leaderboard`/`config` still work exactly as
// before but are no longer the front door — see usage() and R9 in the plan.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/daemon"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/host"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/leaderboard"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/llm"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/save"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/tui"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/version"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/voice"
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
	case "":
		runBare(cfg)
	case "tui", "pet":
		runTUI(cfg)
	case "daemon":
		runDaemon(cfg)
	case "status":
		runStatus(cfg)
	case "leaderboard", "board", "lb":
		runLeaderboard(cfg, os.Args[2:])
	case "dex":
		runDex(cfg)
	case "config":
		runConfig(cfg, os.Args[2:])
	case "card":
		runCard(cfg, os.Args[2:])
	case "collect":
		runCollect(cfg, os.Args[2:])
	case "statusline":
		runStatusLine(cfg)
	case "setup":
		runSetup(os.Args[2:])
	case "trade":
		runTrade(os.Args[2:])
	case "battle":
		runBattle(os.Args[2:])
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

// runBare implements the R9 dispatch for `aipet` with no arguments — the
// only command most users ever type in a shell (the README one-liner):
//
//   - no ~/.aipet/setup.json yet: offer to install the host integration.
//     Interactively (a real terminal) this is a yes/no prompt; piped/
//     scripted (no TTY) it must never block waiting for input that will
//     never come, so it just prints the manual steps and exits cleanly.
//   - already installed: print the pet card plus a one-line hint pointing
//     at /aipet — bare `aipet` is no longer the primary way to see the pet.
func runBare(cfg config.Config) {
	_, installed, err := host.LoadManifest()
	if err != nil {
		fatalf("load setup state: %v", err)
	}
	if !installed {
		runFirstRunWizard()
		return
	}

	_, _, _ = daemon.CollectOnce(cfg, false, time.Now())
	snap, _ := daemon.ReadSnapshot()
	journal, _ := save.ReadJournal()
	out, err := tui.Card("pet", snap, journal, tui.CardOpts{Personality: cfg.Personality, Voice: cfg.Voice, VoiceModel: cfg.VoiceModel})
	if err != nil {
		fatalf("card: %v", err)
	}
	fmt.Println(out)
	fmt.Println()
	fmt.Println("type /aipet inside Claude Code · full app: aipet tui")
}

// runFirstRunWizard is bare `aipet`'s first-ever invocation on a machine: it
// offers to install the host integration so the README one-liner
// (`go install ... && aipet`) is genuinely the whole setup. A non-interactive
// invocation (piped stdin — CI, a script, a non-terminal harness) must never
// block on a prompt nobody can answer, so it prints the same instructions
// and returns instead of asking.
func runFirstRunWizard() {
	fmt.Println("aipet grows a coding companion (a \"Codeling\") from your real Claude Code")
	fmt.Println("and Codex session activity — cache reuse, model routing, session hygiene.")
	fmt.Println("Nothing to configure, no network, no tokens spent.")
	fmt.Println()

	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stdout.Fd()) {
		printManualSetupInstructions()
		return
	}

	fmt.Print("Install /aipet into Claude Code / Codex? [Y/n] ")
	answer, _ := readLine(os.Stdin)
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "" && answer != "y" && answer != "yes" {
		fmt.Println()
		printManualSetupInstructions()
		return
	}

	fmt.Println()
	runSetup(nil)
}

// readLine reads one line from r without pulling in bufio.Scanner's 64KB
// default limits or a full bufio.Reader dependency elsewhere in main — a
// user's yes/no answer is at most a few bytes.
func readLine(r io.Reader) (string, error) {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				return b.String(), nil
			}
			b.WriteByte(buf[0])
		}
		if err != nil {
			return b.String(), err
		}
	}
}

// printManualSetupInstructions is shown when the wizard can't (or the user
// chose not to) run interactively.
func printManualSetupInstructions() {
	fmt.Println("Run `aipet setup` any time to install /aipet into Claude Code and/or Codex.")
	fmt.Println("  aipet setup --print    preview exactly what it would write")
	fmt.Println("  aipet tui              open the full interactive app now")
}

// runSetup implements `aipet setup [--claude] [--codex] [--remove] [--print]`.
func runSetup(args []string) {
	var opts host.Options
	remove := false
	for _, a := range args {
		switch a {
		case "--claude":
			opts.Claude = true
		case "--codex":
			opts.Codex = true
		case "--print":
			opts.Print = true
		case "--remove":
			remove = true
		default:
			fmt.Fprintf(os.Stderr, "usage: aipet setup [--claude] [--codex] [--remove] [--print]\n")
			os.Exit(2)
		}
	}

	if remove {
		res, err := host.Remove()
		if err != nil {
			fatalf("setup --remove: %v", err)
		}
		for _, n := range res.Notes {
			fmt.Println(n)
		}
		return
	}

	res, err := host.Install(opts)
	if err != nil {
		if _, ok := err.(*host.AbortError); ok {
			fmt.Fprintf(os.Stderr, "aipet setup: %v\n", err)
			os.Exit(1)
		}
		fatalf("setup: %v", err)
	}
	for _, n := range res.Notes {
		fmt.Println(n)
	}
	if res.WindowsSkip || res.NoHosts || opts.Print {
		return
	}
	fmt.Println()
	fmt.Println("Done. Type /aipet inside Claude Code (or Codex) to see your pet.")
	fmt.Println("Undo any time with: aipet setup --remove")
}

func runDex(cfg config.Config) {
	// One collect cycle first so freshly-completed days roll their encounters
	// before we print — same "fresh data without a daemon" behavior as the TUI.
	_, _ = daemon.Run(cfg)
	dex, err := save.LoadDex()
	if err != nil {
		fatalf("load dex: %v", err)
	}
	fmt.Println(tui.RenderDex(dex, 80))
}

// runCard implements `aipet card [view] [--width N] [--no-collect]`, the
// one-shot plain-text renderer hosts embed in chat (the Claude Code slash
// command and the Codex prompt both shell out to this). It runs a normal
// (non-forced) CollectOnce first so the card is fresh without a daemon —
// same "collect before showing yourself" contract the TUI and `aipet dex`
// already follow — but a failed collect must never block the render: the
// card falls back to whatever snapshot already exists on disk.
func runCard(cfg config.Config, args []string) {
	view := ""
	width := 0
	noCollect := false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--no-collect":
			noCollect = true
		case a == "--width":
			i++
			if i >= len(args) {
				fatalf("--width requires a value")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil {
				fatalf("invalid --width: %v", err)
			}
			width = n
		case strings.HasPrefix(a, "--width="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--width="))
			if err != nil {
				fatalf("invalid --width: %v", err)
			}
			width = n
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "usage: aipet card [pet|dex|records|overview] [--width N] [--no-collect]\n")
			os.Exit(2)
		case view == "":
			view = a
		default:
			fmt.Fprintf(os.Stderr, "usage: aipet card [pet|dex|records|overview] [--width N] [--no-collect]\n")
			os.Exit(2)
		}
	}

	if !noCollect {
		// Errors are intentionally ignored here: a card render must degrade
		// to the last known snapshot rather than fail a chat turn because a
		// session log was briefly unreadable.
		_, _, _ = daemon.CollectOnce(cfg, false, time.Now())
	}

	snap, err := daemon.ReadSnapshot()
	if err != nil {
		snap = nil // no snapshot on disk at all yet — Card renders its cold-start copy for this
	}
	journal, _ := save.ReadJournal()

	out, err := tui.Card(view, snap, journal, tui.CardOpts{Width: width, Personality: cfg.Personality, Voice: cfg.Voice, VoiceModel: cfg.VoiceModel})
	if err != nil {
		fmt.Fprintf(os.Stderr, "aipet: %v\n\n", err)
		fmt.Fprintf(os.Stderr, "usage: aipet card [pet|dex|records|overview] [--width N] [--no-collect]\n")
		os.Exit(2)
	}
	fmt.Println(out)
}

// runCollect implements `aipet collect [--quiet] [--force]`, the hook-facing
// entry point (Claude Code's Stop/SessionStart hooks run `aipet collect
// --quiet`). --quiet means genuinely silent on success — a hook's stdout
// becomes noise in the host — but errors still go to stderr so a broken
// install is debuggable.
func runCollect(cfg config.Config, args []string) {
	quiet := false
	force := false
	for _, a := range args {
		switch a {
		case "--quiet":
			quiet = true
		case "--force":
			force = true
		default:
			fmt.Fprintf(os.Stderr, "usage: aipet collect [--quiet] [--force]\n")
			os.Exit(2)
		}
	}

	snap, ran, err := daemon.CollectOnce(cfg, force, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "aipet: collect: %v\n", err)
		os.Exit(1)
	}
	if quiet || !ran {
		return
	}
	fmt.Println(daemon.HeartbeatLine(snap))
}

// runStatusLine implements `aipet statusline`, Claude Code's statusLine
// hook: it runs on every render of every session, so it must be fast and it
// must never hang or crash a session's UI over pet state.
//
// Two host-integration constraints drive its shape (§4.2, R4):
//   - it reads ONLY the last published snapshot — never collects, never
//     scans logs — so latency is a single stat+read regardless of how much
//     session history exists;
//   - any failure (missing snapshot, unreadable config) degrades to the
//     friendly no-pet line and exit 0, never a non-zero exit or stderr
//     noise that could surface as a broken statusline in the host UI.
func runStatusLine(cfg config.Config) {
	// Claude Code pipes session JSON on stdin for the statusline command to
	// (optionally) read; we don't use it, but an unread pipe can leave the
	// host's writer blocked, so it must be drained. A human invoking this
	// directly in a terminal has no pipe behind stdin at all — do not read
	// there, or the command would hang waiting for input that will never
	// come.
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		_, _ = io.Copy(io.Discard, os.Stdin)
	}

	snap, err := daemon.ReadSnapshot()
	if err != nil {
		fmt.Println(tui.StatusLine(nil, cfg.DailyBudgetUSD))
		return
	}
	fmt.Println(tui.StatusLine(snap, cfg.DailyBudgetUSD))
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
	// R9: hooks installed via `aipet setup` fully replace the daemon for
	// Claude Code users (§5 of the plan) — this still works for anyone who
	// wants it (or doesn't use Claude Code), but the note keeps people who
	// stumble onto it from running two collection paths without realizing
	// there's a simpler option.
	fmt.Println("note: with /aipet installed, hooks replace the daemon — see aipet setup")
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
	if len(snap.Sources) == 0 {
		printColdStart(cfg)
		return
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
	printPetStatusLine(snap)
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

// printColdStart runs when no Claude Code / Codex session logs have ever
// been found. A brand-new user running `aipet status` on an empty machine
// otherwise sees an all-zero spend report and has no idea a pet/game exists
// at all — this makes the concept and the next step discoverable.
func printColdStart(cfg config.Config) {
	fmt.Println("No Claude Code or Codex sessions found yet on this machine.")
	fmt.Println()
	fmt.Println("aipet grows a coding companion (a \"Codeling\") from your real session")
	fmt.Println("activity — cache reuse, model routing, session hygiene. Nothing to")
	fmt.Println("configure, no network, no tokens spent.")
	fmt.Println()
	if p, ok, _ := save.TryLoadPet(); ok && p.IsEgg() {
		fmt.Println("Your egg is already warming, waiting for that activity to begin.")
	} else {
		fmt.Println("An egg will start warming the moment you run aipet again with some")
		fmt.Println("Claude Code or Codex activity on disk.")
	}
	fmt.Println()
	fmt.Println("  aipet          open the pet (grows itself while it's open)")
	fmt.Println("  aipet daemon   grow it in the background instead")
	fmt.Println()
	fmt.Println("Start (or keep) coding with Claude Code or Codex and run this again.")
	_ = cfg // no config-dependent copy yet, kept for signature symmetry with other run* funcs
}

// printPetStatusLine gives `aipet status` a one-line pet summary so the
// companion stays visible even for users who never open the TUI.
func printPetStatusLine(snap *daemon.Snapshot) {
	if snap.PetError != "" {
		return
	}
	p := snap.Pet
	fmt.Println()
	if p.IsEgg() {
		fmt.Printf("  pet:       egg warming — %d/%d qualifying sessions (run `aipet` to watch)\n",
			min(p.EggSessionCount, sim.HatchSessionThreshold), sim.HatchSessionThreshold)
		return
	}
	sp, ok := species.ByID(p.SpeciesID)
	name := p.SpeciesID
	if ok {
		name = sp.Name
	}
	fmt.Printf("  pet:       %s (level %d, %s) — run `aipet` to see it\n", name, p.Level, p.Mood)
}

func runLeaderboard(cfg config.Config, args []string) {
	snap, err := daemon.Run(cfg)
	if err != nil {
		fatalf("leaderboard: %v", err)
	}
	b := snap.Board

	if len(args) > 0 && args[0] == "--json" {
		out, err := json.MarshalIndent(b, "", "  ")
		if err != nil {
			fatalf("leaderboard: %v", err)
		}
		fmt.Println(string(out))
		return
	}

	fmt.Println("aipet leaderboard — everything below is computed locally")
	printRanking("Top projects (lifetime $)", b.TopProjects, func(e leaderboard.Entry) string {
		return fmt.Sprintf("$%.2f", e.Value)
	})
	printRanking("Top models (lifetime $)", b.TopModels, func(e leaderboard.Entry) string {
		return fmt.Sprintf("$%.2f", e.Value)
	})
	printRanking("Best cache-reuse days", b.BestCacheDays, func(e leaderboard.Entry) string {
		return fmt.Sprintf("%.1f%%  (%s)", e.Value, e.Detail)
	})

	r := b.Records
	fmt.Println("\n  Personal records")
	fmt.Printf("    streak:        %d day(s) now · best %d\n", r.CurrentStreak, r.LongestStreak)
	if r.BiggestDayUSD.Name != "" {
		fmt.Printf("    biggest day:   $%.2f on %s\n", r.BiggestDayUSD.Value, r.BiggestDayUSD.Name)
	}
	if r.BusiestDay.Name != "" {
		fmt.Printf("    busiest day:   %.0f turns on %s\n", r.BusiestDay.Value, r.BusiestDay.Name)
	}
	if r.BestCacheDay.Name != "" {
		fmt.Printf("    best cache:    %.1f%% reuse on %s\n", r.BestCacheDay.Value, r.BestCacheDay.Name)
	}
	fmt.Printf("    lifetime:      $%.2f over %d turns, %d active day(s) since %s\n",
		r.LifetimeSpend, r.TotalTurns, r.ActiveDays, r.FirstSeen)
}

func printRanking(title string, entries []leaderboard.Entry, val func(leaderboard.Entry) string) {
	fmt.Printf("\n  %s\n", title)
	if len(entries) == 0 {
		fmt.Println("    (no qualifying data yet)")
		return
	}
	for i, e := range entries {
		fmt.Printf("    %d. %-32s %s\n", i+1, trunc(e.Name, 32), val(e))
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func runConfig(cfg config.Config, args []string) {
	if len(args) == 0 {
		p, _ := config.Path()
		fmt.Printf("config file: %s\n", p)
		fmt.Printf("  daily_budget_usd:     %.2f\n", cfg.DailyBudgetUSD)
		fmt.Printf("  collect_interval_min: %d\n", cfg.CollectIntervalMin)
		fmt.Printf("  personality:          %s   (%s)\n", cfg.Personality, strings.Join(voice.Personalities(), " | "))
		fmt.Printf("  voice:                %s   (canned = zero-token embedded lines | api = aipet generates ~once/day on your Anthropic credentials | live = host improvises | off)\n", cfg.Voice)
		model := cfg.VoiceModel
		if model == "" {
			model = llm.DefaultModel + " (default)"
		}
		fmt.Printf("  voice_model:          %s   (api mode only; cheapest model unless you override)\n", model)
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
	case "personality":
		if !voice.Valid(val) {
			fatalf("unknown personality %q (want: %s)", val, strings.Join(voice.Personalities(), ", "))
		}
		cfg.Personality = val
	case "voice":
		switch val {
		case "canned", "api", "live", "off":
			cfg.Voice = val
		default:
			fatalf("unknown voice mode %q (want: canned, api, live, off)", val)
		}
		if val == "api" {
			fmt.Println("api voice generates the pet's daily line with YOUR Anthropic credentials")
			fmt.Println("(ANTHROPIC_API_KEY or an `ant auth login` profile) on " + llm.DefaultModel + ":")
			fmt.Println("~1 call/day, ≤60 output tokens, hard-capped, cached, and it falls back to")
			fmt.Println("the built-in lines whenever no credentials or network are available.")
		}
	case "voice_model":
		cfg.VoiceModel = val
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
  aipet              first run: install /aipet; later: pet card + a hint
  /aipet [view]      inside Claude Code or Codex — pet | dex | records | overview
  aipet tui          the full interactive app
  aipet setup        install/inspect/remove the host integration
  aipet trade        export/import .codeling files · barn
  aipet battle       replay a deterministic battle from .codeling cards
                       --claude / --codex   restrict to one host
                       --print              preview writes without touching disk
                       --remove             undo a previous setup
  aipet version      print version
`)
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "aipet: "+format+"\n", a...)
	os.Exit(1)
}
