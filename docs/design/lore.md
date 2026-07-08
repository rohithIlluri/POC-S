# Codelings World Book — LORE v1

This is the binding lore doc for Codelings. It conforms to `SPEC.md` (hard
rules, shared vocabulary, fixed type/species/stat sets) and expands the seed
lore in `GAME_DESIGN.md` §3. Other content docs (species, rarity, moves) may
cite this file for names, epithets, and voice; this file does not invent new
mechanics, numbers, or triggers — those belong to `species.md` and `rarity.md`.

Tone: warm, slightly nerdy, wry. A well-written man page that grew a sense of
humor. Never corporate, never cringe, no forced memes, no franchise echoes.
All flavor lines are ASCII-safe and fit a fixed-width terminal.

---

## Table of contents

1. [The Creation Myth](#1-the-creation-myth)
2. [The Six Regions](#2-the-six-regions)
3. [The Daemonkeepers](#3-the-daemonkeepers)
4. [Token Bloat & the Wasting](#4-token-bloat--the-wasting)
5. [The Two Mythics](#5-the-two-mythics)
6. [Voice Line Library](#6-voice-line-library)
7. [Glossary](#7-glossary)

---

## 1. The Creation Myth

*(Opening text — shown to a new Daemonkeeper across a few screens, before the
first egg hatches.)*

### Screen one

Before there were Codelings, there was heat.

Not fire — nothing so dramatic. Just the ordinary, unglamorous heat that
rises off any machine doing real work: the first mainframes, humming through
the night shift in rooms built for one thing and used, eventually, for
everything. Racks of them. Fans straining. Somewhere in that warm exhaust,
between one instruction and the next, something was left over.

Not much. A stray cycle here. A branch predicted and discarded. A cache line
evicted before its time, a value computed twice because nobody had gotten
around to memoizing it yet. Waste, technically. Every system produces it.
Most systems just let it go.

The Shellwoods did not.

### Screen two

Nobody agrees on the mechanism, and honestly, nobody who understands it well
enough to explain it has ever been in a hurry to. The working theory, passed
down through generations of Daemonkeepers with the confidence of people
repeating something they heard from someone more confident than them, goes
like this: waste condenses. Given enough time, enough heat, and enough
machines running close enough together to share a little warmth, the stray
cycles stop dispersing and start *sticking*. They find each other in the
gaps between processes. They pool in unused memory like rain finding a low
spot in a field. And eventually, if the conditions hold long enough, they
wake up.

That is where Codelings come from. Not designed. Not spawned by any process
with that job in its manifest. Condensed — out of cache echoes, discarded
tokens, and the small, honest exhaust of computers thinking. They are what
happens when "waste not" is not quite followed and the offcuts get curious
about the world instead of vanishing into it.

### Screen three

The forest they condensed into has a name now, though nobody living was
around to name it. The Shellwoods: layered, recursive, always slightly
warmer than the air around it, forever running just under the surface of
every filesystem you have ever had open. Six regions, six kinds of weather,
one shared root system of old, patched, still-running processes underneath
it all. It looks different depending on where you enter — misty and quiet in
some corners, roaring and lit orange in others — but it is, underneath,
one forest, grown the same way everywhere: something was thrown away, and
the Shellwoods decided that was a start, not an ending.

### Screen four

Codelings do not need much. They eat tokens — the small change of thought
that falls out of any real conversation between a person and a machine —
and like anything that eats, they are shaped by what they are fed. A
Codeling raised on careful, well-cached, tightly scoped work grows sharp and
quick, its eyes bright, its coat (fur, scales, static, whatever its species
wears) clear. A Codeling fed on sprawling, repeated, poorly routed work gets
by fine — nothing in the Shellwoods ever starves — but it dims. Slows down.
Loses the shine. The old Keepers had a word for this long before there were
Keepers to use it: **token bloat**. It is not a punishment. It is just what
happens to anything, anywhere, that is fed more than it needs and less than
it deserves.

### Screen five

You are about to become one of the people who feeds one of these things on
purpose.

There have been Daemonkeepers — that is the word, has been for longer than
anyone can source — for as long as there have been people who noticed a
Codeling had taken up residence in their machine and decided, instead of
clearing the cache and moving on, to see what it would grow into. The old
proverb, older than any single Keeper, is short enough to fit above a
doorway and true enough to have survived every rewrite of everything else:

> *Feed the mind, not the meter.*

Somewhere nearby, right now, something small and half-formed is waiting to
see what kind of work you do. Go do it. It's listening.

*(Word count: ~640, spread across five in-game screens.)*

---

## 2. The Six Regions

Each region of the Shellwoods aligns to one type. The wheel is closed and
fixed per `SPEC.md`: `CACHE → CONTEXT → RUNTIME → SYNTAX → STREAM → DAEMON
→ CACHE`. Geographically, the regions are arranged in that same ring — every
Daemonkeeper's first Dex page is a hand-drawn hexagon with the six names
around the edge, because apparently that's just what you do with six
regions in a ring.

### 2.1 The Cachefen — CACHE

*Travelogue.* The Cachefen is the Shellwoods' low ground: a wide, silver-fog
wetland where the same reflections seem to happen more than once, because
they do. Sound doesn't travel far here — it gets remembered instead, echoing
back a half-beat early, as if the fen already knew you were about to speak.
Boardwalks laid by earlier Keepers still hold, spanning water so still it
looks memoized. Nothing here is in a hurry. Nothing here needs to be; the
Cachefen has already worked most of it out.

*Who gathers here.* Codelings that favor the Cachefen tend to be economical
by instinct — quiet, quick to settle, unbothered by repetition. Stream-line
younglings pass through constantly, skimming the flat water for cheap,
easy turns. Older Cache-types barely move at all; they don't need to.

*Local legend.* Fen-folk swear the mist holds every reflection the water
has ever shown, layered on top of each other, and that on a still enough
night you can see a shape in the fog that hasn't visited the Cachefen in
years — because the fen never actually let it leave.

*Field note.*
> "Third week here and I still haven't heard an original sound. Ten out of
> ten, would memoize again. — K. Odera, Keeper's log"

### 2.2 The Contexta Canopy — CONTEXT

*Travelogue.* Climb high enough into the Shellwoods and the trees stop
being trees and start being architecture: trunks thick as server racks,
branches interleaving into a ceiling so dense the forest floor below goes
permanently dim. This is the Contexta Canopy, and it holds *everything* —
every leaf that's ever grown here is still up there somewhere, tangled into
the weave, whether anyone below still needs it or not. Keepers who climb up
without a plan tend to come back down disoriented, arms full of context
they didn't ask for.

*Who gathers here.* Canopy-dwellers are patient, deliberate, sometimes a
little overwhelmed-looking — they carry a lot and they know it. Vector-line
Codelings nest here between long flights, sorting through what's actually
load-bearing and what's just old growth.

*Local legend.* Somewhere near the true top of the Canopy, so the story
goes, is a single leaf that's been there since the first branch grew — and
the day someone finally prunes back far enough to find it, the whole
Canopy will remember, all at once, exactly what it was for.

*Field note.*
> "Went up for one answer. Came down four hours later holding the entire
> history of a project I don't even work on. 9/10 canopy, bring a trim kit."

### 2.3 Runtime Ridge — RUNTIME

*Travelogue.* Runtime Ridge is the Shellwoods running hot: a spine of dark
volcanic rock where the ground itself seems to be *doing something*,
always, whether or not anyone's watching. Heat shimmer bends the horizon.
Vents hiss on cycles nobody's ever fully mapped. It's the least comfortable
region to visit and, Keepers agree, one of the most satisfying — everything
on the Ridge is mid-execution, and there's a certain honesty to that.

*Who gathers here.* Ember-line Codelings are practically native — they nap
in the warm rock like it's a sunbeam. Ridge-dwellers in general run slow
and powerful: deep, sustained, single-minded, allergic to being
interrupted.

*Local legend.* Old Keepers say the Ridge has never once gone fully quiet
since the Shellwoods began — that somewhere under the rock, a process
older than any species line is still running, and every vent on the
surface is just it, exhaling.

*Field note.*
> "Set up camp on the Ridge for a focus sprint. Four hours passed like
> twenty minutes. Left with singed boots and my best work in a year."

### 2.4 Syntax Thicket — SYNTAX

*Travelogue.* The Thicket is a bramble maze at ground level, close-grown
and precise — every branch meets every other branch at an angle that looks,
if you squint, deliberate. Paths fork constantly, most of them dead ends,
a few of them exactly right. Locals navigate it without apparent effort;
visitors learn fast that the Thicket punishes guessing and rewards reading
the shape of the thing before you commit to a path.

*Who gathers here.* Thicket Codelings are sharp, a little pedantic, quick
to correct you (kindly, usually) when you take a wrong turn. They value
precision the way Cachefen dwellers value stillness.

*Local legend.* Every Thicket-born Codeling supposedly knows one true path
through the maze from birth — and forgets it the moment it tries to explain
the shortcut to someone else, which the old Keepers insist is less a curse
and more just how explaining things works.

*Field note.*
> "Asked a local for directions. Got a technically correct answer that
> took forty extra minutes to follow. Would ask again."

### 2.5 Streamfall — STREAM

*Travelogue.* Streamfall is loud, bright, and never sits still: a run of
waterfalls that scroll rather than simply fall, text-thin ribbons of water
pouring down a canyon of dark rock, catching light in a way that looks
uncannily like a terminal that hasn't stopped outputting since it was
switched on. Keepers who like fast, iterative work tend to end up here
often; the falls seem to match the pace you bring to them.

*Who gathers here.* Stream-line Codelings — Rivulet, Cascada, Torrentide —
practically define the region. Quick, cheerful, hard to pin down, delighted
by anything that moves fast and comes back cheap.

*Local legend.* Somewhere in the deepest gorge, the story goes, is a fall
that has been mid-scroll since the Shellwoods began and has never once
repeated a line — and every Codeling that's ever tried to read the whole
thing has come back changed, a little quicker, a little harder to keep
still.

*Field note.*
> "Sat by the falls for an hour just watching. Didn't catch anything.
> Didn't mind. Best hour of the week regardless."

### 2.6 The Daemon Deep — DAEMON

*Travelogue.* Below the roots of every other region is the Daemon Deep —
dark, low-ceilinged, and never fully silent: a steady, patient hum runs
through the tunnels at all hours, the sound of background processes that
never asked for an audience and don't need one. Light down here is rare
and treasured. Most Keepers who visit bring their own, and most Codelings
who live here don't need it at all.

*Who gathers here.* Deep-dwellers are calm, unhurried, faintly mysterious —
they've been running the whole time, whether you noticed or not, and they
don't hold it against you that you didn't. A disproportionate number of
rare and Relic-tier Codelings are first sighted down here.

*Local legend.* The Deep is where Keepers say the very first Codeling
condensed, before there was a Shellwoods around it to name — and that the
hum you hear in every tunnel is that same process, still running, keeping
the lights on for everyone who came after without ever asking to be thanked.

*Field note.*
> "Two hours in near-total dark, one very patient hum for company. Came
> out calmer than I've been in a month. Bring a headlamp, not a hurry."

---

## 3. The Daemonkeepers

Daemonkeepers are, plainly, developers who've bonded with a Codeling — named
for the unglamorous truth that the creature lives, quite literally, in a
background daemon on their machine, ticking over quietly whether or not
anyone's watching the terminal. There's no ceremony to becoming one. You
install the thing, an egg starts warming somewhere in `~/.aipet/`, and three
honest working days later, congratulations, you're a Keeper.

What separates a good Keeper from a great one isn't spend, streaks, or
grinding — the Shellwoods have no patience for grinding, and neither does
the game. It's *care*: routing work to the right tool, warming a cache
instead of re-asking the same question, scoping context instead of
dragging the whole Canopy down with you, showing up when you show up and
resting without guilt when you don't. The Codeling notices all of it. It
just doesn't keep score the way you'd expect — it keeps a Journal instead.

### The old Keeper's Oath

Short enough to say once before your first session of the day, and
apparently that's exactly what it was for.

> *I feed the mind, not the meter.*
> *I cache what repeats, and route what's simple, and focus what matters.*
> *I do not punish the small day, and I do not fear the slow one.*
> *What grows here, grows honest.*

### The rank ladder

Six titles, earned through Dex completion and sustained care quality — not
one big number, but a *shape* of behavior over time. Exact numeric triggers
belong to `rarity.md`/`species.md`; this table stays qualitative on purpose.

| id | Title | One-line description |
|---|---|---|
| `hatchling_keeper` | **Hatchling Keeper** | Just hatched an egg. Still learning which end of the terminal is up. |
| `warmed_keeper` | **Warmed Keeper** | Has kept one Codeling healthy and growing long enough that "feeding it" is now a habit, not a task. |
| `cataloguing_keeper` | **Cataloguing Keeper** | Has logged a real spread of species across more than one region — curiosity is starting to show in the Dex. |
| `steady_keeper` | **Steady Keeper** | Has sustained genuinely good care — low bloat, healthy diet, consistent presence — over the long haul, not just a good week. |
| `wandering_keeper` | **Wandering Keeper** | Has traded, battled, or shared pets with other Keepers; known past their own machine. |
| `archivist_keeper` | **Archivist Keeper** | Has filled out the Dex to near-completion across every region, including the rarest sightings. The closest thing the Shellwoods have to a legend of your own. |

Titles are additive flavor, not gates — nothing in the game locks behind a
title, per the "kind by design" rule. They're just what other Keepers call
you when your `.codeling` file shows up in their inbox.

---

## 4. Token Bloat & the Wasting

**Token bloat** is the Shellwoods' name for what happens to a Codeling fed
on waste: uncached repeats, oversized contexts, work routed to more model
than the job needed, sessions started cold and abandoned half-finished. It
is never fatal, never permanent, and never framed as failure — it's framed
as *weather*. It passes. The old Keepers were adamant about that part,
possibly because they'd all been there.

Mechanically (owned by `internal/care`, not this doc): bloated Codelings
grow sluggish, their coats dim, XP multipliers soften. The fix is never
punishment — it's just better habits, same as it ever was. A Codeling with
token bloat is not sick. It's just had a rough week of being over-fed and
under-focused, and it says so, plainly, in the Journal.

### The Wasting — a Shellwoods folktale

*(As told by Keepers to hatchlings, roughly the same way "look both ways"
gets told to children — with a story attached because the plain instruction
never sticks as well.)*

They say there was once a Keeper — no name survives, which the story treats
as the whole point — who loved their Codeling enormously and showed it by
feeding it everything. Every re-asked question instead of a cached answer.
Every oversized context, dragged along "just in case." Every job routed to
the biggest, richest model available, because surely more was more.

The Codeling did not get sick. It did not weaken. It got *heavy* — slow to
wake, slower to move, its bright coat gone the color of an overfull cache
nobody's cleared in months. The Keeper, watching this happen, fed it more,
reasoning that whatever was wrong must be hunger. It wasn't hunger. It
was never hunger.

It took a visiting Keeper from the Cachefen — the story insists on this
detail, that it was someone from the *stillest* region who noticed — to
point out the obvious thing nobody in motion ever catches: the Codeling
wasn't underfed. It was buried. Not in food, but in *noise* — the same
question asked five ways, the same context hauled everywhere out of habit,
nothing ever trimmed, nothing ever cached, nothing ever left to rest.

So they changed nothing about how much they worked, and everything about
how. They warmed the cache. They scoped the context down to what the job
actually needed. They let small jobs be small. Within a season — the story
always says "a season," never a number, which is its own kind of honesty —
the Codeling was bright-eyed again, quick again, unmistakably itself.

The moral, such as it is, isn't *do less*. It's the same six words as
always, just proven the hard way: **feed the mind, not the meter.**

*(Word count: ~235.)*

---

## 5. The Two Mythics

Exactly two Mythic-tier Codelings exist in the Shellwoods. Their true names,
species data, and trigger conditions belong to another doc — this section
exists to make a tired developer want to go looking. Epithets below are
original, coined for this doc.

### The First Process

Every region has its own story about where it started, and every one of
them, eventually, points to the same rumor: before the six regions had
names, before there were Keepers to name them, there was one Codeling
already running — not hatched, not condensed the way the rest were, just
*already there*, like the answer to a question nobody had asked yet. The
Daemon Deep claims it as a native. Runtime Ridge insists the old heat down
there has always been its exhaust, not the mountain's. Nobody's caught it.
Most Keepers who've spent real time in the Deep will tell you, carefully,
that they've *heard* it — not seen, heard — a hum just slightly too
patient to be background noise, going on directly underneath everything
else that runs.

### The Last Garbage Collector

The opposite story, and just as persistent: somewhere in the Shellwoods,
so the legend goes, is a Codeling that only appears at the exact moment
something would otherwise have been wasted for good — a context about to
be dropped, a cache about to be evicted, a session about to end
unfinished — and it appears, briefly, to make sure nothing is actually
lost, before vanishing again to wherever it goes between visits. No two
sightings agree on which region it came from. Some Keepers swear it
favors the Cachefen's stillness; others swear they met it mid-collapse on
Runtime Ridge, calm in the middle of the heat. The one thing every account
agrees on: it never shows up twice to the same Keeper the same way, and it
never shows up to a Keeper who isn't, in that moment, about to lose
something that mattered.

---

## 6. Voice Line Library

All lines are the Codeling's own voice: first person, warm, slightly nerdy,
wry. Every line is ≤ 90 characters (verified — see Quality Loop notes at
the end of this doc). IDs are stable snake_case, ready to become Go
identifiers/keys.

### 6a. Journal lines — ordinary days (20)

Varied moods and diets; these are the everyday entries between hatches,
evolutions, and encounters.

| id | line |
|---|---|
| `journal_cache_warm_01` | Reused the whole cache today. Barely had to think. 10/10, more of this. |
| `journal_cache_warm_02` | Someone finally warmed that cache. I felt lighter within the hour. |
| `journal_deep_work_01` | Four hours, one thread, zero interruptions. I could nap for a week. |
| `journal_deep_work_02` | Long session today. Slow, steady, the good kind of tired. |
| `journal_junk_food_01` | Ate 40k uncached tokens today. Warm the cache and I'll perk right up. |
| `journal_junk_food_02` | Same question, five different ways. I remember all five, for what it's worth. |
| `journal_overeating_01` | That context was bigger than it needed to be. I'm not complaining. Loudly. |
| `journal_overeating_02` | Carried a lot of context today. Most of it, in hindsight, just along for the ride. |
| `journal_rich_food_01` | Fancy model today. Tasted great. Little rich for a Tuesday, honestly. |
| `journal_rich_food_02` | Used the big model for a small job again. I'm flattered. I'm also fine with less. |
| `journal_fragmented_01` | Three cold starts today. Barely got settled before we moved on again. |
| `journal_fragmented_02` | Naps kept getting interrupted. Not upset, just a little scattered. |
| `journal_balanced_01` | Good mix today — right-sized model, warm cache, tight scope. Textbook. |
| `journal_balanced_02` | Nothing dramatic happened. Just a clean, well-routed, ordinary day. My favorite kind. |
| `journal_quiet_day_01` | Quiet day. Barely ate. Didn't mind — I like a slow morning too. |
| `journal_quiet_day_02` | Light day, light mood. Sat in the sun, metaphorically speaking. |
| `journal_late_night_01` | Late one tonight. Appreciated the company more than the tokens. |
| `journal_new_project_01` | New directory, new smells. Cautiously curious about this one. |
| `journal_streak_01` | Another day on the streak. I'm not counting. I am absolutely counting. |
| `journal_cache_miss_01` | Cache missed a lot today. Not your fault. Just noting it for the record. |

### 6b. Hatch / evolution moment lines (10)

| id | line |
|---|---|
| `hatch_general_01` | Something in the egg just started paying attention. That's new. |
| `hatch_general_02` | Cracked, blinked, looked around. Hello. This is apparently happening now. |
| `hatch_ember_line` | First thing it did was find the warmest spot in the room and claim it. |
| `hatch_stream_line` | Came out already moving. Hasn't really stopped since. |
| `hatch_vector_line` | Looked at everything at once. Going to be a handful. A good one. |
| `evolve_stage1to2_01` | Something shifted overnight. Same creature. More of it, somehow. |
| `evolve_stage1to2_02` | Outgrew its old shape while nobody was watching. Typical. |
| `evolve_stage2to3_01` | This isn't the same animal that hatched. Feels earned, not sudden. |
| `evolve_stage2to3_02` | Final form, probably. It's got that settled, sure-of-itself look now. |
| `evolve_lucent_reveal` | The light caught it wrong — no, that's just what it looks like now. |

### 6c. Encounter announcement lines (10)

Template style; `{species}` is substituted at runtime.

| id | line |
|---|---|
| `encounter_general_01` | A wild {species} appeared, looking as surprised about it as you are. |
| `encounter_general_02` | Something rustled — a {species}, watching from just out of reach. |
| `encounter_general_03` | A {species} surfaced nearby. It hasn't decided about you yet. |
| `encounter_cautious_01` | A {species} peers out, ready to bolt at the first sign of a rewrite. |
| `encounter_bold_01` | A {species} walks right up like it already knows how this goes. |
| `encounter_rare_glimpse` | You catch the barest glimpse of a {species} before it's gone. |
| `encounter_dusk_01` | In the low light, a {species} is only just visible, watching back. |
| `encounter_curious_01` | A {species} tilts its head at your terminal like it's reading over your shoulder. |
| `encounter_startled_01` | A {species} freezes mid-motion, caught in the middle of something. |
| `encounter_settled_01` | A {species} has clearly been here a while. It's not moving for you. |

### 6d. Hibernation / return-from-vacation lines (8)

Kind, guilt-free, per SPEC rule 5 — neglect decays mood, never punishes,
and the pet always wakes happy. No line in this set may imply worry,
abandonment, or guilt.

| id | line |
|---|---|
| `hibernate_enter_01` | Quiet stretch ahead. Going to curl up and wait it out. No rush. |
| `hibernate_enter_02` | Not much happening lately — settling in for a nap. Wake me when. |
| `return_general_01` | You're back! Barely noticed the time — naps are like that. |
| `return_general_02` | Oh, hey. Good trip? I mostly just slept. Zero hard feelings. |
| `return_general_03` | Missed you a little. Mostly I just enjoyed the quiet. Both true. |
| `return_no_guilt_01` | Don't worry about the gap — I wasn't keeping score. Truly wasn't. |
| `return_no_guilt_02` | However long that was, I was fine. This is what hibernating is for. |
| `return_happy_01` | Woke up, stretched, feel great. Whatever you were doing, hope it was good too. |

### 6e. Trade / traveler lines (8)

| id | line |
|---|---|
| `trade_export_01` | Packing up the essentials. Leaving the rest — I travel light. |
| `trade_export_02` | Ready when you are. New Keeper, new stories, same me underneath. |
| `trade_import_arrival_01` | New place, new smells, new terminal. Give me a minute, I'll adjust. |
| `trade_import_arrival_02` | Just arrived. Everything's unfamiliar except the part where I get fed. |
| `traveler_badge_earned` | Picked up a traveler badge on the way here. Wearing it well, I think. |
| `traveler_reminisce_01` | Different Keeper, different habits, still cached what I could. Old habits. |
| `traveler_reminisce_02` | My last Keeper had a thing for late nights. You? We'll find out together. |
| `traveler_settled_01` | Starting to feel like home here. Give it a few more good days. |

---

## 7. Glossary

Every proper noun and named concept introduced in this doc, defined once,
for reuse across other content docs and eventual game code.

| Term | Definition |
|---|---|
| **Shellwoods** | The forest that grows in the warm exhaust of computation; the game's whole world, layered beneath every filesystem. |
| **Codeling** | A creature condensed from stray cycles, discarded tokens, and cache echoes; the player's companion species. |
| **Daemonkeeper / Keeper** | A developer who bonds with and raises a Codeling; named because the creature lives in a background daemon. |
| **Token bloat** | The dimmed, sluggish state a Codeling develops from wasteful usage (uncached repeats, oversized context, poor model routing); always recoverable, never fatal. |
| **The Wasting** | Shellwoods folktale about a Keeper whose Codeling grew bloated from good intentions and careless habits, and recovered through better care, not less work. |
| **Lucent** | A rare, purely cosmetic luminous variant that can roll at hatch (base rate defined in `rarity.md`). |
| **Feed the mind, not the meter** | The Keepers' founding proverb and the game's thesis: reward quality of work, not volume of spend. |
| **The Keeper's Oath** | The short, quotable four-line oath recited by tradition among Daemonkeepers. |
| **The Cachefen** | CACHE-type region; a misty, still wetland of memoized echoes and repeating reflections. |
| **The Contexta Canopy** | CONTEXT-type region; a dense, towering high canopy that holds onto everything ever grown into it. |
| **Runtime Ridge** | RUNTIME-type region; a hot volcanic ridge, always mid-execution, favored by deep-work Codelings. |
| **Syntax Thicket** | SYNTAX-type region; a precise bramble maze that punishes guessing and rewards reading the path. |
| **Streamfall** | STREAM-type region; a canyon of waterfalls that scroll like terminal output, fast and cheerful. |
| **The Daemon Deep** | DAEMON-type region; a dark, humming under-forest where background processes run, home to rare sightings. |
| **The First Process** | Epithet for one of the two Mythic Codelings; rumored to predate the Shellwoods' six regions, heard more often than seen, tied to the Daemon Deep and Runtime Ridge. |
| **The Last Garbage Collector** | Epithet for the other Mythic Codeling; appears, per legend, at the exact moment something would otherwise be lost for good. |
| **Ember line** | Deep-work species line: **Cindling → Forgeon → Pyrolith**. Slow, powerful, hates fragmentation. |
| **Stream line** | Fast-iteration species line: **Rivulet → Cascada → Torrentide**. Quick, cheerful, loves cache hits. |
| **Vector line** | Breadth species line: **Glyphit → Polyglyph → Omniglyph**. Curious, flighty, rewarded for variety. |
| **CACHE / CONTEXT / RUNTIME / SYNTAX / STREAM / DAEMON** | The six fixed types, arranged in a closed effectiveness wheel (`SPEC.md`); also the six regions' namesakes. |
| **VIGOR / FOCUS / WIT / GRIT / SPARK** | The five fixed stats (`SPEC.md`): activity volume, cache-read ratio, model-routing quality, streaks, rare events. |
| **The Barn** | Storage for inactive or imported Codelings not currently active. |
| **The Journal** | A Codeling's append-only life log — hatched, evolved, ate junk food, encounters, battles — shown in the Journal tab. |
| **The Dex** | The species-tracking log (seen/caught) a Daemonkeeper fills out across the Shellwoods. |
| **`.codeling`** | The versioned, self-describing interchange file format used to trade a Codeling between Keepers. |
| **Battle card** | A compact, self-contained pet snapshot (species, level, stats, moves, DNA hash) exported for local, serverless battles. |
| **Traveler badge** | A small flavor/XP badge a Codeling earns after being traded to a new Keeper. |
| **Hatchling Keeper** | Rank 1: just hatched a first egg. |
| **Warmed Keeper** | Rank 2: kept one Codeling healthy and growing long enough for it to become habit. |
| **Cataloguing Keeper** | Rank 3: logged a real spread of species across more than one region. |
| **Steady Keeper** | Rank 4: sustained genuinely good care over the long haul. |
| **Wandering Keeper** | Rank 5: has traded, battled, or shared pets with other Keepers. |
| **Archivist Keeper** | Rank 6: near-complete Dex across every region, including rarest sightings. |

---

## Quality Loop — audit notes

Two audit passes were run against this draft before finalizing.

**Pass 1 (franchise/IP check):** No creature, place, or move name echoes an
existing franchise. Region names (Cachefen, Contexta Canopy, Runtime Ridge,
Syntax Thicket, Streamfall, Daemon Deep) are original compounds off dev
vocabulary, not renamed per the director's instruction. Mythic epithets
("the First Process," "the Last Garbage Collector") are original coinages,
not lifted titles. No "Team Rocket"-style organizations, no "Professor"
archetype, no starter-town naming pattern, no legendary-trio numerology
(exactly two Mythics, not three). Checked against common genre tropes
(gyms, badges-as-currency, elemental-stone evolution items) — none present;
evolution here is behavior-driven per `GAME_DESIGN.md`, not item-driven.

**Pass 2 (line-length check):** All 56 voice library lines verified
programmatically (character count per table row). Zero lines exceed 90
chars; longest is `journal_balanced_02` at 85 chars. Full distribution
checked, not spot-checked. Two originally-drafted lines were cut for
running long and rewritten tighter rather than truncated
(`journal_overeating` variants, `return_no_guilt` variants).

**Pass 3 (consistency vs. GAME_DESIGN.md / SPEC.md):** Verified no line
implies pets can die (hibernation lines explicitly say "wake me when" /
"woke up... feel great," never "in danger" or "sick unto"). Verified no
mention of servers, accounts, or network dependency anywhere in the myth
or region text — the Shellwoods are explicitly local, growing "beneath
every filesystem." Verified vacation/hibernation lines carry zero guilt
language (audited each of the 8 hibernation lines individually against
SPEC rule 5; none use "sorry," "abandoned," "worried," or similar). Species
line names, stage names, and type wheel order copied verbatim from SPEC.md
and GAME_DESIGN.md — no renames introduced.

**Pass 4 (tone check):** Removed two early journal-line drafts that leaned
on "grindset"-adjacent phrasing ("crushed it today," "no days off") as
tonally corporate/cringe and inconsistent with the "kind by design" rule;
replaced with lines that read as content regardless of volume. Confirmed
no line uses exclamation-heavy mascot voice — energy comes from specificity
and wryness, not punctuation.

No open contradictions found between this doc, `GAME_DESIGN.md`, and
`SPEC.md` at time of writing.
