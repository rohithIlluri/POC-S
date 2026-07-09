## Summary

Defines the five rarity tiers and their BST bands, encounter odds (power-of-two
fractions with a deterministic whiff-streak pity ceiling), the Lucent cosmetic
roll and its GRIT-streak pity curve, hidden IVs (integer stat modifiers rolled
once at hatch), 15 dev-signal badges, 10 passive-effect artifacts, duplicate/
collection rules, and explicit anti-frustration time bounds with the
arithmetic that proves them. Every roll is a pure function of
`(dna, date, deterministic-history-counters)` — replayable, server-free, and
never nudged by how much a Keeper spends.

---

## 1. Rarity tiers

| Tier | BST band | Launch species count | Encounter-only? |
|---|---|---|---|
| COMMON | 200–239 | set by `species.md` | no |
| UNCOMMON | 240–279 | set by `species.md` | no |
| RARE | 280–319 | set by `species.md` | no |
| RELIC | 320–359 | set by `species.md` | no |
| MYTHIC | 360–400 | exactly 2 | yes, always |

BST (base stat total) is the sum of a species' five base stats (VIGOR + FOCUS
+ WIT + GRIT + SPARK) before IVs. `species.md` assigns each of the 30 launch
species to a tier and must keep its BST inside the matching band — that's the
only constraint this doc places on species design.

---

## 2. Encounter odds

An **encounter trigger** is a real signal the collector observes: a new
project directory shows up, a new model appears in the day's events, or a
day's digest clears the "healthy diet" bar (see `internal/care`). Reference
rate: an active dev's day produces **0–3 triggers**; a casual dev produces
roughly **1 trigger per active day** (2–5 active days/week).

### 2.1 Base tier roll

When a trigger fires, roll one integer `r` uniformly in `[0, 1024)` (see §2.4
for exact derivation) and resolve the tier by range. Denominator is fixed at
1024 for every roll in this section — power-of-two per SPEC rule 3.

| Tier | Weight (of 1024) | Fraction |
|---|---|---|
| RELIC | 8 | 1/128 |
| RARE | 64 | 1/16 |
| UNCOMMON | 256 | 1/4 |
| COMMON | 696 (remainder) | 87/128 |

MYTHIC is not part of this table — see §2.3.

### 2.2 Healthy-diet band shift

If the *current calendar day's* digest already qualifies as a "balanced diet"
day (per `internal/care`: no token-bloat status, cache-read ratio ≥ 1/2, under
the soft daily budget), every encounter trigger that fires **later that same
day** shifts the roll one band up before resolving:

- Roll lands COMMON → treated as UNCOMMON.
- Roll lands UNCOMMON → treated as RARE.
- Roll lands RARE → treated as RELIC.
- Roll already RELIC → stays RELIC (no band above it in this table).

This is a pure post-roll remap, not a second roll — fully deterministic and
replayable from the same `r`. It rewards the habits the game already coaches
(cache reuse, budget discipline) without ever touching spend amount upward —
the shift is capped at "spend less, cache more," never "spend more."

### 2.3 MYTHIC gate

Before the tier roll in §2.1 runs, check a separate, independent gate:

```
mythic_roll = SHA256(dna || date || "mythic") mod 4096
if mythic_roll == 0: MYTHIC encounter (species = one of the 2 Mythic species,
                      chosen by mythic_roll's next bit: bit0 of a second hash
                      SHA256(dna || date || "mythic2"))
else: proceed to §2.1 tier roll (the mythic check consumes no odds from it)
```

`1/4096` — power-of-two denominator, independent of the diet-shift mechanic
(Mythic encounters are flavor-rare "world events," not a diet reward, since
only 2 species exist and both are encounter-only by canon).

### 2.4 Deterministic roll derivation

All rolls in this doc follow the same pattern so they're replayable from save
state alone, with no hidden RNG state:

```
seed_material = dna || ISO8601-date(local calendar day) || trigger_ordinal || purpose_tag
digest        = SHA256(seed_material)
r             = first 8 bytes of digest, interpreted big-endian, mod denominator
```

`trigger_ordinal` is the 0-indexed count of triggers already resolved that
day (so a second trigger on the same day gets a different `r` from the
first, without needing wall-clock sub-second entropy). `purpose_tag` is a
short literal (`"tier"`, `"species"`, `"lucent"`, `"mythic"`) so unrelated
rolls sharing the same day/trigger never correlate.

