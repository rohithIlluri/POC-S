const { JSDOM } = require("jsdom");

const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
  url: "https://localhost/",
  pretendToBeVisual: true,
});
global.window = dom.window;
global.document = dom.window.document;
global.navigator = dom.window.navigator;
global.Event = dom.window.Event;
global.HTMLIFrameElement = dom.window.HTMLIFrameElement;

// ---- mock window.storage (shared KV), with flaky-mode to test resilience ----
const db = {};
let flaky = false;
let storageCalls = { get: 0, set: 0 };
dom.window.storage = {
  async get(key, shared) {
    storageCalls.get++;
    if (flaky && Math.random() < 0.7) throw new Error("rate limited");
    if (!(key in db)) throw new Error("Key not found");
    return { key, value: db[key], shared: !!shared };
  },
  async set(key, value, shared) {
    storageCalls.set++;
    if (flaky && Math.random() < 0.7) throw new Error("rate limited");
    db[key] = value;
    return { key, value, shared: !!shared };
  },
  async delete(key) {
    delete db[key];
    return { key, deleted: true };
  },
  async list(prefix) {
    return { keys: Object.keys(db).filter((k) => !prefix || k.startsWith(prefix)) };
  },
};

// ---- mock Anthropic API ----
let aiCalls = 0;
let failAI = false;
global.fetch = async (url, opts) => {
  aiCalls++;
  if (failAI) throw new Error("network down");
  const payload = {
    content: [
      {
        type: "text",
        text: JSON.stringify({
          message: "Love this — dog people are loyal users. Want me to sketch the MVP?",
          artifact: { title: "Dog Walk Pairing MVP", content: "# MVP\n1. Sign up\n2. Match neighbors\n3. Schedule walks" },
        }),
      },
    ],
  };
  return { json: async () => payload };
};

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let pass = 0, fail = 0;
function check(name, cond) {
  if (cond) { pass++; console.log("PASS  " + name); }
  else { fail++; console.log("FAIL  " + name); }
}
const bodyText = () => document.body.textContent || "";
function setValue(el, v) {
  const proto = el.tagName === "TEXTAREA" ? dom.window.HTMLTextAreaElement.prototype : dom.window.HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(proto, "value").set.call(el, v);
  el.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
}
const btn = (label) => [...document.querySelectorAll("button")].find((b) => b.textContent.includes(label));

