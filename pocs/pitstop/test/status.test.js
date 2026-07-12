import { test } from "node:test";
import assert from "node:assert/strict";
import { STATUS, statusOf, milesRemaining, daysUntilDue, milesPerDay, projectedDays } from "../app/js/lib/status.js";

const ctx = (currentOdometer, todayISO = "2026-07-12", dueSoonMiles = 500) => ({
  currentOdometer,
  todayISO,
  settings: { dueSoonMiles },
});

test("milesRemaining computes exact math", () => {
  const item = { intervalMiles: 5000, lastServicedMiles: 40000 };
  assert.equal(milesRemaining(item, 42000), 3000);
});

test("milesRemaining is null with no interval or no baseline", () => {
  assert.equal(milesRemaining({ intervalMiles: null, lastServicedMiles: 100 }, 200), null);
  assert.equal(milesRemaining({ intervalMiles: 5000, lastServicedMiles: null }, 200), null);
});

test("statusOf: overdue at exactly 0 miles remaining", () => {
  const item = { intervalMiles: 5000, intervalMonths: null, lastServicedMiles: 37000 };
  assert.equal(statusOf(item, ctx(42000)), STATUS.OVERDUE);
});

test("statusOf: due_soon is inclusive at the threshold", () => {
  const item = { intervalMiles: 5000, intervalMonths: null, lastServicedMiles: 36500, dueSoonMiles: 500 };
  // remaining = 36500 + 5000 - 41000 = 500, exactly at threshold
  assert.equal(statusOf(item, ctx(41000)), STATUS.DUE_SOON);
});

test("statusOf: ok just past the threshold", () => {
  const item = { intervalMiles: 5000, intervalMonths: null, lastServicedMiles: 36500, dueSoonMiles: 500 };
  // remaining = 501, just above threshold
  assert.equal(statusOf(item, ctx(40999)), STATUS.OK);
});

test("statusOf: per-item dueSoonMiles override beats the global setting", () => {
  const item = { intervalMiles: 50000, intervalMonths: null, lastServicedMiles: 0, dueSoonMiles: 100 };
  // remaining = 300 mi: below the global 500 threshold but above the item's own 100
  assert.equal(statusOf(item, ctx(49700, "2026-07-12", 500)), STATUS.OK);
  assert.equal(statusOf(item, ctx(49950, "2026-07-12", 500)), STATUS.DUE_SOON);
});

test("statusOf: months-only item (battery)", () => {
  const item = { intervalMiles: null, intervalMonths: 48, lastServicedDateISO: "2022-07-12" };
  assert.equal(statusOf(item, ctx(10000, "2026-07-12")), STATUS.OVERDUE);
  assert.equal(statusOf(item, ctx(10000, "2023-01-01")), STATUS.OK);
});

test("statusOf: worst-of-two-axes wins", () => {
  const item = {
    intervalMiles: 5000,
    lastServicedMiles: 37200, // remaining = 200, due_soon
    intervalMonths: 6,
    lastServicedDateISO: "2020-01-01", // long overdue
  };
  assert.equal(statusOf(item, ctx(42000, "2026-07-12", 500)), STATUS.OVERDUE);
});

test("statusOf: unknown when no baseline is set", () => {
  const item = { intervalMiles: 5000, intervalMonths: null, lastServicedMiles: null };
  assert.equal(statusOf(item, ctx(42000)), STATUS.UNKNOWN);
});

test("statusOf: a real signal on one axis beats unknown on the other", () => {
  const item = {
    intervalMiles: 5000,
    lastServicedMiles: null, // unknown
    intervalMonths: 6,
    lastServicedDateISO: "2020-01-01", // overdue
  };
  assert.equal(statusOf(item, ctx(42000)), STATUS.OVERDUE);
});

test("daysUntilDue adds months and diffs against today", () => {
  const item = { intervalMonths: 6, lastServicedDateISO: "2026-01-12" };
  assert.equal(daysUntilDue(item, "2026-07-12"), 0);
});

test("daysUntilDue clamps end-of-month overflow instead of rolling into the next month", () => {
  // Jan 31 + 1 month must land on Feb 28 (2026 is not a leap year), not Mar 3.
  const item = { intervalMonths: 1, lastServicedDateISO: "2026-01-31" };
  assert.equal(daysUntilDue(item, "2026-02-28"), 0);
  assert.equal(daysUntilDue(item, "2026-02-27"), 1);
});

test("daysUntilDue clamps into a leap-year February", () => {
  const item = { intervalMonths: 1, lastServicedDateISO: "2028-01-31" };
  assert.equal(daysUntilDue(item, "2028-02-29"), 0);
});

test("milesPerDay needs at least two readings", () => {
  assert.equal(milesPerDay([{ dateISO: "2026-07-01", miles: 100 }], "2026-07-12"), null);
  assert.equal(milesPerDay([], "2026-07-12"), null);
});

test("milesPerDay: same-day readings return null (zero-day span)", () => {
  const readings = [
    { dateISO: "2026-07-12", miles: 100 },
    { dateISO: "2026-07-12", miles: 140 },
  ];
  assert.equal(milesPerDay(readings, "2026-07-12"), null);
});

test("milesPerDay computes average rate over the reading span", () => {
  const readings = [
    { dateISO: "2026-07-01", miles: 40000 },
    { dateISO: "2026-07-11", miles: 40200 },
  ];
  assert.equal(milesPerDay(readings, "2026-07-12"), 20);
});

test("milesPerDay windows to the last 90 days, ignoring older readings", () => {
  const readings = [
    { dateISO: "2025-01-01", miles: 0 }, // far outside the window
    { dateISO: "2026-04-14", miles: 40000 }, // ~89 days before today
    { dateISO: "2026-07-12", miles: 40890 },
  ];
  const rate = milesPerDay(readings, "2026-07-12");
  assert.ok(rate > 9 && rate < 11, `expected ~10 mi/day, got ${rate}`);
});

test("projectedDays is null without a remaining figure or a positive rate", () => {
  assert.equal(projectedDays(null, 20), null);
  assert.equal(projectedDays(100, null), null);
  assert.equal(projectedDays(100, 0), null);
  assert.equal(projectedDays(100, -5), null);
});

test("projectedDays divides remaining miles by the daily rate", () => {
  assert.equal(projectedDays(100, 20), 5);
});
