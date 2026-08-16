# Sparkroom

**A room where humans talk, and an AI collaborator is a peer — not an assistant — turning the conversation into a shared draft everyone owns.**

## The idea

Pitch an idea. It becomes a live room. Anyone can join. `@ai` pulls in an AI
collaborator (currently named **Sable**) who reacts, argues, and — when the
room is ready — turns the conversation into a working draft. From there,
humans and the AI keep revising it together, and every edit (human or AI)
is visibly attributed.

The bigger vision this POC is a wedge into: a "multiplayer AI" workspace —
any idea a person has, worked on instantly and effectively with both human
collaborators and AI, together, in real time. Not a single-player chatbot,
and not a human-only chat app with AI bolted on as a sidebar.

## Why this wedge, not "build everything"

Research into the space (Aug 2026) found:

- **The category is real and funded.** Dust.tt raised a $40M Series B
  (May 2026) around "multiplayer AI" for the enterprise. Y Combinator named
  Multiplayer AI as one of its Fall 2026 Request for Startups. A wave of
  very early entrants (Bloome, Buzz by Jack Dorsey/Block, YC S26's Mosaic
  and Dock, Stoa) are all racing at some version of this.
- **Nobody owns the hard part yet.** The unfilled gap is genuine real-time
  concurrency between humans AND agents on one shared artifact, with clear
  **attribution** (who did what) and clean **undo**. ChatGPT Canvas and
  Claude Artifacts pioneered human+AI co-editing but are explicitly
  single-player sessions — no simultaneous multi-user editing. Slack/Discord
  own human real-time chat but AI is bolted on as an assistant, not a peer.
  Figma/Miro own multiplayer canvases but AI isn't a room participant.
- **"Super app" (do everything) is the wrong shape.** Western super-app
  attempts reliably fail because incumbents already own each individual job.
  The defensible move is to win one specific artifact type + one audience
  first (a "wedge"), not build chat + canvas + code + docs + calls at once.
- **Cold start, not bad code, kills collaboration products.** The proven
  playbook (Figma, Slack, Discord) is: give away the "magic moment" free,
  saturate one small "atomic network" (hundreds of people, not a market),
  and grow through invite loops — not broad launch.

Full research detail (competitive landscape, technical architecture options,
GTM playbook, monetization models, and a learning path) lives in the
project notes; ask Sable/Claude to regenerate it if this directory doesn't
have it attached.

## What's built so far (this POC)

A working, tested proof of concept of the **core loop**:

1. **Lobby** — pitch an idea, it becomes a live room; browse and join any
   open room.
2. **Room** — group chat where an AI collaborator (Sable) is a full
   participant, not a sidebar. `@ai` (or saying its name) brings it in.
   Presence chips show who's actually in the room right now.
3. **Draft** — one shared artifact per room. The AI proposes a first draft
   from the conversation; humans edit it directly; the AI revises it again.
   Every touch — human or AI — appends a revision and shows up in a visible
   attribution strip (cobalt = human, verdigris = AI, hollow = an undo), and
   any participant can undo the newest change.

### Current implementation

- Single-file React component (`app/sparkroom.jsx`), built to run as a
  Claude.ai Artifact using its built-in persistent key-value storage
  (`window.storage`) as the "backend" and a direct call to the Anthropic
  Messages API for the AI collaborator.
- One storage key per room (`{ msgs, art, pres }` together) to stay well
  under storage rate limits — this was a real bug in an earlier version
  that polled 4+ keys separately and would intermittently wipe the UI on a
  failed read.
- Sync is polling-based (~6s), not true realtime — a known, intentional
  limitation of this POC stage (see Roadmap).
- The draft is an **append-only chain of revisions**: each entry carries the
  body that produced it, who made it, and (for an undo) which revision it
  reverted. Undo is itself a revision that re-lands an earlier body rather
  than popping a stack — two people undoing at once then converges instead of
  corrupting history, and undoing an undo is redo, so no second stack exists.
  Only the newest 12 revisions keep their bodies (older ones survive as
  attribution-only entries), so undo has a finite window that the UI reports
  honestly instead of offering a no-op.
- Tested end-to-end with a simulated multi-user browser harness
  (`tests/harness.cjs`) covering: onboarding, room creation, AI auto-reply,
  plain vs. `@ai` messages, draft creation + human edit + attribution log,
  undo/redo (append-only, correctly attributed), migration of drafts written
  before revisions existed, a second simulated user's messages/presence
  arriving via polling, storage failures (verifies no data wipe), AI network
  failures (graceful message, no crash), and archive. 38/38 checks passing as
  of the last run.

### Known limitations (by design, at this stage)

- **Runs only inside a Claude.ai Artifact** — not a deployed, publicly
  reachable app yet. Storage is scoped to this specific artifact; it is not
  a real multi-tenant backend.
- **No real authentication** — display names are self-reported and stored
  locally per-browser, not verified.
- **Polling, not push** — up to ~6 seconds of lag between what one person
  sees and what another does.
- **Single AI model, single persona** — no model choice, no per-room agent
  customization yet.

## Roadmap: from POC to real product

Per the research, the recommended path to a real (non-Artifact) version:

- **Frontend:** Next.js (App Router) + TypeScript + Tailwind, on Vercel.
- **Backend / DB / Auth:** Supabase (Postgres — SQL transfers directly).
- **Real-time layer:** Liveblocks (or tldraw sync) + Yjs (CRDTs) —
  replaces polling with true live presence, cursors, and conflict-free
  concurrent editing between humans AND the AI agent.
- **AI integration:** Vercel AI SDK, agent-as-room-participant pattern
  (the agent holds its own identity/session in the room, same as a human).
- **Attribution & undo** as a first-class feature, not a nice-to-have —
  this is the product's actual differentiation. The revision model above is
  the first pass at it; the CRDT layer should preserve those semantics rather
  than replace them.

Suggested staged plan: validate the wedge with real users on this POC →
learn the TS/React stack (2-4 weeks) → rebuild on Next.js + Supabase +
Liveblocks (8-12 weeks) → launch into one small, real community (an
"atomic network") before opening broadly → layer in hybrid pricing
(seat + AI usage) once there's real weekly-repeat usage.

## Layout

```
app/
  sparkroom.jsx     the current POC (tested, working)
tests/
  entry.jsx         bundle entry: mounts the component into #root
  harness.cjs       jsdom-based end-to-end test harness used to verify it
```

## Setup & usage

Requires Node ≥ 20.11.

```sh
cd pocs/sparkroom
npm install
npm test          # bundles the component, then runs the 38-check harness
```

`npm test` runs `npm run build` first (esbuild → `tests/bundle.cjs`, gitignored),
because the harness loads the component as a CommonJS bundle inside jsdom. The
run takes ~25s: several checks deliberately wait out a full poll cycle to
prove that a second user's messages arrive and that a flaky storage backend
never wipes the UI.

To run the POC for real, paste `app/sparkroom.jsx` into a Claude.ai Artifact —
it needs that environment's `window.storage` and its Anthropic API access. There
is no local dev server; standing one up means the Next.js rebuild in the roadmap
above, not a wrapper around this file.

## Status

Proof of concept. Not deployed. Built and tested Aug 2026. 38/38 harness checks
passing.
