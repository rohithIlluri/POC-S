import { openItemForm, openMarkServicedForm } from "./items.js";
import { renderHistory } from "./odometer.js";
import { ringSvg, animateRings } from "./rings.js";

const STATUS_RANK = { overdue: 3, due_soon: 2, unknown: 1, ok: 0 };
const STATUS_LABEL = { ok: "On track", due_soon: "Due soon", overdue: "Overdue", unknown: "Set baseline" };
const STATUS_ICON = { ok: "✓", due_soon: "!", overdue: "!", unknown: "?" };

function escapeHtml(s) {
  return String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function describeDetail(item, status, app) {
  if (status === "unknown") return "No service history recorded";

  const remaining = app.milesRemaining(item);
  const days = app.daysUntilDue(item);
  const parts = [];

  if (remaining != null) {
    const projected = app.projectedDays(remaining, app.milesPerDay());
    parts.push(
      remaining <= 0
        ? `${Math.abs(Math.round(remaining)).toLocaleString()} mi overdue`
        : `${Math.round(remaining).toLocaleString()} mi remaining${projected != null ? ` (~${Math.max(0, Math.round(projected))} days)` : ""}`
    );
  }
  if (days != null) {
    parts.push(days <= 0 ? `${Math.abs(days)} days overdue` : `${days} days remaining`);
  }
  return parts.join(" · ") || STATUS_LABEL[status];
}

function renderRow(item, status, app, index) {
  const progress = app.intervalProgress(item);
  const ring =
    progress == null
      ? `<span class="status-dot ${status}"></span>`
      : `<div class="item-ring">
          ${ringSvg({ progress: Math.min(1, progress), status, size: 42, strokeWidth: 4 })}
          <div class="item-ring-center" aria-hidden="true">${STATUS_ICON[status]}</div>
        </div>`;

  return `
    <div class="item-row" style="--i:${index}">
      ${ring}
      <div class="item-info">
        <div class="item-name">${escapeHtml(item.name)}</div>
        <div class="item-detail ${status}">${describeDetail(item, status, app)}</div>
      </div>
      <div class="item-actions">
        <button class="btn-small" data-action="service" data-id="${item.id}">Serviced</button>
        <button class="btn-small" data-action="edit" data-id="${item.id}">Edit</button>
      </div>
    </div>
  `;
}

function renderHero(rows, app) {
  const counts = { overdue: 0, due_soon: 0, ok: 0, unknown: 0 };
  for (const { status } of rows) counts[status] += 1;

  const tracked = rows.length - counts.unknown;
  const attention = counts.overdue + counts.due_soon;
  const healthy = tracked > 0 ? (tracked - attention) / tracked : 0;
  const worst = counts.overdue > 0 ? "overdue" : counts.due_soon > 0 ? "due_soon" : "ok";

  const center =
    tracked === 0
      ? `<span class="big">–</span><span class="small">no data</span>`
      : attention === 0
        ? `<span class="big">✓</span><span class="small">all good</span>`
        : `<span class="big">${attention}</span><span class="small">need${attention === 1 ? "s" : ""} work</span>`;

  const chips = [
    counts.overdue > 0 ? `<span class="chip overdue"><span class="status-dot overdue"></span>${counts.overdue} overdue</span>` : "",
    counts.due_soon > 0 ? `<span class="chip due_soon"><span class="status-dot due_soon"></span>${counts.due_soon} due soon</span>` : "",
    counts.ok > 0 ? `<span class="chip ok"><span class="status-dot ok"></span>${counts.ok} on track</span>` : "",
    counts.unknown > 0 ? `<span class="chip"><span class="status-dot unknown"></span>${counts.unknown} no baseline</span>` : "",
  ].join("");

  const rate = app.milesPerDay();
  const sub =
    tracked === 0
      ? "Log an odometer reading and mark items serviced to start tracking."
      : rate != null
        ? `Driving ≈${Math.round(rate)} mi/day`
        : "Log readings over a few days to unlock time estimates.";

  return `
    <div class="hero">
      <div class="hero-ring">
        ${ringSvg({ progress: healthy, status: worst, size: 92, strokeWidth: 8 })}
        <div class="hero-ring-center">${center}</div>
      </div>
      <div class="hero-info">
        <div class="hero-title">Vehicle health</div>
        <div class="hero-sub">${sub}</div>
        <div class="hero-chips">${chips}</div>
      </div>
    </div>
  `;
}

export function render(container, app) {
  const doc = app.store.get();
  const currentOdo = app.store.currentOdometer();

  const rows = doc.items
    .map((item) => ({ item, status: app.statusOf(item) }))
    .sort((a, b) => STATUS_RANK[b.status] - STATUS_RANK[a.status] || a.item.name.localeCompare(b.item.name));

  const noOdoBanner =
    currentOdo == null ? `<div class="banner warn">Log an odometer reading to see maintenance status.</div>` : "";

  container.innerHTML = `
    ${noOdoBanner}
    ${renderHero(rows, app)}
    <div class="section-title">Maintenance</div>
    <div class="card" id="items-list"></div>
    <button class="btn secondary" id="add-item-btn" style="width:100%;margin-bottom:20px;">+ Add custom item</button>
    <div class="section-title">Odometer history</div>
    <div class="card" id="odo-history"></div>
  `;

  const list = container.querySelector("#items-list");
  list.innerHTML =
    rows.length === 0
      ? '<p class="empty-state">No maintenance items yet.</p>'
      : rows.map(({ item, status }, i) => renderRow(item, status, app, i)).join("");

  list.querySelectorAll("[data-action='service']").forEach((btn) => {
    btn.addEventListener("click", () => openMarkServicedForm(app, btn.dataset.id));
  });
  list.querySelectorAll("[data-action='edit']").forEach((btn) => {
    btn.addEventListener("click", () => openItemForm(app, btn.dataset.id));
  });
  container.querySelector("#add-item-btn").addEventListener("click", () => openItemForm(app, null));

  renderHistory(container.querySelector("#odo-history"), app);
  animateRings(container);
}
