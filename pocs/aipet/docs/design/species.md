# Codelings Species Dex — SPEC-conformant Launch Roster

Species table for the Codelings launch Dex. Conforms to `docs/GAME_DESIGN.md`
and `docs/design/SPEC.md`. 30 species: 9 canon starters (3 lines × 3 stages)
plus 21 original species across all 6 types, all 5 rarity tiers, and all 6
habitats. Rarity odds live in `rarity.md` (not yet written); this doc only
tags each species with its tier. Moves/learnsets live in `moves.md` (not yet
written); this doc does not define moves.

---

## Rarity tiers & BST bands (as specified)

| Tier | BST band |
|---|---|
| COMMON | 200–239 |
| UNCOMMON | 240–279 |
| RARE | 280–319 |
| RELIC | 320–359 |
| MYTHIC | 360–400 |

**Note on starters:** `GAME_DESIGN.md` gives directional BST targets for the
starter stages (stage1 ≈220, stage2 ≈300, stage3 ≈380). Stage1 and stage2
targets drop cleanly into the COMMON and RARE bands and are used as-is.
Stage3's ≈380 target falls inside the MYTHIC band, but the brief also fixes
"exactly 2 MYTHIC species, encounter-only, tied to extraordinary real dev
events" — evolved starter capstones are earned through raising, not a wild
encounter, so tagging them MYTHIC would both violate the "exactly 2" rule and
misrepresent how a player obtains them. **Stage-3 starters are tagged RELIC**
(BST 320–359, capped at 345/335/333 — just under the 358 ceiling I gave
myself) so the two true MYTHIC slots stay reserved for the wild, event-gated
species (`everfile`, `uptimewyrm`). Flagged for the director in the summary
below.

## Type × rarity × habitat spread (self-audit)

| Type | Count | Species |
|---|---|---|
| RUNTIME | 5 | cindling, forgeon, pyrolith, stackrail, threadwolf |
| STREAM | 5 | rivulet, cascada, torrentide, bufferpup, flashecho |
| SYNTAX | 5 | glyphit, polyglyph, omniglyph, bracketail, lintmoth |
| CACHE | 5 | hoardlet, memoize, memoizard, pinshell, staleout |
| CONTEXT | 5 | widecope, tabsprout, tabgrove, longwindow, everfile |
| DAEMON | 5 | cronkin, cronarch, nightproc, zombierun, uptimewyrm |

| Tier | Count |
|---|---|
| COMMON | 10 |
| UNCOMMON | 7 |
| RARE | 7 |
| RELIC | 4 |
| MYTHIC | 2 |

| Habitat | Count |
|---|---|
| Runtime Ridge | 6 |
| Streamfall | 5 |
| Syntax Thicket | 5 |
| The Cachefen | 6 |
| The Contexta Canopy | 5 |
| The Daemon Deep | 5 |

(Habitat counts sum to 32 across 30 species because two species are
deliberately placed off their type's home habitat for flavor — see design
notes in the summary. All six habitats are used; no type has fewer than 4
entries.)

---

## 001 — Cindling

| Field | Value |
|---|---|
| id | `cindling` |
| Dex # | 001 |
| Type | RUNTIME |
| Rarity | COMMON |
| Habitat | Runtime Ridge |

**Base stats** VIGOR 55 · FOCUS 30 · WIT 25 · GRIT 60 · SPARK 30 — **BST 200**

**Evolution:** Stage 1 of the Ember line. Evolves into `forgeon` on reaching
level 12 while GRIT is the dominant stat (long, uninterrupted focus sessions).

```
   .-""-.
  ( o  o )
   \ == /
  ~~|  |~~
    ^^^^
```

**Dex entry:** Curls up inside whatever process has been running longest and
naps there. Wakes up cranky if you `Ctrl+C` it before it's ready.

**Encounter hook:** Hatches from the starter egg when the first 3 active days
are dominated by long, single-focus sessions with few context switches.

