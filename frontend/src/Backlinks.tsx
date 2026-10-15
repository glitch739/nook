import { useEffect, useState } from 'react'
import { listBacklinks } from './api'
import { useLiveRefresh } from './liveEvents'
import PageIcon from './PageIcon'
import './Backlinks.css'

type Link = { id: string; title: string; icon: string }

// "Linked from": the pages whose text links to this one (PageMention.tsx),
// as the server collected them from their saved versions. Hidden while
// there are none. Re-read when pages change anywhere.
export default function Backlinks({ pageId, onOpenPage }: { pageId: string; onOpenPage: (id: string) => void }) {
  const [links, setLinks] = useState<Link[]>([])
  const [reload, setReload] = useState(0)
  useLiveRefresh(['tree'], () => setReload((n) => n + 1))

  useEffect(() => {
    let cancelled = false
    listBacklinks(pageId).then(
      (list) => !cancelled && setLinks(list),
      () => {
        // Not worth a toast: the page itself works without the list.
      },
    )
    return () => {
      cancelled = true
    }
  }, [pageId, reload])

  if (!links.length) return null
  return (
    <section className="backlinks" aria-label="Linked from">
      <h3>Linked from</h3>
      <ul>
        {links.map((l) => (
          <li key={l.id}>
            <button type="button" onClick={() => onOpenPage(l.id)}>
              <PageIcon icon={l.icon} className="page-icon" />
              <span>{l.title || 'Untitled'}</span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  )
}
