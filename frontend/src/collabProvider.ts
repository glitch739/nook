// Realtime collaboration client (ADR 0006): one Y.Doc per page, synced
// over a WebSocket with the Go relay (backend internal/collab).
//
// No-data-loss rules this file enforces:
//   * every local change stays in `pending` until the server acks it;
//     unacked changes are resent after any reconnect (Yjs updates are
//     idempotent, so a duplicate is harmless);
//   * a provider whose editor unmounted stays alive until everything is
//     acked (and keeps reconnecting while offline);
//   * leaving the browser tab with unacked changes triggers the
//     browser's "leave page?" warning;
//   * unacked changes are also kept in IndexedDB (offlineEdits.ts) and
//     resent the next time the page opens, after a reload or a crash.
import type { Schema } from '@tiptap/pm/model'
import { prosemirrorJSONToYXmlFragment, yXmlFragmentToProsemirrorJSON } from '@tiptap/y-tiptap'
import * as awarenessProtocol from 'y-protocols/awareness'
import * as Y from 'yjs'
import { getCollabUser, getCollabUserId, onCollabUserChange } from './collabUser'
import { loadOfflineEdits, saveOfflineEdits } from './offlineEdits'

const MSG_UPDATE = 1
const MSG_AWARENESS = 2
const FLUSH_MS = 60
const SNAPSHOT_MS = 2500

export type CollabStatus = 'connecting' | 'online' | 'offline'

// Your name/color on other people's screens lives in collabUser.ts, so
// the main bundle can set it without pulling in Yjs; this module just
// keeps every open provider's awareness in step with it.
onCollabUserChange((user) => {
  for (const p of providers.values()) p.awareness.setLocalStateField('user', user)
})

