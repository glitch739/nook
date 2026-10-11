import { afterEach, describe, expect, it, vi } from 'vitest'
import { dismissToast, getToasts, showToast } from './toast'

describe('toast queue', () => {
  afterEach(() => {
    for (const t of getToasts()) dismissToast(t.id)
    vi.useRealTimers()
  })

  it('shows, dismisses and expires toasts', () => {
    vi.useFakeTimers()
    const id = showToast('saved')
    expect(getToasts().map((t) => t.text)).toEqual(['saved'])
    dismissToast(id)
    expect(getToasts()).toEqual([])
    showToast('later')
    vi.advanceTimersByTime(6000)
    expect(getToasts()).toEqual([])
  })

  it('keeps only the newest three', () => {
    for (const n of [1, 2, 3, 4]) showToast(`t${n}`)
    expect(getToasts().map((t) => t.text)).toEqual(['t2', 't3', 't4'])
  })
})
