# Codelings Content Bible — SPEC v1

This is the binding spec for all content design work (species, rarity, moves,
lore). Every content doc under `docs/design/` MUST conform. The game design
context is in `docs/GAME_DESIGN.md` — read it first.

## Hard rules (violations = rejected draft)

1. **Original IP only.** No Pokémon (or any franchise) names, creatures, moves,
   items, or recognizable riffs ("Pikachip" = rejected). Dev-culture puns are
   the house style instead.
2. **Terminal-native.** All art is ASCII/box-drawing, max **16 columns × 6 rows**,
   chars from ASCII printable + `─│┌┐└┘├┤┬┴┼═║╔╗╚╝▲▼◆●○≋~` only. No emoji, no
   ANSI colors in the art itself (color is applied by the TUI per-type).
3. **Determinism-friendly numbers.** All probabilities are exact fractions with
   power-of-two denominators (1/8, 1/64, 1/512, 3/1024…). All stat math is
   integer. No percentages with decimals.
4. **Derived from real signals only.** Every mechanic must trigger off data the
   collector/store can actually observe: tokens in/out, cache read/write, model
   names, project paths, session timestamps, costs, streak days. Nothing that
   requires the game to ask the user to do artificial actions.
5. **Kind by design.** Nothing punishes taking a vacation, working little, or
   being a beginner. Negative states are always recoverable and explained.
   Neglect → hibernation, never death.

## Shared vocabulary (use exactly these terms)

- Creatures: **Codelings**. Players: **Daemonkeepers** (or "Keepers").
- World: **the Shellwoods**. Bad state from wasteful usage: **token bloat**.
- Shiny-equivalent cosmetic: **Lucent** (base roll 1/512 at hatch).
- Inactive pet storage: **the Barn**. Pet's life log: **the Journal**.
- Interchange file: `.codeling`. Battle snapshot: **battle card**.
- Proverb / thesis: *"Feed the mind, not the meter."*

## Types (fixed set of 6)

`CACHE → CONTEXT → RUNTIME → SYNTAX → STREAM → DAEMON → CACHE`
(each is super-effective 2× against the next, ½× against the previous, 1×
otherwise; the wheel is closed). Do not add or rename types.

## Stats (fixed set of 5)

VIGOR (activity volume) · FOCUS (cache-read ratio) · WIT (model routing) ·
GRIT (streaks) · SPARK (rare events). Base stat totals (BST) per rarity tier
are defined in `rarity.md` and must be respected by `species.md`.

## Species lines already canon (do not rename)

- Ember line (deep work): **Cindling → Forgeon → Pyrolith**
- Stream line (fast iteration): **Rivulet → Cascada → Torrentide**
- Vector line (breadth): **Glyphit → Polyglyph → Omniglyph**

## Deliverable format

Each doc starts with a 5-line summary block, then content. Tables over prose
where possible. Every species/move/item gets a stable snake_case `id` (these
become Go identifiers later). Flavor text lines ≤ 90 chars (they render in a
fixed-width TUI). Write journal/encounter lines in the pet's voice: warm,
slightly nerdy, never corporate.

## Quality bar ("production grade")

A draft ships when: ids are unique and stable; numbers respect rules 3 & 4;
every table is internally consistent (no orphan references between docs);
names are pronounceable and Google-clean (no obvious existing product/franchise
collision); flavor is funny to a developer without being cringe; and a Go
engineer could implement the doc without asking a single clarifying question.
