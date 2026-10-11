// Edits the server hasn't acked yet, kept in IndexedDB so a reload, a
// closed tab or a phone killing the app in the background doesn't lose
// them (collabProvider.ts). One merged Yjs update per user and page;
// deleted once everything is acked. Without IndexedDB (private mode,
// old browsers) these quietly do nothing -- the in-memory copy and the
// leave-page warning still apply.

const DB_NAME = 'nook-offline'
const STORE = 'pending'

let dbPromise: Promise<IDBDatabase> | null = null

function open(): Promise<IDBDatabase> {
  dbPromise ??= new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, 1)
    req.onupgradeneeded = () => req.result.createObjectStore(STORE)
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
  return dbPromise
}

function run<T>(mode: IDBTransactionMode, fn: (s: IDBObjectStore) => IDBRequest<T>): Promise<T | undefined> {
  if (typeof indexedDB === 'undefined') return Promise.resolve(undefined)
  return open()
    .then(
      (db) =>
        new Promise<T>((resolve, reject) => {
          const req = fn(db.transaction(STORE, mode).objectStore(STORE))
          req.onsuccess = () => resolve(req.result)
          req.onerror = () => reject(req.error)
        }),
    )
    .catch((err) => {
      console.error('offline edits store', err)
      return undefined
    })
}

export const loadOfflineEdits = (key: string) =>
  run<Uint8Array | undefined>('readonly', (s) => s.get(key) as IDBRequest<Uint8Array | undefined>)

export function saveOfflineEdits(key: string, update: Uint8Array | null) {
  void (update ? run('readwrite', (s) => s.put(update, key)) : run('readwrite', (s) => s.delete(key)))
}
