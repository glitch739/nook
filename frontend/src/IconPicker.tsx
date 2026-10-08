import { useEffect, useMemo, useRef, useState } from 'react'
import { DEFAULT_ICON_SVGS } from './defaultIconSvgs.generated'
import Emoji from './Emoji'
import { ALL_EMOJI, EMOJI_CATEGORIES } from './emojiData'
import { DEFAULT_ICONS, LUCIDE_ICONS, loadIconKeywords } from './lucideIcons'
import PageIcon from './PageIcon'
import {
  DEFAULT_ICON,
  ICON_COLORS,
  ICON_COLOR_LABELS,
  encodeLucideIcon,
  parseLucideIcon,
  type IconColor,
} from './pageIcon'
import Popover from './Popover'

// One icon glyph in a given color. The curated default set
// (defaultIconSvgs.generated.ts) is baked-in markup -- inlined directly,
// zero network requests, and its stroke="currentColor" takes the color
// straight from data-icon-color (see index.css). Anything outside that
// set falls back to an <img src> against a real static file when
// uncolored (cheap, lazy -- fine for search results), or to PageIcon's
// lazily fetched inline markup when it needs a color, since an <img>
// can't be recolored from CSS.
function IconGlyph({
  name,
  url,
  color,
  inline = false,
}: {
  name: string
  url: string | undefined
  color: IconColor
  inline?: boolean
}) {
  const svg = DEFAULT_ICON_SVGS[name]
  const colorAttr = color === 'default' ? undefined : color
  if (svg) {
    return (
      <span
        className="icon-picker-icon-svg"
        data-icon-color={colorAttr}
        dangerouslySetInnerHTML={{ __html: svg }}
      />
    )
  }
  if (colorAttr || inline) {
    return <PageIcon icon={encodeLucideIcon(name, color)} className="icon-picker-icon-svg" />
  }
  return <img src={url} alt="" loading="lazy" />
}

// Renders one grid cell in the Icons tab. Hovering it (or right-click /
// long-press, for when there's no hover) opens the color palette for
// that icon -- see ColorMenu in IconPicker below.
function IconGridButton({
  name,
  url,
  color = 'default',
  highlighted,
  onClick,
  onHover,
  onLeave,
}: {
  name: string
  url: string | undefined
  color?: IconColor
  highlighted: boolean
  onClick: () => void
  onHover: (el: HTMLElement, immediate: boolean) => void
  onLeave: () => void
}) {
  return (
    <button
      type="button"
      className={highlighted ? 'icon-picker-icon-btn highlighted' : 'icon-picker-icon-btn'}
      title={name}
      onClick={onClick}
      onMouseEnter={(e) => onHover(e.currentTarget, false)}
      onMouseLeave={onLeave}
      onContextMenu={(e) => {
        e.preventDefault()
        onHover(e.currentTarget, true)
      }}
    >
      <IconGlyph name={name} url={url} color={color} />
    </button>
  )
}

// Hover-to-open timing for the color palette: long enough that just
// sweeping the pointer across the grid doesn't flash it open over every
// icon, short enough to feel responsive once you pause on one. While a
// palette is already open, moving onto another icon switches faster,
// and leaving gets a small grace period so the pointer can travel from
// the icon down into the palette without it closing underneath.
const COLOR_MENU_OPEN_DELAY = 450
const COLOR_MENU_SWITCH_DELAY = 150
const COLOR_MENU_CLOSE_DELAY = 200
const COLOR_MENU_WIDTH = 5 * 32 + 4 * 6 + 2 * 8
const COLOR_MENU_HEIGHT = 2 * 32 + 6 + 2 * 8

interface ColorMenuState {
  key: string
  name: string
  url: string | undefined
  top: number
  left: number
}

