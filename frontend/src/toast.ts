// A tiny app-wide notice queue: errors that used to go only to the
// console, and "Archived · Undo". Plain module state plus a subscribe,
// so any file can raise one without a React context.

export interface ToastAction {
  label: string
  run: () => void
}

export interface Toast {
  id: number
  text: string
  error: boolean
  action?: ToastAction
}

const TIMEOUT_MS = 6000
// Keeps a burst (e.g. a whole drag failing) from filling the screen.
const MAX_SHOWN = 3

let toasts: Toast[] = []
let nextId = 1
const listeners = new Set<() => void>()

function emit() {
  for (const fn of listeners) fn()
}

export function subscribeToasts(fn: () => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

export const getToasts = () => toasts

export function dismissToast(id: number) {
  if (!toasts.some((t) => t.id === id)) return
  toasts = toasts.filter((t) => t.id !== id)
  emit()
}

export function showToast(text: string, opts: { error?: boolean; action?: ToastAction } = {}): number {
  const id = nextId++
  toasts = [...toasts, { id, text, error: opts.error ?? false, action: opts.action }].slice(-MAX_SHOWN)
  emit()
  setTimeout(() => dismissToast(id), TIMEOUT_MS)
  return id
}

// For a user action that failed: logged as before, and now shown too,
// with the server's own reason when it gave one.
export function notifyError(text: string, err?: unknown) {
  console.error(text, err)
  const reason = err instanceof Error && err.message ? ` (${err.message})` : ''
  showToast(text + reason, { error: true })
}
