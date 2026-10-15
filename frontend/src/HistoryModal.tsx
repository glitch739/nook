import { useEffect, useState } from 'react'
import { EditorContent, useEditor } from '@tiptap/react'
import { getVersion, listVersions, type PageVersion } from './api'
import { contentExtensions, liveEditor } from './Editor'
import { relativeTime } from './PageMeta'
import { notifyError, showToast } from './toast'
import './Settings.css'
import './Archive.css'
import './Editor.css'
import './HistoryModal.css'

// A page's earlier versions (backend collab/history.go keeps one per
// editing stretch): pick one to read it, then restore it. Restoring
// replaces the open editor's content as one ordinary edit, so it syncs
// to everyone like typing does and Cmd/Ctrl+Z takes it back.
export default function HistoryModal({ pageId, onClose }: { pageId: string; onClose: () => void }) {
  const [versions, setVersions] = useState<PageVersion[] | null>(null)
  const [selected, setSelected] = useState<string | null>(null)
  const [content, setContent] = useState<{ id: string; json: unknown } | null>(null)

  useEffect(() => {
    listVersions(pageId).then(
      (list) => {
        setVersions(list)
        // The newest is the current text; the one before it is what
        // someone looking for a lost paragraph usually wants.
        setSelected(list[1]?.id ?? list[0]?.id ?? null)
      },
      (e) => {
        notifyError('Could not load the history', e)
        setVersions([])
      },
    )
  }, [pageId])

  useEffect(() => {
    if (!selected) return
    let cancelled = false
    getVersion(pageId, selected).then(
      (v) => !cancelled && setContent({ id: selected, json: JSON.parse(v.content_json) }),
      (e) => notifyError('Could not load this version', e),
    )
    return () => {
      cancelled = true
    }
  }, [pageId, selected])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const editor = liveEditor(pageId)
  const canRestore = !!editor?.isEditable && content?.id === selected && selected !== versions?.[0]?.id
  const chosen = versions?.find((v) => v.id === selected)

  function restore() {
    if (!editor || !content || !chosen) return
    editor.chain().focus().setContent(content.json as never).run()
    showToast(`Restored the version from ${new Date(chosen.updated_at).toLocaleString()}. Cmd/Ctrl+Z undoes it.`)
    onClose()
  }

  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="archive-modal history-modal" role="dialog" aria-modal="true" aria-label="Page history">
        <header className="archive-header">
          <h2>Page history</h2>
          <p className="hint">A version is saved after each stretch of editing. Pick one to read it.</p>
          <button type="button" className="settings-close" onClick={onClose} aria-label="Close">
            ×
          </button>
        </header>
        <div className="history-body">
          <ul className="history-list">
            {versions === null && <li className="hint">Loading…</li>}
            {versions?.length === 0 && <li className="hint">No versions yet: they are saved as the page is edited.</li>}
            {versions?.map((v, i) => (
              <li key={v.id}>
                <button
                  type="button"
                  className={v.id === selected ? 'history-item active' : 'history-item'}
                  onClick={() => setSelected(v.id)}
                >
                  <span className="history-when">
                    {new Date(v.updated_at).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })}
                  </span>
                  <span className="hint">
                    {i === 0 ? 'Current · ' : ''}
                    {v.user_name || 'Unknown'} · {relativeTime(v.updated_at)}
                  </span>
                </button>
              </li>
            ))}
          </ul>
          <div className="history-preview">
            {content && content.id === selected ? (
              <VersionPreview key={content.id} pageId={pageId} json={content.json} />
            ) : (
              selected && <p className="hint">Loading…</p>
            )}
          </div>
        </div>
        <footer className="history-footer">
          <span className="hint">
            {!editor?.isEditable && 'Restoring needs edit rights and the page open.'}
          </span>
          <button type="button" className="settings-primary" disabled={!canRestore} onClick={restore}>
            Restore this version
          </button>
        </footer>
      </div>
    </div>
  )
}

function VersionPreview({ pageId, json }: { pageId: string; json: unknown }) {
  const editor = useEditor({ editable: false, extensions: contentExtensions(pageId), content: json as never })
  return <EditorContent editor={editor} className="history-doc" />
}
