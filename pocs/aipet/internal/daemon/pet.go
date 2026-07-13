package daemon

import (
	"time"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/care"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/config"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/save"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/species"
	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/store"
)

// runPetTick advances the pet by at most one calendar day, catching up any
// days that were missed while the machine was off — GAME_DESIGN.md §5.4:
// "runs at most one sim tick per calendar day (catch-up ticks if the
// machine was off)". It never runs more than one tick per call for TODAY,
// since today's digest is necessarily partial until the day ends; only
// strictly-past days are caught up in full.
//
// events is the full history (already collected this cycle); cfg supplies
// the soft daily budget for the foraging-cap diet signal.
func runPetTick(events []store.Event, cfg config.Config, now time.Time) (sim.Pet, save.DexState, error) {
	pet, err := save.LoadPet(now)
	if err != nil {
		return sim.Pet{}, save.DexState{}, err
	}
	dex, err := save.LoadDex()
	if err != nil {
		return pet, save.DexState{}, err
	}

	digests := sim.Digests(events)
	byDay := make(map[string]sim.Digest, len(digests))
	for _, d := range digests {
		byDay[d.Day] = d
	}

	today := now.Local().Format("2006-01-02")
	wokeFromSleep := pet.Mood == sim.MoodAsleep

	for _, day := range pendingDays(pet.LastTickDay, pet.EggStartedAt, today, byDay) {
		d, active := byDay[day]
		if !active {
			pet = sim.AdvanceIdle(pet, day)
			continue
		}
		if wokeFromSleep {
			pet = sim.WakeFromHibernation(pet)
			wokeFromSleep = false
		}

		verdict := care.Evaluate(d, 0, 0, cfg.DailyBudgetUSD)
		window := hatchWindow(byDay, pet.EggStartedAt, day)
		result := sim.Tick(pet, day, d, verdict.AsSimVerdict(), window, now)
		pet = result.Pet

		if err := journalDay(day, now, d, verdict, result); err != nil {
			return pet, dex, err
		}

		// Wild encounters roll only for COMPLETED days (strictly before
		// today): the diet verdict — which drives both the tier shift and
		// catch-by-doing — only closes when the day does. Today's triggers
		// resolve on tomorrow's first cycle. Eggs don't encounter: the
		// world starts noticing you once something has hatched.
		if day < today && !pet.IsEgg() {
			if err := rollDayEncounters(&dex, pet, day, d, cfg, now); err != nil {
				return pet, dex, err
			}
		}
	}

	if err := save.SaveDex(dex); err != nil {
		return pet, dex, err
	}
	if err := save.SavePet(pet); err != nil {
		return pet, dex, err
	}
	return pet, dex, nil
}

// rollDayEncounters resolves one completed day's wild encounters and mythic
// gates against the collection, journaling each appearance.
func rollDayEncounters(dex *save.DexState, pet sim.Pet, day string, d sim.Digest, cfg config.Config, now time.Time) error {
	healthy := care.IsHealthyDiet(d, cfg.DailyBudgetUSD)
	view := sim.DexView{
		Caught:          caughtSet(dex),
		WhiffsSinceRare: dex.WhiffsSinceRare,
		GritStreak:      pet.GritStreak,
	}

	encounters, whiffs := sim.RollEncounters(pet.DNA, day, d, healthy, view)
	dex.WhiffsSinceRare = whiffs
	if m := sim.MythicEncounter(day, d, pet.GritStreak, view.Caught); m != nil {
		encounters = append(encounters, *m)
	}

	for _, e := range encounters {
		essence := dex.Record(e.SpeciesID, e.Day, string(e.Rarity), e.Caught)
		if err := journalEncounter(e, essence, now); err != nil {
			return err
		}
	}
	return nil
}

func caughtSet(dex *save.DexState) map[string]bool {
	out := make(map[string]bool, len(dex.Caught))
	for id := range dex.Caught {
		out[id] = true
	}
	return out
}