function toBase64(bytes: Uint8Array): string {
  let s = ''
  for (let i = 0; i < bytes.length; i += 0x8000) {
    s += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return btoa(s)
}

function frame(type: number, data: Uint8Array): Uint8Array<ArrayBuffer> {
  const out = new Uint8Array(data.length + 1)
  out[0] = type
  out.set(data, 1)
  return out
}

export class CollabProvider {
  readonly doc = new Y.Doc()
  readonly awareness = new awarenessProtocol.Awareness(this.doc)
  readonly fragment = this.doc.getXmlFragment('default')

  status: CollabStatus = 'connecting'
  readonly = false
  ready = false
  // Set when the server asks this client to seed the Yjs doc from the
  // page's existing content_json (first-ever open of a page).
  private needInit = false
  private synced = false
  private schema: Schema | null = null
  private initialContent: string

  private ws: WebSocket | null = null
  private outbox: Uint8Array[] = []
  private pending: Uint8Array[] = []
  private flushTimer = 0
  private snapTimer = 0
  private retryTimer = 0
  private retries = 0
  private lastSeq = 0
  private snapUpto = 0
  private refs = 0
  private closing = false
  private listeners = new Set<() => void>()

  readonly pageId: string
  private readonly offlineKey: string

  constructor(pageId: string, initialContent: string) {
    this.pageId = pageId
    this.offlineKey = `${getCollabUserId()}:${pageId}`
    this.initialContent = initialContent
    this.doc.on('update', this.onDocUpdate)
    this.awareness.on('update', this.onAwarenessUpdate)
    this.awareness.setLocalStateField('user', getCollabUser())
    // Edits a previous visit never got acked come back first, as pending,
    // so the first sync resends them.
    void loadOfflineEdits(this.offlineKey).then((saved) => {
      if (this.doc.isDestroyed) return
      if (saved) {
        Y.applyUpdate(this.doc, saved, this)
        this.pending.unshift(saved)
      }
      this.connect()
    })
  }

  // Mirrors pending + outbox into IndexedDB (or clears it once all acked).
  private persist() {
    const all = [...this.pending, ...this.outbox]
    saveOfflineEdits(this.offlineKey, all.length ? Y.mergeUpdates(all) : null)
  }

  // ---- subscription (React) ----
  subscribe = (fn: () => void) => {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }
  private emit() {
    for (const fn of this.listeners) fn()
  }
  get hasUnsaved() {
    return this.pending.length > 0 || this.outbox.length > 0
  }

  setSchema(schema: Schema) {
    this.schema = schema
    this.maybeInit()
  }

  // ---- connection ----
  private connect() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(`${proto}//${location.host}/api/pages/${this.pageId}/collab`)
    ws.binaryType = 'arraybuffer'
    this.ws = ws
    this.synced = false
    this.setStatus('connecting')
    ws.onmessage = (e) => this.onMessage(e)
    ws.onclose = () => this.onClose(ws)
  }

  private onClose(ws: WebSocket) {
    if (this.ws !== ws) return
    this.ws = null
    this.synced = false
    const others = [...this.awareness.getStates().keys()].filter((id) => id !== this.doc.clientID)
    awarenessProtocol.removeAwarenessStates(this.awareness, others, 'offline')
    if (this.closing && !this.hasUnsaved) {
      this.dispose()
      return
    }
    this.setStatus('offline')
    const delay = Math.min(30000, 500 * 2 ** this.retries++) * (0.75 + Math.random() / 2)
    this.retryTimer = window.setTimeout(() => this.connect(), delay)
  }

  private setStatus(s: CollabStatus) {
    this.status = s
    this.emit()
  }

  private sendRaw(data: Uint8Array<ArrayBuffer> | string) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) this.ws.send(data)
  }

  private onMessage(e: MessageEvent) {
    if (typeof e.data === 'string') {
      const m = JSON.parse(e.data)
      switch (m.t) {
        case 'hello':
          this.readonly = !!m.readonly
          this.needInit = !!m.init
          this.retries = 0
          break
        case 'synced':
          this.synced = true
          this.lastSeq = Math.max(this.lastSeq, m.seq)
          this.setStatus('online')
          this.resendPending()
          this.sendRaw(frame(MSG_AWARENESS, awarenessProtocol.encodeAwarenessUpdate(this.awareness, [this.doc.clientID])))
          this.maybeInit()
          break
        case 'ready':
          this.ready = true
          this.emit()
          break
        case 'init':
          this.needInit = true
          this.maybeInit()
          break
        case 'ack':
          this.pending.shift()
          this.persist()
          this.lastSeq = Math.max(this.lastSeq, m.seq)
          this.scheduleSnapshot()
          if (this.closing && !this.hasUnsaved) this.finishClosing()
          this.emit()
          break
        case 'snap':
          this.snapUpto = Math.max(this.snapUpto, m.upto)
          break
      }
      return
    }
    const data = new Uint8Array(e.data as ArrayBuffer)
    if (data[0] === MSG_UPDATE) {
      const seq = Number(new DataView(data.buffer, data.byteOffset + 1, 8).getBigUint64(0))
      Y.applyUpdate(this.doc, data.subarray(9), this)
      this.lastSeq = Math.max(this.lastSeq, seq)
      if (this.synced) this.scheduleSnapshot()
    } else if (data[0] === MSG_AWARENESS) {
      awarenessProtocol.applyAwarenessUpdate(this.awareness, data.subarray(1), this)
    }
  }

  // Seeds the shared doc from content_json -- only ever on the one
  // client the server picked, so two clients can't both insert it.
  private maybeInit() {
    if (!this.needInit || !this.synced || !this.schema) return
    this.needInit = false
    try {
      const json = JSON.parse(this.initialContent)
      if (this.fragment.length === 0 && json?.content?.length) {
        this.doc.transact(() => prosemirrorJSONToYXmlFragment(this.schema!, json, this.fragment))
      }
    } catch (err) {
      console.error('collab: could not seed document from content_json', err)
    }
    this.flush()
    this.sendRaw(JSON.stringify({ t: 'init_done' }))
  }

  // ---- outgoing updates ----
  private onDocUpdate = (update: Uint8Array, origin: unknown) => {
    if (origin === this) return
    this.outbox.push(update)
    if (!this.flushTimer) this.flushTimer = window.setTimeout(() => this.flush(), FLUSH_MS)
    this.emit()
  }

  private flush() {
    window.clearTimeout(this.flushTimer)
    this.flushTimer = 0
    if (this.outbox.length === 0) return
    const merged = Y.mergeUpdates(this.outbox)
    this.outbox = []
    this.pending.push(merged)
    this.persist()
    if (this.synced) this.sendRaw(frame(MSG_UPDATE, merged))
  }

  private resendPending() {
    if (this.readonly) {
      this.pending = []
      this.persist()
      return
    }
    if (this.pending.length === 0) return
    this.pending = [Y.mergeUpdates(this.pending)]
    this.sendRaw(frame(MSG_UPDATE, this.pending[0]))
  }

  private onAwarenessUpdate = (
    { added, updated, removed }: { added: number[]; updated: number[]; removed: number[] },
    origin: unknown,
  ) => {
    if (origin === 'offline') return
    if (origin === this) {
      // Someone new showed up: tell them who we are right away instead
      // of leaving them blind until awareness's 15s renewal.
      if (added.length > 0 && this.synced && this.awareness.getLocalState() !== null) {
        this.sendRaw(frame(MSG_AWARENESS, awarenessProtocol.encodeAwarenessUpdate(this.awareness, [this.doc.clientID])))
      }
      return
    }
    const changed = [...added, ...updated, ...removed]
    if (this.synced) this.sendRaw(frame(MSG_AWARENESS, awarenessProtocol.encodeAwarenessUpdate(this.awareness, changed)))
  }

  // ---- snapshots (compaction + content_json projection) ----
  private scheduleSnapshot() {
    if (this.readonly) return
    window.clearTimeout(this.snapTimer)
    this.snapTimer = window.setTimeout(() => this.sendSnapshot(), SNAPSHOT_MS + Math.random() * 1000)
  }

  private sendSnapshot() {
    if (!this.ready || this.readonly || !this.synced || this.lastSeq <= this.snapUpto) return
    const content = JSON.stringify(yXmlFragmentToProsemirrorJSON(this.fragment))
    this.sendRaw(
      JSON.stringify({ t: 'snap', upto: this.lastSeq, state: toBase64(Y.encodeStateAsUpdate(this.doc)), content }),
    )
  }

  // ---- lifecycle ----
  acquire() {
    this.refs++
    this.closing = false
    if (this.awareness.getLocalState() === null) this.awareness.setLocalStateField('user', getCollabUser())
  }

  release() {
    if (--this.refs > 0) return
    this.closing = true
    this.flush()
    if (this.hasUnsaved) return // finishClosing runs after the last ack
    this.finishClosing()
  }

  private finishClosing() {
    if (this.refs > 0) return
    window.clearTimeout(this.snapTimer)
    this.sendSnapshot()
    this.awareness.setLocalState(null)
    // Give the snapshot/awareness frames a moment to leave the socket.
    window.setTimeout(() => {
      if (this.refs > 0 || this.hasUnsaved) return
      this.dispose()
    }, 300)
  }

  private dispose() {
    providers.delete(this.pageId)
    window.clearTimeout(this.retryTimer)
    window.clearTimeout(this.flushTimer)
    window.clearTimeout(this.snapTimer)
    const ws = this.ws
    this.ws = null
    ws?.close()
    this.awareness.destroy()
    this.doc.destroy()
  }
}

const providers = new Map<string, CollabProvider>()

// acquireProvider returns the page's shared provider (one per page per
// browser tab, reused by every editor showing that page).
export function acquireProvider(pageId: string, initialContent: string): CollabProvider {
  let p = providers.get(pageId)
  if (!p) {
    p = new CollabProvider(pageId, initialContent)
    providers.set(pageId, p)
  }
  p.acquire()
  return p
}

export function hasUnsavedCollabChanges(): boolean {
  for (const p of providers.values()) if (p.hasUnsaved) return true
  return false
}

window.addEventListener('beforeunload', (e) => {
  if (hasUnsavedCollabChanges()) {
    e.preventDefault()
    e.returnValue = ''
  }
})

export { getCollabUser, type CollabUser } from './collabUser'
