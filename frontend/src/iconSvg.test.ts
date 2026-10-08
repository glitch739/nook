// Regression tests for the 2026-10-02 sidebar-icon bug: every lucide
// file starts with a license comment before <svg, and a too-strict check
// rejected them all, so every icon fell back to the default.
import heartSvg from 'lucide-static/icons/heart.svg?raw'
import { afterEach, describe, expect, it, vi } from 'vitest'

async function freshModule() {
  vi.resetModules() // the module caches icons; each test starts clean
  return import('./iconSvg')
}

function mockFetch(impl: (url: string) => Response | Promise<Response>) {
  const fn = vi.fn((url: string) => Promise.resolve(impl(url)))
  vi.stubGlobal('fetch', fn)
  return fn
}

afterEach(() => vi.unstubAllGlobals())

describe('loadIconSvg', () => {
  it('accepts a real lucide file, license comment and all', async () => {
    expect(heartSvg.trimStart().startsWith('<!--')).toBe(true) // the trap
    mockFetch(() => new Response(heartSvg))
    const { loadIconSvg, cachedIconSvg } = await freshModule()
    const svg = await loadIconSvg('heart')
    expect(svg).toContain('<svg')
    expect(cachedIconSvg('heart')).toBe(svg)
  })

  it('fetches from the versioned asset path', async () => {
    const fetch = mockFetch(() => new Response(heartSvg))
    const { loadIconSvg } = await freshModule()
    await loadIconSvg('heart')
    expect(fetch.mock.calls[0][0]).toMatch(/^\/assets\/lucide-[\d.]+\/heart\.svg$/)
  })

  it('returns null for a name lucide does not have', async () => {
    mockFetch(() => new Response('not found', { status: 404 }))
    const { loadIconSvg } = await freshModule()
    expect(await loadIconSvg('no-such-icon')).toBeNull()
  })

  it('returns null for an HTML page instead of an icon', async () => {
    mockFetch(() => new Response('<!doctype html><html><body>oops</body></html>'))
    const { loadIconSvg } = await freshModule()
    expect(await loadIconSvg('heart')).toBeNull()
  })

  it('never fetches a path-like or odd name', async () => {
    const fetch = mockFetch(() => new Response(heartSvg))
    const { loadIconSvg } = await freshModule()
    for (const bad of ['../secret', 'Heart', 'a/b', '', 'x y']) {
      expect(await loadIconSvg(bad)).toBeNull()
    }
    expect(fetch).not.toHaveBeenCalled()
  })

  it('shares one request between concurrent loads of the same icon', async () => {
    const fetch = mockFetch(() => new Response(heartSvg))
    const { loadIconSvg } = await freshModule()
    await Promise.all([loadIconSvg('heart'), loadIconSvg('heart'), loadIconSvg('heart')])
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})