func journalEncounter(e sim.Encounter, essence int, now time.Time) error {
	name := e.SpeciesID
	if sp, ok := species.ByID(e.SpeciesID); ok {
		name = sp.Name
	}
	entry := save.Entry{Day: e.Day, At: now, Kind: "encounter"}
	switch {
	case e.Mythic:
		entry.VoiceID = "encounter_rare_glimpse"
		entry.Text = "Something impossible happened. " + name + " was there — and stayed."
	case essence > 0:
		entry.VoiceID = "encounter_settled_01"
		entry.Text = "A wild " + name + " appeared — an old friend. Its echo joined the collection."
	case e.Caught:
		entry.VoiceID = "encounter_bold_01"
		entry.Text = "A wild " + name + " appeared — and after a clean day, it joined you!"
	default:
		entry.VoiceID = "encounter_cautious_01"
		entry.Text = "A wild " + name + " appeared… and slipped away. A cleaner day might tempt it."
	}
	return save.AppendJournal(entry)
}

// pendingDays returns every calendar day from the pet's last ticked day (or
// egg start, if never ticked) up to and including today, in ascending
// order — the exact set of days the catch-up loop must visit. Only days
// strictly before today are guaranteed complete; today is included so an
// active day-in-progress still grows the pet, matching how the rest of the
// companion (advisor, leaderboard) already treats "today" as live data.
func pendingDays(lastTickDay string, eggStartedAt time.Time, today string, byDay map[string]sim.Digest) []string {
	start := eggStartedAt.Local().Format("2006-01-02")
	if lastTickDay != "" {
		start = nextDay(lastTickDay)
	}
	if start > today {
		return nil
	}

	var out []string
	for d := start; d <= today; d = nextDay(d) {
		out = append(out, d)
		if d == today {
			break
		}
	}
	return out
}

func nextDay(day string) string {
	t, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		return day
	}
	return t.AddDate(0, 0, 1).Format("2006-01-02")
}

// hatchWindow collects the digests for the HatchWindowDays days up to and
// including `through`, for PickLine's playstyle scoring at hatch time.
func hatchWindow(byDay map[string]sim.Digest, eggStartedAt time.Time, through string) []sim.Digest {
	start := eggStartedAt.Local().Format("2006-01-02")
	var out []sim.Digest
	for d := start; d <= through; d = nextDay(d) {
		if dig, ok := byDay[d]; ok {
			out = append(out, dig)
		}
		if d == through {
			break
		}
	}
	return out
}

func journalDay(day string, now time.Time, d sim.Digest, v care.Verdict, r sim.TickResult) error {
	if r.HatchedNow {
		sp := r.Pet.SpeciesID
		if err := save.AppendJournal(save.Entry{
			Day: day, At: now, Kind: "hatched", VoiceID: "hatch_general_02",
			Text: "Cracked, blinked, looked around. Hatched into " + sp + ".",
		}); err != nil {
			return err
		}
	}
	if r.EvolvedNote != "" {
		if err := save.AppendJournal(save.Entry{
			Day: day, At: now, Kind: "evolved", VoiceID: r.EvolvedNote,
			Text: "Evolved into " + r.Pet.SpeciesID + ".",
		}); err != nil {
			return err
		}
	}
	if !r.Pet.IsEgg() && len(v.Reasons) > 0 {
		if err := save.AppendJournal(save.Entry{
			Day: day, At: now, Kind: "diet", VoiceID: voiceForSignals(v),
			Text: v.Reasons[0],
		}); err != nil {
			return err
		}
	}
	return nil
}

// voiceForSignals maps the day's strongest care signal to a lore voice-line
// id (docs/design/lore.md §6a). Priority order matches how care.Evaluate
// itself orders severity: a foraging cap or bloat day is more notable than
// an ordinary balanced day.
func voiceForSignals(v care.Verdict) string {
	for _, s := range v.Signals {
		switch s {
		case care.ForagingCap:
			return "journal_junk_food_01"
		case care.Overeating:
			return "journal_overeating_01"
		case care.JunkFood:
			return "journal_junk_food_02"
		case care.RichFood:
			return "journal_rich_food_01"
		case care.Fragmented:
			return "journal_fragmented_01"
		case care.Balanced:
			return "journal_balanced_01"
		}
	}
	return ""
}