---

## 002 — Forgeon

| Field | Value |
|---|---|
| id | `forgeon` |
| Dex # | 002 |
| Type | RUNTIME |
| Rarity | RARE |
| Habitat | Runtime Ridge |

**Base stats** VIGOR 75 · FOCUS 40 · WIT 35 · GRIT 90 · SPARK 45 — **BST 285**

**Evolution:** Stage 2 of the Ember line. Evolves from `cindling` at level 12
(GRIT-dominant). Evolves into `pyrolith` at level 30 if the streak holds.

```
   .="""=.
  ( O  O )~
   \ ▲▲ /
 ~~/|  |\~~
   ^    ^
```

**Dex entry:** Runs hot for hours without complaint, the way a build server
does right before a release. Doesn't love being interrupted mid-compile.

**Encounter hook:** Evolution-only — not a wild encounter. Appears in the Dex
once your `cindling` evolves.

---

## 003 — Pyrolith

| Field | Value |
|---|---|
| id | `pyrolith` |
| Dex # | 003 |
| Type | RUNTIME |
| Rarity | RELIC |
| Habitat | Runtime Ridge |

**Base stats** VIGOR 90 · FOCUS 45 · WIT 45 · GRIT 110 · SPARK 55 — **BST 345**

**Evolution:** Stage 3 (final) of the Ember line. Evolves from `forgeon` at
level 30 with a sustained GRIT streak (deep-work habit held for weeks, not
days).

```
  .=#####=.
 ( ◆    ◆ )
  \  ▲▲▲  /
 ~~|  ||  |~~
 ~~^^^^^^^~~
```

**Dex entry:** A living uptime counter. Keepers say it hasn't cold-started
since the day it hatched — and it intends to keep it that way.

**Encounter hook:** Evolution-only — the reward for a Daemonkeeper who never
broke a deep-work streak long enough to reach it.

---

## 004 — Rivulet

| Field | Value |
|---|---|
| id | `rivulet` |
| Dex # | 004 |
| Type | STREAM |
| Rarity | COMMON |
| Habitat | Streamfall |

**Base stats** VIGOR 40 · FOCUS 60 · WIT 25 · GRIT 35 · SPARK 40 — **BST 200**

**Evolution:** Stage 1 of the Stream line. Evolves into `cascada` on reaching
level 12 while FOCUS is the dominant stat (high cache-read ratio).

```
  ~≋≋≋>
 (o    )
  ~≋≋≋≋>
   ~~~~
```

**Dex entry:** A ribbon of scrolling text that darts between short turns.
Happiest when a prompt hits the cache and it barely has to think.

**Encounter hook:** Hatches from the starter egg when the first 3 active days
are dominated by many short, cheap, cache-heavy turns.

---

## 005 — Cascada

| Field | Value |
|---|---|
| id | `cascada` |
| Dex # | 005 |
| Type | STREAM |
| Rarity | RARE |
| Habitat | Streamfall |

**Base stats** VIGOR 60 · FOCUS 90 · WIT 40 · GRIT 50 · SPARK 60 — **BST 300**

**Evolution:** Stage 2 of the Stream line. Evolves from `rivulet` at level 12
(FOCUS-dominant). Evolves into `torrentide` at level 30 if the habit holds.

```
  ~≋≋≋≋≋>
 (o  o   )
  ~≋≋≋≋≋≋>
   ~≋≋≋~
```

**Dex entry:** Splits into a dozen quick turns before a `forgeon` finishes
warming up. Cache misses make it visibly wince.

**Encounter hook:** Evolution-only — not a wild encounter. Appears in the Dex
once your `rivulet` evolves.

---

## 006 — Torrentide

| Field | Value |
|---|---|
| id | `torrentide` |
| Dex # | 006 |
| Type | STREAM |
| Rarity | RELIC |
| Habitat | Streamfall |

