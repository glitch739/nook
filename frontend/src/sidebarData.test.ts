import { describe, expect, it } from 'vitest'
import { atLeast, reorderPagesInTree, type PageNode, type TeamSpace } from './sidebarData'

const page = (id: string): PageNode => ({ id, title: id, icon: '', kind: 'document' })
const space: TeamSpace = { id: 's', name: 'S', icon: '', type: 'private', groupId: 'g', role: 'owner', pages: [page('a'), page('b')] }

describe('reorderPagesInTree', () => {
  it('reorders siblings', () => {
    expect(reorderPagesInTree([space], 's', null, ['b', 'a'])[0].pages.map((p) => p.id)).toEqual(['b', 'a'])
  })

  it('skips ids that are not siblings instead of inserting undefined', () => {
    const pages = reorderPagesInTree([space], 's', null, ['b', 'gone', 'a'])[0].pages
    expect(pages.map((p) => p.id)).toEqual(['b', 'a'])
  })
})

describe('atLeast', () => {
  it('ranks roles like the server', () => {
    expect(atLeast('editor', 'editor')).toBe(true)
    expect(atLeast('viewer', 'editor')).toBe(false)
    expect(atLeast('owner', 'admin')).toBe(true)
    expect(atLeast(undefined, 'viewer')).toBe(false)
  })
})
