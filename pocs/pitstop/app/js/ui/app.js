import { createStore } from "../lib/store.js";
import { statusOf, milesRemaining, daysUntilDue, milesPerDay, projectedDays, intervalProgress } from "../lib/status.js";
import { computeNotifications } from "../lib/notify.js";
import { openLogReadingForm } from "./odometer.js";
import * as dashboard from "./dashboard.js";
import * as trip from "./trip.js";
import * as shops from "./shops.js";
import * as settings from "./settings.js";

const TABS = ["dashboard", "trip", "shops", "settings"];

function todayISO() {
  return new Date().toISOString().slice(0, 10);
}

function createApp() {
  const store = createStore(window.localStorage);
  store.load();

  let activeTab = "dashboard";
  const panels = {};
  TABS.forEach((t) => {
    panels[t] = document.querySelector(`.tab-panel[data-tab="${t}"]`);
  });
  const tabButtons = document.querySelectorAll(".tab-btn");
  const odoValueEl = document.getElementById("odo-value");

  const app = {
    store,
    todayISO,
    statusOf: (item) =>
      statusOf(item, {
        currentOdometer: store.currentOdometer() ?? 0,
        todayISO: todayISO(),
        settings: store.get().settings,
      }),
    milesRemaining: (item) => milesRemaining(item, store.currentOdometer() ?? 0),
    daysUntilDue: (item) => daysUntilDue(item, todayISO()),
    milesPerDay: () => milesPerDay(store.get().odometer, todayISO()),
    intervalProgress: (item) =>
      intervalProgress(item, { currentOdometer: store.currentOdometer() ?? 0, todayISO: todayISO() }),
    projectedDays,
    refresh,
  };

  let displayedOdo = null;
  const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

  function renderOdo() {
    const cur = store.currentOdometer();
    if (cur == null) {
      displayedOdo = null;
      odoValueEl.textContent = "— mi";
      return;
    }
    const from = displayedOdo;
    displayedOdo = cur;
    if (from == null || from === cur || reducedMotion.matches) {
      odoValueEl.textContent = `${cur.toLocaleString()} mi`;
      return;
    }
    // count-up animation from the previously shown value
    const start = performance.now();
    const duration = 650;
    const tick = (now) => {
      if (displayedOdo !== cur) return; // a newer value took over
      const t = Math.min(1, (now - start) / duration);
      if (t >= 1) {
        odoValueEl.textContent = `${cur.toLocaleString()} mi`; // exact, incl. fractions
        return;
      }
      const eased = 1 - Math.pow(1 - t, 3);
      odoValueEl.textContent = `${Math.round(from + (cur - from) * eased).toLocaleString()} mi`;
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }

  function renderActive() {
    const panel = panels[activeTab];
    if (activeTab === "dashboard") dashboard.render(panel, app);
    else if (activeTab === "trip") trip.render(panel, app);
    else if (activeTab === "shops") shops.render(panel, app);
    else if (activeTab === "settings") settings.render(panel, app);
  }

  function setActiveTab(tab) {
    if (tab === activeTab) return;
    const apply = () => {
      activeTab = tab;
      TABS.forEach((t) => {
        panels[t].hidden = t !== tab;
      });
      tabButtons.forEach((b) => b.classList.toggle("active", b.dataset.tab === tab));
      renderActive();
    };
    if (document.startViewTransition && !reducedMotion.matches) {
      document.startViewTransition(apply);
    } else {
      apply();
    }
  }

  function describeNotification(itemId) {
    const item = store.get().items.find((i) => i.id === itemId);
    if (!item) return "";
    const remaining = app.milesRemaining(item);
    if (remaining != null) {
      const days = projectedDays(remaining, app.milesPerDay());
      const milesText = `${Math.max(0, Math.round(remaining)).toLocaleString()} mi remaining`;
      return days != null ? `${milesText} (~${Math.max(0, Math.round(days))} days)` : milesText;
    }
    const days = app.daysUntilDue(item);
    return days != null ? `${Math.max(0, days)} days remaining` : "";
  }

  function fireNotifications(notifications) {
    if (!("Notification" in window) || Notification.permission !== "granted") return;
    for (const n of notifications) {
      try {
        new Notification(n.title, { body: describeNotification(n.itemId), icon: "./icons/icon-192.png" });
      } catch {
        // Notification constructor can throw on some platforms; nothing to recover.
      }
    }
  }

  function checkNotifications() {
    const doc = store.get();
    if (store.currentOdometer() == null) return;

    const statuses = {};
    for (const item of doc.items) statuses[item.id] = app.statusOf(item);

    const { notifications, lastNotifiedStatus } = computeNotifications(doc.items, statuses, doc.lastNotifiedStatus);
    store.setLastNotified(lastNotifiedStatus);
    if (notifications.length && doc.settings.notificationsEnabled) fireNotifications(notifications);
  }

  function refresh() {
    renderOdo();
    renderActive();
    checkNotifications();
  }

  tabButtons.forEach((btn) => {
    btn.addEventListener("click", () => setActiveTab(btn.dataset.tab));
  });

  document.getElementById("log-reading-btn").addEventListener("click", () => openLogReadingForm(app));

  return app;
}

function registerServiceWorker() {
  if (!("serviceWorker" in navigator)) return;
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("./sw.js").catch(() => {});
  });
}

const app = createApp();
app.refresh();
registerServiceWorker();