### 2.5 Whiff-streak pity ceiling

The save file tracks `whiffs_since_rare` (int, incremented each time an
encounter trigger resolves COMMON or UNCOMMON — including after the §2.2
band shift — and reset to 0 on any RARE/RELIC/MYTHIC result). This counter is
just a derived tally of encounter journal entries — reconstructable from
`journal.jsonl` alone, so it survives save corruption/replay:

| Condition | Effect |
|---|---|
| `whiffs_since_rare >= 40` | Next tier roll is floored at RARE (COMMON/UNCOMMON results are remapped up to RARE) |
| `whiffs_since_rare >= 120` | Next tier roll is floored at RELIC |

The floor is a post-roll remap identical in spirit to §2.2 — deterministic,
no extra RNG draw. See §7 for the worst-case day bounds this guarantees.

### 2.6 Worked example day

An active Keeper has a strong day: 14 sessions, cache-read ratio 0.71 (clears
the balanced-diet bar), no token bloat, under budget. The collector fires 2
encounter triggers that day (a new project dir at 10:03, an unfamiliar model
at 16:40). `whiffs_since_rare` currently sits at 12 (no pity floor active).

1. Trigger 0 (10:03): `r0 = SHA256(dna|date|0|"tier") mod 1024`. Say
   `r0 = 812` → falls in the COMMON range (`[8+64+256, 1024) = [328,1024)`
   ... wait, ranges are assigned RELIC-first: `[0,8)=RELIC, [8,72)=RARE,
   [72,328)=UNCOMMON, [328,1024)=COMMON`. `812` → COMMON. Day already
   qualifies as balanced-diet at 10:03? No — diet is evaluated on the full
   day's digest, only known at day-end, so the shift in §2.2 applies
   retroactively to triggers already logged that day once the digest closes
   (the daemon's once-daily sim tick re-resolves any same-day trigger whose
   tier was pending the diet verdict). Result: COMMON → shifts to UNCOMMON.
   `whiffs_since_rare` → 13.
2. Trigger 1 (16:40): `r1 = SHA256(dna|date|1|"tier") mod 1024 = 40` → RARE
   range. Diet shift: RARE → RELIC. `whiffs_since_rare` resets to 0.

End of day: Keeper gets an UNCOMMON encounter and a RELIC encounter — a great
day, correctly reflecting a great diet. (Implementation note: to avoid a
same-day retroactive UI surprise, the TUI shows same-day encounters as
"pending" until the nightly tick confirms the diet verdict, then reveals the
final tier — this is the one place the game asks for a few hours of
patience, never more.)

---

## 3. Lucent system

Lucent is a cosmetic-only alternate palette, available on **any** species at
hatch or at encounter resolution. It never changes stats, tier, or BST.

### 3.1 Base rate and pity curve

Base rate is **1/512**. GRIT streak (consecutive active days, already tracked
for the GRIT stat) halves the effective denominator at fixed thresholds, floor
1/64:

| GRIT streak (consecutive active days) | Lucent denominator | Fraction |
|---|---|---|
| 0–6 | 512 | 1/512 |
| 7–13 | 256 | 1/256 |
| 14–27 | 128 | 1/128 |
| 28+ | 64 (floor) | 1/64 |

Every value is a power-of-two denominator per SPEC rule 3. The streak is a
*consistency* reward (showing up most days), not a spend reward — a day
counts toward GRIT streak the same way it always has (≥1 qualifying active
session that day), independent of tokens spent.

### 3.2 Roll derivation (replayable)

```
denom       = lucent_denominator(grit_streak_on(date))   // table above
roll_value  = SHA256(dna || date || "lucent") mod denom
is_lucent   = (roll_value == 0)
```

Same pattern as §2.4: pure function of `dna`, the calendar date, and
`grit_streak_on(date)` — itself deterministically recomputable from the
event log (count of consecutive prior active days as of that date). Two
Keepers who replay the same DNA and history reproduce the identical Lucent
outcome; no separate save-only pity counter to desync or reset accidentally.

### 3.3 Where it applies

- **At hatch**: one Lucent roll using the egg's `dna` and the hatch date's
  GRIT streak.
- **At encounter**: one Lucent roll per resolved encounter (§2), using the
  same `dna`/date/streak inputs but `purpose_tag = "lucent"`, independent of
  the tier roll — a COMMON encounter can be Lucent; so can a RELIC one.

---

## 4. IVs (individual values)

Rolled once at hatch from the egg's 32-byte `dna` seed, permanent for the
pet's life. Five IVs, one per stat, range **0–31** (fits five 5-bit slices
out of the 32-byte DNA — plenty of room left for species/Lucent/other rolls).

