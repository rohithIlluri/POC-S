import { openItemForm } from "./items.js";
import { STORAGE_KEY } from "../lib/store.js";

function updateNotifyHint(container, override) {
  const hint = container.querySelector("#s-notify-hint");
  if (override) {
    hint.textContent = override;
    return;
  }
  if (!("Notification" in window)) {
    hint.textContent = "Notifications aren't supported in this browser.";
    return;
  }
  if (Notification.permission === "denied") {
    hint.textContent = "Notifications are blocked for this site in your browser settings.";
    return;
  }
  hint.textContent = "";
}

export function render(container, app) {
  const doc = app.store.get();

  container.innerHTML = `
    <div class="section-title">Alerts</div>
    <div class="card">
      <div class="form-row">
        <label for="s-due-soon">Default "due soon" threshold (miles)</label>
        <input id="s-due-soon" type="number" min="1" value="${doc.settings.dueSoonMiles}" />
      </div>
      <div class="switch-row">
        <span>Browser notifications</span>
        <button class="btn-small" id="s-notify-toggle">${doc.settings.notificationsEnabled ? "On" : "Off"}</button>
      </div>
      <p class="item-detail" id="s-notify-hint"></p>
    </div>
    <div class="section-title">Data</div>
    <div class="card">
      <button class="btn secondary" id="s-add-item" style="width:100%;margin-bottom:10px;">+ Add custom maintenance item</button>
      <button class="btn danger" id="s-reset" style="width:100%;">Reset all data</button>
    </div>
    <div class="section-title">About</div>
    <div class="card item-detail">
      Pitstop is local-first: everything is stored on this device only. GPS trip distance is an estimate (±a few percent versus your real odometer). Notifications only fire while the app is open — there's no push server in this POC.
    </div>
  `;

  updateNotifyHint(container);

  container.querySelector("#s-due-soon").addEventListener("change", (e) => {
    const v = Number(e.target.value);
    if (Number.isFinite(v) && v > 0) app.store.setSettings({ dueSoonMiles: v });
    app.refresh();
  });

  container.querySelector("#s-notify-toggle").addEventListener("click", async () => {
    const enabling = !app.store.get().settings.notificationsEnabled;
    if (enabling) {
      if (!("Notification" in window)) {
        updateNotifyHint(container, "Notifications aren't supported in this browser.");
        return;
      }
      const permission = await Notification.requestPermission();
      if (permission !== "granted") {
        updateNotifyHint(container, "Notifications were blocked. Enable them in your browser's site settings to turn this on.");
        return;
      }
    }
    app.store.setSettings({ notificationsEnabled: enabling });
    app.refresh();
  });

  container.querySelector("#s-add-item").addEventListener("click", () => openItemForm(app, null));

  container.querySelector("#s-reset").addEventListener("click", () => {
    if (window.confirm("This deletes all odometer readings and maintenance history from this device. Continue?")) {
      window.localStorage.removeItem(STORAGE_KEY);
      window.location.reload();
    }
  });
}