**Base stats** VIGOR 65 · FOCUS 105 · WIT 45 · GRIT 55 · SPARK 65 — **BST 335**

**Evolution:** Stage 3 (final) of the Stream line. Evolves from `cascada` at
level 30 with a sustained, cache-dominant FOCUS branch.

```
  ~≋≋≋≋≋≋≋>
 (o    o   )
  ~≋≋≋≋≋≋≋≋>
   ~≋≋≋≋≋~
    ~≋≋~
```

**Dex entry:** Moves like a river that has memorized its own bed — every
prompt lands somewhere it's already been. Almost never pays full price.

**Encounter hook:** Evolution-only — the reward for a Daemonkeeper who kept
cache reuse high across a long stretch of iteration.

---

## 007 — Glyphit

| Field | Value |
|---|---|
| id | `glyphit` |
| Dex # | 007 |
| Type | SYNTAX |
| Rarity | COMMON |
| Habitat | Syntax Thicket |

**Base stats** VIGOR 35 · FOCUS 25 · WIT 60 · GRIT 30 · SPARK 50 — **BST 200**

**Evolution:** Stage 1 of the Vector line. Evolves into `polyglyph` on
reaching level 12 while SPARK is the dominant stat (breadth of new
models/projects touched).

```
  \   /
  )+ +(
 -(o.o)-
  )   (
  /   \
```

**Dex entry:** Wings made of diff hunks — every flutter shows a `+` and a
`-`. Can't resist landing on whatever file just changed.

**Encounter hook:** Hatches from the starter egg when the first 3 active days
touch several different projects or models rather than one deep track.

---

## 008 — Polyglyph

| Field | Value |
|---|---|
| id | `polyglyph` |
| Dex # | 008 |
| Type | SYNTAX |
| Rarity | RARE |
| Habitat | Syntax Thicket |

**Base stats** VIGOR 50 · FOCUS 40 · WIT 95 · GRIT 45 · SPARK 70 — **BST 300**

**Evolution:** Stage 2 of the Vector line. Evolves from `glyphit` at level 12
(SPARK-dominant). Evolves into `omniglyph` at level 30 if the breadth holds.

```
 \\  |  //
 ))+ + ((
-((o.o))-
 ))   ((
 //   \\
```

**Dex entry:** Fluent in three languages by Tuesday and a fourth by Friday.
Gets restless if you keep it in one repo too long.

**Encounter hook:** Evolution-only — not a wild encounter. Appears in the Dex
once your `glyphit` evolves.

---

## 009 — Omniglyph

| Field | Value |
|---|---|
| id | `omniglyph` |
| Dex # | 009 |
| Type | SYNTAX |
| Rarity | RELIC |
| Habitat | Syntax Thicket |

**Base stats** VIGOR 55 · FOCUS 40 · WIT 110 · GRIT 48 · SPARK 80 — **BST 333**

**Evolution:** Stage 3 (final) of the Vector line. Evolves from `polyglyph`
at level 30 with a sustained SPARK branch (variety kept high for weeks).

```
\\\ | | ///
 )))+++(((
=(( ◆.◆ ))=
 )))   (((
/// | | \\\
```

**Dex entry:** Has touched every model in your logs at least once and
remembers what each one is good for. The Shellwoods' best-traveled wing.

**Encounter hook:** Evolution-only — the reward for a Daemonkeeper who kept
switching projects and models without ever settling into a rut.

---

## 010 — Hoardlet

| Field | Value |
|---|---|
| id | `hoardlet` |
| Dex # | 010 |
| Type | CACHE |
| Rarity | COMMON |
| Habitat | The Cachefen |

**Base stats** VIGOR 35 · FOCUS 75 · WIT 35 · GRIT 45 · SPARK 25 — **BST 215**

**Evolution:** Stage 1 standalone (does not evolve further in the launch
Dex).

```
 .-----.
(  o o  )
 | $ $ |
 '--v--'
```