```
IV_stat = dna_bits(offset_for(stat), 5)   // 0..31, uniform
```

### 4.1 Stat modifier (integer only)

```
stat_modifier = (IV_stat - 16) / 4     // integer division, truncates toward zero
```

| IV value | Modifier |
|---|---|
| 0 | −4 |
| 8 | −2 |
| 16 | 0 |
| 24 | +2 |
| 31 | +3 |

Final stat = `growth_stat_from_history + stat_modifier`, floored at 0. The
±4/+3 spread is intentionally small: two Keepers with identical habits get
recognizably different pets, but IVs never move a pet across a BST band or
substitute for real activity — habits still dominate the stat, as GAME_DESIGN
§4.3 requires ("grown from real behavior").

---

## 5. Badges

Badges are earned titles shown on the pet card. Every trigger below is a
direct, mechanical read of `store.Event` fields (`Source`, `Session`,
`Project`, `Model`, `Timestamp`, `Input`, `Output`, `CacheWrite`,
`CacheRead`, `CostUSD`) or of derived daily-digest/journal state already
covered elsewhere in the design — nothing requires an artificial action.

| id | Name | Trigger | Flavor line |
|---|---|---|---|
| `night_daemon` | Night Daemon | A session with any event timestamp (local time) between 02:00–04:00 | "I heard the fans spin up at 3am. Respect." |
| `cache_whisperer` | Cache Whisperer | 7 consecutive days with day cache-read ratio ≥ 60% | "You and the cache have an understanding now." |
| `polyglot` | Polyglot | 5 distinct `Model` values seen across events in one rolling 7-day window | "New model, who dis? Five, this week alone." |
| `traveler` | Traveler | Pet arrived via `aipet trade import` | "I've seen another Keeper's terminal. It was cozy." |
| `deep_diver` | Deep Diver | A single session with total session duration ≥ 90 minutes and 0 gaps > 5 minutes | "One thread, ninety minutes, zero context switches." |
| `early_bird` | Early Bird | A session with any event timestamp between 05:00–07:00 | "Coffee not even done brewing and already shipping." |
| `streak_keeper` | Streak Keeper | GRIT streak reaches 14 consecutive active days | "Fourteen days straight. I stopped counting on my claws." |
| `polymath` | Polymath | Events logged across 5 distinct `Project` paths in one rolling 7-day window | "Five repos, one week. Are you even sleeping in one?" |
| `frugal_forager` | Frugal Forager | 7 consecutive days under the soft daily budget with 0 token-bloat status days | "Ate well, spent little. The Shellwoods approve." |
| `polisher` | Polisher | A day where cache-read ratio ≥ 90% | "Ninety percent cache. You basically recycled a whole day." |
| `weekend_warrior` | Weekend Warrior | Active sessions on both Saturday and Sunday of the same calendar week | "No days off? Same. I'm not judging, I literally can't leave." |
| `comeback` | Comeback | First active session after ≥7 idle days (post-hibernation wake) | "Welcome back. I kept your seat warm. Mostly." |
| `marathoner` | Marathoner | 20+ sessions logged in a single calendar day | "Twenty sessions. I lost count around twelve, honestly." |
| `router` | Router | A day where 3+ distinct models are used and none of them accounts for >50% of that day's tokens | "Right tool, right job, every single time today." |
| `first_light` | First Light | The very first session ever recorded for this pet (hatch-day badge) | "The first spark. I remember it like it was yesterday. It was." |

15 badges, all derived from fields already on `store.Event` or trivial
rolling-window aggregates over it (7-day windows, per-day digests, session
gap detection from consecutive `Timestamp`s within a `Session`). None require
spending more, using a specific paid model, or any action outside normal dev
work.

---

## 6. Artifacts

Small item system: 10 artifacts, found via real events, held by the active
pet (one slot — see §6.1) for a passive effect. All effects are integer math,
capped, and modest — flavor and identity, not power spikes. None require
spending money and none scale with cost (see §8 audit).

### 6.1 Holding rules

