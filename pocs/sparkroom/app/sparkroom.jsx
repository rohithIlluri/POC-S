import { useState, useEffect, useRef } from "react";

/*
  SPARKROOM POC v2 — multiplayer-AI workspace core loop.
  • Lobby: pitch an idea → live room. Browse & join anyone's room.
  • Room: group chat where Sable (AI) is a peer (@ai), presence chips,
    one shared draft with an attribution weave (cobalt=human, verdigris=AI).
  Reliability design:
  • ONE storage key per room ({msgs, art, pres}) + one rooms index →
    few requests, far under storage rate limits.
  • Reads that fail keep the last good state (never wipe the UI);
    a "syncing…" pill shows when the last poll failed.
  • In-memory fallback if window.storage is unavailable.
  • System fonts only (no external imports).
*/

const ROOMS_KEY = "sr4:rooms";
const ME_KEY = "sr4:me";
const rKey = (id) => "sr4:room:" + id;
const AI_NAME = "Sable";

const C = {
  paper: "#F2F3F0",
  panel: "#FAFBF9",
  ink: "#23272B",
  faint: "#7A8078",
  line: "#D8DBD4",
  human: "#2B50C7",
  humanTint: "#E9EDFB",
  ai: "#2E7D62",
  aiTint: "#E3F0E9",
  danger: "#A63D2F",
};
const fontDisplay = "'Avenir Next','Segoe UI',system-ui,-apple-system,sans-serif";
const fontBody = "system-ui,-apple-system,'Segoe UI',Roboto,sans-serif";
const fontMeta = "ui-monospace,'SF Mono',Menlo,Consolas,monospace";

// ---------------- storage adapter ----------------
const memDB = {}; // fallback store if window.storage is missing
const hasStorage = () =>
  typeof window !== "undefined" && window.storage && typeof window.storage.get === "function";

async function sGet(key, shared = true) {
  // returns { ok, value } — ok:false means transient error (keep old state)
  if (!hasStorage()) return { ok: true, value: key in memDB ? memDB[key] : null };
  try {
    const r = await window.storage.get(key, shared);
    if (!r || r.value == null) return { ok: true, value: null };
    try {
      return { ok: true, value: JSON.parse(r.value) };
    } catch {
      return { ok: true, value: null }; // corrupted → treat as empty
    }
  } catch {
    // missing key OR transient failure — callers decide via firstLoad flag
    return { ok: false, value: null };
  }
}
async function sSet(key, value, shared = true) {
  if (!hasStorage()) {
    memDB[key] = value;
    return true;
  }
  try {
    const r = await window.storage.set(key, JSON.stringify(value), shared);
    return !!r;
  } catch {
    return false;
  }
}
async function sDelete(key, shared = true) {
  if (!hasStorage()) {
    delete memDB[key];
    return true;
  }
  try {
    await window.storage.delete(key, shared);
    return true;
  } catch {
    return false;
  }
}

const uid = () => Date.now().toString(36) + Math.random().toString(36).slice(2, 7);
const sleep = (ms) => new Promise((res) => setTimeout(res, ms));

function timeAgo(ts) {
  const s = Math.max(1, Math.floor((Date.now() - ts) / 1000));
  if (s < 60) return "now";
  const m = Math.floor(s / 60);
  if (m < 60) return m + "m";
  const h = Math.floor(m / 60);
  if (h < 24) return h + "h";
  return Math.floor(h / 24) + "d";
}

// ---------------- artifact revisions ----------------
/*
  The draft is an append-only chain of revisions. Every touch — human, AI, or
  an undo — appends one; nothing is ever rewritten or popped.

  Undo is itself a revision that re-lands an earlier body. That matters for a
  multiplayer room: a pop-the-stack undo run by two people at once corrupts the
  history, while a re-land is idempotent — the second undo either changes
  nothing or reverts the first, and both outcomes are honest. It also keeps the
  attribution weave truthful about who reverted whom, which a pop would erase.

  Bodies are the expensive part, so only the newest REV_FULL revisions keep
  theirs; older ones survive as attribution-only entries. Undo therefore has a
  finite window, and undoTarget() reports when a body has aged out rather than
  offering an undo that would silently do nothing.
*/
const REV_FULL = 12; // newest revisions that keep their body (these are undoable)
const REV_META = 24; // older revisions kept as attribution-only

const revKind = (k) => (k === "ai" ? "ai" : k === "undo" ? "undo" : "human");

function pruneRevs(log) {
  const kept = log.slice(-REV_META);
  const cut = Math.max(0, kept.length - REV_FULL);
  return kept.map((e, i) => (i < cut && e.content != null ? { ...e, content: null, dropped: true } : e));
}

