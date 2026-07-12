// Bump this on every app-shell change so clients pick up new files instead of
// serving a stale cached version.
const CACHE = "pitstop-v2";

const SHELL_FILES = [
  "./",
  "./index.html",
  "./manifest.webmanifest",
  "./css/app.css",
  "./js/ui/app.js",
  "./js/ui/dashboard.js",
  "./js/ui/odometer.js",
  "./js/ui/items.js",
  "./js/ui/trip.js",
  "./js/ui/shops.js",
  "./js/ui/settings.js",
  "./js/ui/modal.js",
  "./js/ui/toast.js",
  "./js/ui/rings.js",
  "./js/lib/schedule.js",
  "./js/lib/store.js",
  "./js/lib/status.js",
  "./js/lib/geo.js",
  "./js/lib/overpass.js",
  "./js/lib/notify.js",
  "./icons/icon-192.png",
  "./icons/icon-512.png",
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(CACHE)
      .then((cache) => cache.addAll(SHELL_FILES))
      .then(() => self.skipWaiting())
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((key) => key !== CACHE).map((key) => caches.delete(key))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (event) => {
  const { request } = event;
  if (request.method !== "GET" || new URL(request.url).origin !== self.location.origin) {
    return; // let cross-origin requests (e.g. Overpass) and non-GETs pass through untouched
  }

  event.respondWith(
    caches.match(request).then(
      (cached) =>
        cached ||
        fetch(request)
          .then((response) => {
            const copy = response.clone();
            caches.open(CACHE).then((cache) => cache.put(request, copy));
            return response;
          })
          .catch(() => cached)
    )
  );
});
