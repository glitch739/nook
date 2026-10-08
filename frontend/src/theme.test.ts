import { describe, expect, it } from 'vitest'
import { THEMES, resolveTheme, sanitizeAppearance } from './theme'

describe('sanitizeAppearance', () => {
  it('falls back field by field', () => {
    expect(sanitizeAppearance(null)).toEqual({
      theme: 'system',
      lightTheme: 'nook-light',
      darkTheme: 'nook-dark',
      accent: 'default',
    })
    expect(sanitizeAppearance({ theme: 'nope', accent: 'blue', lightTheme: 'dracula', darkTheme: 'nord' })).toEqual({
      theme: 'system',
      lightTheme: 'nook-light', // dracula is dark, not valid as the light half
      darkTheme: 'nord',
      accent: 'blue',
    })
  })
})

describe('resolveTheme', () => {
  const a = sanitizeAppearance({ theme: 'system', lightTheme: 'paper', darkTheme: 'tokyo-night' })
  it('uses the light or dark half for system', () => {
    expect(resolveTheme(a, false).id).toBe('paper')
    expect(resolveTheme(a, true).id).toBe('tokyo-night')
  })
  it('uses a fixed theme regardless of the OS', () => {
    expect(resolveTheme({ ...a, theme: 'github-dark' }, false).id).toBe('github-dark')
  })
})

describe('THEMES', () => {
  it('has unique ids', () => {
    expect(new Set(THEMES.map((t) => t.id)).size).toBe(THEMES.length)
  })
})
