import { useCallback, useEffect, useState } from 'react'
import { adminListUsers } from './api'

const POLL_MS = 60_000

/** For admins: how many sign-ups are waiting for approval. Polled every
 * minute while the tab is visible, and whenever it becomes visible again, so a new registration
 * shows up without a reload. Always 0 for non-admins (no request made).
 * `refresh` re-checks right away, e.g. after approving in Settings. */
export function usePendingApprovals(isAdmin: boolean): { count: number; refresh: () => void } {
  const [count, setCount] = useState(0)

  const refresh = useCallback(() => {
    if (!isAdmin) return
    adminListUsers().then(
      (users) => setCount(users.filter((u) => u.status === 'pending').length),
      () => {
        // Keep the last known count; the next poll tries again.
      },
    )
  }, [isAdmin])

  useEffect(() => {
    if (!isAdmin) return
    refresh()
    // Not while the tab is hidden (a background tab, a locked phone):
    // coming back fires focus/visibilitychange, which refreshes anyway.
    const timer = window.setInterval(() => {
      if (!document.hidden) refresh()
    }, POLL_MS)
    const onVisible = () => {
      if (!document.hidden) refresh()
    }
    window.addEventListener('focus', refresh)
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('focus', refresh)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [isAdmin, refresh])

  // A non-admin (or a user who just lost admin) never shows a stale count.
  return { count: isAdmin ? count : 0, refresh }
}
