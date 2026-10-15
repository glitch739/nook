import { describe, expect, it } from 'vitest'
import { TITLE_COLUMN_ID, type Property, type Row } from './api'
import { applyView, boardColumns, boardValue, EMPTY_VIEW, moveInBoardOrder } from './dbView'

const status: Property = {
  id: 's', page_id: 'db', name: 'Status', type: 'select', position: '1',
  options: [{ id: 'todo', label: 'Todo', color: '' }, { id: 'done', label: 'Done', color: '' }],
}
const num: Property = { id: 'n', page_id: 'db', name: 'Points', type: 'number', position: '2', options: [] }
const row = (id: string, values: Row['values']): Row => ({ id, title: id, icon: '', position: '', values })
const rows = [
  row('b', { s: { option_id: 'done' }, n: 3 }),
  row('a', { s: { option_id: 'todo' } }),
  row('c', { n: 1 }),
]
const ids = (rs: Row[]) => rs.map((r) => r.id)
const title = (r: Row) => r.title

describe('applyView', () => {
  it('leaves rows alone with no view', () => {
    expect(ids(applyView(rows, [status, num], EMPTY_VIEW, title))).toEqual(['b', 'a', 'c'])
  })
  it('sorts by title', () => {
    expect(ids(applyView(rows, [status, num], { ...EMPTY_VIEW, sortBy: TITLE_COLUMN_ID }, title))).toEqual(['a', 'b', 'c'])
  })
  it('sorts select columns by option order, empty last both ways', () => {
    expect(ids(applyView(rows, [status], { ...EMPTY_VIEW, sortBy: 's' }, title))).toEqual(['a', 'b', 'c'])
    expect(ids(applyView(rows, [status], { ...EMPTY_VIEW, sortBy: 's', desc: true }, title))).toEqual(['b', 'a', 'c'])
  })
  it('sorts numbers numerically', () => {
    expect(ids(applyView(rows, [num], { ...EMPTY_VIEW, sortBy: 'n' }, title))).toEqual(['c', 'b', 'a'])
  })
  it('filters on titles and shown cell text', () => {
    expect(ids(applyView(rows, [status, num], { ...EMPTY_VIEW, filter: 'done' }, title))).toEqual(['b'])
    expect(ids(applyView(rows, [status, num], { ...EMPTY_VIEW, filter: 'C' }, title))).toEqual(['c'])
  })
})

describe('boardColumns', () => {
  it('groups by option in option order, unset and unknown first', () => {
    const cols = boardColumns([...rows, row('d', { s: { option_id: 'gone' } })], status)
    expect(cols.map((c) => [c.key, ids(c.rows)])).toEqual([
      [null, ['c', 'd']],
      ['todo', ['a']],
      ['done', ['b']],
    ])
    expect(cols[0].label).toBe('No Status')
  })
})

describe('boardColumns by person', () => {
  it('one column per person picked so far, values by user id', () => {
    const owner: Property = {
      id: 'o', page_id: 'db', name: 'Owner', type: 'person', position: '3',
      options: [{ id: 'u1', label: 'Anna', color: '' }, { id: 'u2', label: 'Béla', color: '' }],
    }
    const rs = [row('x', { o: { user_id: 'u2' } }), row('y', { o: 'legacy name' }), row('z', {})]
    expect(boardColumns(rs, owner).map((c) => [c.label, ids(c.rows)])).toEqual([
      ['No Owner', ['y', 'z']],
      ['Anna', []],
      ['Béla', ['x']],
    ])
    expect(boardValue(owner, 'u1')).toEqual({ user_id: 'u1' })
    expect(boardValue(status, 'done')).toEqual({ option_id: 'done' })
    expect(boardValue(owner, null)).toBeNull()
  })
})

describe('board order', () => {
  it('sorts cards within columns by the board order, unlisted ones after', () => {
    const rs = [row('a', { s: { option_id: 'todo' } }), row('b', { s: { option_id: 'todo' } }), row('c', { s: { option_id: 'todo' } })]
    expect(ids(boardColumns(rs, status, ['c', 'a'])[1].rows)).toEqual(['c', 'a', 'b'])
    expect(ids(boardColumns(rs, status)[1].rows)).toEqual(['a', 'b', 'c'])
  })

  it('moves a card before another or to the end, dropping gone rows', () => {
    expect(moveInBoardOrder([], ['a', 'b', 'c'], 'c', 'a')).toEqual(['c', 'a', 'b'])
    expect(moveInBoardOrder(['c', 'a', 'b'], ['a', 'b', 'c'], 'c', null)).toEqual(['a', 'b', 'c'])
    expect(moveInBoardOrder(['x', 'b', 'a'], ['a', 'b', 'd'], 'd', 'b')).toEqual(['d', 'b', 'a'])
    expect(moveInBoardOrder(['b', 'a'], ['a', 'b'], 'a', 'a')).toEqual(['b', 'a'])
  })
})

describe('board column order', () => {
  it('puts the saved columns first, unknown keys ignored, the rest after', () => {
    const labels = (o?: string[]) => boardColumns(rows, status, undefined, o).map((c) => c.label)
    expect(labels()).toEqual(['No Status', 'Todo', 'Done'])
    expect(labels(['done', '', 'gone'])).toEqual(['Done', 'No Status', 'Todo'])
  })
})
