// Fan-out for the server's "something changed" signals (GET
// /api/events, see backend internal/httpapi/events.go). App.tsx owns
// the one EventSource and calls emitLiveEvent; components subscribe
// with useLiveRefresh. The signals carry no data: a listener re-fetches
// what it shows, through the normal permission-checked API.
import { useEffect, useMemo, useRef } from 'react'

export type LiveEventKind = 'tree' | 'data'

type Listener = (kind: LiveEventKind) => void
const listeners = new Set<Listener>()

export function emitLiveEvent(kind: LiveEventKind) {
  for (const fn of listeners) fn(kind)
}

export function onLiveEvent(fn: Listener): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

// Calls refresh (debounced, so a burst of edits is one re-fetch) after
// any of the given kinds is signalled. refresh may change between
// renders; the latest one is used.
export function useLiveRefresh(kinds: LiveEventKind[], refresh: () => void, delayMs = 300) {
  const refreshRef = useRef(refresh)
  useEffect(() => {
    refreshRef.current = refresh
  })
  const key = kinds.join(',')
  useEffect(() => {
    const wanted = new Set(key.split(','))
    let timer: number | undefined
    const off = onLiveEvent((kind) => {
      if (!wanted.has(kind)) return
      window.clearTimeout(timer)
      timer = window.setTimeout(() => refreshRef.current(), delayMs)
    })
    return () => {
      window.clearTimeout(timer)
      off()
    }
  }, [key, delayMs])
}

// Guards a live re-fetch against this component's own writes. Without
// it, two quick edits race: edit A's signal triggers a re-fetch that
// can return before edit B is saved, briefly reverting B on screen.
// Wrap every write in track(); take a snapshot() when a re-fetch
// starts and only apply its result if canApply(snapshot). A skipped
// re-fetch loses nothing: each successful write sends its own signal,
// so another re-fetch follows once the writes have landed.
export function useLocalWrites() {
  const state = useRef({ inFlight: 0, gen: 0 })
  return useMemo(
    () => ({
      track<T>(p: Promise<T>): Promise<T> {
        state.current.inFlight++
        state.current.gen++
        return p.finally(() => {
          state.current.inFlight--
        })
      },
      snapshot: () => state.current.gen,
      canApply: (snap: number) => state.current.inFlight === 0 && state.current.gen === snap,
    }),
    [],
  )
}