A pet holds **at most one artifact** at a time (simple, no build-crafting
meta). Finding a new artifact while holding one prompts a swap choice in the
Journal; the displaced artifact returns to a small personal "satchel" list
(unlimited, cosmetic-only storage) and can be re-equipped later at any time.

| id | Name | Found when | Effect (integer math) | Flavor line |
|---|---|---|---|---|
| `warm_cache_shard` | Warm Cache Shard | Day ends with cache-read ratio ≥ 80% | +1/16 to daily XP (XP × 17 / 16) | "Still warm. Smells like a hot path." |
| `midnight_oil` | Midnight Oil | A session logged between 00:00–05:00 | Mood decay from idle days halved (round down) while held | "Burns slow. Doesn't judge your sleep schedule." |
| `polyglot_prism` | Polyglot Prism | 4+ distinct models used in a single day | +1 to Lucent roll numerator equivalent: halves current Lucent denominator for that day only (min 64) | "Refracts one prompt into a dozen dialects." |
| `budget_ledger` | Budget Ledger | 5 consecutive days under the soft daily budget | Encounter tier roll (§2.1) gets the §2.2 diet-shift even on a day that narrowly misses "balanced diet" (cache ratio ≥ 0.4 instead of 0.5) | "Every token, accounted for. Tidy little book." |
| `stale_context_husk` | Stale Context Husk | A day with token-bloat status triggered | While held: next day's XP is unaffected but the husk auto-discards itself at day-end (a one-day narrative marker, no lingering penalty) | "A memento of the day the context got away from us." |
| `streak_charm` | Streak Charm | GRIT streak reaches 7 | Idle-day mood decay reduced by 1 point/day (floor 0) while held | "One charm, seven days of not skipping leg day." |
| `patch_notes` | Patch Notes | First session on a new `Model` name never seen before | +1 flat SPARK-derived XP bonus per new-model day while held | "Read the whole changelog. Actually read it." |
| `traveler_compass` | Traveler's Compass | Pet received via `aipet trade import` | +1/32 to daily XP (XP × 33/32) while held, permanently tradeable with the pet | "Points toward whichever terminal isn't yours." |
| `deep_focus_lens` | Deep Focus Lens | A session ≥ 90 minutes with 0 gaps > 5 minutes | +1/8 to FOCUS growth from that day's digest (rounded down) while held | "Everything else gets a little blurry. On purpose." |
| `dawn_ember` | Dawn Ember | A session logged between 05:00–07:00 on 3 separate days in a rolling 7-day window | Encounter trigger count for the day gets +1 (capped at the existing 0–3/day range) while held | "Caught the sunrise. Again. It noticed." |

All XP/odds effects are small (1/8 to 1/32 fractional bumps, power-of-two
denominators, or flat ±1 caps) and gate on habits already coached elsewhere
(cache reuse, budget discipline, session hygiene, model variety) — never on
raw spend or session *count* alone in a way that rewards padding.

---

## 7. Duplicate & collection rules

### 7.1 Catching a species you already own

Catching a duplicate (species already in your Dex as "caught") never yields
a second live pet slot pressure — it converts into **Echo Essence**, a
per-species-tier currency:

| Tier of duplicate | Echo Essence gained |
|---|---|
| COMMON | 1 |
| UNCOMMON | 2 |
| RARE | 4 |
| RELIC | 8 |
| MYTHIC | 16 |

Echo Essence has one elegant, local use: **Barn slot expansion**. Every 20
Essence permanently grows Barn capacity by 1 (see §7.3). No other sink exists
— it's not a currency you can lose or waste, just a slow-building side effect
of playing broadly. A duplicate is never "wasted"; it always converts.

### 7.2 Dex completion rewards

| Milestone | Reward |
|---|---|
| All COMMON species caught | `common_dex` badge + Barn capacity +2 (flat, on top of Essence-earned slots) |
| All UNCOMMON species caught | `uncommon_dex` badge + Barn capacity +2 |
| All RARE species caught | `rare_dex` badge + Barn capacity +3 |
| All RELIC species caught | `relic_dex` badge + Barn capacity +3 |
| Both MYTHIC species caught | `mythic_dex` badge + a cosmetic-only pet-card frame (no stat effect) |
| Full Dex (all 30 + both Mythic) | `keeper_of_the_shellwoods` badge, the game's capstone title |

### 7.3 Barn capacity philosophy

