import { lazy, useState, type ComponentType } from 'react'

// A lazy() component that can be preloaded -- and, once loaded, renders
// without suspending at all.
//
// Plain React.lazy suspends on its first render even when the module is
// already in memory (it learns the import resolved only a microtask
// later), and React then keeps the Suspense fallback up for at least
// 300 ms. Measured here: preloading the editor chunk alone still left
// "Loading…" on screen for exactly 300 ms on the first page open. With
// this wrapper a preloaded component renders immediately.
//
// Each mount keeps the component type it started with, so a mount that
// began on the lazy path is never swapped (and remounted) mid-use.
export function preloadable<P extends object>(load: () => Promise<{ default: ComponentType<P> }>) {
  let loaded: ComponentType<P> | null = null
  const loadAndRemember = () =>
    load().then((m) => {
      loaded = m.default
      return m
    })
  const Lazy = lazy(loadAndRemember)

  function Preloadable(props: P) {
    const [Component] = useState<ComponentType<P>>(() => loaded ?? Lazy)
    return <Component {...props} />
  }

  return {
    Component: Preloadable,
    /** Starts loading in the background; failures are left for the
     * lazy path to surface on first real use. */
    preload: () => {
      loadAndRemember().catch(() => {})
    },
  }
}