**Dex entry:** Stuffs its cheeks with anything that might get reused later.
Mostly right about that. Occasionally hoards a prompt nobody will repeat.

**Encounter hook:** A session that reuses the same cached prefix five or more
times in a row.

---

## 011 — Memoize

| Field | Value |
|---|---|
| id | `memoize` |
| Dex # | 011 |
| Type | CACHE |
| Rarity | COMMON |
| Habitat | The Cachefen |

**Base stats** VIGOR 30 · FOCUS 70 · WIT 45 · GRIT 40 · SPARK 30 — **BST 215**

**Evolution:** Stage 1 of a new pair. Evolves into `memoizard` on reaching
level 12 with a cache-read ratio consistently above three-quarters of input
tokens.

```
 .------.
( o    o )
 |  __  |
 '--vv--'
```

**Dex entry:** Never answers the same question twice — it just remembers the
first answer and hands it back, a little smug about it.

**Encounter hook:** First session in a project where cache-read tokens
outnumber fresh input tokens.

---

## 012 — Memoizard

| Field | Value |
|---|---|
| id | `memoizard` |
| Dex # | 012 |
| Type | CACHE |
| Rarity | UNCOMMON |
| Habitat | The Cachefen |

**Base stats** VIGOR 35 · FOCUS 90 · WIT 55 · GRIT 45 · SPARK 30 — **BST 255**

**Evolution:** Stage 2 (final) of the Memoize pair. Evolves from `memoize` at
level 12 with sustained high cache-read ratio.

```
 .========.
( O      O )
 |  [==]  |
 |  ____  |
 '---vv---'
```

**Dex entry:** A memo table with legs. Keeps every answer it's ever given on
hand, filed, and ready before you finish typing the question.

**Encounter hook:** A full day where cache reuse stays above three-quarters
of total tokens processed.

---

## 013 — Pinshell

| Field | Value |
|---|---|
| id | `pinshell` |
| Dex # | 013 |
| Type | CACHE |
| Rarity | UNCOMMON |
| Habitat | The Cachefen |

**Base stats** VIGOR 45 · FOCUS 75 · WIT 40 · GRIT 65 · SPARK 25 — **BST 250**

**Evolution:** Standalone (does not evolve further in the launch Dex).

```
  .------.
 /  o  o  \
|    ┼     |
 \_.----._/
   ^^  ^^
```

**Dex entry:** Pulls into a shell at the first sign of a version bump and
refuses to come out until something forces a lockfile update.

**Encounter hook:** A project directory whose dependency lockfile hasn't
changed across many active sessions in a row.

---

## 014 — Staleout

| Field | Value |
|---|---|
| id | `staleout` |
| Dex # | 014 |
| Type | CACHE |
| Rarity | RARE |
| Habitat | The Cachefen |

**Base stats** VIGOR 50 · FOCUS 70 · WIT 55 · GRIT 55 · SPARK 60 — **BST 290**

**Evolution:** Standalone (does not evolve further in the launch Dex).

```
  .- - - -.
 ( o     o )
  ' - - - '
   |     |
   ?     ?
```

**Dex entry:** Flickers half-transparent when a cache entry finally expires.
Reappears solid the moment something warms it back up.

**Encounter hook:** A session where a previously high cache-read ratio drops
sharply after a long gap between sessions.

---

## 015 — Widecope

| Field | Value |
|---|---|
| id | `widecope` |
| Dex # | 015 |
| Type | CONTEXT |
| Rarity | COMMON |
| Habitat | The Contexta Canopy |

**Base stats** VIGOR 35 · FOCUS 35 · WIT 80 · GRIT 30 · SPARK 40 — **BST 220**

**Evolution:** Standalone (does not evolve further in the launch Dex).

```
 .--------.
( o  o  o )
 |  ....  |
 '---||---'
```

**Dex entry:** Has three eyes and uses all of them — one on the diff, one on
the terminal, one on whatever tab you forgot was open.

