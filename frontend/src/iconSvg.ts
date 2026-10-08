// Raw <svg> markup of one lucide icon, by name -- what PageIcon.tsx
// inlines so stroke="currentColor" follows the text color and theme.
// Deliberately tiny: the main bundle needs only the base path, never
// the list of ~2000 icons (that lives in lucideIcons.ts, used only by
// the lazy IconPicker chunk). See lucideIcons() in vite.config.ts.

const NAME_RE = /^[a-z0-9]+(?:-[a-z0-9]+)*$/

export function lucideIconUrl(name: string): string {
  return `${__LUCIDE_BASE__}${name}.svg`
}

const cache = new Map<string, string>()
const inflight = new Map<string, Promise<string | null>>()

/** Synchronous cache read, for a component that already warmed the
 * cache via loadIconSvg and just needs the value back during render. */
export function cachedIconSvg(name: string): string | undefined {
  return cache.get(name)
}

async function fetchSvg(name: string, retries: number): Promise<string | null> {
  try {
    const res = await fetch(lucideIconUrl(name))
    if (!res.ok) return null // not an icon this lucide version ships
    const text = await res.text()
    // lucide-static files open with a license comment before <svg, so
    // look for the element rather than requiring it first. Anything
    // without one (an HTML error page, say) isn't an icon.
    if (!/<svg[\s>]/.test(text)) return null
    cache.set(name, text)
    return text
  } catch (err) {
    // Network hiccup, not a bad name: one retry before giving up.
    if (retries > 0) {
      await new Promise((r) => setTimeout(r, 1500))
      return fetchSvg(name, retries - 1)
    }
    throw err
  }
}

/** Fetches (and caches) one icon's markup. Resolves null for a name that
 * isn't a real icon (stale or tampered data); concurrent calls for the
 * same icon share one request. */
export function loadIconSvg(name: string): Promise<string | null> {
  const cached = cache.get(name)
  if (cached) return Promise.resolve(cached)
  // Also keeps any "../" or other path tricks out of the fetch URL.
  if (!NAME_RE.test(name)) return Promise.resolve(null)
  let p = inflight.get(name)
  if (!p) {
    p = fetchSvg(name, 1).finally(() => inflight.delete(name))
    inflight.set(name, p)
  }
  return p
}