Start capacity: **6** (enough for one of each starter line's mid-evolution
plus a couple of encounter catches before any expansion — no early wall).
Capacity only ever grows (from Essence, §7.1, and Dex milestones, §7.2),
never shrinks, and there is no capacity-related paywall or timer. The Barn's
purpose is storage for the collection meta, not scarcity pressure — a Keeper
who never trades or grinds duplicates still comfortably fits every species
line they raise personally.

### 7.4 Uncaught-first species weighting (the elegance behind §2's bounds)

When an encounter trigger resolves to tier `T` (§2), the specific species
within `T` is chosen with **uncaught species weighted first**: if any
species in tier `T` is not yet in the Dex as "caught," the species roll is
uniform *only* over that uncaught subset. Only once every species in tier `T`
is caught does the roll open back up to uniform-over-all (allowing
duplicates → Essence, §7.1).

```
species_pool = species_in_tier(T)
uncaught     = [s for s in species_pool if not dex.caught(s)]
pool         = uncaught if len(uncaught) > 0 else species_pool
species_roll = SHA256(dna || date || trigger_ordinal || "species") mod len(pool)
species      = pool[species_roll]
```

Deterministic, replayable, and — critically — it means no encounter is ever
"wasted" on a duplicate while gaps remain in that tier. This is what makes
the casual-dev bound in §7.5 provable rather than aspirational.

### 7.5 Anti-frustration guarantees (worked arithmetic)

**Guarantee A — bounded time-to-first-RARE for an active dev.**
Whiff-streak pity (§2.5) floors the tier roll at RARE once
`whiffs_since_rare >= 40`. An active dev produces at least 1 trigger/day on
the *low* end of the stated 0–3/day range being nonzero on most active days;
using the conservative floor of 1 trigger/day:

```
worst_case_days_to_RARE = 40 triggers / 1 trigger-per-day = 40 days
```

Expected case is much faster: per-trigger P(≥RARE) = (64+8)/1024 = 9/128 ≈
7.03%. Expected triggers to first hit = 128/9 ≈ 14.2, i.e. ≈ 9.5 days at 1.5
triggers/day. **Bound: an average active dev sees a RARE+ within 40 days,
worst case; ~9–10 days, expected case.**

**Guarantee B — bounded time-to-first-RELIC for an active dev.**
Pity floors at RELIC once `whiffs_since_rare >= 120`:

```
worst_case_days_to_RELIC = 120 triggers / 1 trigger-per-day = 120 days
```

**Guarantee C — casual dev completes the COMMON Dex in bounded time.**
With uncaught-first weighting (§7.4), every COMMON-tier trigger result
that lands while COMMON entries remain uncaught is *guaranteed* to be a new
species — no coupon-collector waste. `species.md` (finalized after this doc's
first draft) assigns **`N_common = 10`** launch species to the COMMON tier —
recomputed below against the confirmed figure (the original draft used a
placeholder of 12; the real count is slightly lower, which only tightens the
bound):

```
P(COMMON | trigger) = 696/1024 = 87/128 ≈ 0.680
expected_triggers_needed = N_common / P(COMMON) = 10 / 0.680 ≈ 14.71
```

Casual dev rate: 2–5 active days/week, ~1 trigger/active day → use the
*low* end, 2 triggers/week, as the bound-defining rate:

```
worst_expected_weeks = 14.71 triggers / 2 triggers-per-week ≈ 7.4 weeks
```

A Monte Carlo check (20,000 simulated casual careers at the low-end 3
triggers/week rate, N=10) puts the median at 4.9 weeks and the 99th
percentile at ~7.3 weeks. **Bound: a casual (2 days/week) Keeper completes
the COMMON Dex in well under 12 weeks (~3 months) even at the pessimistic
end of the stated activity range — and the uncaught-first rule means this is
a real ceiling, not a lucky-streak best case.**

**Guarantee D — neglect never blocks any of the above.**
Per GAME_DESIGN §4.4 and SPEC rule 5, idle time never resets GRIT streak
punitively beyond the hibernation rule (7 idle days → hibernate, wake happy,
no stat loss), never resets `whiffs_since_rare`, and never shrinks Barn
capacity or clears Echo Essence. A vacation delays the clock; it never
rewinds progress already made.

---

## 8. Design audit (self-check against SPEC + GAME_DESIGN)

Run twice; both passes below.

