import { describe, expect, it } from 'vitest'
import { TITLE_COLUMN_ID, type Property, type Row } from './api'
import { applyView, EMPTY_VIEW } from './dbView'

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
