import { openModal, closeModal } from "./modal.js";

function escapeAttr(s) {
  return String(s).replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
}

function parseOptionalNumber(value) {
  if (value === "" || value == null) return null;
  const n = Number(value);
  return Number.isFinite(n) ? n : null;
}

export function openItemForm(app, itemId) {
  const doc = app.store.get();
  const existing = itemId ? doc.items.find((i) => i.id === itemId) : null;
  const title = existing ? `Edit ${existing.name}` : "Add maintenance item";

  const bodyHtml = `
    <div class="form-row">
      <label for="f-name">Name</label>
      <input id="f-name" type="text" value="${existing ? escapeAttr(existing.name) : ""}" ${existing?.builtin ? "readonly" : ""} required />
    </div>
    <div class="form-row">
      <label for="f-interval-miles">Interval (miles, optional)</label>
      <input id="f-interval-miles" type="number" min="1" value="${existing?.intervalMiles ?? ""}" />
    </div>
    <div class="form-row">
      <label for="f-interval-months">Interval (months, optional)</label>
      <input id="f-interval-months" type="number" min="1" value="${existing?.intervalMonths ?? ""}" />
    </div>
    <div class="form-row">
      <label for="f-due-soon">Due-soon threshold override (miles, optional)</label>
      <input id="f-due-soon" type="number" min="1" value="${existing?.dueSoonMiles ?? ""}" />
    </div>
    <p class="form-error" id="f-item-error"></p>
  `;
  const buttons = `
    <button type="submit" class="btn">Save</button>
    ${existing && !existing.builtin ? '<button type="button" id="f-delete" class="btn danger">Delete</button>' : ""}
    <button type="button" id="f-cancel" class="btn secondary">Cancel</button>
  `;

  const dialog = openModal({ title, bodyHtml, buttons });
  dialog.querySelector("#f-cancel").addEventListener("click", () => closeModal());

  const deleteBtn = dialog.querySelector("#f-delete");
  if (deleteBtn) {
    deleteBtn.addEventListener("click", () => {
      app.store.deleteItem(existing.id);
      closeModal();
      app.refresh();
    });
  }

  dialog.querySelector("form").addEventListener("submit", (e) => {
    e.preventDefault();
    const name = dialog.querySelector("#f-name").value.trim();
    const intervalMiles = parseOptionalNumber(dialog.querySelector("#f-interval-miles").value);
    const intervalMonths = parseOptionalNumber(dialog.querySelector("#f-interval-months").value);
    const dueSoonMiles = parseOptionalNumber(dialog.querySelector("#f-due-soon").value);

    if (!name || (intervalMiles == null && intervalMonths == null)) {
      dialog.querySelector("#f-item-error").textContent = "Please provide a name and at least one interval.";
      return;
    }

    app.store.upsertItem({ id: existing?.id, name, intervalMiles, intervalMonths, dueSoonMiles });
    closeModal();
    app.refresh();
  });
}

export function openMarkServicedForm(app, itemId) {
  const doc = app.store.get();
  const item = doc.items.find((i) => i.id === itemId);
  if (!item) return;
  const currentOdo = app.store.currentOdometer();

  const bodyHtml = `
    <div class="form-row">
      <label for="f-ms-miles">Odometer at service (miles)</label>
      <input id="f-ms-miles" type="number" min="0" value="${currentOdo ?? ""}" required />
    </div>
    <div class="form-row">
      <label for="f-ms-date">Date</label>
      <input id="f-ms-date" type="date" value="${app.todayISO()}" required />
    </div>
  `;
  const buttons = `
    <button type="submit" class="btn">Mark serviced</button>
    <button type="button" id="f-cancel" class="btn secondary">Cancel</button>
  `;

  const dialog = openModal({ title: `Mark "${item.name}" serviced`, bodyHtml, buttons });
  dialog.querySelector("#f-cancel").addEventListener("click", () => closeModal());
  dialog.querySelector("form").addEventListener("submit", (e) => {
    e.preventDefault();
    const miles = Number(dialog.querySelector("#f-ms-miles").value);
    const dateISO = dialog.querySelector("#f-ms-date").value;
    app.store.markServiced(item.id, { miles: Number.isFinite(miles) ? miles : undefined, dateISO });
    closeModal();
    app.refresh();
  });
}
