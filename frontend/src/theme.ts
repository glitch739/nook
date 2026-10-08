import { useEffect, useRef, useState } from 'react'
import { flushSync } from 'react-dom'
import { getPreferences, updatePreferences } from './api'

// Appearance (Settings → Appearance): a color theme, an optional accent
// color, and for "System" a separate light and dark theme. Each theme
// is a palette of color tokens (themes.css) on a light or dark base:
//   <html data-theme="light|dark" data-palette="<id>" data-accent="<id>">
// data-theme keeps every light/dark-specific rule working; data-palette
// swaps the tokens; data-accent (unless "default") overrides --accent.
//
// Stored in this browser (instant on load) and in the account
// (/api/me/preferences, key "appearance"), so it follows the person to
// other devices. The account copy wins when it exists.

export type Base = 'light' | 'dark'
export type ToggleOrigin = { x: number; y: number }

export interface ThemeInfo {
  id: string
  label: string
  base: Base
  // Preview in the picker: background, surface, accent, text.
  swatch: [string, string, string, string]
}

export const THEMES: ThemeInfo[] = [
  // Light
  { id: 'nook-light', label: 'Nook Light', base: 'light', swatch: ['#f5f5f7', '#ffffff', '#1d1d1f', '#6e6e73'] },
  { id: 'github-light', label: 'GitHub Light', base: 'light', swatch: ['#ffffff', '#f6f8fa', '#0969da', '#1f2328'] },
  { id: 'light-modern', label: 'Light Modern', base: 'light', swatch: ['#ffffff', '#f8f8f8', '#005fb8', '#3b3b3b'] },
  { id: 'catppuccin-latte', label: 'Catppuccin Latte', base: 'light', swatch: ['#eff1f5', '#e6e9ef', '#8839ef', '#4c4f69'] },
  { id: 'nord-light', label: 'Nord Light', base: 'light', swatch: ['#eceff4', '#e5e9f0', '#5e81ac', '#2e3440'] },
  { id: 'solarized-light', label: 'Solarized Light', base: 'light', swatch: ['#fdf6e3', '#eee8d5', '#268bd2', '#586e75'] },
  { id: 'gruvbox-light', label: 'Gruvbox Light', base: 'light', swatch: ['#fbf1c7', '#ebdbb2', '#af3a03', '#3c3836'] },
  { id: 'paper', label: 'Paper', base: 'light', swatch: ['#f4ecd8', '#ede3cc', '#8b5e34', '#3b3127'] },
  { id: 'high-contrast-light', label: 'High Contrast Light', base: 'light', swatch: ['#ffffff', '#ffffff', '#0f4a85', '#000000'] },
  // Dark
  { id: 'nook-dark', label: 'Nook Dark', base: 'dark', swatch: ['#191919', '#252525', '#f5f5f7', '#98989d'] },
  { id: 'nook-aurora-dark', label: 'Nook Aurora Dark', base: 'dark', swatch: ['#101014', '#5a6eff', '#dc50aa', '#f5f5f7'] },
  { id: 'github-dark', label: 'GitHub Dark', base: 'dark', swatch: ['#0d1117', '#151b23', '#2f81f7', '#e6edf3'] },
  { id: 'dark-modern', label: 'Dark Modern', base: 'dark', swatch: ['#1f1f1f', '#181818', '#0078d4', '#cccccc'] },
  { id: 'one-dark-pro', label: 'One Dark Pro', base: 'dark', swatch: ['#282c34', '#21252b', '#61afef', '#abb2bf'] },
  { id: 'dracula', label: 'Dracula', base: 'dark', swatch: ['#282a36', '#44475a', '#bd93f9', '#f8f8f2'] },
  { id: 'nord', label: 'Nord', base: 'dark', swatch: ['#2e3440', '#3b4252', '#88c0d0', '#eceff4'] },
  { id: 'catppuccin-frappe', label: 'Catppuccin Frappé', base: 'dark', swatch: ['#303446', '#414559', '#ca9ee6', '#c6d0f5'] },
  { id: 'catppuccin-macchiato', label: 'Catppuccin Macchiato', base: 'dark', swatch: ['#24273a', '#363a4f', '#c6a0f6', '#cad3f5'] },
  { id: 'catppuccin-mocha', label: 'Catppuccin Mocha', base: 'dark', swatch: ['#1e1e2e', '#313244', '#cba6f7', '#cdd6f4'] },
  { id: 'solarized-dark', label: 'Solarized Dark', base: 'dark', swatch: ['#002b36', '#073642', '#268bd2', '#93a1a1'] },
  { id: 'tokyo-night', label: 'Tokyo Night', base: 'dark', swatch: ['#1a1b26', '#1f2335', '#7aa2f7', '#c0caf5'] },
  { id: 'gruvbox-dark', label: 'Gruvbox Dark', base: 'dark', swatch: ['#282828', '#3c3836', '#fabd2f', '#ebdbb2'] },
  { id: 'high-contrast-dark', label: 'High Contrast Dark', base: 'dark', swatch: ['#000000', '#000000', '#f38518', '#ffffff'] },
]

