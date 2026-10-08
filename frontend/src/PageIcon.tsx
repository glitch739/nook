import { useEffect, useState } from 'react'
import Emoji from './Emoji'
import { cachedIconSvg, loadIconSvg } from './iconSvg'
import { FALLBACK_GLYPH, iconColorAttr, lucideIconName } from './pageIcon'

// Renders any page icon value -- a literal emoji/character (unchanged
// from before the Icons tab existed) or a "lucide:<name>" value. The
// lucide case is inlined as raw <svg> markup rather than an <img> tag
// specifically so its stroke="currentColor" follows the surrounding
// text color and the app's light/dark theme -- an <img> renders as an
// opaque image with no CSS context, so it'd always come out the same
// (effectively black) regardless of theme. See lucideIcons.ts's header
// comment for why the *picker grid* uses <img> anyway (cheap, lazy,
// browsing thousands of icons) -- this is the one place per rendered
// icon, not thousands, so the inline-and-theme tradeoff goes the other
// way here.
export default function PageIcon({ icon, className }: { icon: string; className?: string }) {
  const name = lucideIconName(icon)
  // trackedName lets a change in `name` (a new icon prop, cache hit or
  // not) update svg during render itself rather than one render late
  // via an effect -- the effect below is left to do only what actually
  // needs one: the async fetch on a cache miss, a real external system
  // to synchronize with.
  const [trackedName, setTrackedName] = useState(name)
  const [svg, setSvg] = useState<string | null | undefined>(name ? cachedIconSvg(name) : undefined)
  if (name !== trackedName) {
    setTrackedName(name)
    setSvg(name ? cachedIconSvg(name) : undefined)
  }

  useEffect(() => {
    if (!name || cachedIconSvg(name)) return
    let cancelled = false
    loadIconSvg(name).then(
      (s) => {
        if (!cancelled) setSvg(s)
      },
      () => {
        // Network failure after a retry: keep the empty placeholder
        // rather than claiming the icon doesn't exist; the next mount
        // tries again.
      },
    )
    return () => {
      cancelled = true
    }
  }, [name])

  if (!name) {
    return (
      <span className={className}>
        <Emoji char={icon} />
      </span>
    )
  }
  // "lucide:<name>:<color>" -- index.css maps data-icon-color to
  // --icon-color and sets it as this span's text color, which the
  // inlined SVG's stroke="currentColor" then picks up.
  const color = iconColorAttr(icon)
  if (svg === undefined) {
    // Still loading -- an empty placeholder with the same footprint so
    // surrounding layout doesn't jump once it arrives.
    return <span className={className} data-icon-color={color} />
  }
  if (svg === null) {
    // Not a name lucide-static actually ships (e.g. stale/tampered
    // data) -- fall back to the schema default rather than rendering
    // nothing.
    return (
      <span className={className}>
        <Emoji char={FALLBACK_GLYPH} />
      </span>
    )
  }
  return <span className={className} data-icon-color={color} dangerouslySetInnerHTML={{ __html: svg }} />
}