function normalizeArtifact(a) {
  if (!a || typeof a !== "object" || typeof a.content !== "string") return null;
  const title = (typeof a.title === "string" && a.title) || "Untitled draft";
  const log = (Array.isArray(a.log) ? a.log : [])
    .filter((e) => e && typeof e === "object")
    .map((e, i) => ({
      id: typeof e.id === "string" ? e.id : "r" + i + "-" + (Number(e.ts) || 0),
      by: typeof e.by === "string" ? e.by : "someone",
      kind: revKind(e.kind),
      ts: Number(e.ts) || 0,
      title: typeof e.title === "string" ? e.title : null,
      content: typeof e.content === "string" ? e.content : null,
      undoOf: typeof e.undoOf === "string" ? e.undoOf : null,
      dropped: !!e.dropped,
    }));

  // Drafts written before revisions existed (or by an older client) log bare
  // touches with no bodies. Adopt the live content as the head revision so
  // history starts here instead of being lost. Idempotent: once the head
  // matches the body, re-normalizing is a no-op.
  const head = log[log.length - 1];
  if (head && head.content == null && !head.dropped) {
    log[log.length - 1] = { ...head, content: a.content, title: head.title || title };
  } else if (!head || head.content !== a.content) {
    log.push({
      id: "head-" + (Number(a.ts) || 0),
      by: typeof a.editedBy === "string" ? a.editedBy : "someone",
      kind: "human",
      ts: Number(a.ts) || 0,
      title,
      content: a.content,
      undoOf: null,
      dropped: false,
    });
  }

  const h = log[log.length - 1];
  return { title: h.title || title, content: a.content, editedBy: h.by, ts: h.ts, log: pruneRevs(log) };
}

// The revision an undo would re-land, or null when there is nothing to undo.
function undoTarget(art) {
  if (!art || !Array.isArray(art.log) || art.log.length < 2) return null;
  const head = art.log[art.log.length - 1];
  const prev = art.log[art.log.length - 2];
  if (!prev || prev.content == null) return null; // body aged out of the window
  if (prev.content === head.content) return null; // would change nothing
  return { head, prev };
}

function commitRevision(d, { by, kind, title, content, undoOf = null }) {
  const prev = d.art;
  const rev = {
    id: uid(),
    by,
    kind: revKind(kind),
    ts: Date.now(),
    title: title || (prev && prev.title) || "Untitled draft",
    content,
    undoOf,
    dropped: false,
  };
  return {
    ...d,
    art: {
      title: rev.title,
      content,
      editedBy: by,
      ts: rev.ts,
      log: pruneRevs([...((prev && prev.log) || []), rev]),
    },
  };
}

function undoLast(d, by) {
  const t = undoTarget(d.art);
  if (!t) return d;
  return commitRevision(d, { by, kind: "undo", title: t.prev.title, content: t.prev.content, undoOf: t.head.id });
}

function revLabel(e, log) {
  const when = e.ts ? " · " + timeAgo(e.ts) + " ago" : "";
  if (e.kind === "undo") {
    const target = log.find((x) => x.id === e.undoOf);
    return e.by + " undid " + (target ? target.by + "'s" : "an earlier") + " edit" + when;
  }
  return e.by + (e.kind === "ai" ? " (AI)" : "") + " edited" + when;
}

const emptyRoomData = () => ({ msgs: [], art: null, pres: {} });
function normalizeRoomData(v) {
  if (!v || typeof v !== "object") return emptyRoomData();
  return {
    msgs: Array.isArray(v.msgs) ? v.msgs : [],
    art: normalizeArtifact(v.art),
    pres: v.pres && typeof v.pres === "object" ? v.pres : {},
  };
}
function extractJSON(text) {
  if (!text) return null;
  const m = text.match(/\{[\s\S]*\}/);
  if (!m) return null;
  try {
    return JSON.parse(m[0]);
  } catch {
    return null;
  }
}

const css = `
  * { box-sizing: border-box; }
  html, body { margin: 0; }
  textarea, input { font-family: ${fontBody}; }
  textarea:focus, input:focus, button:focus-visible { outline: 2px solid ${C.human}; outline-offset: 2px; }
  @keyframes rise { from { opacity: 0; transform: translateY(4px);} to { opacity: 1; transform: none;} }
  @keyframes blink { 0%,80%,100% { opacity: .25;} 40% { opacity: 1;} }
  @keyframes pulse { 0%,100% { opacity: 1;} 50% { opacity: .35;} }
  .msg { animation: rise .25s ease both; }
  .dot { animation: blink 1.2s infinite; }
  .dot2 { animation-delay: .2s; } .dot3 { animation-delay: .4s; }
  .live { animation: pulse 2s infinite; }
  @media (prefers-reduced-motion: reduce) { .msg, .dot, .live { animation: none; } }
`;

