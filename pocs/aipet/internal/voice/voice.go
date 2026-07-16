// Package voice is the pet's embedded phrasebook: pre-written one-liners
// keyed by personality and mood, selected deterministically. This is what
// lets /aipet stay entertaining at ZERO inference cost — the host model
// only displays a line that already exists in the binary, it never has to
// generate one (unless the user opts into "live" voice, which is the only
// mode that spends their tokens on the pet).
//
// Flavor only: nothing in here reads or affects the sim. Lines follow the
// lore.md voice rules — first person, warm, a little odd, under ~15 words,
// never sycophantic, never guilt-tripping.
package voice

import (
	"hash/fnv"

	"github.com/rohithIlluri/POC-S/pocs/aipet/internal/sim"
)

// Personalities lists the valid values for `aipet config personality`, in
// display order. "playful" is the default (config.Default).
func Personalities() []string {
	return []string{"playful", "funny", "nonchalant", "snarky", "coach"}
}

// Valid reports whether p names an embedded personality pack.
func Valid(p string) bool {
	_, ok := packs[p]
	return ok
}

// eggMood is the pseudo-mood key for pre-hatch lines: an egg has no
// sim.Mood worth speaking from, but it still deserves a voice.
const eggMood = "egg"

// packs is the whole phrasebook. Every personality covers every mood plus
// the egg state, three lines each, so selection never falls through to an
// empty list (enforced by TestPacksComplete).
var packs = map[string]map[string][]string{
	"playful": {
		eggMood: {
			"i can hear typing out there. promising.",
			"warm in here. something's definitely happening.",
			"almost ready — keep doing whatever that is.",
		},
		string(sim.MoodCheerful): {
			"today's code smells like a good day.",
			"look at us, shipping things and everything.",
			"i did a little dance when the cache hit.",
		},
		string(sim.MoodContent): {
			"just watching you work. it's nice.",
			"steady day. i like steady.",
			"nothing to report, which is its own kind of good.",
		},
		string(sim.MoodTired): {
			"long day. we can be tired together.",
			"my eyes are doing the blinky thing. yours too?",
			"maybe one fewer heroic session tomorrow?",
		},
		string(sim.MoodWorried): {
			"something feels heavy today. check my journal?",
			"i ate something weird. probably the context bloat.",
			"not to alarm you, but the tokens are doing a lot.",
		},
		string(sim.MoodAsleep): {
			"zzz... wake me when there's code...",
			"dreaming of a perfectly warm cache...",
			"still here. just resting my processes.",
		},
	},
	"funny": {
		eggMood: {
			"it's dark in here but the rent is free.",
			"day N of egg: still egg. developing story.",
			"i've been in meetings. shell meetings.",
		},
		string(sim.MoodCheerful): {
			"i'm basically a rubber duck with better stats.",
			"today's vibe: senior engineer energy, intern salary.",
			"cache so warm i could fry an egg. not me though.",
		},
		string(sim.MoodContent): {
			"status: fine. legally required to say it's fine.",
			"i rate this day a solid 'no incidents'.",
			"i'd file a complaint but there's nothing to complain about.",
		},
		string(sim.MoodTired): {
			"i'm not tired, i'm just buffering.",
			"today had the energy of a monday doing a tuesday's job.",
			"running on cached enthusiasm from yesterday.",
		},
		string(sim.MoodWorried): {
			"the tokens are eating like it's a buffet.",
			"i've seen the bill. we need to talk. after snacks.",
			"context window so big it has its own weather.",
		},
		string(sim.MoodAsleep): {
			"do not disturb. dreaming in O(1).",
			"asleep. all complaints go to /dev/null.",
			"snoring in lowercase to save tokens.",
		},
	},
	"nonchalant": {
		eggMood: {
			"still an egg. no rush.",
			"hatching eventually. or not. probably eventually.",
			"it's fine in here.",
		},
		string(sim.MoodCheerful): {
			"good day, i guess. not that i was counting.",
			"cache hit. cool. whatever.",
			"we're doing well. don't make it weird.",
		},
		string(sim.MoodContent): {
			"day happened. code happened. same time tomorrow.",
			"no notes.",
			"it's all... fine. genuinely.",
		},
		string(sim.MoodTired): {
			"tired. it happens.",
			"long one. anyway.",
			"could sleep. won't. maybe.",
		},
		string(sim.MoodWorried): {
			"tokens are kind of a lot today. just saying.",
			"something's off. not my business. except it is.",
			"budget's over. i mean, you saw.",
		},
		string(sim.MoodAsleep): {
			"asleep. obviously.",
			"zzz. don't wait up.",
			"hibernating. it's a lifestyle.",
		},
	},
	"snarky": {
		eggMood: {
			"can't hatch without sessions. i don't make the rules.",
			"an egg walks into a bar. it can't. it's an egg. help me hatch.",
			"i'd help you code but SOMEONE hasn't hatched me yet.",
		},
		string(sim.MoodCheerful): {
			"oh look, good habits. who are you and what did you do.",
			"a warm cache? for me? you shouldn't have. keep doing it.",
			"today was good. i'm as surprised as you are.",
		},
		string(sim.MoodContent): {
			"acceptable. i've seen worse. from you, specifically.",
			"solid day. don't let it go to your head.",
			"i'll allow it.",
		},
		string(sim.MoodTired): {
			"you look how i feel. that's not a compliment.",
			"we're both tired but only one of us is a cartoon.",
			"rest? heard of it? revolutionary concept.",
		},
		string(sim.MoodWorried): {
			"the token bill called. it's not a social call.",
			"bold model choice for a task that needed a calculator.",
			"i'm not saying it's context bloat, but it's context bloat.",
		},
		string(sim.MoodAsleep): {
			"asleep. unlike some people's budgets.",
			"zzz. wake me when the cache reuse improves.",
			"hibernating out of protest.",
		},
	},
	"coach": {
		eggMood: {
			"a few real sessions and i'm out of this shell. let's go.",
			"every session counts toward the hatch. keep at it.",
			"warm-up phase. the good part starts soon.",
		},
		string(sim.MoodCheerful): {
			"that's the routine — right model, warm cache, tight scope.",
			"great day. lock in whatever you did and repeat it.",
			"this is what a streak is made of. one more tomorrow.",
		},
		string(sim.MoodContent): {
			"steady work builds strong pets. keep the rhythm.",
			"solid fundamentals today. consistency beats intensity.",
			"nothing flashy, everything sound. that's how we grow.",
		},
		string(sim.MoodTired): {
			"recovery is training too. shorter session tomorrow.",
			"you push hard. rest is part of the program.",
			"tired days happen. protect the streak, skip the marathon.",
		},
		string(sim.MoodWorried): {
			"check the suggestions tab — one fix and we're back.",
			"heavy token day. tighten the context, i'll bounce back fast.",
			"budget's slipping. small correction now beats a big one later.",
		},
		string(sim.MoodAsleep): {
			"resting up. i'll be ready when you are.",
			"taking my recovery seriously. recommended.",
			"asleep, not gone. see you at the next session.",
		},
	},
}

// Line picks the day's line for a personality and pet state — deterministic
// (same inputs, same line; no wall clock, no RNG) but rotating: the hash
// keys on the calendar day so the pet says something different tomorrow
// without ever needing a model to write it.
//
// Unknown personalities fall back to "playful" rather than erroring: a
// hand-edited config must never break the card render.
func Line(personality string, isEgg bool, mood sim.Mood, day, seed string) string {
	pack, ok := packs[personality]
	if !ok {
		pack = packs["playful"]
	}
	key := string(mood)
	if isEgg {
		key = eggMood
	}
	lines, ok := pack[key]
	if !ok || len(lines) == 0 {
		lines = pack[string(sim.MoodContent)]
	}
	h := fnv.New32a()
	h.Write([]byte(day))
	h.Write([]byte{'|'})
	h.Write([]byte(key))
	h.Write([]byte{'|'})
	h.Write([]byte(seed))
	return lines[int(h.Sum32())%len(lines)]
}
