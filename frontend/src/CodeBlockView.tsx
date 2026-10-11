import { useEffect, useRef, useState } from 'react'
import { NodeViewContent, NodeViewWrapper, type NodeViewProps } from '@tiptap/react'
import { CODE_LANGUAGES } from './codeLanguages'

// The React node view for codeBlock (wired in via
// CodeBlockLowlight.extend({ addNodeView }) in Editor.tsx). Only reason
// this needs a custom node view at all: the language picker button and
// its dropdown have to sit *outside* the contentEditable code region
// (so clicking them doesn't get treated as typing into the block), and
// Tiptap's node views are the supported way to mix ordinary React UI
// into an otherwise-editable node like that.
export default function CodeBlockView({ node, updateAttributes, editor }: NodeViewProps) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)

  const language: string | null = (node.attrs.language as string | null) ?? null
  const current = CODE_LANGUAGES.find((l) => l.value === language) ?? {
    value: language,
    label: language ?? 'Plain text',
  }

  useEffect(() => {
    if (!open) return
    searchRef.current?.focus()

    function onClickOutside(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onClickOutside)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onClickOutside)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  const filtered = CODE_LANGUAGES.filter((l) =>
    l.label.toLowerCase().includes(query.trim().toLowerCase()),
  )

  function choose(value: string | null) {
    updateAttributes({ language: value })
    setOpen(false)
    setQuery('')
    // The picker button lives outside the editable region, so clicking
    // it moved DOM focus away from the editor -- send it back so typing
    // can continue right after picking a language, same as Notion does.
    editor.chain().focus().run()
  }

  return (
    <NodeViewWrapper className="code-block-view">
      <div className="code-block-header" contentEditable={false} ref={rootRef}>
        <button type="button" className="code-lang-btn" onClick={() => editor.isEditable && setOpen((v) => !v)}>
          {current.label}
          <span className="code-lang-chevron">▾</span>
        </button>
        {open && (
          <div className="code-lang-menu">
            <input
              ref={searchRef}
              className="code-lang-search"
              placeholder="Search languages…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <div className="code-lang-list">
              {filtered.length === 0 && (
                <div className="code-lang-empty">No results</div>
              )}
              {filtered.map((l) => (
                <button
                  type="button"
                  key={l.value ?? 'plaintext'}
                  className={
                    l.value === language ? 'code-lang-item active' : 'code-lang-item'
                  }
                  onClick={() => choose(l.value)}
                >
                  {l.label}
                </button>
              ))}
            </div>
          </div>
        )}
      </div>
      <pre>
        <NodeViewContent<'code'> as="code" />
      </pre>
    </NodeViewWrapper>
  )
}
