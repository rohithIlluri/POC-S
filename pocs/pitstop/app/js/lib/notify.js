// Notification severity, independent from the dashboard's STATUS_RANK in
// status.js: here "unknown" (no baseline set yet) is treated the same as "ok"
// so a missing baseline never triggers a notification by itself.
const NOTIFY_RANK = { ok: 0, unknown: 0, due_soon: 1, overdue: 2 };

// Decides which items should notify right now, by comparing each item's
// current status against the status it was last notified at. Only fires when
// an item *worsens* (rank increases); improvements are never notified but do
// reset the latch so a later re-worsening notifies again.
//
// items: [{ id, name }]; statuses: { [itemId]: status }; lastNotifiedStatus: { [itemId]: status }
// Returns { notifications: [{ itemId, name, status, title }], lastNotifiedStatus }
export function computeNotifications(items, statuses, lastNotifiedStatus = {}) {
  const notifications = [];
  const nextLastNotified = {};

  for (const item of items) {
    const current = statuses[item.id] ?? "unknown";
    const previous = lastNotifiedStatus[item.id] ?? "ok";
    nextLastNotified[item.id] = current;

    if ((NOTIFY_RANK[current] ?? 0) > (NOTIFY_RANK[previous] ?? 0)) {
      notifications.push({
        itemId: item.id,
        name: item.name,
        status: current,
        title: current === "overdue" ? `${item.name} overdue` : `${item.name} due soon`,
      });
    }
  }

  return { notifications, lastNotifiedStatus: nextLastNotified };
}
