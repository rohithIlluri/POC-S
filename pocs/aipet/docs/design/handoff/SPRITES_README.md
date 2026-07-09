# Handoff: Codelings Companion Sprite Redesign (Dex v2)

## Overview
Redesign of all 30 Codelings companion sprites for the POC-S `design/codelings-companions` branch. The old ASCII silhouettes in `running.html`'s `DEX` array were cluttered and misaligned; this set replaces them with cleaner, cuter, alignment-safe Unicode sprites, plus an idle-animation spec. The page chrome (CRT phosphor theme, filters, cards) is unchanged from `running.html`.

## About the Design Files
`Codelings Dex.dc.html` is a **design reference created in HTML** — a prototype showing intended look and behavior, not production code to copy directly. The task is to **recreate this design in the repo's existing surfaces**: the `DEX` array in `running.html`, and eventually `internal/tui` (Bubble Tea) sprites. `sprites.json` is the machine-readable source of truth for the new art.

## Fidelity
**High-fidelity.** Colors, art strings, spacing, and animation timings are final. Recreate exactly.

## The one critical rendering rule
Sprites render in a monospace `<pre>` with `text-align:center`. **Each line centers independently**, so:
1. Every sprite line is written as a symmetric, self-centering unit — no leading-space padding.
2. Only monospace-safe glyphs are used: box drawing (`╭╮╰╯─═║│┬╥`), blocks (`▐▌█▤▩`), geometric (`▲▴◆●◕◔•`), and `◡ ˘ ω ᴥ ∿ ≋ ‿ ≈ ˖ ˘ ʌ ∪ ☾ ◷ ✕ ╌ ≫`.
3. **Never** full-width CJK punctuation (`＼ ＋ － 《 》`) or exotic marks (`๑ ﹏ ᴗ`) — they have inconsistent advance widths and break alignment. This was the root cause of the old "broken" look.

In the Go TUI, the same strings work in any terminal font; render them centered per-line within a fixed-width cell.

## What to change in the repo
1. **`running.html`** — replace each `art` field in the `DEX` array (script tag, ~line 922) with the matching `art` from `sprites.json` (same `id` keys). Bump `.dexcard pre` to `font-size:12px; line-height:1.3`.
2. **Optional, idle animation** — add to the dexcard `pre`:
   - bob: `0%,100% translateY(0); 50% translateY(-3px)` · duration `3 + (i%4)*0.35`s · ease-in-out
   - blink: `0%,88%,100% scaleY(1); 92%,96% scaleY(0.66)` · duration `4.5 + (i%5)*0.7`s · `transform-origin: center 60%`
   - stagger: `animation-delay: (i%7)*0.4s`
   - mythic only (everfile, uptimewyrm): flicker `opacity 1 → .55 @45% → .9 @47% → .4 @70% → .95 @72%` · 3.6s loop
3. **`internal/tui/pet.go`** (later) — the `faces` map can adopt the same face vocabulary: happy `(◕ ◡ ◕)`, thinking `(◔ ╌ ◔)`, worried `(> ﹏ <)` → use `(> ╌ <)` for width safety.

## Design Tokens (unchanged, from running.html `:root`)
- Background `#070c08` / panel `#0e1710` / lines `#1e3322`, `#2a4630`
- Ink `#c8e6c9` / dim `#7ba382` / faint `#4a6b52` / phosphor `#54fb7e`
- Type colors: cache `#54fb7e` · context `#ffc857` · runtime `#ff6b7f` · syntax `#b18cff` · stream `#5fd7ff` · daemon `#c8e6c9`
- Rarity colors: common `#7ba382` · uncommon `#5fd7ff` · rare `#b18cff` · relic `#ffc857` · mythic `#ff6b7f`
- Sprite glow: `text-shadow: 0 0 9px rgba(typeColor, 0.4)`
- Mythic card: `box-shadow: 0 0 0 1px rgba(255,107,127,.3), 0 0 24px rgba(255,107,127,.12)`; relic: `0 0 16px rgba(255,200,87,.08)`
- Font: `ui-monospace,"SF Mono",SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace`

## Sprite design language (for future species)
- 3–4 rows: [crown/ears] → [face] → [body/jaw] → [feet/detail]
- Faces are `(eye mouth eye)` with spaces: `(◕ ◡ ◕)` happy, `(˘ ◡ ˘)` content, `(˘ ω ˘)` asleep, `(◆ ◡ ◆)` relic, `(✕ ╌ ✕)` zombie, `(◔ ╌ ◔)` stale
- Evolution reads as growth: wider crowns (`▴` → `▴▴▴` → `▲▲▲`), heavier borders (`╭╮` → `╔╗`), extra detail row
- One signature detail per species: Hoardlet's `$$` belly, Threadwolf's twin faces, Uptimewyrm's `·365·`, Nightproc's `ᶻ ᶻ ᶻ`

## Files
- `sprites.json` — all 30 species: id, name, type, rarity, BST, habitat, final `art` string (source of truth)
- `Codelings Dex.dc.html` — the visual reference prototype (design-tool format; open for reference, don't ship)
