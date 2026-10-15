// Shared pieces of the UI end-to-end suites (run by scripts/ui-e2e.sh,
// see README there): a signed-in browser per engine, API helpers to set
// data up, real-mouse drags, and polling instead of fixed sleeps.
import { chromium, webkit } from 'playwright'

export const BASE = process.env.NOOK_URL ?? 'http://127.0.0.1:18099'
const USER = { username: 'admin', password: 'e2e-password-123' }
const engines = { chromium, webkit }

let failures = 0
export const failed = () => failures

/** Logs one check; a failing one makes the run exit non-zero. */
export function check(tag, label, ok, extra = '') {
  if (!ok) failures++
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${tag} ${label}${extra ? ' — ' + extra : ''}`)
}

/** Polls fn until it returns something truthy (or throws after ms). */
export async function until(fn, ms = 8000, step = 100) {
  const end = Date.now() + ms
  for (;;) {
    const v = await fn()
    if (v) return v
    if (Date.now() > end) throw new Error('until: timed out')
    await new Promise((r) => setTimeout(r, step))
  }
}

/**
 * A signed-in browser of engine type with a fresh page. Every name the
 * suite creates is prefixed with `${suite}-${type}-` so suites and
 * engines can share one server and run at the same time.
 */
export async function session(type, suite, { width = 1400, height = 900 } = {}) {
  const browser = await engines[type].launch()
  const ctx = await browser.newContext({ viewport: { width, height }, extraHTTPHeaders: { Origin: BASE } })
  const api = ctx.request
  // Many suites log in at once; the login rate limit answers 429 then.
  let r = await api.post(BASE + '/api/auth/login', { data: USER })
  for (let i = 0; r.status() === 429 && i < 6; i++) {
    await new Promise((ok) => setTimeout(ok, 13000))
    r = await api.post(BASE + '/api/auth/login', { data: USER })
  }
  if (!r.ok()) throw new Error('login ' + r.status())
  const me = await (await api.get(BASE + '/api/me')).json()
  const spaces = await (await api.get(BASE + '/api/spaces')).json()
  const spaceId = spaces[0].id
  const tag = `${suite}-${type}`
  const name = (n) => `${tag}-${n}`
  const json = async (res) => {
    if (!res.ok()) throw new Error(`${res.url()} ${res.status()} ${await res.text()}`)
    return res.status() === 204 ? null : res.json()
  }
  const call = {
    get: (path) => api.get(BASE + path).then(json),
    post: (path, data) => api.post(BASE + path, { data }).then(json),
    put: (path, data) => api.put(BASE + path, { data }).then(json),
  }
  /** A page in the user's space; extra: {kind, parent_page_id}. */
  const mk = (n, extra = {}) => call.post(`/api/spaces/${spaceId}/pages`, { title: name(n), ...extra })
  /** A database with a select column "Status" (Todo, Done). */
  async function statusDb(n) {
    const db = await mk(n, { kind: 'database' })
    const status = await call.post(`/api/pages/${db.id}/properties`, { name: 'Status', type: 'select' })
    await call.put(`/api/properties/${status.id}`, {
      options: [
        { id: 'todo', label: 'Todo', color: 'blue' },
        { id: 'done', label: 'Done', color: 'green' },
      ],
    })
    return { db, status }
  }
  const page = await ctx.newPage()
  const errors = []
  page.on('pageerror', (e) => errors.push(e.message))
  await page.goto(BASE + '/')
  await page.waitForSelector('.sidebar')

  /** Opens a page by clicking its sidebar row. */
  async function open(n) {
    await page.locator('.sidebar .page-row', { hasText: name(n) }).first().click()
  }
  return {
    browser, ctx, page, call, me, spaceId, tag, name, mk, statusDb, open, errors,
    check: (label, ok, extra) => check(tag, label, ok, extra),
    done: async () => {
      check(tag, 'no page errors', errors.length === 0, errors.join('; '))
      await browser.close()
    },
  }
}

/** A point inside a locator; fy 0.25 is its upper half, 0.75 its lower. */
export async function at(loc, fy = 0.5, fx = 0.5) {
  const b = await loc.boundingBox()
  return { x: b.x + b.width * fx, y: b.y + b.height * fy }
}

/** A real mouse drag (pointer events, not emulated HTML5 drag-and-drop). */
export async function mouseDrag(page, from, to) {
  await page.mouse.move(from.x, from.y)
  await page.mouse.down()
  await page.mouse.move(from.x + 8, from.y + 4, { steps: 3 })
  await page.mouse.move(to.x, to.y, { steps: 12 })
  await page.mouse.up()
}

/** Drags by the grip that shows on hover (board cards and columns). */
export async function dragByGrip(page, item, grip, to) {
  await item.hover()
  await mouseDrag(page, await at(item.locator(grip)), to)
}