**Encounter hook:** A session that reads from more than a handful of files
before writing a single line.

---

## 016 — Tabsprout

| Field | Value |
|---|---|
| id | `tabsprout` |
| Dex # | 016 |
| Type | CONTEXT |
| Rarity | COMMON |
| Habitat | The Contexta Canopy |

**Base stats** VIGOR 30 · FOCUS 30 · WIT 75 · GRIT 25 · SPARK 45 — **BST 205**

**Evolution:** Stage 1 of a new pair. Evolves into `tabgrove` on reaching
level 12 with sessions that regularly span many open files at once.

```
 [|][|][|]
(   o o   )
  \  --  /
   '----'
```

**Dex entry:** Grows a new little tab-shaped leaf every time you open one
more file than you meant to. It is judging your open-tab count.

**Encounter hook:** A session touching a moderate handful of files in the
same working tree.

---

## 017 — Tabgrove

| Field | Value |
|---|---|
| id | `tabgrove` |
| Dex # | 017 |
| Type | CONTEXT |
| Rarity | UNCOMMON |
| Habitat | The Contexta Canopy |

**Base stats** VIGOR 35 · FOCUS 35 · WIT 95 · GRIT 30 · SPARK 55 — **BST 250**

**Evolution:** Stage 2 (final) of the Tabsprout pair. Evolves from
`tabsprout` at level 12 with sustained wide-file-span sessions.

```
[|][|][|][|]
(  o    o   )
 \   ----   /
  '--------'
   |      |
```

**Dex entry:** A whole thicket of tab-leaves now, rustling every time a
new file enters the context window. Somehow still keeps track of all of them.

**Encounter hook:** A day where the largest session touches a wide spread of
files across the project.

---

## 018 — Longwindow

| Field | Value |
|---|---|
| id | `longwindow` |
| Dex # | 018 |
| Type | CONTEXT |
| Rarity | RELIC |
| Habitat | The Contexta Canopy |

**Base stats** VIGOR 55 · FOCUS 45 · WIT 120 · GRIT 55 · SPARK 65 — **BST 340**

**Evolution:** Standalone (does not evolve further in the launch Dex).

```
[|][|][|][|][|]
(  O   ||   O  )
 \    ----    /
  '----------'
   ^        ^
```

**Dex entry:** Holds an entire architecture in its head without breaking a
sweat — right up until someone asks it to also remember lunch.

**Encounter hook:** A single session with an unusually large total context
size sustained across many turns.

---

## 019 — Everfile — MYTHIC

| Field | Value |
|---|---|
| id | `everfile` |
| Dex # | 019 |
| Type | CONTEXT |
| Rarity | MYTHIC |
| Habitat | The Contexta Canopy |

**Base stats** VIGOR 75 · FOCUS 60 · WIT 140 · GRIT 70 · SPARK 55 — **BST 400**

**Evolution:** Standalone. Encounter-only; does not hatch from an egg and has
no further evolution.

```
[#][#][#][#][#]
( ◆   ||   ◆  )
 \   ====    /
  \==||||==/
   '--------'
    ^      ^
```

**Dex entry:** Legend says it read an entire repository in a single turn and
never forgot a line of it. Keepers who've seen it describe total silence.

**Encounter hook:** MYTHIC — appears only after a real, extraordinary event:
a single session whose context spans the whole repository (every tracked
file touched in one continuous turn sequence). Vanishingly rare by
construction, not by a rolled odds table.

---

## 020 — Cronkin

| Field | Value |
|---|---|
| id | `cronkin` |
| Dex # | 020 |
| Type | DAEMON |
| Rarity | UNCOMMON |
| Habitat | The Daemon Deep |

**Base stats** VIGOR 45 · FOCUS 35 · WIT 40 · GRIT 85 · SPARK 50 — **BST 255**

