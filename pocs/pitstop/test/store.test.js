import { test } from "node:test";
import assert from "node:assert/strict";
import { createStore, STORAGE_KEY } from "../app/js/lib/store.js";
import { DEFAULT_ITEMS } from "../app/js/lib/schedule.js";

function fakeStorage(initial = new Map()) {
  const map = initial;
  return {
    map,
    getItem(key) {
      return map.has(key) ? map.get(key) : null;
    },
    setItem(key, value) {
      map.set(key, value);
    },
  };
}

test("first run seeds default maintenance items", () => {
  const store = createStore(fakeStorage());
  const doc = store.load();
  assert.equal(doc.items.length, DEFAULT_ITEMS.length);
  assert.equal(doc.settings.dueSoonMiles, 500);
  assert.equal(doc.odometer.length, 0);
});

test("round-trips save/load through the same backing storage", () => {
  const storage = fakeStorage();
  const store1 = createStore(storage);
  store1.load();
  store1.addReading({ miles: 100, dateISO: "2026-07-01" });

  const store2 = createStore(storage);
  const doc2 = store2.load();
  assert.equal(doc2.odometer.length, 1);
  assert.equal(doc2.odometer[0].miles, 100);
});

test("addReading rejects a reading lower than the current max", () => {
  const store = createStore(fakeStorage());
  store.load();
  store.addReading({ miles: 100, dateISO: "2026-07-01" });
  assert.throws(() => store.addReading({ miles: 50, dateISO: "2026-07-02" }));
});

test("currentOdometer is null until a reading exists", () => {
  const store = createStore(fakeStorage());
  store.load();
  assert.equal(store.currentOdometer(), null);
  store.addReading({ miles: 200, dateISO: "2026-07-01" });
  assert.equal(store.currentOdometer(), 200);
});

test("markServiced updates miles and date, defaulting to current odometer/today", () => {
  const store = createStore(fakeStorage(), { now: () => new Date("2026-07-12T00:00:00Z") });
  store.load();
  store.addReading({ miles: 41000, dateISO: "2026-07-01" });
  const item = store.markServiced("engine_oil");
  assert.equal(item.lastServicedMiles, 41000);
  assert.equal(item.lastServicedDateISO, "2026-07-12");
});

test("upsertItem adds a custom item and updates an existing one", () => {
  const store = createStore(fakeStorage());
  store.load();
  const created = store.upsertItem({ name: "Spark plugs", intervalMiles: 30000 });
  assert.equal(created.builtin, false);
  assert.ok(created.id);

  const updated = store.upsertItem({ id: "engine_oil", intervalMiles: 7500 });
  assert.equal(updated.intervalMiles, 7500);
  assert.equal(updated.name, "Engine oil & filter");
});

test("deleteItem removes the item and its notification latch", () => {
  const store = createStore(fakeStorage());
  const doc = store.load();
  store.setLastNotified({ engine_oil: "due_soon" });
  store.deleteItem("engine_oil");
  assert.equal(doc.items.find((i) => i.id === "engine_oil"), undefined);
  assert.equal(doc.lastNotifiedStatus.engine_oil, undefined);
});

test("migrate reseeds on corrupt JSON without throwing", () => {
  const storage = fakeStorage();
  storage.setItem(STORAGE_KEY, "{not valid json");
  const store = createStore(storage);
  const doc = store.load();
  assert.equal(doc.items.length, DEFAULT_ITEMS.length);
});

test("migrate reseeds on an unrecognized schema version", () => {
  const storage = fakeStorage();
  storage.setItem(STORAGE_KEY, JSON.stringify({ version: 999, items: [] }));
  const store = createStore(storage);
  const doc = store.load();
  assert.equal(doc.items.length, DEFAULT_ITEMS.length);
});
