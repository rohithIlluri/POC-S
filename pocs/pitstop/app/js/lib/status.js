export const STATUS = {
  OK: "ok",
  DUE_SOON: "due_soon",
  OVERDUE: "overdue",
  UNKNOWN: "unknown",
};

const STATUS_RANK = {
  [STATUS.OK]: 0,
  [STATUS.UNKNOWN]: 1,
  [STATUS.DUE_SOON]: 2,
  [STATUS.OVERDUE]: 3,
};

const MS_PER_DAY = 86400000;
const DUE_SOON_DAYS = 30;
const PROJECTION_WINDOW_DAYS = 90;

function toUtcDate(dateISO) {
  return new Date(`${dateISO}T00:00:00Z`);
}

function daysBetween(fromISO, toISO) {
  return (toUtcDate(toISO).getTime() - toUtcDate(fromISO).getTime()) / MS_PER_DAY;
}

// Miles left until a mile-based interval is due; null if the axis isn't applicable
// (no intervalMiles configured, or no baseline reading yet).
export function milesRemaining(item, currentOdometer) {
  if (item.intervalMiles == null || item.lastServicedMiles == null) return null;
  return item.lastServicedMiles + item.intervalMiles - currentOdometer;
}

// Adds calendar months to an ISO date, clamping to the target month's last day
// instead of overflowing (e.g. Jan 31 + 1 month => Feb 28/29, not Mar 3).
function addMonthsClamped(dateISO, months) {
  const d = toUtcDate(dateISO);
  const day = d.getUTCDate();
  d.setUTCDate(1);
  d.setUTCMonth(d.getUTCMonth() + months);
  const daysInTargetMonth = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 0)).getUTCDate();
  d.setUTCDate(Math.min(day, daysInTargetMonth));
  return d;
}

// Days left until a month-based interval is due; null if not applicable.
export function daysUntilDue(item, todayISO) {
  if (item.intervalMonths == null || item.lastServicedDateISO == null) return null;
  const due = addMonthsClamped(item.lastServicedDateISO, item.intervalMonths);
  return Math.round((due.getTime() - toUtcDate(todayISO).getTime()) / MS_PER_DAY);
}

function worstStatus(statuses) {
  return statuses.reduce((worst, s) => (STATUS_RANK[s] > STATUS_RANK[worst] ? s : worst), STATUS.OK);
}

// ctx: { currentOdometer, todayISO, settings: { dueSoonMiles } }
export function statusOf(item, ctx) {
  const dueSoonMiles = item.dueSoonMiles ?? ctx.settings.dueSoonMiles;
  const results = [];

  if (item.intervalMiles != null) {
    if (item.lastServicedMiles == null) {
      results.push(STATUS.UNKNOWN);
    } else {
      const remaining = milesRemaining(item, ctx.currentOdometer);
      results.push(remaining <= 0 ? STATUS.OVERDUE : remaining <= dueSoonMiles ? STATUS.DUE_SOON : STATUS.OK);
    }
  }

  if (item.intervalMonths != null) {
    if (item.lastServicedDateISO == null) {
      results.push(STATUS.UNKNOWN);
    } else {
      const days = daysUntilDue(item, ctx.todayISO);
      results.push(days <= 0 ? STATUS.OVERDUE : days <= DUE_SOON_DAYS ? STATUS.DUE_SOON : STATUS.OK);
    }
  }

  if (results.length === 0) return STATUS.UNKNOWN;
  return worstStatus(results);
}

// Average miles driven per day, using readings from the last 90 days when there
// are enough of them, falling back to the full history. Returns null when there's
// not enough data to estimate (fewer than 2 readings, or a zero-day span).
export function milesPerDay(readings, todayISO) {
  if (!Array.isArray(readings) || readings.length < 2) return null;
  const sorted = [...readings].sort((a, b) => (a.dateISO < b.dateISO ? -1 : a.dateISO > b.dateISO ? 1 : a.miles - b.miles));
  const cutoffISO = new Date(toUtcDate(todayISO).getTime() - PROJECTION_WINDOW_DAYS * MS_PER_DAY).toISOString().slice(0, 10);
  let window = sorted.filter((r) => r.dateISO >= cutoffISO);
  if (window.length < 2) window = sorted;
  const first = window[0];
  const last = window[window.length - 1];
  const spanDays = daysBetween(first.dateISO, last.dateISO);
  if (spanDays < 1) return null;
  return (last.miles - first.miles) / spanDays;
}

// Projected days remaining given a miles-remaining figure and an estimated
// miles/day rate. Null when there's no remaining figure or no usable rate.
export function projectedDays(remainingMiles, ratePerDay) {
  if (remainingMiles == null || ratePerDay == null || ratePerDay <= 0) return null;
  return remainingMiles / ratePerDay;
}