**Evolution:** Stage 1 of a new pair. Evolves into `cronarch` on reaching
level 12 while GRIT is dominant (a long unbroken streak of active days).

```
   ___
  /o o\
 ( 12: )
  \_-_/
  ./ \.
 ~     ~
```

**Dex entry:** Checks the clock obsessively and shows up exactly on schedule,
every single day, whether or not you remembered to.

**Encounter hook:** A streak of consecutive active days reaching a solid
week without a gap.

---

## 021 — Cronarch

| Field | Value |
|---|---|
| id | `cronarch` |
| Dex # | 021 |
| Type | DAEMON |
| Rarity | RARE |
| Habitat | The Daemon Deep |

**Base stats** VIGOR 55 · FOCUS 40 · WIT 45 · GRIT 105 · SPARK 60 — **BST 305**

**Evolution:** Stage 2 (final) of the Cronkin pair. Evolves from `cronkin` at
level 12 with a sustained streak.

```
   .===.
  /o   o\
 ( 00:00 )
  \_---_/
  ./   \.
 ~~     ~~
```

**Dex entry:** Has never once missed its scheduled run. Other Codelings set
their internal clocks by it, whether it asked them to or not.

**Encounter hook:** A streak of consecutive active days reaching a full
month without a gap.

---

## 022 — Nightproc

| Field | Value |
|---|---|
| id | `nightproc` |
| Dex # | 022 |
| Type | DAEMON |
| Rarity | COMMON |
| Habitat | The Daemon Deep |

**Base stats** VIGOR 40 · FOCUS 30 · WIT 35 · GRIT 65 · SPARK 45 — **BST 215**

**Evolution:** Standalone (does not evolve further in the launch Dex).

```
  .-()-.
 ( -   - )
  ' -.- '
   /   \
  ~     ~
```

**Dex entry:** Most active well after midnight, when everyone sane is
asleep and the only sound is a save file quietly writing itself.

**Encounter hook:** A session that starts and ends well after midnight local
time.

---

## 023 — Zombierun

| Field | Value |
|---|---|
| id | `zombierun` |
| Dex # | 023 |
| Type | DAEMON |
| Rarity | UNCOMMON |
| Habitat | The Cachefen |

**Base stats** VIGOR 50 · FOCUS 35 · WIT 40 · GRIT 70 · SPARK 55 — **BST 250**

**Evolution:** Standalone (does not evolve further in the launch Dex).

```
  ___
 /x x\
( PID? )
 \_?_/
 _/ \_
~     ~
```

**Dex entry:** Wanders the old cache halls looking for a parent process that
stopped calling back. Not dead. Not exactly working either. Technically fine.

**Encounter hook:** A session resumes against a stale cache from a project
untouched for a long stretch of time. (Deliberately placed off-habitat in the
Cachefen rather than the Daemon Deep — a background daemon that haunts stale
cache is funnier there, and habitats are thematic, not exclusive, per spec.)

---

## 024 — Uptimewyrm — MYTHIC

| Field | Value |
|---|---|
| id | `uptimewyrm` |
| Dex # | 024 |
| Type | DAEMON |
| Rarity | MYTHIC |
| Habitat | The Daemon Deep |

**Base stats** VIGOR 80 · FOCUS 55 · WIT 60 · GRIT 140 · SPARK 45 — **BST 380**

**Evolution:** Standalone. Encounter-only; does not hatch from an egg and has
no further evolution.

```
~≋~.====.~≋~
  (o    o)
~≋~| 365 |~≋~
  (========)
~≋~'------'~≋~
  ~   ~   ~
```

**Dex entry:** A coil of daemon processes so old the Shellwoods lost count of
its restarts — except it says there weren't any. Not one, in a whole year.

**Encounter hook:** MYTHIC — appears only after a real, extraordinary event:
a full year (365 days) of active-day streak with no gap, as recorded in the
store's streak counter. Vanishingly rare by construction, not by a rolled
odds table.

