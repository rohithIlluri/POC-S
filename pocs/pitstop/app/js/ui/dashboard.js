import { openItemForm, openMarkServicedForm } from "./items.js";
import { renderHistory } from "./odometer.js";

const STATUS_RANK = { overdue: 3, due_soon: 2, unknown: 1, ok: 0 };
const STATUS_LABEL = { ok: "On track", due_soon: "Due soon", overdue: "Overdue", unknown: "Set baseline" };

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

function renderRow(item, status, app) {
  return `
    <div class="item-row">
      <span class="status-dot ${status}"></span>
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
      : rows.map(({ item, status }) => renderRow(item, status, app)).join("");

  list.querySelectorAll("[data-action='service']").forEach((btn) => {
    btn.addEventListener("click", () => openMarkServicedForm(app, btn.dataset.id));
  });
  list.querySelectorAll("[data-action='edit']").forEach((btn) => {
    btn.addEventListener("click", () => openItemForm(app, btn.dataset.id));
  });
  container.querySelector("#add-item-btn").addEventListener("click", () => openItemForm(app, null));

  renderHistory(container.querySelector("#odo-history"), app);
}