const RECENTS_KEY = 'nook-recent-icons'
const MAX_RECENTS = 16
// A broad search (e.g. a single common letter) can still match
// hundreds of the ~2000 icons -- capped for the same reason the
// no-query default view is capped in lucideIcons.ts (DEFAULT_ICONS):
// too many <img> elements mounting near-simultaneously overwhelmed the
// browser's per-origin connection limit and some came back as broken
// images.
const MAX_ICON_SEARCH_RESULTS = 200

const PICKER_WIDTH = 304
const PICKER_MAX_HEIGHT = 352

function readRecents(): string[] {
  try {
    const raw = localStorage.getItem(RECENTS_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed.filter((v) => typeof v === 'string') : []
  } catch {
    return []
  }
}

function pushRecent(icon: string) {
  try {
    const next = [icon, ...readRecents().filter((v) => v !== icon)].slice(0, MAX_RECENTS)
    localStorage.setItem(RECENTS_KEY, JSON.stringify(next))
  } catch {
    // Not persisted this session -- the picker still works without recents.
  }
}

type Tab = 'emoji' | 'icons'

// Replaces the old plain-text IconEditor: a real, searchable picker with
// two sources -- a curated emoji set (emojiData.ts, no dependency, no
// network) and lucide-static's full icon set (lucideIcons.ts, pinned
// exact version) -- plus a shared "recent" row and a way to reset back
// to the schema default. Same onChange(icon)/onClose contract as the
// editor it replaces, so Sidebar.tsx's call site barely changed.
//
// Rendered through Popover -- see its own header comment for why (the
// sidebar's overflow-y: auto used to clip this whenever it wasn't wide
// enough to fit the popover entirely inline).
export default function IconPicker({
  icon,
  anchorEl,
  onChange,
  onClose,
}: {
  icon: string
  anchorEl: HTMLElement
  onChange: (icon: string) => void
  onClose: () => void
}) {
  const [tab, setTab] = useState<Tab>(parseLucideIcon(icon) ? 'icons' : 'emoji')
  const [query, setQuery] = useState('')
  const [recents, setRecents] = useState<string[]>(readRecents)
  const searchRef = useRef<HTMLInputElement>(null)
  const [colorMenu, setColorMenu] = useState<ColorMenuState | null>(null)
  const openTimer = useRef<number | undefined>(undefined)
  const closeTimer = useRef<number | undefined>(undefined)
  const current = parseLucideIcon(icon)

  useEffect(() => {
    searchRef.current?.focus()
    return () => {
      window.clearTimeout(openTimer.current)
      window.clearTimeout(closeTimer.current)
    }
  }, [])

  // The palette is rendered inside the picker's own panel (not a second
  // portaled Popover) so a click in it counts as "inside" for the
  // picker's outside-click-to-close. The panel is position: fixed, so
  // it's the containing block for the palette's absolute coordinates;
  // it opens below the icon, or above it when that would run past the
  // panel's bottom edge (overflow: hidden would clip it otherwise).
  function openColorMenu(key: string, name: string, url: string | undefined, el: HTMLElement) {
    const panel = el.closest('.popover-panel')
    if (!panel) return
    const p = panel.getBoundingClientRect()
    const b = el.getBoundingClientRect()
    let top = b.bottom - p.top + 2
    if (top + COLOR_MENU_HEIGHT > panel.clientHeight - 4) {
      top = Math.max(4, b.top - p.top - 2 - COLOR_MENU_HEIGHT)
    }
    const left = Math.min(Math.max(4, b.left - p.left - 8), panel.clientWidth - COLOR_MENU_WIDTH - 4)
    setColorMenu({ key, name, url, top, left })
  }

  function hoverIcon(key: string, name: string, url: string | undefined) {
    return (el: HTMLElement, immediate: boolean) => {
      window.clearTimeout(openTimer.current)
      window.clearTimeout(closeTimer.current)
      if (colorMenu?.key === key) return
      if (immediate) {
        openColorMenu(key, name, url, el)
        return
      }
      openTimer.current = window.setTimeout(
        () => openColorMenu(key, name, url, el),
        colorMenu ? COLOR_MENU_SWITCH_DELAY : COLOR_MENU_OPEN_DELAY,
      )
    }
  }

  function leaveIcon() {
    window.clearTimeout(openTimer.current)
    window.clearTimeout(closeTimer.current)
    closeTimer.current = window.setTimeout(() => setColorMenu(null), COLOR_MENU_CLOSE_DELAY)
  }

  function closeColorMenu() {
    window.clearTimeout(openTimer.current)
    window.clearTimeout(closeTimer.current)
    setColorMenu(null)
  }

  function choose(value: string) {
    pushRecent(value)
    setRecents(readRecents())
    onChange(value)
    onClose()
  }

  function remove() {
    onChange(DEFAULT_ICON)
    onClose()
  }

  const q = query.trim().toLowerCase()

  const filteredEmoji = useMemo(() => {
    if (!q) return null
    return ALL_EMOJI.filter((e) => e.keywords.some((k) => k.includes(q)))
  }, [q])

  // Icon search keywords load on the first search (lucideIcons.ts);
  // keywordsReady re-runs the search once they're in.
  const [keywordsReady, setKeywordsReady] = useState(false)
  useEffect(() => {
    if (!q || keywordsReady) return
    let cancelled = false
    loadIconKeywords().then(
      () => {
        if (!cancelled) setKeywordsReady(true)
      },
      () => {},
    )
    return () => {
      cancelled = true
    }
  }, [q, keywordsReady])

  // One pass: the shown results plus whether there were more.
  const { filteredIcons, iconsTruncated } = useMemo(() => {
    if (!q) return { filteredIcons: DEFAULT_ICONS, iconsTruncated: false }
    const all = LUCIDE_ICONS.filter((i) => i.name.includes(q) || i.keywords.some((k) => k.includes(q)))
    return { filteredIcons: all.slice(0, MAX_ICON_SEARCH_RESULTS), iconsTruncated: all.length > MAX_ICON_SEARCH_RESULTS }
    // keywordsReady: LUCIDE_ICONS' keywords were filled in place.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q, keywordsReady])

  const recentEmoji = tab === 'emoji' ? recents.filter((r) => !parseLucideIcon(r)) : []
  // Recents keep the full value, color included -- the same icon picked
  // in two colors is two recents, and clicking one re-applies its color.
  const recentIcons =
    tab === 'icons'
      ? recents.flatMap((value) => {
          const parsed = parseLucideIcon(value)
          return parsed ? [{ value, ...parsed }] : []
        })
      : []

  return (
    <Popover anchorEl={anchorEl} width={PICKER_WIDTH} maxHeight={PICKER_MAX_HEIGHT} onClose={onClose} className="icon-picker">
      <div className="icon-picker-tabs">
        <button
          type="button"
          className={tab === 'emoji' ? 'icon-picker-tab active' : 'icon-picker-tab'}
          onClick={() => {
            closeColorMenu()
            setTab('emoji')
          }}
        >
          Emoji
        </button>
        <button
          type="button"
          className={tab === 'icons' ? 'icon-picker-tab active' : 'icon-picker-tab'}
          onClick={() => setTab('icons')}
        >
          Icons
        </button>
        <button type="button" className="icon-picker-remove" onClick={remove}>
          Remove
        </button>
      </div>

      <input
        ref={searchRef}
        className="icon-picker-search"
        placeholder="Search…"
        value={query}
        onChange={(e) => {
          closeColorMenu()
          setQuery(e.target.value)
        }}
        onKeyDown={(e) => {
          if (e.key === 'Escape') onClose()
        }}
      />

      <div className="icon-picker-body" onScroll={colorMenu ? closeColorMenu : undefined}>
        {tab === 'emoji' ? (
          <>
            {recentEmoji.length > 0 && (
              <div className="icon-picker-section">
                <div className="icon-picker-section-label">Recently used</div>
                <div className="icon-picker-emoji-grid">
                  {recentEmoji.map((e) => (
                    <button
                      type="button"
                      key={e}
                      className="icon-picker-emoji"
                      onClick={() => choose(e)}
                    >
                      <Emoji char={e} />
                    </button>
                  ))}
                </div>
              </div>
            )}
            {filteredEmoji ? (
              <div className="icon-picker-section">
                <div className="icon-picker-emoji-grid">
                  {filteredEmoji.map((e) => (
                    <button
                      type="button"
                      key={e.char}
                      className="icon-picker-emoji"
                      onClick={() => choose(e.char)}
                    >
                      <Emoji char={e.char} />
                    </button>
                  ))}
                  {filteredEmoji.length === 0 && (
                    <div className="icon-picker-empty">No results</div>
                  )}
                </div>
              </div>
            ) : (
              EMOJI_CATEGORIES.map((cat) => (
                <div className="icon-picker-section" key={cat.label}>
                  <div className="icon-picker-section-label">{cat.label}</div>
                  <div className="icon-picker-emoji-grid">
                    {cat.emoji.map((e) => (
                      <button
                        type="button"
                        key={e.char}
                        className="icon-picker-emoji"
                        onClick={() => choose(e.char)}
                      >
                        <Emoji char={e.char} />
                      </button>
                    ))}
                  </div>
                </div>
              ))
            )}
          </>
        ) : (
          <>
            {recentIcons.length > 0 && (
              <div className="icon-picker-section">
                <div className="icon-picker-section-label">Recently used</div>
                <div className="icon-picker-icon-grid">
                  {recentIcons.map((r) => {
                    const key = 'recent:' + r.value
                    const url = LUCIDE_ICONS.find((i) => i.name === r.name)?.url
                    return (
                      <IconGridButton
                        key={r.value}
                        name={r.name}
                        url={url}
                        color={r.color}
                        highlighted={colorMenu?.key === key}
                        onClick={() => choose(r.value)}
                        onHover={hoverIcon(key, r.name, url)}
                        onLeave={leaveIcon}
                      />
                    )
                  })}
                </div>
              </div>
            )}
            <div className="icon-picker-section">
              {!q && <div className="icon-picker-section-label">Common icons</div>}
              <div className="icon-picker-icon-grid">
                {filteredIcons.map((i) => (
                  <IconGridButton
                    key={i.name}
                    name={i.name}
                    url={i.url}
                    highlighted={colorMenu?.key === 'grid:' + i.name}
                    onClick={() => choose(encodeLucideIcon(i.name))}
                    onHover={hoverIcon('grid:' + i.name, i.name, i.url)}
                    onLeave={leaveIcon}
                  />
                ))}
                {filteredIcons.length === 0 && (
                  <div className="icon-picker-empty">No results</div>
                )}
              </div>
              {!q && (
                <div className="icon-picker-hint">Type a keyword for more icons…</div>
              )}
              {iconsTruncated && (
                <div className="icon-picker-hint">
                  Too many results -- refine your search for more.
                </div>
              )}
            </div>
          </>
        )}
      </div>

      {colorMenu && (
        <div
          className="icon-color-menu"
          style={{ top: colorMenu.top, left: colorMenu.left, width: COLOR_MENU_WIDTH }}
          onMouseEnter={() => {
            window.clearTimeout(openTimer.current)
            window.clearTimeout(closeTimer.current)
          }}
          onMouseLeave={leaveIcon}
        >
          {ICON_COLORS.map((color) => (
            <button
              type="button"
              key={color}
              className={
                current?.name === colorMenu.name && current.color === color
                  ? 'icon-color-menu-btn active'
                  : 'icon-color-menu-btn'
              }
              title={ICON_COLOR_LABELS[color]}
              onClick={() => choose(encodeLucideIcon(colorMenu.name, color))}
            >
              <IconGlyph name={colorMenu.name} url={colorMenu.url} color={color} inline />
            </button>
          ))}
        </div>
      )}
    </Popover>
  )
}