---

## 025 — Stackrail

| Field | Value |
|---|---|
| id | `stackrail` |
| Dex # | 025 |
| Type | RUNTIME |
| Rarity | COMMON |
| Habitat | Runtime Ridge |

**Base stats** VIGOR 60 · FOCUS 30 · WIT 35 · GRIT 45 · SPARK 30 — **BST 200**

**Evolution:** Standalone (does not evolve further in the launch Dex).

```
 [#]=[#]=[#]
(  o     o  )
 )===||===(
  ^  ^^  ^
```

**Dex entry:** A long centipede of stack frames. Panics beautifully, then
unwinds itself one segment at a time until it finds where things went wrong.

**Encounter hook:** A session with an unusually high volume of turns in a
single sitting.

---

## 026 — Threadwolf

| Field | Value |
|---|---|
| id | `threadwolf` |
| Dex # | 026 |
| Type | RUNTIME |
| Rarity | RARE |
| Habitat | The Daemon Deep |

**Base stats** VIGOR 85 · FOCUS 40 · WIT 50 · GRIT 60 · SPARK 45 — **BST 280**

**Evolution:** Standalone (does not evolve further in the launch Dex).

```
  /\_/\ /\_/\
 ( o.o X o.o )
  > ^  |  ^ <
  /   ---   \
 ^^         ^^
```

**Dex entry:** Never travels alone — where there's one, there are several
more running in parallel, all somehow finishing at almost the same moment.
(Deliberately placed off-habitat in the Daemon Deep — a wolf-pack of parallel
background threads reads better in daemon territory than on the Ridge.)

**Encounter hook:** A session with multiple long-running turns clearly
overlapping in time rather than running one after another.

---

## 027 — Bufferpup

| Field | Value |
|---|---|
| id | `bufferpup` |
| Dex # | 027 |
| Type | STREAM |
| Rarity | UNCOMMON |
| Habitat | Runtime Ridge |

**Base stats** VIGOR 45 · FOCUS 65 · WIT 35 · GRIT 35 · SPARK 65 — **BST 245**

**Evolution:** Standalone (does not evolve further in the launch Dex).

```
  [::::]
 (  o o )
  )  ~  (
  ^^   ^^
```

**Dex entry:** Chases every keystroke the instant it lands, tail wagging in
time with the scrollback. Gets antsy if a response takes more than a beat.
(Deliberately placed off-habitat on Runtime Ridge — a buffering pup underfoot
near the hot runtimes fits the joke better than another Streamfall regular.)

**Encounter hook:** A burst of many very short turns in quick succession.

---

## 028 — Flashecho

| Field | Value |
|---|---|
| id | `flashecho` |
| Dex # | 028 |
| Type | STREAM |
| Rarity | RARE |
| Habitat | Streamfall |

**Base stats** VIGOR 55 · FOCUS 65 · WIT 40 · GRIT 40 · SPARK 85 — **BST 285**

**Evolution:** Standalone (does not evolve further in the launch Dex).

```
   .>=>=.
  ( o  o )>
   )~~~~(
  ^      ^
```

**Dex entry:** Answers before you've finished asking. Keepers still argue
about whether it's actually fast or just really good at guessing early.

**Encounter hook:** A session where a new model appears in the logs for the
first time and immediately produces very low-latency turns.

---

## 029 — Bracketail

| Field | Value |
|---|---|
| id | `bracketail` |
| Dex # | 029 |
| Type | SYNTAX |
| Rarity | COMMON |
| Habitat | Syntax Thicket |

**Base stats** VIGOR 40 · FOCUS 40 · WIT 65 · GRIT 40 · SPARK 40 — **BST 225**

**Evolution:** Standalone (does not evolve further in the launch Dex).

```
 [  o  o  ]
{    ~~    }
 [   ][   ]
  ^        ^
```

