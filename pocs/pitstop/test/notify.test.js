import { test } from "node:test";
import assert from "node:assert/strict";
import { computeNotifications } from "../app/js/lib/notify.js";

const items = [{ id: "oil", name: "Engine oil" }, { id: "tires", name: "Tires" }];

test("fires when an item worsens from ok to due_soon", () => {
  const { notifications, lastNotifiedStatus } = computeNotifications(
    items,
    { oil: "due_soon", tires: "ok" },
    { oil: "ok", tires: "ok" }
  );
  assert.equal(notifications.length, 1);
  assert.equal(notifications[0].itemId, "oil");
  assert.equal(lastNotifiedStatus.oil, "due_soon");
});

test("fires when an item worsens from due_soon to overdue", () => {
  const { notifications } = computeNotifications(items, { oil: "overdue", tires: "ok" }, { oil: "due_soon", tires: "ok" });
  assert.equal(notifications.length, 1);
  assert.equal(notifications[0].itemId, "oil");
  assert.equal(notifications[0].title, "Engine oil overdue");
});

test("does not repeat when status is unchanged", () => {
  const { notifications } = computeNotifications(items, { oil: "due_soon", tires: "ok" }, { oil: "due_soon", tires: "ok" });
  assert.equal(notifications.length, 0);
});

test("improvement fires nothing but resets the latch", () => {
  const { notifications, lastNotifiedStatus } = computeNotifications(
    items,
    { oil: "ok", tires: "ok" },
    { oil: "overdue", tires: "ok" }
  );
  assert.equal(notifications.length, 0);
  assert.equal(lastNotifiedStatus.oil, "ok");
});

test("an item unseen in lastNotifiedStatus is treated as an ok baseline", () => {
  const { notifications } = computeNotifications(items, { oil: "due_soon", tires: "ok" }, {});
  assert.equal(notifications.length, 1);
  assert.equal(notifications[0].itemId, "oil");
});

test("unknown status never fires (treated like ok)", () => {
  const { notifications } = computeNotifications(items, { oil: "unknown", tires: "ok" }, { oil: "ok", tires: "ok" });
  assert.equal(notifications.length, 0);
});
