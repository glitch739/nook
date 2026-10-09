import { useEffect, useRef, useState } from 'react'
import EditableIcon from './EditableIcon'
import './PageTitle.css'

// The open page's icon and name, above the editor toolbar (or the
// database/collection view). The name is edited in place: it saves on
// Enter or when the field loses focus, Escape puts the saved name back.
// This is also the only rename path for pages that have no sidebar row,
// such as the documents and databases inside a collection.
export default function PageTitle({
  title,
  icon,
  autoFocus,
  onAutoFocused,
  onRename,
  onChangeIcon,
}: {
  title: string
  icon: string
  /** Focus the empty name right away (a page that was just created). */
  autoFocus?: boolean
  onAutoFocused?: () => void
  onRename: (title: string) => void
  onChangeIcon: (icon: string) => void
}) {
  // null while not editing: the field then shows the saved name, so a
  // rename from elsewhere (the sidebar, another user) appears here too.
  const [draft, setDraft] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  // Escape blurs the field without saving; blur's handler still holds
  // the typed draft at that moment, so it checks this flag.
  const cancelled = useRef(false)

  useEffect(() => {
    if (!autoFocus) return
    inputRef.current?.focus()
    onAutoFocused?.()
    // Once, on mount: a later re-render must not pull the focus back.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function commit() {
    if (cancelled.current) {
      cancelled.current = false
      setDraft(null)
      return
    }
    if (draft === null) return
    const trimmed = draft.trim()
    setDraft(null)
    if (trimmed !== title.trim()) onRename(trimmed)
  }

  return (
    <div className="page-title-bar">
      <EditableIcon icon={icon} onChange={onChangeIcon} className="page-title-icon" iconClassName="page-title-glyph" />
      <input
        ref={inputRef}
        className="page-title-field"
        value={draft ?? title}
        placeholder="Untitled"
        aria-label="Page name"
        spellCheck={false}
        onFocus={() => setDraft(title)}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            e.currentTarget.blur()
          } else if (e.key === 'Escape') {
            e.preventDefault()
            cancelled.current = true
            e.currentTarget.blur()
          }
        }}
      />
    </div>
  )
}