(async () => {
  require("./bundle.cjs"); // renders <App/> into #root

  await sleep(200);
  check("boot → name screen renders", bodyText().includes("Sparkroom") || bodyText().includes("Spark"));
  check("name input present", !!document.querySelector('input[placeholder="Your name"]'));

  // claim name
  setValue(document.querySelector('input[placeholder="Your name"]'), "Rohith");
  await sleep(50);
  btn("Step inside").click();
  await sleep(300);
  check("lobby renders after claiming name", bodyText().includes("PITCH AN IDEA"));
  check("identity persisted to personal storage", db["sr4:me"] === undefined ? false : JSON.parse(db["sr4:me"]).name === "Rohith");

  // create a room from an idea
  setValue(document.querySelector("textarea"), "An app pairing neighbors for dog walks");
  await sleep(50);
  btn("Open a room for it").click();
  await sleep(800);
  check("room screen renders with room name", bodyText().includes("An app pairing neighbors for dog walks"));
  check("pitch became first message", bodyText().includes("Rohith"));
  check("AI was called on room creation", aiCalls >= 1);
  check("Sable replied in chat", bodyText().includes("dog people are loyal users"));
  check("presence chip shows me", bodyText().includes("Rohith (you)"));
  check("room persisted under ONE key", Object.keys(db).some((k) => k.startsWith("sr4:room:")));
  const roomKey = Object.keys(db).find((k) => k.startsWith("sr4:room:"));
  const stored = JSON.parse(db[roomKey]);
  check("stored room has msgs+art+pres in one object", Array.isArray(stored.msgs) && "pres" in stored && "art" in stored);
  check("AI draft was saved with attribution", stored.art && stored.art.log && stored.art.log[0].kind === "ai");

  // send a plain message (no AI trigger)
  const before = aiCalls;
  setValue(document.querySelector("textarea"), "I think we start with one neighborhood");
  await sleep(50);
  btn("Send").click();
  await sleep(400);
  check("plain message posted", bodyText().includes("one neighborhood"));
  check("plain message does NOT trigger AI", aiCalls === before);

  // @ai message triggers AI
  setValue(document.querySelector("textarea"), "@ai what do you think?");
  await sleep(50);
  btn("Send").click();
  await sleep(500);
  check("@ai triggers the AI", aiCalls === before + 1);

  // draft tab: view, human edit, attribution
  btn("The draft").click();
  await sleep(200);
  check("draft tab shows AI draft", bodyText().includes("Dog Walk Pairing MVP"));
  btn("Edit as Rohith").click();
  await sleep(100);
  const editArea = [...document.querySelectorAll("textarea")].find((t) => t.rows === 14 || t.value.includes("MVP"));
  setValue(editArea, "# MVP v2\nHuman-edited plan");
  await sleep(50);
  btn("Save your edit").click();
  await sleep(400);
  check("human edit saved & shown", bodyText().includes("Human-edited plan"));
  const stored2 = JSON.parse(db[roomKey]);
  const lastLog = stored2.art.log[stored2.art.log.length - 1];
  check("attribution weave logs human edit", lastLog.kind === "human" && lastLog.by === "Rohith");
  check("revision carries the body that produced it", lastLog.content === "# MVP v2\nHuman-edited plan");

  // ---- undo: append-only revert of the newest revision ----
  const logBefore = stored2.art.log.length;
  check("undo offered once there is a prior revision", !!btn("Undo Rohith's change"));
  btn("Undo Rohith's change").click();
  await sleep(400);
  check("undo restores the AI draft body", bodyText().includes("1. Sign up") && !bodyText().includes("Human-edited plan"));
  const stored3 = JSON.parse(db[roomKey]);
  const undoRev = stored3.art.log[stored3.art.log.length - 1];
  check("undo appends a revision, never pops one", stored3.art.log.length === logBefore + 1);
  check("undo is attributed to whoever undid", undoRev.kind === "undo" && undoRev.by === "Rohith");
  check("undo records which revision it reverted", undoRev.undoOf === lastLog.id);

  // undoing an undo is redo — no separate stack needed
  btn("Undo Rohith's change").click();
  await sleep(400);
  check("undoing the undo redoes the human edit", bodyText().includes("Human-edited plan"));
  const stored4 = JSON.parse(db[roomKey]);
  check("redo is also append-only", stored4.art.log.length === logBefore + 2);
  check("draft content matches its head revision", stored4.art.content === stored4.art.log[stored4.art.log.length - 1].content);

  // simulate ANOTHER user writing to the same room (multi-user sync via polling)
  const other = JSON.parse(db[roomKey]);
  other.msgs.push({ id: "ext1", author: "Priya", role: "human", text: "Joining from my phone!", ts: Date.now() });
  other.pres["Priya"] = Date.now();
  db[roomKey] = JSON.stringify(other);
  btn("The room").click();
  await sleep(6800); // wait one poll cycle
  check("second user's message arrives via polling", bodyText().includes("Joining from my phone!"));
  check("second user appears in presence", bodyText().includes("Priya"));

  // resilience: storage goes flaky — UI must keep last good state, not wipe
  flaky = true;
  await sleep(7000);
  check("messages survive storage failures (no wipe)", bodyText().includes("Joining from my phone!"));
  flaky = false;

  // resilience: AI network failure posts a graceful message, no crash
  failAI = true;
  setValue(document.querySelector("textarea"), "@ai are you there?");
  await sleep(50);
  btn("Send").click();
  await sleep(600);
  check("AI failure handled gracefully", bodyText().includes("couldn't reach the model"));
  failAI = false;

  // back to lobby, room listed
  btn("←").click();
  await sleep(300);
  check("back in lobby, room listed", bodyText().includes("LIVE ROOMS") && bodyText().includes("dog walks"));

  // archive flow (two-tap)
  [...document.querySelectorAll("button")].find((b) => b.textContent.includes("dog walks")).click();
  await sleep(400);
  btn("archive").click();
  await sleep(100);
  btn("tap again").click();
  await sleep(400);
  check("archive removes room & returns to lobby", bodyText().includes("PITCH AN IDEA") && !JSON.parse(db["sr4:rooms"]).length);

  // ---- legacy data: a draft written before revisions existed ----
  // Its log is bare touches with no bodies, so history before now is
  // unrecoverable — but the draft must still open, and the live body must be
  // adopted as the head revision so new edits are undoable against it.
  const legacyId = "legacy1";
  db["sr4:rooms"] = JSON.stringify([
    { id: legacyId, name: "Old room from an earlier build", pitch: "carried over", createdBy: "Priya", ts: Date.now() },
  ]);
  db["sr4:room:" + legacyId] = JSON.stringify({
    msgs: [{ id: "m1", author: "Priya", role: "human", text: "carried over", ts: Date.now() }],
    art: {
      title: "Legacy plan",
      content: "# Legacy body",
      editedBy: "Priya",
      ts: Date.now(),
      log: [
        { by: "Sable", kind: "ai", ts: Date.now() - 2000 },
        { by: "Priya", kind: "human", ts: Date.now() - 1000 },
      ],
    },
    pres: {},
  });
  await sleep(8200); // one lobby poll cycle
  btn("Old room from an earlier build").click();
  await sleep(500);
  btn("The draft").click();
  await sleep(200);
  check("legacy draft still opens", bodyText().includes("Legacy body"));
  check("legacy touches with no bodies are not undoable", bodyText().includes("Nothing to undo"));
  btn("Edit as Rohith").click();
  await sleep(100);
  setValue([...document.querySelectorAll("textarea")].find((t) => t.rows === 14), "# Rewritten");
  await sleep(50);
  btn("Save your edit").click();
  await sleep(400);
  const legacyStored = JSON.parse(db["sr4:room:" + legacyId]);
  check("legacy log is preserved, not discarded", legacyStored.art.log.length === 3);
  check("legacy body was adopted as a revision", legacyStored.art.log[1].content === "# Legacy body");
  btn("Undo Rohith's change").click();
  await sleep(400);
  check("new edit undoes back to the legacy body", bodyText().includes("Legacy body"));

  console.log("\nstorage calls:", storageCalls, "| AI calls:", aiCalls);
  console.log("\n==== RESULT: " + pass + " passed, " + fail + " failed ====");
  process.exit(fail === 0 ? 0 : 1);
})().catch((e) => {
  console.error("HARNESS CRASH:", e);
  process.exit(2);
});