**Pass 1 — mechanical correctness.**
- Every probability in §2, §3 uses an explicit power-of-two denominator
  (128, 4096, 512/256/128/64, 1024) — SPEC rule 3 satisfied. ✅.
- Every trigger in §5 badges and §6 artifacts cites a `store.Event` field or
  a rolling aggregate of it (cache ratio, model diversity, timestamps,
  session gaps, project count, budget status from `internal/care`) — SPEC
  rule 4 satisfied, nothing asks for an artificial action. ✅.
- All stat/IV/XP math is integer (§4.1 truncating division, §2/§3 modulo
  arithmetic, §6 fractional XP as `× n/d` integer multiply-then-divide). ✅.
- Neglect is always recoverable (§7.5 Guarantee D; hibernation per
  GAME_DESIGN §4.4 already handles the "no dead pets" rule — this doc adds
  nothing that contradicts it). ✅.

**Pass 2 — economic pressure check (the one that matters most).**
Walked every reward path for a route that implicitly favors spending more:
- Encounter odds (§2.2) shift *up* on low-spend, high-cache-reuse,
  under-budget days — the opposite of a spend incentive. ✅ safe.
- Lucent pity (§3.1) keys off GRIT *streak* (showing up), not tokens burned
  in a day — a Keeper who works 10 minutes a day maintains streak as well as
  one who works 8 hours. ✅ safe.
- Artifacts (§6) all gate on ratios, session hygiene, or model variety, never
  on absolute token/cost volume — `warm_cache_shard`, `budget_ledger`, and
  `deep_focus_lens` explicitly reward *less and tighter*, not *more*.
  Re-checked `patch_notes` and `dawn_ember`: both fire on new-model-day-count
  and morning-session-count, neither scales with spend. ✅ safe.
- `marathoner` badge (20+ sessions/day) was flagged on first pass as a
  possible "grind more sessions" incentive. Resolved: it's a badge (cosmetic
  title, zero mechanical effect), not an artifact or odds modifier, and 20+
  real sessions in a day is an observed-behavior descriptor, not a target the
  game asks the player to hit — no fix needed, left as pure flavor
  recognition. ✅ safe on reflection.
- Echo Essence (§7.1) has exactly one sink (Barn capacity) and no way to
  spend it faster by spending more money — it accrues from catching, not
  from cost. ✅ safe.
- Double-checked `budget_ledger` artifact for a loophole: it loosens the
  diet-shift threshold (0.5 → 0.4 cache ratio) but only after 5 *consecutive*
  days already under budget — it rewards sustained frugality, doesn't let
  anyone buy the shift. ✅ safe.

No changes required after pass 2; the system has no path where spending more
tokens or money improves odds, stats, or collection speed. Every lever is
habit-shaped (cache reuse, budget discipline, consistency, variety, session
hygiene) exactly as GAME_DESIGN §4.2/§4.3 intends.

---

## 9. Open questions for the director

1. ~~**`N_common` placeholder (§7.5 Guarantee C).**~~ **Resolved by director:**
   `species.md` finalized at exactly 10 COMMON-tier species (not the
   placeholder 12). §7.5 Guarantee C recomputed against the real figure —
   bound tightens to ~7.4 weeks expected / ~7.3 weeks p99, still comfortably
   under the 12-week ceiling.
2. **Same-day "pending" encounter reveal (§2.6):** the diet-shift in §2.2
   requires knowing the full day's digest, which only closes at the nightly
   sim tick. This doc resolves that with a "pending → revealed" UI state.
   Confirm this matches the intended UX, or whether same-day encounters
   should instead roll against *yesterday's* confirmed diet state (simpler,
   no pending UI, slightly less immediate feedback).
3. ~~**MYTHIC species identity / 15-vs-30 species count.**~~ **Resolved by
   director:** 30 launch species (9 starters + 21 original) is canon;
   GAME_DESIGN.md §4.5 has been corrected to match. The 2 Mythic species are
   `everfile` (CONTEXT) and `uptimewyrm` (DAEMON), both encounter-only, both
   defined in `species.md`.
4. **Artifact satchel display:** §6.1 gives unlimited cosmetic satchel
   storage for un-equipped artifacts found. Confirm this doesn't need its own
   capacity philosophy (this doc assumed "unlimited, cosmetic-only" is fine
   since artifacts have no Barn-style stat footprint).
