import { describe, expect, it } from 'vitest'
import { loadOfflineEdits, saveOfflineEdits } from './offlineEdits'

// Private mode / old browsers have no IndexedDB: the store must be a
// silent no-op there, never a thrown error that breaks the editor.
describe('offline edits without IndexedDB', () => {
  it('loads nothing and saves without throwing', async () => {
    expect(typeof indexedDB).toBe('undefined')
    expect(() => saveOfflineEdits('u:p', new Uint8Array([1]))).not.toThrow()
    await expect(loadOfflineEdits('u:p')).resolves.toBeUndefined()
  })
})
