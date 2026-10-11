// Nook's service worker: keeps the app shell and its (content-hashed,
// immutable) assets on the device, so the app opens without a network
// and loads faster on a slow one. It never touches /api/: data always
// comes from the server, through the normal permission checks.
//
// shortcut: hashed assets from older deploys stay in the cache; bump
// CACHE (or add pruning) if it ever grows noticeably.
const CACHE = 'nook-shell-v1'

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(CACHE).then((c) => c.add('/')).catch(() => {}))
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  )
})

// Only a plain 200 from this origin is kept -- never a redirect (e.g. to
// the Cloudflare Access login) or an error page.
function keep(request, response) {
  if (response.ok && response.type === 'basic' && !response.redirected) {
    const copy = response.clone()
    caches.open(CACHE).then((c) => c.put(request, copy))
  }
  return response
}

self.addEventListener('fetch', (event) => {
  const req = event.request
  if (req.method !== 'GET') return
  const url = new URL(req.url)
  if (url.origin !== self.location.origin || url.pathname.startsWith('/api/')) return

  if (req.mode === 'navigate') {
    // Network first, so a deploy shows up at once; the cached shell offline.
    event.respondWith(
      fetch(req)
        .then((res) => keep('/', res))
        .catch(() => caches.match('/').then((hit) => hit || Response.error())),
    )
    return
  }
  if (url.pathname.startsWith('/assets/')) {
    event.respondWith(caches.match(req).then((hit) => hit || fetch(req).then((res) => keep(req, res))))
  }
})
