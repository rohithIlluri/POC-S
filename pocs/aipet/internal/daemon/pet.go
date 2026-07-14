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

// runPetTick advances the pet through every day of activity implied by
// events, up to and including "today." Strictly-past days are ticked once
// each and sealed permanently (GAME_DESIGN.md §5.4: "runs at most one sim
// tick per calendar day"). "Today" is different: it may be re-processed
// many times in a single calendar day (the TUI's background collector runs
// every couple of minutes, `aipet daemon` runs continuously, `aipet
// status` can be invoked repeatedly) as new activity keeps arriving before
// midnight closes the day out. Tick's own cumulative fields (ActiveDayCount,
// GritStreak, XP, EggSessionCount, Stats) only tolerate being applied ONCE
// per calendar day, so "today" is always replayed from a sealed
// end-of-yesterday snapshot (pet.PreTodayPet) rather than accumulated onto
// directly — see sim.Pet's PreTodayPet/PreTodayDay doc comment for the full
// contract this maintains.
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

	// 1. Seal every strictly-past pending day onto the persisted pet, exactly
	// once each, same as before. After this loop `pet` is a true end-of-
	// yesterday state — it becomes the baseline "today" replays from.
	sealedAny := false
	for _, day := range pastPendingDays(pet.LastTickDay, pet.EggStartedAt, today, byDay) {
		d, active := byDay[day]
		if !active {
			pet = sim.AdvanceIdle(pet, day)
			continue
		}
		wokeFromSleep := pet.Mood == sim.MoodAsleep
		if wokeFromSleep {
			pet = sim.WakeFromHibernation(pet)
		}
		verdict := care.Evaluate(d, 0, 0, cfg.DailyBudgetUSD)
		window := hatchWindow(byDay, pet.EggStartedAt, day)
		result := sim.Tick(pet, day, d, verdict.AsSimVerdict(), window, now)
		pet = result.Pet
		sealedAny = true
		if err := journalDay(day, now, d, verdict, result); err != nil {
			return pet, dex, err
		}
	}

	// 2. Establish/refresh the sealed pre-today baseline. If a new calendar
	// day has started since we last replayed today (or this is the very
	// first tick ever), the pet as it stands right now — after step 1's
	// seal — IS the clean end-of-yesterday baseline.
	if pet.PreTodayDay != today {
		baseline := pet // shallow copy is fine: PreTodayPet is not set on pet yet
		baseline.PreTodayPet = nil
		baseline.PreTodayDay = ""
		pet.PreTodayPet = &baseline
		pet.PreTodayDay = today
	}

	// 3. Replay "today" fresh from the sealed baseline every cycle, so
	// re-collecting later the same day reflects new activity instead of
	// silently no-op'ing. This is a REPLACE of today's state, not an
	// accumulation: ActiveDayCount/GritStreak/XP/EggSessionCount/Stats each
	// advance by exactly one day's worth no matter how many times this
	// cycle runs today.
	todayBaseline := *pet.PreTodayPet
	replayed := todayBaseline
	if d, active := byDay[today]; active {
		wokeFromSleep := replayed.Mood == sim.MoodAsleep
		if wokeFromSleep {
			replayed = sim.WakeFromHibernation(replayed)
		}
		verdict := care.Evaluate(d, 0, 0, cfg.DailyBudgetUSD)
		window := hatchWindow(byDay, replayed.EggStartedAt, today)
		result := sim.Tick(replayed, today, d, verdict.AsSimVerdict(), window, now)
		replayed = result.Pet
		if err := journalDayOnce(today, now, d, verdict, result); err != nil {
			return pet, dex, err
		}
	} else if sealedAny || replayed.LastTickDay != today {
		// No activity today (yet): only advance idle bookkeeping once per
		// day, mirroring the old idle-day semantics, not on every re-run.
		replayed = sim.AdvanceIdle(replayed, today)
	}
	replayed.PreTodayPet = pet.PreTodayPet
	replayed.PreTodayDay = today
	pet = replayed

	// Wild-encounter sweep, decoupled from the pet tick: a day's encounters
	// roll only once the day is COMPLETE (its diet verdict — which drives
	// both the tier shift and catch-by-doing — closes at midnight), so the
	// sweep lags one day behind and advances its own cursor. Eggs don't
	// encounter: the world starts noticing you once something has hatched.
	if !pet.IsEgg() {
		hatchDay := pet.HatchedAt.Local().Format("2006-01-02")
		for _, d := range digests {
			if d.Day >= today || d.Day <= dex.LastEncounterDay || d.Day < hatchDay {
				continue
			}
			if err := rollDayEncounters(&dex, pet, d.Day, d, cfg, now); err != nil {
				return pet, dex, err
			}
			dex.LastEncounterDay = d.Day
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

// pastPendingDays returns every calendar day STRICTLY BEFORE today, from
// the pet's last ticked day (or egg start, if never ticked), in ascending
// order — the days the seal loop must visit exactly once each. "Today"
// itself is deliberately excluded: it's handled separately by the
// replay-from-baseline step in runPetTick, which is safe to re-run.
func pastPendingDays(lastTickDay string, eggStartedAt time.Time, today string, byDay map[string]sim.Digest) []string {
	start := eggStartedAt.Local().Format("2006-01-02")
	if lastTickDay != "" {
		start = nextDay(lastTickDay)
	}
	if start >= today {
		return nil
	}

	var out []string
	for d := start; d < today; d = nextDay(d) {
		out = append(out, d)
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

// journalDayOnce is journalDay for "today," which — unlike sealed past
// days — can be ticked many times in the same calendar day as the replay
// step re-runs. Without de-duplication every re-collection would append a
// fresh "hatched into X" / "evolved into X" / diet line, spamming the
// journal. Entries are de-duplicated by (day, kind) against what's already
// on disk; "diet" is allowed to have at most one entry per day too, since
// it's meant to be that day's single summary, not a running log.
func journalDayOnce(day string, now time.Time, d sim.Digest, v care.Verdict, r sim.TickResult) error {
	existing, err := save.ReadJournal()
	if err != nil {
		return err
	}
	already := make(map[string]bool, len(existing))
	for _, e := range existing {
		if e.Day == day {
			already[e.Kind] = true
		}
	}

	if r.HatchedNow && !already["hatched"] {
		sp := r.Pet.SpeciesID
		if err := save.AppendJournal(save.Entry{
			Day: day, At: now, Kind: "hatched", VoiceID: "hatch_general_02",
			Text: "Cracked, blinked, looked around. Hatched into " + sp + ".",
		}); err != nil {
			return err
		}
	}
	if r.EvolvedNote != "" && !already["evolved"] {
		if err := save.AppendJournal(save.Entry{
			Day: day, At: now, Kind: "evolved", VoiceID: r.EvolvedNote,
			Text: "Evolved into " + r.Pet.SpeciesID + ".",
		}); err != nil {
			return err
		}
	}
	if !r.Pet.IsEgg() && len(v.Reasons) > 0 && !already["diet"] {
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
