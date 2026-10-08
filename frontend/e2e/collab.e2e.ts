// End-to-end test of the realtime collaboration stack: the real
// collabProvider (browser code) against a real Nook server.
// Run: scripts/collab-e2e.sh  (starts a throwaway server + DB)
import { getSchema } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import { yXmlFragmentToProsemirrorJSON } from '@tiptap/y-tiptap'
import * as Y from 'yjs'

const BASE = process.env.NOOK_URL!
const host = new URL(BASE).host

// Minimal browser shims for collabProvider.
const g = globalThis as any
g.window = globalThis
g.addEventListener = () => {}
g.location = { protocol: 'http:', host }
let cookie = ''
const NodeWS = g.WebSocket
g.WebSocket = class extends NodeWS {
  constructor(url: string) {
    super(url, { headers: { cookie } })
  }
}

const { acquireProvider } = await import('../src/collabProvider')
type P = ReturnType<typeof acquireProvider>

function fail(msg: string): never {
  console.error('FAIL:', msg)
  process.exit(1)
}

async function api(method: string, path: string, body?: unknown) {
  const res = await fetch(BASE + path, {
    method,
    headers: { 'Content-Type': 'application/json', cookie },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const set = res.headers.get('set-cookie')
  if (set) cookie = set.split(';')[0]
  const text = await res.text()
  return { status: res.status, json: text ? (() => { try { return JSON.parse(text) } catch { return text } })() : null }
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
async function until(what: string, fn: () => boolean, ms = 8000) {
  const t = Date.now()
  while (!fn()) {
    if (Date.now() - t > ms) fail(`timeout waiting for: ${what}`)
    await sleep(20)
  }
}

const text = (p: P) => JSON.stringify(yXmlFragmentToProsemirrorJSON(p.fragment))
const schema = getSchema([StarterKit])

function typeAt(p: P, where: 'start' | 'end', s: string) {
  p.doc.transact(() => {
    const para = p.fragment.get(0) as Y.XmlElement
    const t = para.get(0) as Y.XmlText
    t.insert(where === 'start' ? 0 : t.length, s)
  })
}

// ---- setup ----
let r = await api('POST', '/api/auth/register', { email: 'e2e@example.hu', username: 'e2e', display_name: 'E2E', password: 'e2e-password' })
if (r.status !== 201) fail('register ' + r.status + ' ' + JSON.stringify(r.json))
const spaces = (await api('GET', '/api/spaces')).json
const page = (await api('POST', `/api/spaces/${spaces[0].id}/pages`, { title: 'Collab', parent_page_id: null })).json
const seed = { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'hello' }] }] }
r = await api('PUT', `/api/pages/${page.id}`, { content_json: JSON.stringify(seed) })
if (r.status !== 200) fail('seed content ' + r.status)

// ---- 1. two clients join at once: exactly one seeds, no duplication ----
const A = acquireProvider(page.id, JSON.stringify(seed))
// A second browser tab = a separate module-level registry, so build B
// directly rather than sharing A through acquireProvider.
const { CollabProvider } = await import('../src/collabProvider')
const B = new CollabProvider(page.id, JSON.stringify(seed))
B.acquire()
A.setSchema(schema)
B.setSchema(schema)
await until('both ready', () => A.ready && B.ready)
await until('seed visible on both', () => text(A).includes('hello') && text(B).includes('hello'))
if ((text(A).match(/hello/g) || []).length !== 1) fail('seed duplicated: ' + text(A))
console.log('ok 1 - single seeding')

// ---- 2. concurrent edits converge ----
typeAt(A, 'start', 'A1 ')
typeAt(B, 'end', ' B1')
await until('converged', () => text(A) === text(B) && text(A).includes('A1') && text(A).includes('B1'))
console.log('ok 2 - concurrent edits converge:', (A.fragment.get(0) as Y.XmlElement).toString())

// ---- 3. offline edits are kept and delivered on reconnect ----
;(B as any).ws.close()
await until('B offline', () => B.status === 'offline')
typeAt(B, 'end', ' OFFLINE')
typeAt(A, 'start', 'A2 ')
await sleep(200)
if (!B.hasUnsaved) fail('offline edit should be pending')
await until('B reconnected + converged', () => B.status === 'online' && text(A) === text(B) && text(A).includes('OFFLINE') && text(B).includes('A2'), 15000)
await until('B acked', () => !B.hasUnsaved)
console.log('ok 3 - offline edits survive reconnect')

// ---- 4. close everything; snapshot writes content_json ----
const final = text(A)
A.release()
B.release()
await sleep(4500)
const got = (await api('GET', `/api/pages/${page.id}`)).json
for (const s of ['hello', 'A1', 'A2', 'B1', 'OFFLINE']) if (!got.content_json.includes(s)) fail(`content_json missing ${s}: ${got.content_json}`)
console.log('ok 4 - content_json snapshot is complete')

// ---- 5. plain content writes are refused now ----
r = await api('PUT', `/api/pages/${page.id}`, { content_json: '{"type":"doc","content":[]}' })
if (r.status !== 409) fail('content PUT should be 409, got ' + r.status)
console.log('ok 5 - stale REST content write refused')

// ---- 6. a fresh client loads exactly the same document ----
const C = new CollabProvider(page.id, '{"type":"doc","content":[]}')
C.acquire()
C.setSchema(schema)
await until('C ready', () => C.ready)
await until('C content', () => text(C) === final)
console.log('ok 6 - fresh load identical')
C.release()
await sleep(500)
console.log('ALL PASS')
process.exit(0)
