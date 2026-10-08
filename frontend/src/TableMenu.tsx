import { useLayoutEffect, useRef } from 'react'
import { createPortal } from 'react-dom'
import { useEditorState, type Editor } from '@tiptap/react'

const GAP = 8 // px between the bar and the table / sticky toolbar

// Contextual controls for a table in a page's text (not a database --
// see DatabaseView.tsx for those). Floats just above the table the
// cursor is in, so you never have to scroll back to the top of a long
// page. On a table taller than the screen it sticks under the editor's
// sticky toolbar while the table is still visible, then disappears
// with it.
//
// It subscribes to the editor itself via useEditorState: the
// surrounding CollabEditor does not re-render on every selection
// change, so reading editor.isActive('table') there would go stale.
function scrollParentTop(el: HTMLElement): number {
  for (let p = el.parentElement; p; p = p.parentElement) {
    const oy = getComputedStyle(p).overflowY
    if ((oy === 'auto' || oy === 'scroll') && p.scrollHeight > p.clientHeight) return p.getBoundingClientRect().top
  }
  return 0
}

export default function TableMenu({ editor }: { editor: Editor }) {
  const state = useEditorState({
    editor,
    selector: ({ editor: e }) => {
      let tablePos: number | null = null
      if (e.isEditable) {
        const $a = e.state.selection.$anchor
        for (let d = $a.depth; d > 0; d--) {
          if ($a.node(d).type.name === 'table') {
            tablePos = $a.before(d)
            break
          }
        }
      }
      return {
        tablePos,
        canMerge: tablePos !== null && e.can().mergeCells(),
        canSplit: tablePos !== null && e.can().splitCell(),
      }
    },
  })

  const barRef = useRef<HTMLDivElement>(null)
  const { tablePos } = state

  // Positioned with position: fixed from the table's on-screen rect,
  // rendered into <body> so no transformed/blurred ancestor can turn
  // "fixed" into "relative to me". Recomputed on scroll (any scroll
  // container), resize, and every transaction -- a collaborator's edit
  // above the table moves it without changing our selection.
  useLayoutEffect(() => {
    if (tablePos === null) return
    const update = () => {
      const bar = barRef.current
      const dom = editor.view.nodeDOM(tablePos) as HTMLElement | null
      if (!bar || !dom?.getBoundingClientRect) return
      const rect = dom.getBoundingClientRect()
      // Never above the visible part of the page: the sticky toolbar's
      // bottom if it is on screen, else the scrolling area's top edge
      // (keeps the bar off the tab bar above it).
      const toolbar = editor.view.dom.closest('.editor-shell')?.querySelector('.toolbar')
      const minTop = Math.max(toolbar?.getBoundingClientRect().bottom ?? 0, scrollParentTop(editor.view.dom)) + GAP
      const h = bar.offsetHeight
      const w = bar.offsetWidth
      let top = rect.top - h - GAP
      if (top < minTop) top = Math.min(minTop, rect.bottom - h - GAP)
      const visible = rect.bottom - GAP > minTop && top + h < window.innerHeight
      const left = Math.max(GAP, Math.min(rect.left, window.innerWidth - w - GAP))
      bar.style.top = `${Math.round(top)}px`
      bar.style.left = `${Math.round(left)}px`
      bar.style.visibility = visible ? 'visible' : 'hidden'
    }
    update()
    window.addEventListener('scroll', update, true)
    window.addEventListener('resize', update)
    editor.on('transaction', update)
    return () => {
      window.removeEventListener('scroll', update, true)
      window.removeEventListener('resize', update)
      editor.off('transaction', update)
    }
  }, [editor, tablePos])

  if (tablePos === null) return null

  // Every button keeps the editor's selection: preventing mousedown's
  // default stops the click from blurring ProseMirror first.
  const btn = (label: string, title: string, run: () => boolean, opts: { danger?: boolean; disabled?: boolean } = {}) => (
    <button
      type="button"
      title={title}
      disabled={opts.disabled}
      className={opts.danger ? 'table-menu-btn danger' : 'table-menu-btn'}
      onMouseDown={(e) => e.preventDefault()}
      onClick={run}
    >
      {label}
    </button>
  )

  const chain = () => editor.chain().focus()

  return createPortal(
    <div
      ref={barRef}
      className="table-menu"
      role="toolbar"
      aria-label="Table"
      style={{ visibility: 'hidden' }}
      onMouseDown={(e) => e.preventDefault()}
    >
      <div className="table-menu-group">
        {btn('↑ Row', 'Insert row above', () => chain().addRowBefore().run())}
        {btn('↓ Row', 'Insert row below', () => chain().addRowAfter().run())}
        {btn('✕ Row', 'Delete row', () => chain().deleteRow().run())}
      </div>
      <div className="table-menu-group">
        {btn('← Column', 'Insert column left', () => chain().addColumnBefore().run())}
        {btn('→ Column', 'Insert column right', () => chain().addColumnAfter().run())}
        {btn('✕ Column', 'Delete column', () => chain().deleteColumn().run())}
      </div>
      <div className="table-menu-group">
        {btn('Header row', 'Toggle first row as header', () => chain().toggleHeaderRow().run())}
        {btn('Merge', 'Merge selected cells', () => chain().mergeCells().run(), { disabled: !state.canMerge })}
        {btn('Split', 'Split merged cell', () => chain().splitCell().run(), { disabled: !state.canSplit })}
      </div>
      <div className="table-menu-group">
        {btn('Delete', 'Delete the whole table', () => chain().deleteTable().run(), { danger: true })}
      </div>
    </div>,
    document.body,
  )
}