function Btn({ onClick, disabled, kind = "ink", children, style }) {
  const looks = {
    ink: { background: C.ink, color: C.paper, border: "none" },
    solidHuman: { background: C.human, color: "#fff", border: "none" },
    human: { background: C.humanTint, color: C.human, border: "1.5px solid " + C.human },
    ai: { background: C.aiTint, color: C.ai, border: "1.5px solid " + C.ai },
    ghost: { background: "transparent", color: C.faint, border: "1.5px solid " + C.line },
  };
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      style={{
        padding: "10px 14px",
        borderRadius: 9,
        fontFamily: fontBody,
        fontWeight: 600,
        fontSize: 13.5,
        cursor: disabled ? "default" : "pointer",
        opacity: disabled ? 0.45 : 1,
        ...looks[kind],
        ...style,
      }}
    >
      {children}
    </button>
  );
}

export default function SparkroomPOC() {
  const [booted, setBooted] = useState(false);
  const [me, setMe] = useState(null);
  const [nameDraft, setNameDraft] = useState("");

  const [view, setView] = useState({ screen: "lobby" });
  const [rooms, setRooms] = useState([]);
  const [newIdea, setNewIdea] = useState("");
  const [creating, setCreating] = useState(false);

  const [rd, setRd] = useState(emptyRoomData());
  const [roomLoaded, setRoomLoaded] = useState(false);
  const [syncTrouble, setSyncTrouble] = useState(false);
  const [tab, setTab] = useState("talk");
  const [draft, setDraft] = useState("");
  const [aiBusy, setAiBusy] = useState(false);
  const [editing, setEditing] = useState(false);
  const [editText, setEditText] = useState("");
  const [confirmWipe, setConfirmWipe] = useState(false);
  const feedRef = useRef(null);

  const viewRef = useRef(view);
  viewRef.current = view;
  const rdRef = useRef(rd);
  rdRef.current = rd;
  const meRef = useRef(me);
  meRef.current = me;

  const room = rooms.find((r) => r.id === view.id);

  // ---------------- boot ----------------
  useEffect(() => {
    (async () => {
      const saved = await sGet(ME_KEY, false);
      if (saved.ok && saved.value && saved.value.name) setMe(saved.value.name);
      setBooted(true);
    })();
  }, []);

  // ---------------- lobby polling (only on lobby) ----------------
  useEffect(() => {
    if (view.screen !== "lobby") return;
    let stop = false;
    const load = async () => {
      const r = await sGet(ROOMS_KEY);
      if (!stop && r.ok) setRooms(Array.isArray(r.value) ? r.value : []);
    };
    load();
    const id = setInterval(load, 8000);
    return () => {
      stop = true;
      clearInterval(id);
    };
  }, [view.screen]);

  // ---------------- room polling + heartbeat ----------------
  const pollRoom = async (roomId, firstLoad) => {
    const r = await sGet(rKey(roomId));
    if (viewRef.current.screen !== "room" || viewRef.current.id !== roomId) return;
    if (r.ok) {
      setRd(normalizeRoomData(r.value));
      setRoomLoaded(true);
      setSyncTrouble(false);
    } else if (firstLoad) {
      // nothing to preserve yet — start empty rather than hanging
      setRd(emptyRoomData());
      setRoomLoaded(true);
      setSyncTrouble(true);
    } else {
      setSyncTrouble(true); // keep last good state
    }
  };

  // read-merge-write with one retry; never throws
  const mutateRoom = async (roomId, fn) => {
    for (let attempt = 0; attempt < 2; attempt++) {
      const r = await sGet(rKey(roomId));
      const base = r.ok && r.value ? normalizeRoomData(r.value) : normalizeRoomData(rdRef.current);
      const next = normalizeRoomData(fn(base));
      // stamp own presence on every write (free heartbeat)
      if (meRef.current) next.pres = { ...next.pres, [meRef.current]: Date.now() };
      // prune stale presence
      for (const k of Object.keys(next.pres)) {
        if (Date.now() - next.pres[k] > 5 * 60 * 1000) delete next.pres[k];
      }
      const wrote = await sSet(rKey(roomId), next);
      if (wrote) {
        if (viewRef.current.screen === "room" && viewRef.current.id === roomId) setRd(next);
        return next;
      }
      await sleep(400);
    }
    setSyncTrouble(true);
    return rdRef.current;
  };

  useEffect(() => {
    if (view.screen !== "room" || !me) return;
    const roomId = view.id;
    setRoomLoaded(false);
    setSyncTrouble(false);
    pollRoom(roomId, true);
    mutateRoom(roomId, (d) => d); // announce presence
    const poll = setInterval(() => pollRoom(roomId, false), 6000);
    const hb = setInterval(() => mutateRoom(roomId, (d) => d), 45000);
    return () => {
      clearInterval(poll);
      clearInterval(hb);
    };
  }, [view.screen, view.id, me]);

  useEffect(() => {
    if (feedRef.current) feedRef.current.scrollTop = feedRef.current.scrollHeight;
  }, [rd.msgs.length, aiBusy, tab]);

  // ---------------- actions ----------------
  const claimName = async () => {
    const nm = nameDraft.trim().slice(0, 20);
    if (!nm) return;
    setMe(nm);
    await sSet(ME_KEY, { name: nm }, false);
  };

  const addMessage = (d, author, role, text) => ({
    ...d,
    msgs: [...d.msgs, { id: uid(), author, role, text, ts: Date.now() }].slice(-60),
  });

  const undoDraft = async () => {
    if (!room || !undoTarget(rd.art)) return;
    // mutateRoom re-reads before applying, so this undoes whatever is actually
    // the newest revision — not the one this tab happened to be showing.
    await mutateRoom(room.id, (d) => undoLast(d, me));
  };

  const callAI = async (roomId, roomInfo) => {
    setAiBusy(true);
    try {
      const fresh = await sGet(rKey(roomId));
      const data = fresh.ok && fresh.value ? normalizeRoomData(fresh.value) : rdRef.current;
      const recent = data.msgs
        .slice(-16)
        .map((m) => m.author + (m.role === "ai" ? " (AI)" : "") + ": " + m.text)
        .join("\n");
      const artNote = data.art
        ? '\n\nCurrent shared draft titled "' + data.art.title + '" (last edited by ' + data.art.editedBy + "):\n---\n" + data.art.content + "\n---"
        : "\n\nNo shared draft exists yet.";
      const prompt =
        "You are " + AI_NAME + ", an AI collaborator who is a full participant in a shared workroom, alongside humans. You are a peer, not an assistant: brief, warm, direct, opinionated when it helps the group move.\n\n" +
        'Room: "' + ((roomInfo && roomInfo.name) || "Untitled") + '" — pitched as: ' + ((roomInfo && roomInfo.pitch) || "(no pitch)") + "\n\n" +
        "Recent conversation (oldest first):\n" + recent + artNote + "\n\n" +
        'Respond ONLY with valid JSON. No markdown fences, no text outside the JSON. Exact shape:\n' +
        '{"message": "your chat reply to the room, under 80 words, conversational", "artifact": null}\n' +
        "OR, when the room clearly wants a draft created or revised (asked to draft, build, write up, plan, revise, update the doc):\n" +
        '{"message": "...", "artifact": {"title": "short title", "content": "the FULL updated document in plain markdown, complete and self-contained"}}';

      const response = await fetch("https://api.anthropic.com/v1/messages", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          model: "claude-sonnet-4-6",
          max_tokens: 1200,
          messages: [{ role: "user", content: prompt }],
        }),
      });
      const out = await response.json();
      const text = ((out && out.content) || [])
        .filter((b) => b && b.type === "text")
        .map((b) => b.text)
        .join("\n");
      const parsed = extractJSON(text);
      const reply =
        (parsed && typeof parsed.message === "string" && parsed.message) ||
        (text ? text.slice(0, 400) : "I lost my train of thought — ask me again?");
      await mutateRoom(roomId, (d) => {
        let next = addMessage(d, AI_NAME, "ai", reply);
        if (parsed && parsed.artifact && typeof parsed.artifact.content === "string" && parsed.artifact.content) {
          next = commitRevision(next, {
            by: AI_NAME,
            kind: "ai",
            title: parsed.artifact.title,
            content: parsed.artifact.content,
          });
        }
        return next;
      });
    } catch (e) {
      console.error("AI call failed", e);
      await mutateRoom(roomId, (d) =>
        addMessage(d, AI_NAME, "ai", "I couldn't reach the model just now — try me again in a moment.")
      );
    } finally {
      setAiBusy(false);
    }
  };

  const createRoom = async () => {
    const pitch = newIdea.trim();
    if (!pitch || creating) return;
    setCreating(true);
    try {
      const idx = await sGet(ROOMS_KEY);
      const list = idx.ok && Array.isArray(idx.value) ? idx.value : rooms;
      const r = {
        id: uid(),
        name: pitch.length > 42 ? pitch.slice(0, 42) + "…" : pitch,
        pitch,
        createdBy: me,
        ts: Date.now(),
      };
      const nextList = [r, ...list].slice(0, 30);
      await sSet(ROOMS_KEY, nextList);
      setRooms(nextList);
      setNewIdea("");
      setRd(emptyRoomData());
      setRoomLoaded(true);
      setTab("talk");
      setView({ screen: "room", id: r.id });
      await mutateRoom(r.id, (d) => addMessage(d, me, "human", pitch));
      await callAI(r.id, r);
    } finally {
      setCreating(false);
    }
  };

  const handleSend = async () => {
    const text = draft.trim();
    if (!text || !room) return;
    setDraft("");
    await mutateRoom(room.id, (d) => addMessage(d, me, "human", text));
    if (/@ai\b/i.test(text) || new RegExp("\\b" + AI_NAME + "\\b", "i").test(text)) {
      await callAI(room.id, room);
    }
  };

  const askForDraft = async () => {
    if (aiBusy || !room) return;
    await mutateRoom(room.id, (d) =>
      addMessage(d, me, "human", "@ai — turn what we've got into a working draft we can all edit.")
    );
    setTab("draft");
    await callAI(room.id, room);
  };

  const archiveRoom = async () => {
    if (!confirmWipe) {
      setConfirmWipe(true);
      setTimeout(() => setConfirmWipe(false), 3500);
      return;
    }
    const idx = await sGet(ROOMS_KEY);
    const list = (idx.ok && Array.isArray(idx.value) ? idx.value : rooms).filter((r) => r.id !== view.id);
    await sSet(ROOMS_KEY, list);
    await sDelete(rKey(view.id));
    setRooms(list);
    setConfirmWipe(false);
    setView({ screen: "lobby" });
  };

  const headRev = rd.art && rd.art.log && rd.art.log.length ? rd.art.log[rd.art.log.length - 1] : null;
  const undoable = undoTarget(rd.art);
  const canUndo = !!undoable;

  const here = Object.entries(rd.pres)
    .filter(([, ts]) => Date.now() - ts < 90 * 1000)
    .map(([n]) => n);

  // ================= SCREENS =================
  if (!booted) {
    return (
      <div style={{ minHeight: "100vh", background: C.paper, display: "flex", alignItems: "center", justifyContent: "center", fontFamily: fontMeta, color: C.faint, fontSize: 12 }}>
        <style>{css}</style>
        opening sparkroom…
      </div>
    );
  }

  if (!me) {
    return (
      <div style={{ minHeight: "100vh", background: C.paper, color: C.ink, fontFamily: fontBody, display: "flex", alignItems: "center", justifyContent: "center", padding: 24 }}>
        <style>{css}</style>
        <div style={{ width: "100%", maxWidth: 430 }}>
          <div style={{ fontFamily: fontMeta, fontSize: 11, letterSpacing: "0.18em", color: C.faint, marginBottom: 14 }}>
            IDEAS → ROOMS → DRAFTS · HUMANS + AI
          </div>
          <h1 style={{ fontFamily: fontDisplay, fontWeight: 800, fontSize: 46, lineHeight: 1.02, margin: "0 0 10px" }}>
            Spark<span style={{ color: C.human }}>room</span>
          </h1>
          <p style={{ fontSize: 15, lineHeight: 1.55, color: "#3D423F", margin: "0 0 6px" }}>
            Pitch an idea and it becomes a live room. People join, <strong style={{ color: C.ai }}>{AI_NAME}</strong> (an AI) works alongside you, and together you weave one shared draft.
          </p>
          <p style={{ fontSize: 12.5, color: C.faint, margin: "0 0 22px" }}>
            Everything here is one shared, public space — rooms, messages, and drafts are visible to everyone who opens this prototype.
          </p>
          <input
            value={nameDraft}
            onChange={(e) => setNameDraft(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && claimName()}
            placeholder="Your name"
            style={{ width: "100%", padding: "13px 14px", fontSize: 16, border: "1.5px solid " + C.line, borderRadius: 10, background: C.panel, color: C.ink, marginBottom: 10 }}
          />
          <Btn onClick={claimName} disabled={!nameDraft.trim()} style={{ width: "100%", padding: "13px 14px", fontSize: 15 }}>
            Step inside
          </Btn>
        </div>
      </div>
    );
  }

  if (view.screen === "lobby") {
    return (
      <div style={{ minHeight: "100vh", background: C.paper, color: C.ink, fontFamily: fontBody }}>
        <style>{css}</style>
        <div style={{ maxWidth: 560, margin: "0 auto", padding: "18px 16px 40px" }}>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline", marginBottom: 4 }}>
            <div style={{ fontFamily: fontDisplay, fontWeight: 800, fontSize: 24 }}>
              Spark<span style={{ color: C.human }}>room</span>
            </div>
            <span style={{ fontFamily: fontMeta, fontSize: 11, color: C.faint }}>you are {me}</span>
          </div>
          <p style={{ fontSize: 13.5, color: C.faint, margin: "0 0 18px" }}>
            Every idea below is a live room. Open one, or start your own.
          </p>

          <div style={{ background: C.panel, border: "1.5px solid " + C.line, borderRadius: 14, padding: 14, marginBottom: 22 }}>
            <div style={{ fontFamily: fontMeta, fontSize: 10.5, letterSpacing: "0.14em", color: C.faint, marginBottom: 8 }}>
              PITCH AN IDEA
            </div>
            <textarea
              value={newIdea}
              onChange={(e) => setNewIdea(e.target.value)}
              rows={2}
              placeholder={'"An app pairing neighbors for dog walks" — anything. ' + AI_NAME + " meets you inside."}
              style={{ width: "100%", resize: "none", padding: "10px 12px", fontSize: 14.5, border: "1.5px solid " + C.line, borderRadius: 10, background: C.paper, color: C.ink, marginBottom: 8, lineHeight: 1.45 }}
            />
            <Btn onClick={createRoom} disabled={!newIdea.trim() || creating} style={{ width: "100%" }}>
              {creating ? "Opening your room…" : "Open a room for it →"}
            </Btn>
          </div>

          <div style={{ fontFamily: fontMeta, fontSize: 10.5, letterSpacing: "0.14em", color: C.faint, marginBottom: 10 }}>
            LIVE ROOMS · {rooms.length}
          </div>
          {rooms.length === 0 && (
            <div style={{ border: "1.5px dashed " + C.line, borderRadius: 12, padding: 18, fontSize: 14, color: "#4A4F4B", background: C.panel }}>
              No rooms yet — yours could be the first on the board.
            </div>
          )}
          {rooms.map((r) => (
            <button
              key={r.id}
              onClick={() => {
                setRd(emptyRoomData());
                setTab("talk");
                setView({ screen: "room", id: r.id });
              }}
              style={{ display: "block", width: "100%", textAlign: "left", background: C.panel, border: "1.5px solid " + C.line, borderRadius: 12, padding: "13px 14px", marginBottom: 10, cursor: "pointer", fontFamily: fontBody }}
            >
              <div style={{ fontWeight: 600, fontSize: 15, color: C.ink, marginBottom: 3 }}>{r.name}</div>
              <div style={{ fontFamily: fontMeta, fontSize: 10.5, color: C.faint }}>
                opened by <span style={{ color: C.human }}>{r.createdBy}</span> · {timeAgo(r.ts)} ago
              </div>
            </button>
          ))}
        </div>
      </div>
    );
  }

  if (!room) {
    return (
      <div style={{ minHeight: "100vh", background: C.paper, display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", gap: 12, fontFamily: fontBody, color: C.faint }}>
        <style>{css}</style>
        <div>This room was archived.</div>
        <Btn kind="ghost" onClick={() => setView({ screen: "lobby" })}>← Back to the lobby</Btn>
      </div>
    );
  }

  return (
    <div style={{ height: "100vh", display: "flex", flexDirection: "column", background: C.paper, color: C.ink, fontFamily: fontBody }}>
      <style>{css}</style>

      <div style={{ padding: "10px 14px 0", borderBottom: "1px solid " + C.line, background: C.panel }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <button onClick={() => setView({ screen: "lobby" })} style={{ border: "none", background: "transparent", color: C.faint, fontSize: 18, cursor: "pointer", padding: 2 }}>
            ←
          </button>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontFamily: fontDisplay, fontWeight: 700, fontSize: 16, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{room.name}</div>
          </div>
          {syncTrouble && (
            <span style={{ fontFamily: fontMeta, fontSize: 10, color: C.danger }}>syncing…</span>
          )}
          <button onClick={archiveRoom} style={{ fontFamily: fontMeta, fontSize: 10.5, border: "1px solid " + (confirmWipe ? C.danger : C.line), color: confirmWipe ? C.danger : C.faint, background: "transparent", borderRadius: 7, padding: "4px 8px", cursor: "pointer" }}>
            {confirmWipe ? "tap again" : "archive"}
          </button>
        </div>

        <div style={{ display: "flex", gap: 6, alignItems: "center", margin: "8px 0 0", flexWrap: "wrap" }}>
          <span className="live" style={{ width: 7, height: 7, borderRadius: 99, background: C.ai, display: "inline-block" }} />
          <span style={{ fontFamily: fontMeta, fontSize: 10.5, color: C.faint }}>here now:</span>
          {[...new Set([me, ...here])].map((n) => (
            <span key={n} style={{ fontFamily: fontMeta, fontSize: 10.5, color: n === me ? C.human : "#4A4F4B", border: "1px solid " + C.line, borderRadius: 99, padding: "2px 8px", background: C.paper }}>
              {n}{n === me ? " (you)" : ""}
            </span>
          ))}
          <span style={{ fontFamily: fontMeta, fontSize: 10.5, color: C.ai, border: "1px solid " + C.ai, borderRadius: 99, padding: "2px 8px", background: C.aiTint }}>
            {AI_NAME} · AI
          </span>
        </div>

        <div style={{ display: "flex", gap: 4, marginTop: 8 }}>
          {["talk", "draft"].map((t) => (
            <button key={t} onClick={() => setTab(t)} style={{ flex: 1, padding: "9px 6px", border: "none", cursor: "pointer", background: "transparent", borderBottom: tab === t ? "2.5px solid " + C.ink : "2.5px solid transparent", fontFamily: fontBody, fontWeight: 600, fontSize: 13.5, color: tab === t ? C.ink : C.faint, display: "flex", alignItems: "center", justifyContent: "center", gap: 8 }}>
              {t === "talk" ? "The room" : "The draft"}
              {t === "draft" && rd.art && (
                <span style={{ display: "flex", gap: 1.5 }}>
                  {(rd.art.log || []).slice(-8).map((e, i) => (
                    <span
                      key={i}
                      style={{
                        width: 5,
                        height: 9,
                        borderRadius: 1,
                        background: e.kind === "undo" ? "transparent" : e.kind === "ai" ? C.ai : C.human,
                        boxShadow: e.kind === "undo" ? "inset 0 0 0 1.5px " + C.faint : "none",
                      }}
                    />
                  ))}
                </span>
              )}
            </button>
          ))}
        </div>
      </div>

      {tab === "talk" && (
        <>
          <div ref={feedRef} style={{ flex: 1, overflowY: "auto", padding: "14px 16px" }}>
            {!roomLoaded && (
              <div style={{ fontFamily: fontMeta, fontSize: 12, color: C.faint }}>joining the room…</div>
            )}
            {roomLoaded && rd.msgs.length === 0 && !aiBusy && (
              <div style={{ border: "1.5px dashed " + C.line, borderRadius: 12, padding: 16, fontSize: 14, lineHeight: 1.6, color: "#4A4F4B", background: C.panel }}>
                Quiet in here. Say something — add <span style={{ fontFamily: fontMeta, color: C.ai, fontWeight: 600 }}>@ai</span> to bring {AI_NAME} in.
              </div>
            )}
            {rd.msgs.map((m) => {
              const isAI = m.role === "ai";
              const mine = m.author === me && !isAI;
              return (
                <div key={m.id} className="msg" style={{ display: "flex", gap: 10, margin: "0 0 14px" }}>
                  <div style={{ width: 3, borderRadius: 2, background: isAI ? C.ai : C.human, opacity: mine ? 1 : 0.55 }} />
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ display: "flex", gap: 8, alignItems: "baseline", marginBottom: 3 }}>
                      <span style={{ fontWeight: 600, fontSize: 13, color: isAI ? C.ai : C.human }}>
                        {m.author}{isAI ? " · AI" : mine ? " · you" : ""}
                      </span>
                      <span style={{ fontFamily: fontMeta, fontSize: 10.5, color: C.faint }}>{timeAgo(m.ts)}</span>
                    </div>
                    <div style={{ fontSize: 14.5, lineHeight: 1.5, whiteSpace: "pre-wrap", overflowWrap: "break-word" }}>{m.text}</div>
                  </div>
                </div>
              );
            })}
            {aiBusy && (
              <div style={{ display: "flex", gap: 10, alignItems: "center", color: C.ai, fontSize: 13 }}>
                <div style={{ width: 3, height: 18, borderRadius: 2, background: C.ai }} />
                <span style={{ fontWeight: 600 }}>{AI_NAME} is working</span>
                <span className="dot">●</span>
                <span className="dot dot2">●</span>
                <span className="dot dot3">●</span>
              </div>
            )}
          </div>
          <div style={{ padding: "10px 12px 14px", borderTop: "1px solid " + C.line, background: C.panel }}>
            <div style={{ display: "flex", gap: 8 }}>
              <textarea
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.shiftKey) {
                    e.preventDefault();
                    handleSend();
                  }
                }}
                rows={1}
                placeholder={"Message the room — @ai brings " + AI_NAME + " in"}
                style={{ flex: 1, resize: "none", padding: "11px 12px", fontSize: 15, border: "1.5px solid " + C.line, borderRadius: 10, background: C.paper, color: C.ink }}
              />
              <Btn onClick={handleSend} disabled={!draft.trim()} style={{ padding: "0 16px" }}>
                Send
              </Btn>
            </div>
            <Btn kind="ai" onClick={askForDraft} disabled={aiBusy || rd.msgs.length === 0} style={{ marginTop: 8, width: "100%" }}>
              ✦ Ask {AI_NAME} to build the draft from this conversation
            </Btn>
          </div>
        </>
      )}

      {tab === "draft" && (
        <div style={{ flex: 1, overflowY: "auto", padding: 16 }}>
          {!rd.art ? (
            <div style={{ border: "1.5px dashed " + C.line, borderRadius: 12, padding: 18, fontSize: 14, lineHeight: 1.6, color: "#4A4F4B", background: C.panel }}>
              No draft yet. This page holds the room's one shared artifact — every edit, human or AI, gets woven into the strip.
              <div style={{ marginTop: 12 }}>
                <Btn kind="ai" onClick={askForDraft} disabled={aiBusy || rd.msgs.length === 0}>
                  ✦ Have {AI_NAME} start it
                </Btn>
              </div>
            </div>
          ) : (
            <div>
              <div style={{ fontFamily: fontDisplay, fontWeight: 700, fontSize: 23, lineHeight: 1.15, marginBottom: 6 }}>{rd.art.title}</div>
              <div style={{ fontFamily: fontMeta, fontSize: 11, color: C.faint, marginBottom: 12 }}>
                {headRev && headRev.kind === "undo" ? "reverted by " : "last touched by "}
                <span style={{ color: headRev && headRev.kind === "ai" ? C.ai : C.human, fontWeight: 600 }}>{rd.art.editedBy}</span>{" "}
                · {timeAgo(rd.art.ts)} ago
              </div>
              <div style={{ display: "flex", gap: 2, marginBottom: 14, alignItems: "center", flexWrap: "wrap" }}>
                {(rd.art.log || []).map((e, i) => (
                  <span
                    key={e.id || i}
                    title={revLabel(e, rd.art.log || [])}
                    style={{
                      width: 14,
                      height: 8,
                      borderRadius: 2,
                      background: e.kind === "undo" ? "transparent" : e.kind === "ai" ? C.ai : C.human,
                      boxShadow: e.kind === "undo" ? "inset 0 0 0 1.5px " + C.faint : "none",
                      opacity: e.dropped ? 0.4 : 1,
                    }}
                  />
                ))}
                <span style={{ fontFamily: fontMeta, fontSize: 10, color: C.faint, marginLeft: 6 }}>
                  woven by <span style={{ color: C.human }}>humans</span> + <span style={{ color: C.ai }}>{AI_NAME}</span>
                </span>
              </div>
              {!editing && (
                <div style={{ marginBottom: 12 }}>
                  <Btn kind="ghost" onClick={undoDraft} disabled={!canUndo} style={{ width: "100%", padding: "8px 12px", fontSize: 12.5 }}>
                    {canUndo ? "↩ Undo " + undoable.head.by + "'s change" : "Nothing to undo"}
                  </Btn>
                </div>
              )}

              {editing ? (
                <>
                  <textarea value={editText} onChange={(e) => setEditText(e.target.value)} rows={14} style={{ width: "100%", padding: 12, fontSize: 14, lineHeight: 1.55, border: "1.5px solid " + C.human, borderRadius: 10, background: C.panel, color: C.ink }} />
                  <div style={{ display: "flex", gap: 8, marginTop: 8 }}>
                    <Btn
                      kind="solidHuman"
                      onClick={async () => {
                        const txt = editText;
                        setEditing(false);
                        await mutateRoom(room.id, (d) =>
                          commitRevision(d, { by: me, kind: "human", title: d.art && d.art.title, content: txt })
                        );
                      }}
                      style={{ flex: 1 }}
                    >
                      Save your edit
                    </Btn>
                    <Btn kind="ghost" onClick={() => setEditing(false)}>Cancel</Btn>
                  </div>
                </>
              ) : (
                <>
                  <div style={{ background: C.panel, border: "1px solid " + C.line, borderRadius: 12, padding: 16, fontSize: 14.5, lineHeight: 1.62, whiteSpace: "pre-wrap", overflowWrap: "break-word" }}>
                    {rd.art.content}
                  </div>
                  <div style={{ display: "flex", gap: 8, marginTop: 10 }}>
                    <Btn kind="human" onClick={() => { setEditText(rd.art.content); setEditing(true); }} style={{ flex: 1 }}>
                      Edit as {me}
                    </Btn>
                    <Btn kind="ai" onClick={askForDraft} disabled={aiBusy} style={{ flex: 1 }}>
                      ✦ {AI_NAME}, revise it
                    </Btn>
                  </div>
                </>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
