// One-time tokens arriving in links (?reset=, ?invite=, ?verify=; see
// ADR 0005). Read exactly once at module load and removed from the
// address bar right away, so they don't linger in history or get
// shared by accident (and StrictMode double-renders can't lose them).
function take(name: string): string | null {
  const url = new URL(window.location.href)
  const v = url.searchParams.get(name)
  if (v !== null) {
    url.searchParams.delete(name)
    window.history.replaceState(null, '', url.pathname + url.search + url.hash)
  }
  return v
}

export const urlTokens = {
  reset: take('reset'),
  invite: take('invite'),
  verify: take('verify'),
}
