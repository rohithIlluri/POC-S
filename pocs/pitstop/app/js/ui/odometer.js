import { openModal, closeModal } from "./modal.js";

export function openLogReadingForm(app) {
  const currentOdo = app.store.currentOdometer();
  const bodyHtml = `
    <div class="form-row">
      <label for="f-odo-miles">Odometer reading (miles)</label>
      <input id="f-odo-miles" type="number" min="0" step="1" placeholder="${currentOdo ?? "e.g. 42000"}" required />
    </div>
    <div class="form-row">
      <label for="f-odo-date">Date</label>
      <input id="f-odo-date" type="date" value="${app.todayISO()}" required />
    </div>
    <p class="form-error" id="f-odo-error"></p>
  `;
  const buttons = `
    <button type="submit" class="btn">Save reading</button>
    <button type="button" id="f-cancel" class="btn secondary">Cancel</button>
  `;
  const dialog = openModal({ title: "Log odometer reading", bodyHtml, buttons });
  dialog.querySelector("#f-cancel").addEventListener("click", () => closeModal());
  dialog.querySelector("form").addEventListener("submit", (e) => {
    e.preventDefault();
    const miles = Number(dialog.querySelector("#f-odo-miles").value);
    const dateISO = dialog.querySelector("#f-odo-date").value;
    try {
      app.store.addReading({ miles, dateISO, source: "manual" });
      closeModal();
      app.refresh();
    } catch (err) {
      dialog.querySelector("#f-odo-error").textContent = err.message;
    }
  });
}

export function renderHistory(container, app) {
  const doc = app.store.get();
  const readings = [...doc.odometer]
    .sort((a, b) => (a.dateISO < b.dateISO ? 1 : a.dateISO > b.dateISO ? -1 : b.miles - a.miles))
    .slice(0, 10);

  if (readings.length === 0) {
    container.innerHTML = '<p class="empty-state">No odometer readings yet. Log one to get started.</p>';
    return;
  }

  container.innerHTML = readings
    .map(
      (r) => `
    <div class="item-row">
      <div class="item-info">
        <div class="item-name">${r.miles.toLocaleString()} mi</div>
        <div class="item-detail">${r.dateISO} · ${r.source === "trip" ? "GPS trip" : "manual"}</div>
      </div>
    </div>
  `
    )
    .join("");
}
