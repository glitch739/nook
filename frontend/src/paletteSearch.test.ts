import { describe, expect, it } from 'vitest'
import { fold, score } from './paletteSearch'

describe('palette search', () => {
  it('ignores accents and case', () => {
    expect(fold('Esküvői Teendők')).toBe('eskuvoi teendok')
    expect(score('Esküvői teendők listája', fold('ESKÜ'))).toBe(0)
  })

  it('ranks prefix < word start < substring < loose < none', () => {
    const q = fold('note')
    expect(score('Notes', q)).toBe(0)
    expect(score('Go Notes', q)).toBe(1)
    expect(score('Keynotes', q)).toBe(2)
    expect(score('No time', q)).toBe(3)
    expect(score('Recipes', q)).toBe(-1)
  })
})