export interface AccentInfo {
  id: string
  label: string
  // What the picker dot shows; themes.css has the real light/dark values.
  color: string
}

export const ACCENTS: AccentInfo[] = [
  { id: 'default', label: 'Theme default', color: '' },
  { id: 'blue', label: 'Blue', color: '#2f7cf6' },
  { id: 'purple', label: 'Purple', color: '#8b5cf6' },
  { id: 'pink', label: 'Pink', color: '#e0559b' },
  { id: 'red', label: 'Red', color: '#e5484d' },
  { id: 'orange', label: 'Orange', color: '#f07f1a' },
  { id: 'yellow', label: 'Yellow', color: '#d6a10a' },
  { id: 'green', label: 'Green', color: '#22a35a' },
  { id: 'teal', label: 'Teal', color: '#14a3a3' },
]

export interface Appearance {
  theme: string // a THEMES id, or 'system'
  lightTheme: string // used by 'system' when the OS is light
  darkTheme: string // ... and when it is dark
  accent: string // an ACCENTS id
}

const DEFAULT_APPEARANCE: Appearance = {
  theme: 'system',
  lightTheme: 'nook-light',
  darkTheme: 'nook-dark',
  accent: 'default',
}

const STORAGE_KEY = 'nook-appearance'
const LEGACY_KEY = 'nook-theme' // a single theme id (or 'light'/'dark' before themes)
const DARK_QUERY = '(prefers-color-scheme: dark)'

const themeById = (id: string) => THEMES.find((t) => t.id === id)

// Accepts anything (localStorage, the server) and returns a valid
// Appearance, falling back field by field.
export function sanitizeAppearance(v: unknown): Appearance {
  const o = (v && typeof v === 'object' ? v : {}) as Record<string, unknown>
  const str = (k: string) => (typeof o[k] === 'string' ? (o[k] as string) : '')
  const theme = str('theme')
  const light = themeById(str('lightTheme'))
  const dark = themeById(str('darkTheme'))
  return {
    theme: theme === 'system' || themeById(theme) ? theme : DEFAULT_APPEARANCE.theme,
    lightTheme: light?.base === 'light' ? light.id : DEFAULT_APPEARANCE.lightTheme,
    darkTheme: dark?.base === 'dark' ? dark.id : DEFAULT_APPEARANCE.darkTheme,
    accent: ACCENTS.some((a) => a.id === str('accent')) ? str('accent') : DEFAULT_APPEARANCE.accent,
  }
}

function readStoredAppearance(): Appearance {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored) return sanitizeAppearance(JSON.parse(stored))
    const legacy = localStorage.getItem(LEGACY_KEY)
    if (legacy === 'light') return { ...DEFAULT_APPEARANCE, theme: 'nook-light' }
    if (legacy === 'dark') return { ...DEFAULT_APPEARANCE, theme: 'nook-dark' }
    if (legacy) return sanitizeAppearance({ theme: legacy })
  } catch {
    // localStorage unavailable or bad JSON
  }
  return DEFAULT_APPEARANCE
}

function storeAppearance(a: Appearance) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(a))
  } catch {
    // Applies for this page load, just won't persist locally.
  }
}

function systemIsDark(): boolean {
  return window.matchMedia(DARK_QUERY).matches
}

