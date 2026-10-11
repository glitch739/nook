import { useEffect, useRef, useState } from 'react'
import {
  listRows,
  setRowProperty as setRowPropertyApi,
  updateProperty as updatePropertyApi,
  type Property,
  type SelectOption,
} from './api'
import { notifyError } from './toast'
import { useLiveRefresh, useLocalWrites } from './liveEvents'
import './RowPropertiesBar.css'
import PropertyCell from './PropertyCell'

interface RowProperties {
  properties: Property[]
  values: Record<string, unknown>
}

// Shown above the editor when the open page is a database row (ADR
// 0004) but was opened as its own full-page tab from the sidebar, not
// through the table's row-detail peek. The peek already renders these
// same property cells; a row opened as a plain page previously showed
// nothing but its rich-text body, as if the database's own columns
// didn't apply to it. Fetches the parent database's whole table just
// to read this one row's own properties/values -- there's no
// dedicated "one row's property values" endpoint, and reusing listRows
// avoids adding one for what's still a small self-hosted instance.
export default function RowPropertiesBar({
  rowId,
  databasePageId,
  readOnly = false,
}: {
  rowId: string
  databasePageId: string
  readOnly?: boolean
}) {
  const [data, setData] = useState<RowProperties | null>(null)

  // Resets during render, not with a synchronous setState(null) at the
  // top of the effect below -- same "compare incoming identity to a
  // tracked copy, reset here instead" pattern PageIcon.tsx/
  // DatabaseView.tsx/PageCover.tsx already use for their own pageId
  // resets, so the effect only ever calls setState from the async
  // fetch actually resolving, not synchronously on every run.
  const trackedKey = `${rowId}:${databasePageId}`
  const [trackedRowKey, setTrackedRowKey] = useState(trackedKey)
  if (trackedKey !== trackedRowKey) {
    setTrackedRowKey(trackedKey)
    setData(null)
  }

  useEffect(() => {
    let cancelled = false
    listRows(databasePageId)
      .then((t) => {
        if (cancelled) return
        setData({ properties: t.properties, values: t.rows.find((r) => r.id === rowId)?.values ?? {} })
      })
      .catch((e) => console.error('failed to load row properties', e))
    return () => {
      cancelled = true
    }
  }, [rowId, databasePageId])

  // Another user changing one of these cells (or the columns) arrives
  // as a live "data" signal; re-fetch, but only apply it if this is
  // still the row being shown.
  const writes = useLocalWrites()
  const liveKeyRef = useRef(trackedKey)
  useEffect(() => {
    liveKeyRef.current = trackedKey
  }, [trackedKey])
  useLiveRefresh(['data'], () => {
    const key = trackedKey
    const snap = writes.snapshot()
    listRows(databasePageId)
      .then((t) => {
        if (liveKeyRef.current !== key || !writes.canApply(snap)) return
        setData({ properties: t.properties, values: t.rows.find((r) => r.id === rowId)?.values ?? {} })
      })
      .catch((e) => console.error('failed to refresh row properties', e))
  })

  function handleChange(propertyId: string, value: unknown) {
    setData((d) => (d ? { ...d, values: { ...d.values, [propertyId]: value } } : d))
    writes.track(setRowPropertyApi(rowId, propertyId, value)).catch((e) => notifyError('Could not save cell', e))
  }

  function handleOptionsChanged(propertyId: string, options: SelectOption[]) {
    setData((d) =>
      d ? { ...d, properties: d.properties.map((p) => (p.id === propertyId ? { ...p, options } : p)) } : d,
    )
    writes.track(updatePropertyApi(propertyId, { options })).catch((e) => notifyError('Could not save options', e))
  }

  if (!data || data.properties.length === 0) return null

  return (
    <div className="row-properties-bar">
      <div className="row-detail-properties">
        {data.properties.map((prop) => (
          <div className="row-detail-property" key={prop.id}>
            <span className="row-detail-property-name">{prop.name}</span>
            <PropertyCell
              property={prop}
              value={data.values[prop.id]}
              onChange={(v) => handleChange(prop.id, v)}
              onOptionsChanged={(options) => handleOptionsChanged(prop.id, options)}
              readOnly={readOnly}
            />
          </div>
        ))}
      </div>
    </div>
  )
}
