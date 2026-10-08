import { describe, expect, it } from 'vitest'
import { extensionOf, formatSize, isNookAttachmentUrl } from './fileAttachment'

describe('file attachments', () => {
  it('formats sizes', () => {
    expect(formatSize(512)).toBe('512 B')
    expect(formatSize(1536)).toBe('1.5 KB')
    expect(formatSize(5 * 1024 * 1024)).toBe('5.0 MB')
  })

  it('reads extensions case-insensitively', () => {
    expect(extensionOf('Számla 2026.PDF')).toBe('pdf')
    expect(extensionOf('noext')).toBe('')
    expect(extensionOf('.hidden')).toBe('')
  })

  it('only accepts links to files Nook stores', () => {
    expect(isNookAttachmentUrl('/api/pages/p1/attachments/a1')).toBe(true)
    expect(isNookAttachmentUrl('https://evil.example/x.pdf')).toBe(false)
    expect(isNookAttachmentUrl('/api/pages/p1/attachments/a1/../../x')).toBe(false)
    expect(isNookAttachmentUrl('javascript:alert(1)')).toBe(false)
  })
})
