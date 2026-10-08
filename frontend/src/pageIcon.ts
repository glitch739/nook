// A page's icon is one opaque TEXT column on the backend (see
// migrations/0002_page_icon.sql) -- this file is the one place the
// frontend gives that string meaning. Shapes it can hold:
//  - a literal emoji/character (the only kind that existed before
//    IconPicker.tsx grew an "Icons" tab) -- rendered as plain text.
//  - "lucide:<name>", picked from the Icons tab -- rendered as an
//    inlined SVG instead (PageIcon.tsx), in the surrounding text color.
//  - "lucide:<name>:<color>", the same icon tinted with one of
//    ICON_COLORS below (Notion-style colored icons). Lucide names are
//    kebab-case and never contain ':', so the suffix is unambiguous,
//    and every value written before colors existed still parses as
//    "no color". The backend never needs to know the difference; it
//    just stores whatever string it's given, same as it always has.
const LUCIDE_PREFIX = 'lucide:'

// What a new page gets, and what "Remove" in the icon picker resets to
// (mirror of pages.DefaultDocumentIcon on the backend).
export const DEFAULT_ICON = 'lucide:square-text'
export const DEFAULT_DATABASE_ICON = 'lucide:table'
export const DEFAULT_COLLECTION_ICON = 'lucide:library-big'

// Shown as plain text when a stored icon can't be drawn at all (a lucide
// name that doesn't exist, from stale or tampered data). Deliberately an
// emoji, not DEFAULT_ICON: the fallback must never itself need loading.
export const FALLBACK_GLYPH = '📄'

// Same ten-swatch palette Notion's icon picker offers. 'default' means
// "no color suffix" -- the icon follows the surrounding text color.
// The actual color values live in index.css (--icon-<color>, light and
// dark variants), so the palette follows the theme like everything else.
export const ICON_COLORS = [
  'default',
  'gray',
  'brown',
  'yellow',
  'orange',
  'green',
  'blue',
  'purple',
  'pink',
  'red',
] as const

export type IconColor = (typeof ICON_COLORS)[number]

export const ICON_COLOR_LABELS: Record<IconColor, string> = {
  default: 'Default',
  gray: 'Gray',
  brown: 'Brown',
  yellow: 'Yellow',
  orange: 'Orange',
  green: 'Green',
  blue: 'Blue',
  purple: 'Purple',
  pink: 'Pink',
  red: 'Red',
}

function isIconColor(v: string): v is IconColor {
  return (ICON_COLORS as readonly string[]).includes(v)
}

export function encodeLucideIcon(name: string, color: IconColor = 'default'): string {
  return color === 'default' ? LUCIDE_PREFIX + name : `${LUCIDE_PREFIX}${name}:${color}`
}

/** Splits a "lucide:<name>[:<color>]" value; null for emoji/text icons.
 * An unknown color suffix (stale/tampered data) degrades to 'default'
 * rather than breaking the icon. */
export function parseLucideIcon(icon: string): { name: string; color: IconColor } | null {
  if (!icon.startsWith(LUCIDE_PREFIX)) return null
  const rest = icon.slice(LUCIDE_PREFIX.length)
  const sep = rest.indexOf(':')
  if (sep === -1) return { name: rest, color: 'default' }
  const color = rest.slice(sep + 1)
  return { name: rest.slice(0, sep), color: isIconColor(color) ? color : 'default' }
}

export function lucideIconName(icon: string): string | null {
  return parseLucideIcon(icon)?.name ?? null
}

/** The icon's color, or undefined when it has none (emoji, or an
 * uncolored lucide icon) -- shaped for a data-icon-color attribute,
 * which index.css maps to --icon-color. */
export function iconColorAttr(icon: string): IconColor | undefined {
  const color = parseLucideIcon(icon)?.color
  return color && color !== 'default' ? color : undefined
}