export function resolveTheme(a: Appearance, osDark: boolean): ThemeInfo {
  const id = a.theme === 'system' ? (osDark ? a.darkTheme : a.lightTheme) : a.theme
  return themeById(id) ?? themeById(osDark ? 'nook-dark' : 'nook-light')!
}

function applyToDocument(a: Appearance, osDark: boolean) {
  const t = resolveTheme(a, osDark)
  const root = document.documentElement
  root.dataset.theme = t.base
  root.dataset.palette = t.id
  if (a.accent === 'default') delete root.dataset.accent
  else root.dataset.accent = a.accent
  // The browser's own UI around the page (iOS Safari's top bar, Android
  // Chrome's status bar) follows <meta name="theme-color">; match it to
  // the phone header (mobile.css: .tab-bar on --surface-solid) instead
  // of letting the browser guess from the page.
  const color = getComputedStyle(root).getPropertyValue('--surface-solid').trim()
  if (color) {
    let meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')
    if (!meta) {
      meta = document.createElement('meta')
      meta.name = 'theme-color'
      document.head.appendChild(meta)
    }
    meta.content = color
  }
}

// Called from main.tsx before React renders, so a reload doesn't flash
// the default theme first.
export function applyStoredThemeEarly() {
  applyToDocument(readStoredAppearance(), systemIsDark())
}

function prefersReducedMotion(): boolean {
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

// userId: the signed-in account (null when signed out). On sign-in the
// account's saved appearance is loaded; if the account has none yet,
// this browser's current choice is saved to it.
export function useTheme(userId: string | null): {
  appearance: Appearance
  theme: ThemeInfo
  setAppearance: (patch: Partial<Appearance>, origin?: ToggleOrigin) => void
} {
  const [appearance, setAppearanceState] = useState<Appearance>(readStoredAppearance)
  const [osDark, setOsDark] = useState(systemIsDark)
  const appearanceRef = useRef(appearance)
  useEffect(() => {
    appearanceRef.current = appearance
  })

  useEffect(() => {
    const mq = window.matchMedia(DARK_QUERY)
    const onChange = () => setOsDark(mq.matches)
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])

  useEffect(() => {
    applyToDocument(appearance, osDark)
    storeAppearance(appearance)
  }, [appearance, osDark])

  useEffect(() => {
    if (!userId) return
    let cancelled = false
    getPreferences()
      .then((prefs) => {
        if (cancelled) return
        if (prefs.appearance) {
          setAppearanceState(sanitizeAppearance(prefs.appearance))
        } else {
          updatePreferences({ appearance: appearanceRef.current }).catch(() => {})
        }
      })
      .catch((e) => console.error('failed to load preferences', e))
    return () => {
      cancelled = true
    }
  }, [userId])

  function setAppearance(patch: Partial<Appearance>, origin?: ToggleOrigin) {
    const next = sanitizeAppearance({ ...appearanceRef.current, ...patch })
    appearanceRef.current = next
    if (userId) {
      updatePreferences({ appearance: next }).catch((e) => console.error('failed to save appearance', e))
    }
    const startViewTransition = document.startViewTransition?.bind(document)
    const before = resolveTheme(appearance, osDark)
    const after = resolveTheme(next, osDark)
    // The circle reveal only when the visible theme changes (not for an
    // inactive System half or an accent tweak).
    if (!origin || !startViewTransition || prefersReducedMotion() || before.id === after.id) {
      setAppearanceState(next)
      return
    }
    const root = document.documentElement
    const radius = Math.hypot(
      Math.max(origin.x, window.innerWidth - origin.x),
      Math.max(origin.y, window.innerHeight - origin.y),
    )
    root.style.setProperty('--theme-toggle-x', `${origin.x}px`)
    root.style.setProperty('--theme-toggle-y', `${origin.y}px`)
    root.style.setProperty('--theme-toggle-radius', `${radius}px`)
    startViewTransition(() => {
      // flushSync so the DOM shows the new theme before the browser
      // captures the "new" snapshot.
      applyToDocument(next, osDark)
      flushSync(() => setAppearanceState(next))
    })
  }

  return { appearance, theme: resolveTheme(appearance, osDark), setAppearance }
}
