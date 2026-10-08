import { describe, expect, it } from 'vitest'
import { DEFAULT_ICON, encodeLucideIcon, iconColorAttr, lucideIconName, parseLucideIcon } from './pageIcon'

describe('page icon strings', () => {
  it('round-trips lucide icons with and without a color', () => {
    expect(parseLucideIcon(encodeLucideIcon('heart'))).toEqual({ name: 'heart', color: 'default' })
    expect(parseLucideIcon(encodeLucideIcon('heart', 'red'))).toEqual({ name: 'heart', color: 'red' })
  })

  it('treats emoji as non-lucide', () => {
    expect(parseLucideIcon('📄')).toBeNull()
    expect(lucideIconName('📄')).toBeNull()
  })

  it('degrades an unknown color to default', () => {
    expect(parseLucideIcon('lucide:heart:chartreuse')).toEqual({ name: 'heart', color: 'default' })
    expect(iconColorAttr('lucide:heart:chartreuse')).toBeUndefined()
  })

  it('new pages default to a lucide icon', () => {
    expect(lucideIconName(DEFAULT_ICON)).toBe('square-text')
  })
})
