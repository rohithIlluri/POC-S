package main

import (
	"fmt"
	"os"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/battle"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/codeling"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/save"
)

// runTrade implements `aipet trade export <file>` / `aipet trade import
// <file>` / `aipet trade barn` — GAME_DESIGN §4.7. Export writes the
// active pet as a .codeling; import hardens the file and stables the pet
// in the Barn (one active pet at a time, the Barn holds the rest).
func runTrade(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: aipet trade export <file.codeling> | import <file.codeling> | barn")
		os.Exit(2)
	}
	switch args[0] {
	case "export":
		if len(args) != 2 {
			fatalf("usage: aipet trade export <file.codeling>")
		}
		p, err := save.LoadPet(time.Now())
		if err != nil {
			fatalf("load pet: %v", err)
		}
		f, err := codeling.Export(p, "Raised from real coding sessions by its keeper.")
		if err != nil {
			fatalf("export: %v", err)
		}
		f.ExportedAt = time.Now().UTC().Format("2006-01-02")
		if err := codeling.WriteFile(args[1], f); err != nil {
			fatalf("write: %v", err)
		}
		fmt.Printf("%s exported to %s — share it anywhere; import with `aipet trade import`.\n", f.Card.Name(), args[1])

	case "import":
		if len(args) != 2 {
			fatalf("usage: aipet trade import <file.codeling>")
		}
		res, err := codeling.Import(args[1])
		if err != nil {
			fatalf("import: %v", err)
		}
		if res.Counterfeit {
			fmt.Println("⚠ counterfeit: this card's signature doesn't match its contents — someone edited it.")
		}
		for _, adj := range res.Adjustments {
			fmt.Println("  adjusted:", adj)
		}
		entries, err := save.LoadBarn()
		if err != nil {
			fatalf("load barn: %v", err)
		}
		entries = append(entries, save.BarnEntry{
			Card:       res.File.Card,
			History:    res.File.History,
			Traveler:   true,
			ImportedAt: time.Now().UTC().Format("2006-01-02"),
		})
		if err := save.SaveBarn(entries); err != nil {
			fatalf("save barn: %v", err)
		}
		fmt.Printf("%s joins your Barn (%d resident(s)). Battle it with `aipet battle %s`.\n",
			res.File.Card.Name(), len(entries), args[1])

	case "barn":
		entries, err := save.LoadBarn()
		if err != nil {
			fatalf("load barn: %v", err)
		}
		if len(entries) == 0 {
			fmt.Println("The Barn is empty — import a friend's .codeling with `aipet trade import <file>`.")
			return
		}
		fmt.Printf("The Barn — %d resident(s):\n", len(entries))
		for i, e := range entries {
			badge := ""
			if e.Traveler {
				badge = " ✈ traveler"
			}
			fmt.Printf("  %d. %-12s lv %-3d%s  (arrived %s)\n", i+1, e.Card.Name(), e.Card.Level, badge, e.ImportedAt)
		}

	default:
		fatalf("unknown trade subcommand %q (want: export, import, barn)", args[0])
	}
}

// runBattle implements `aipet battle <a.codeling> [b.codeling]` —
// GAME_DESIGN §4.6's serverless battle. With one file, the challenger
// fights YOUR active pet; with two, the files fight each other. Both
// machines replay the identical battle from the same cards and UTC date.
func runBattle(args []string) {
	if len(args) < 1 || len(args) > 2 {
		fatalf("usage: aipet battle <challenger.codeling> [opponent.codeling]")
	}

	loadCard := func(path string) battle.Card {
		res, err := codeling.Import(path)
		if err != nil {
			fatalf("%s: %v", path, err)
		}
		if res.Counterfeit {
			fmt.Printf("⚠ %s is counterfeit (edited after signing) — battling anyway, honor is gone.\n", path)
		}
		return res.File.Card
	}

	var a, b battle.Card
	if len(args) == 2 {
		a, b = loadCard(args[0]), loadCard(args[1])
	} else {
		p, err := save.LoadPet(time.Now())
		if err != nil {
			fatalf("load pet: %v", err)
		}
		mine, err := codeling.Export(p, "")
		if err != nil {
			fatalf("your pet can't battle yet: %v", err)
		}
		a, b = mine.Card, loadCard(args[0])
	}

	date := time.Now().UTC().Format("2006-01-02")
	res, err := battle.Fight(a, b, date)
	if err != nil {
		fatalf("battle: %v", err)
	}

	fmt.Printf("=== %s vs %s — %s (both machines replay this identically) ===\n\n",
		a.Name(), b.Name(), date)
	for _, line := range res.Log {
		fmt.Println(line)
	}
	fmt.Println()
	switch {
	case res.Winner < 0:
		fmt.Println("Result: DRAW")
	default:
		fmt.Printf("Result: %s wins in %d turns.\n", res.Cards[res.Winner].Name(), res.Turns)
	}
}
