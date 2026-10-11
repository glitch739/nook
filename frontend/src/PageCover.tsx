import { useEffect, useRef, useState } from 'react'
import {
  deletePageCover,
  getPageCoverMeta,
  pageCoverUrl,
  setPageCoverPosition,
  uploadPageCover,
} from './api'
import { notifyError } from './toast'
import './PageCover.css'
import { startPointerDrag } from './pointerDrag'

const ACCEPT = 'image/png,image/jpeg,image/webp,image/gif'

// A page's cover banner (ADR 0004's deliberately-last piece), including
// Notion-style drag-to-reposition: which vertical slice of the image
// shows through the fixed-height crop window (object-position, backed
// by internal/assets' position_y). Shown above both Editor and
// DatabaseView in App.tsx, for either kind of page.
// readOnly: a viewer sees the cover but none of the controls.
export default function PageCover({ pageId, readOnly = false }: { pageId: string; readOnly?: boolean }) {
  const [status, setStatus] = useState<'checking' | 'present' | 'absent'>('checking')
  const [positionY, setPositionY] = useState(50)
  // The cover URL is stable per page (/api/pages/{id}/cover, not an
  // asset-id-based path) -- replacing an *existing* cover changes the
  // server-side file but not that URL string, so neither React (same
  // src prop, no re-render of the <img>) nor the browser's own HTTP
  // cache (same URL) would ever notice a "Replace" actually happened.
  // This counter, appended as a query param, is what forces both to
  // treat a replacement as a genuinely new image to fetch.
  const [version, setVersion] = useState(0)
  const [uploading, setUploading] = useState(false)
  const [repositioning, setRepositioning] = useState(false)
  const [draftPositionY, setDraftPositionY] = useState(50)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const imgWrapRef = useRef<HTMLDivElement>(null)

  // Resets to "checking" (re-probing the new page's own cover) as soon
  // as pageId changes, during render -- same pattern as
  // DatabaseView.tsx's identical tracked-prop reset.
  const [trackedPageId, setTrackedPageId] = useState(pageId)
  if (pageId !== trackedPageId) {
    setTrackedPageId(pageId)
    setStatus('checking')
    setRepositioning(false)
  }

  useEffect(() => {
    let cancelled = false
    getPageCoverMeta(pageId)
      .then((meta) => {
        if (cancelled) return
        if (meta) {
          setPositionY(meta.position_y)
          setStatus('present')
        } else {
          setStatus('absent')
        }
      })
      .catch((e) => {
        console.error('failed to load cover metadata', e)
        if (!cancelled) setStatus('absent')
      })
    return () => {
      cancelled = true
    }
  }, [pageId])

  async function handleFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (!file) return
    setUploading(true)
    try {
      const meta = await uploadPageCover(pageId, file)
      setPositionY(meta.position_y)
      setVersion((v) => v + 1)
      setStatus('present')
      setRepositioning(false)
    } catch (err) {
      notifyError('Could not upload cover', err)
    } finally {
      setUploading(false)
    }
  }

  async function handleRemove() {
    try {
      await deletePageCover(pageId)
      setStatus('absent')
      setRepositioning(false)
    } catch (err) {
      notifyError('Could not remove cover', err)
    }
  }

  function startReposition() {
    setDraftPositionY(positionY)
    setRepositioning(true)
  }

  async function saveReposition() {
    const next = draftPositionY
    setRepositioning(false)
    setPositionY(next)
    try {
      await setPageCoverPosition(pageId, next)
    } catch (err) {
      notifyError('Could not save cover position', err)
    }
  }

  // Dragging the image down should reveal more of its *top* (the part
  // that was cropped off above) -- position_y 0 means "align the
  // image's top edge with the frame's top edge", so moving the pointer
  // down (positive deltaPx) needs to *decrease* position_y, not
  // increase it.
  function handlePointerDown(e: React.PointerEvent<HTMLDivElement>) {
    if (!repositioning) return
    const wrap = imgWrapRef.current
    if (!wrap) return
    const height = wrap.getBoundingClientRect().height
    const startPosition = draftPositionY
    startPointerDrag(e, {
      onMove: (_dx, dy) => setDraftPositionY(Math.min(100, Math.max(0, startPosition - (dy / height) * 100))),
    })
  }

  const shownPositionY = repositioning ? draftPositionY : positionY

  return (
    <div className={status === 'present' ? 'page-cover-area has-cover' : 'page-cover-area'}>
      {status === 'present' && (
        <div
          ref={imgWrapRef}
          className={repositioning ? 'page-cover-imgwrap repositioning' : 'page-cover-imgwrap'}
          onPointerDown={handlePointerDown}
        >
          <img
            className="page-cover-img"
            src={`${pageCoverUrl(pageId)}?v=${version}`}
            alt=""
            draggable={false}
            style={{ objectPosition: `center ${shownPositionY}%` }}
          />
        </div>
      )}

      {status === 'absent' && !readOnly && (
        <button
          type="button"
          className="page-cover-add"
          onClick={() => fileInputRef.current?.click()}
          disabled={uploading}
        >
          {uploading ? 'Uploading…' : '+ Add cover image'}
        </button>
      )}

      {status === 'present' && !repositioning && !readOnly && (
        <div className="page-cover-actions">
          <button type="button" className="page-cover-btn" onClick={() => fileInputRef.current?.click()}>
            Replace
          </button>
          <button type="button" className="page-cover-btn" onClick={startReposition}>
            Reposition
          </button>
          <button type="button" className="page-cover-btn" onClick={handleRemove}>
            Remove
          </button>
        </div>
      )}

      {repositioning && (
        <div className="page-cover-reposition-actions">
          <button type="button" className="page-cover-btn" onClick={() => setRepositioning(false)}>
            Cancel
          </button>
          <button type="button" className="page-cover-btn page-cover-btn-primary" onClick={saveReposition}>
            Save
          </button>
        </div>
      )}

      <input ref={fileInputRef} type="file" accept={ACCEPT} onChange={handleFileChange} hidden />
    </div>
  )
}
