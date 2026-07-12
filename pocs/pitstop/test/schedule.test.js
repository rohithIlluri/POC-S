import { test } from "node:test";
import assert from "node:assert/strict";
import { DEFAULT_ITEMS } from "../app/js/lib/schedule.js";

test("seed items have unique ids", () => {
  const ids = DEFAULT_ITEMS.map((item) => item.id);
  assert.equal(new Set(ids).size, ids.length);
});

test("seed items have positive intervals", () => {
  for (const item of DEFAULT_ITEMS) {
    if (item.intervalMiles != null) assert.ok(item.intervalMiles > 0, `${item.id} intervalMiles`);
    if (item.intervalMonths != null) assert.ok(item.intervalMonths > 0, `${item.id} intervalMonths`);
  }
});

test("every seed item configures at least one interval axis", () => {
  for (const item of DEFAULT_ITEMS) {
    assert.ok(item.intervalMiles != null || item.intervalMonths != null, `${item.id} has no interval`);
  }
});
