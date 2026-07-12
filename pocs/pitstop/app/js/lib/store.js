import { DEFAULT_ITEMS } from "./schedule.js";

export const STORAGE_KEY = "pitstop:v1";
export const SCHEMA_VERSION = 1;

let idCounter = 0;
function makeId(prefix) {
  idCounter += 1;
  return `${prefix}_${Date.now().toString(36)}_${idCounter.toString(36)}`;
}

function seedDoc() {
  return {
    version: SCHEMA_VERSION,
    settings: { dueSoonMiles: 500, notificationsEnabled: false },
    odometer: [],
    items: DEFAULT_ITEMS.map((item) => ({ ...item })),
    lastNotifiedStatus: {},
  };
}

function migrate(parsed) {
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return seedDoc();
  if (parsed.version !== SCHEMA_VERSION) return seedDoc();
  return {
    version: SCHEMA_VERSION,
    settings: { dueSoonMiles: 500, notificationsEnabled: false, ...parsed.settings },
    odometer: Array.isArray(parsed.odometer) ? parsed.odometer : [],
    items: Array.isArray(parsed.items) ? parsed.items : DEFAULT_ITEMS.map((item) => ({ ...item })),
    lastNotifiedStatus: parsed.lastNotifiedStatus && typeof parsed.lastNotifiedStatus === "object" ? parsed.lastNotifiedStatus : {},
  };
}

// storage: any object implementing getItem(key)/setItem(key, value), e.g. window.localStorage.
export function createStore(storage, options = {}) {
  const key = options.key || STORAGE_KEY;
  const now = options.now || (() => new Date());
  let doc = null;

  function persist() {
    storage.setItem(key, JSON.stringify(doc));
  }

  function load() {
    const raw = storage.getItem(key);
    if (!raw) {
      doc = seedDoc();
      persist();
      return doc;
    }
    try {
      doc = migrate(JSON.parse(raw));
    } catch {
      doc = seedDoc();
    }
    persist();
    return doc;
  }

  function get() {
    if (!doc) load();
    return doc;
  }

  function currentOdometer() {
    const d = get();
    if (d.odometer.length === 0) return null;
    return d.odometer.reduce((max, r) => Math.max(max, r.miles), -Infinity);
  }

  function addReading({ miles, dateISO, source = "manual" }) {
    if (typeof miles !== "number" || !Number.isFinite(miles) || miles < 0) {
      throw new Error("miles must be a non-negative finite number");
    }
    const d = get();
    const current = currentOdometer();
    if (current !== null && miles < current) {
      throw new Error(`Reading ${miles} is less than current odometer ${current}`);
    }
    const reading = { id: makeId("r"), miles, dateISO, source };
    d.odometer.push(reading);
    d.odometer.sort((a, b) => {
      if (a.dateISO !== b.dateISO) return a.dateISO < b.dateISO ? -1 : 1;
      return a.miles - b.miles;
    });
    persist();
    return reading;
  }

  function upsertItem(item) {
    const d = get();
    const idx = d.items.findIndex((i) => i.id === item.id);
    if (idx >= 0) {
      d.items[idx] = { ...d.items[idx], ...item };
      persist();
      return d.items[idx];
    }
    const created = {
      builtin: false,
      intervalMiles: null,
      intervalMonths: null,
      dueSoonMiles: null,
      lastServicedMiles: null,
      lastServicedDateISO: null,
      ...item,
      // resolved last so a caller passing an explicit `id: undefined` can't
      // wipe out the generated id
      id: item.id || makeId("item"),
    };
    d.items.push(created);
    persist();
    return created;
  }

  function deleteItem(id) {
    const d = get();
    d.items = d.items.filter((i) => i.id !== id);
    delete d.lastNotifiedStatus[id];
    persist();
  }

  function markServiced(id, { miles, dateISO } = {}) {
    const d = get();
    const item = d.items.find((i) => i.id === id);
    if (!item) throw new Error(`Unknown item ${id}`);
    item.lastServicedMiles = miles ?? currentOdometer() ?? item.lastServicedMiles;
    item.lastServicedDateISO = dateISO || now().toISOString().slice(0, 10);
    persist();
    return item;
  }

  function setSettings(patch) {
    const d = get();
    d.settings = { ...d.settings, ...patch };
    persist();
    return d.settings;
  }

  function setLastNotified(map) {
    const d = get();
    d.lastNotifiedStatus = { ...map };
    persist();
  }

  return {
    load,
    get,
    currentOdometer,
    addReading,
    upsertItem,
    deleteItem,
    markServiced,
    setSettings,
    setLastNotified,
  };
}
