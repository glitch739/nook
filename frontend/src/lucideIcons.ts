// The "Icons" tab of IconPicker.tsx draws from lucide-static (pinned
// exact version, same convention as the @tiptap/* packages) -- plain
// pre-rendered SVG files, not a React component library, since a picker
// browsing 2000+ icons at once is exactly the case where per-icon React
// components would cost far more than the icons themselves.
//
// Two different loading strategies for two different needs:
//  - The picker GRID renders icons as <img src>, one real static asset
//    per icon (emitted to /assets/lucide-<version>/ by vite.config.ts) --
//    cheap, browser-cached, and network-lazy since only visible <img>s
//    actually fetch. This is what keeps browsing thousands of icons from
//    bloating the JS bundle.
//  - Once an icon is actually *chosen* for a page, PageIcon.tsx needs
//    its raw <svg> markup instead of an <img> tag, so its
//    stroke="currentColor" can pick up the surrounding text color and
//    follow the app's light/dark theme (an <img> can't do that -- it
//    renders as an opaque image with no CSS context of its own).
//    iconSvg.ts fetches+caches that raw markup lazily, per icon name,
//    only for icons actually displayed somewhere. That module is the
//    only icon code in the main bundle; this one (tags, full name list)
//    is imported by the lazy IconPicker chunk only.
import { LUCIDE_NAMES } from 'virtual:lucide-names'
import defaultIconNames from './defaultIconNames.json'
import { lucideIconUrl } from './iconSvg'

interface LucideIconEntry {
  name: string
  url: string
  keywords: string[]
}

// keywords start empty: lucide's tags.json (~260 KB) is only needed to
// search, so it loads on the first search (loadIconKeywords) instead of
// with the picker. Until then search matches icon names only.
export const LUCIDE_ICONS: LucideIconEntry[] = LUCIDE_NAMES.map((name) => ({
  name,
  url: lucideIconUrl(name),
  keywords: [] as string[],
})).sort((a, b) => a.name.localeCompare(b.name))

let keywordsLoading: Promise<void> | null = null

/** Fills in every icon's search keywords (once). */
export function loadIconKeywords(): Promise<void> {
  keywordsLoading ??= import('lucide-static/tags.json').then((m) => {
    const tags = m.default as Record<string, string[]>
    for (const icon of LUCIDE_ICONS) icon.keywords = tags[icon.name] ?? []
  })
  return keywordsLoading
}

const LUCIDE_ICONS_BY_NAME = new Map(LUCIDE_ICONS.map((i) => [i.name, i]))

// A curated subset shown by default (no search query yet) instead of
// all ~2000 icons at once -- IconPicker.tsx originally rendered the
// whole LUCIDE_ICONS list unfiltered, which meant mounting ~2000 <img>
// elements the moment the Icons tab opened. Most loaded fine, but a
// visible chunk came back as broken-image placeholders in practice --
// almost certainly the browser's per-origin connection limit (6 for
// HTTP/1.1) getting overwhelmed by that many near-simultaneous
// requests, not a server/hardware problem. The names themselves live in
// defaultIconNames.json (not inline here) because
// scripts/gen-default-icon-svgs.mjs also reads that same file to bake
// this exact set's SVG markup into defaultIconSvgs.generated.ts --
// IconPicker.tsx renders *that* for the default grid instead of an
// <img>, so even this capped set costs zero network requests, not just
// fewer of them. Search still reaches the full set (see maxResults in
// IconPicker.tsx), fetched from real files same as before.
export const DEFAULT_ICONS: LucideIconEntry[] = (defaultIconNames as string[]).map((name) => {
  const entry = LUCIDE_ICONS_BY_NAME.get(name)
  if (!entry) throw new Error(`lucideIcons: curated default "${name}" isn't a real lucide-static icon`)
  return entry
})