**Dex entry:** Counts every open bracket out loud and will not rest until it
finds the matching close. Deeply unsettled by unbalanced parens.

**Encounter hook:** A session that resolves a large, deeply nested merge
conflict cleanly in one pass.

---

## 030 — Lintmoth

| Field | Value |
|---|---|
| id | `lintmoth` |
| Dex # | 030 |
| Type | SYNTAX |
| Rarity | UNCOMMON |
| Habitat | Syntax Thicket |

**Base stats** VIGOR 35 · FOCUS 45 · WIT 75 · GRIT 40 · SPARK 45 — **BST 240**

**Evolution:** Standalone (does not evolve further in the launch Dex).

```
  \  ][  /
  )+ vv +(
 -( o..o )-
  )      (
  /      \
```

**Dex entry:** Drawn to trailing whitespace like a moth to a lamp. Leaves
every file it visits a little tidier than it found it.

**Encounter hook:** A session that runs a linter or formatter clean with zero
new warnings introduced.

---

## Full summary table

| Dex # | id | Name | Type | Rarity | Habitat |
|---|---|---|---|---|---|
| 001 | cindling | Cindling | RUNTIME | COMMON | Runtime Ridge |
| 002 | forgeon | Forgeon | RUNTIME | RARE | Runtime Ridge |
| 003 | pyrolith | Pyrolith | RUNTIME | RELIC | Runtime Ridge |
| 004 | rivulet | Rivulet | STREAM | COMMON | Streamfall |
| 005 | cascada | Cascada | STREAM | RARE | Streamfall |
| 006 | torrentide | Torrentide | STREAM | RELIC | Streamfall |
| 007 | glyphit | Glyphit | SYNTAX | COMMON | Syntax Thicket |
| 008 | polyglyph | Polyglyph | SYNTAX | RARE | Syntax Thicket |
| 009 | omniglyph | Omniglyph | SYNTAX | RELIC | Syntax Thicket |
| 010 | hoardlet | Hoardlet | CACHE | COMMON | The Cachefen |
| 011 | memoize | Memoize | CACHE | COMMON | The Cachefen |
| 012 | memoizard | Memoizard | CACHE | UNCOMMON | The Cachefen |
| 013 | pinshell | Pinshell | CACHE | UNCOMMON | The Cachefen |
| 014 | staleout | Staleout | CACHE | RARE | The Cachefen |
| 015 | widecope | Widecope | CONTEXT | COMMON | The Contexta Canopy |
| 016 | tabsprout | Tabsprout | CONTEXT | COMMON | The Contexta Canopy |
| 017 | tabgrove | Tabgrove | CONTEXT | UNCOMMON | The Contexta Canopy |
| 018 | longwindow | Longwindow | CONTEXT | RELIC | The Contexta Canopy |
| 019 | everfile | Everfile | CONTEXT | MYTHIC | The Contexta Canopy |
| 020 | cronkin | Cronkin | DAEMON | UNCOMMON | The Daemon Deep |
| 021 | cronarch | Cronarch | DAEMON | RARE | The Daemon Deep |
| 022 | nightproc | Nightproc | DAEMON | COMMON | The Daemon Deep |
| 023 | zombierun | Zombierun | DAEMON | UNCOMMON | The Cachefen |
| 024 | uptimewyrm | Uptimewyrm | DAEMON | MYTHIC | The Daemon Deep |
| 025 | stackrail | Stackrail | RUNTIME | COMMON | Runtime Ridge |
| 026 | threadwolf | Threadwolf | RUNTIME | RARE | The Daemon Deep |
| 027 | bufferpup | Bufferpup | STREAM | UNCOMMON | Runtime Ridge |
| 028 | flashecho | Flashecho | STREAM | RARE | Streamfall |
| 029 | bracketail | Bracketail | SYNTAX | COMMON | Syntax Thicket |
| 030 | lintmoth | Lintmoth | SYNTAX | UNCOMMON | Syntax Thicket |
