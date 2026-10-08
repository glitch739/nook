// Values and modules provided at build time by vite.config.ts.

/** Base URL of the lucide-static icon files, e.g. "/assets/lucide-1.43.0/". */
declare const __LUCIDE_BASE__: string

declare module 'virtual:lucide-names' {
  /** Every lucide-static icon name, sorted. */
  export const LUCIDE_NAMES: string[]
}

// @dragdroptouch/drag-drop-touch ships no type declarations; this is
// the one function main.tsx uses (options are the package's own names).
declare module '@dragdroptouch/drag-drop-touch' {
  export function enableDragDropTouch(
    dragRoot?: Document | Element,
    dropRoot?: Document | Element,
    options?: {
      isPressHoldMode?: boolean
      pressHoldDelayMS?: number
      pressHoldMargin?: number
      allowDragScroll?: boolean
    },
  ): void
}
