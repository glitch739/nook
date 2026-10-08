import { useEffect, useRef, useState, type CSSProperties } from 'react'
import { listPeople, type Person, type Property, type SelectOption } from './api'
import Popover from './Popover'
import { SELECT_COLORS } from './selectColors'
import './PropertyCell.css'

function newOptionID(): string {
  return Math.random().toString(36).slice(2, 10)
}

export function SelectPill({ option }: { option: SelectOption }) {
  // Notion-style status pill: a solid dot in the option's color, a
  // muted tint of it behind, and the normal text color on top (the
  // color itself lives in --pill-color, DatabaseView.css does the rest).
  return (
    <span className="select-pill" style={{ '--pill-color': option.color } as CSSProperties}>
      <span className="select-pill-dot" />
      <span className="select-pill-label">{option.label}</span>
    </span>
  )
}

// The native color input behind the "custom color" swatch. React's
// onChange on <input type="color"> fires on every drag movement inside
// the OS picker, which would save (one request each) continuously --
// the native `change` event fires once, when the picker is dismissed.
function CustomColorInput({ color, onPick }: { color: string; onPick: (color: string) => void }) {
  const ref = useRef<HTMLInputElement>(null)
  const onPickRef = useRef(onPick)
  useEffect(() => {
    onPickRef.current = onPick
  })
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const handler = () => onPickRef.current(el.value)
    el.addEventListener('change', handler)
    return () => el.removeEventListener('change', handler)
  }, [])
  return <input ref={ref} type="color" defaultValue={/^#[0-9a-f]{6}$/i.test(color) ? color : '#888888'} />
}

// The swatch grid (presets + custom color) shared by the status and
// person popovers. onPick's `done` is true for a preset click (close the
// grid) and false for the custom picker (it may be adjusted again).
function ColorChoices({ color, onPick }: { color: string; onPick: (color: string, done: boolean) => void }) {
  return (
    <div className="select-color-picker">
      {SELECT_COLORS.map((c) => (
        <button
          key={c}
          type="button"
          className={'select-color-choice' + (c === color ? ' is-current' : '')}
          style={{ background: c }}
          aria-label={`Color ${c}`}
          onClick={() => onPick(c, true)}
        />
      ))}
      <label className={'select-color-custom' + (SELECT_COLORS.includes(color) ? '' : ' is-current')} title="Custom color">
        <CustomColorInput color={color} onPick={(c) => onPick(c, false)} />
      </label>
    </div>
  )
}

// The popover a select/status cell opens: pick one of the column's
// options, or manage the option list itself (add/rename/recolor/
// remove) right here -- this is the "custom status editing"
// ask, and putting it on the cell rather than a separate column-header
// surface means there's exactly one place options ever get edited.
function SelectCellPopover({
  options,
  currentOptionId,
  anchorEl,
  onChoose,
  onOptionsChanged,
  onClose,
}: {
  options: SelectOption[]
  currentOptionId: string | null
  anchorEl: HTMLElement
  onChoose: (optionId: string | null) => void
  onOptionsChanged: (options: SelectOption[]) => void
  onClose: () => void
}) {
  const [editingId, setEditingId] = useState<string | null>(null)
  const [draftLabel, setDraftLabel] = useState('')
  const [newLabel, setNewLabel] = useState('')
  // Which option's color palette is expanded under its row, if any.
  const [colorPickerId, setColorPickerId] = useState<string | null>(null)

  function addOption() {
    const label = newLabel.trim()
    if (!label) return
    const color = SELECT_COLORS[options.length % SELECT_COLORS.length]
    onOptionsChanged([...options, { id: newOptionID(), label, color }])
    setNewLabel('')
  }

  function renameOption(id: string) {
    const label = draftLabel.trim()
    if (label) onOptionsChanged(options.map((o) => (o.id === id ? { ...o, label } : o)))
    setEditingId(null)
  }

  function recolorOption(id: string, color: string) {
    onOptionsChanged(options.map((o) => (o.id === id ? { ...o, color } : o)))
  }

  function removeOption(id: string) {
    onOptionsChanged(options.filter((o) => o.id !== id))
    if (currentOptionId === id) onChoose(null)
  }

  return (
    <Popover anchorEl={anchorEl} width={260} maxHeight={360} onClose={onClose} className="select-popover">
      <div className="select-popover-options">
        {options.map((option) => (
          <div className="select-option" key={option.id}>
          <div className="select-option-row">
            <button
              type="button"
              className="select-option-swatch"
              style={{ background: option.color }}
              title="Change color"
              aria-expanded={colorPickerId === option.id}
              onClick={(e) => {
                e.stopPropagation()
                setColorPickerId(colorPickerId === option.id ? null : option.id)
              }}
            />
            {editingId === option.id ? (
              <input
                className="select-option-input"
                autoFocus
                value={draftLabel}
                onChange={(e) => setDraftLabel(e.target.value)}
                onBlur={() => renameOption(option.id)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') renameOption(option.id)
                  if (e.key === 'Escape') setEditingId(null)
                }}
              />
            ) : (
              <button
                type="button"
                className="select-option-label"
                onClick={() => onChoose(currentOptionId === option.id ? null : option.id)}
              >
                {option.label}
              </button>
            )}
            <button
              type="button"
              className="select-option-edit"
              title="Rename"
              onClick={() => {
                setDraftLabel(option.label)
                setEditingId(option.id)
              }}
            >
              ✎
            </button>
            <button
              type="button"
              className="select-option-remove"
              title="Delete"
              onClick={() => removeOption(option.id)}
            >
              ×
            </button>
          </div>
          {colorPickerId === option.id && (
            <ColorChoices
              color={option.color}
              onPick={(color, done) => {
                recolorOption(option.id, color)
                if (done) setColorPickerId(null)
              }}
            />
          )}
          </div>
        ))}
      </div>
      <div className="select-popover-add">
        <input
          className="select-option-input"
          placeholder="+ New option…"
          value={newLabel}
          onChange={(e) => setNewLabel(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') addOption()
          }}
        />
      </div>
    </Popover>
  )
}

function SelectCell({
  property,
  value,
  onChange,
  onOptionsChanged,
}: {
  property: Property
  value: unknown
  onChange: (value: unknown) => void
  onOptionsChanged: (options: SelectOption[]) => void
}) {
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
  const btnRef = useRef<HTMLButtonElement>(null)
  const current = (value as { option_id: string } | null | undefined) ?? null
  const option = current ? property.options.find((o) => o.id === current.option_id) : undefined

  return (
    <>
      <button
        ref={btnRef}
        type="button"
        className="cell-select-btn"
        onClick={(e) => {
          e.stopPropagation()
          setAnchor(btnRef.current)
        }}
      >
        {option ? <SelectPill option={option} /> : <span className="cell-empty">Empty</span>}
      </button>
      {anchor && (
        <SelectCellPopover
          options={property.options}
          currentOptionId={current?.option_id ?? null}
          anchorEl={anchor}
          onChoose={(optionId) => {
            onChange(optionId ? { option_id: optionId } : null)
            setAnchor(null)
          }}
          onOptionsChanged={onOptionsChanged}
          onClose={() => setAnchor(null)}
        />
      )}
    </>
  )
}

// -- Text / date / number / checkbox / person -------------------------

function TextCell({ value, onChange }: { value: unknown; onChange: (value: unknown) => void }) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(typeof value === 'string' ? value : '')
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus()
      inputRef.current?.select()
    }
  }, [editing])

  function commit() {
    onChange(draft)
    setEditing(false)
  }

  if (editing) {
    return (
      <input
        ref={inputRef}
        className="cell-text-input"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onClick={(e) => e.stopPropagation()}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === 'Enter') commit()
          if (e.key === 'Escape') setEditing(false)
        }}
      />
    )
  }
  return (
    <button
      type="button"
      className="cell-text-btn"
      onClick={(e) => {
        e.stopPropagation()
        setDraft(typeof value === 'string' ? value : '')
        setEditing(true)
      }}
    >
      {typeof value === 'string' && value ? value : <span className="cell-empty">Empty</span>}
    </button>
  )
}

function DateCell({ value, onChange }: { value: unknown; onChange: (value: unknown) => void }) {
  return (
    <input
      type="date"
      className="cell-date-input"
      value={typeof value === 'string' ? value : ''}
      onClick={(e) => e.stopPropagation()}
      onChange={(e) => onChange(e.target.value || null)}
    />
  )
}

function NumberCell({ value, onChange }: { value: unknown; onChange: (value: unknown) => void }) {
  // Resyncs the draft text whenever the external value changes (e.g.
  // after a commit round-trips through the parent) -- tracked during
  // render rather than via an effect, same reasoning as PageIcon.tsx's
  // identical pattern.
  const [trackedValue, setTrackedValue] = useState(value)
  const [draft, setDraft] = useState(typeof value === 'number' ? String(value) : '')
  if (value !== trackedValue) {
    setTrackedValue(value)
    setDraft(typeof value === 'number' ? String(value) : '')
  }

  function commit() {
    const n = draft.trim() === '' ? null : Number(draft)
    onChange(Number.isFinite(n) ? n : null)
  }

  return (
    <input
      type="number"
      className="cell-number-input"
      value={draft}
      onClick={(e) => e.stopPropagation()}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') commit()
      }}
    />
  )
}

function CheckboxCell({ value, onChange }: { value: unknown; onChange: (value: unknown) => void }) {
  return (
    <input
      type="checkbox"
      className="cell-checkbox-input"
      checked={value === true}
      onClick={(e) => e.stopPropagation()}
      onChange={(e) => onChange(e.target.checked)}
    />
  )
}

// -- Person ---------------------------------------------------------------
// Value: {user_id} of someone who can reach the database's space (the
// server checks). The column's options double as the person's color and
// last known name, exactly like a status option -- so a pill still reads
// right after that person leaves the group. Older cells may hold a plain
// name string from before 2026-10-03; it shows as a gray pill until
// someone picks a real person.

const peopleCache = new Map<string, { at: number; list: Promise<Person[]> }>()

function loadPeople(databasePageId: string): Promise<Person[]> {
  const hit = peopleCache.get(databasePageId)
  if (hit && Date.now() - hit.at < 60_000) return hit.list
  const list = listPeople(databasePageId)
  peopleCache.set(databasePageId, { at: Date.now(), list })
  list.catch(() => peopleCache.delete(databasePageId))
  return list
}

function defaultPersonColor(userId: string): string {
  let h = 0
  for (const ch of userId) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return SELECT_COLORS[h % SELECT_COLORS.length]
}

const LEGACY_COLOR = '#94a3b8'

function PersonPopover({
  property,
  currentUserId,
  anchorEl,
  onChoose,
  onOptionsChanged,
  onClose,
}: {
  property: Property
  currentUserId: string | null
  anchorEl: HTMLElement
  onChoose: (userId: string | null) => void
  onOptionsChanged: (options: SelectOption[]) => void
  onClose: () => void
}) {
  const [people, setPeople] = useState<Person[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [filter, setFilter] = useState('')
  const [colorPickerId, setColorPickerId] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    loadPeople(property.page_id).then(
      (l) => !cancelled && setPeople(l),
      (e) => !cancelled && setError(e instanceof Error ? e.message : 'Could not load people'),
    )
    return () => {
      cancelled = true
    }
  }, [property.page_id])

  const optionFor = (p: Person): SelectOption =>
    property.options.find((o) => o.id === p.id) ?? { id: p.id, label: p.display_name || p.username, color: defaultPersonColor(p.id) }

  // Keeps the column's options in step for this person (color + current
  // name); returns whether anything changed.
  function upsert(p: Person, color?: string): SelectOption[] | null {
    const base = optionFor(p)
    const next: SelectOption = { id: p.id, label: p.display_name || p.username, color: color ?? base.color }
    const existing = property.options.find((o) => o.id === p.id)
    if (existing && existing.label === next.label && existing.color === next.color) return null
    return existing ? property.options.map((o) => (o.id === p.id ? next : o)) : [...property.options, next]
  }

  function choose(p: Person) {
    const opts = upsert(p)
    if (opts) onOptionsChanged(opts)
    onChoose(currentUserId === p.id ? null : p.id)
  }

  const q = filter.trim().toLowerCase()
  const shown = (people ?? []).filter((p) => !q || `${p.display_name} ${p.username}`.toLowerCase().includes(q))

  return (
    <Popover anchorEl={anchorEl} width={260} maxHeight={380} onClose={onClose} className="select-popover">
      {(people?.length ?? 0) > 6 && (
        <div className="select-popover-add person-filter">
          <input
            className="select-option-input"
            autoFocus
            placeholder="Search people…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        </div>
      )}
      <div className="select-popover-options">
        {error && <p className="cell-empty person-note">{error}</p>}
        {!people && !error && <p className="cell-empty person-note">Loading…</p>}
        {people && shown.length === 0 && <p className="cell-empty person-note">No one matches.</p>}
        {shown.map((p) => {
          const opt = optionFor(p)
          return (
            <div className="select-option" key={p.id}>
              <div className="select-option-row">
                <button
                  type="button"
                  className="select-option-swatch"
                  style={{ background: opt.color }}
                  title="Change color"
                  aria-expanded={colorPickerId === p.id}
                  onClick={(e) => {
                    e.stopPropagation()
                    setColorPickerId(colorPickerId === p.id ? null : p.id)
                  }}
                />
                <button
                  type="button"
                  className={'select-option-label' + (currentUserId === p.id ? ' is-chosen' : '')}
                  onClick={() => choose(p)}
                >
                  {opt.label}
                  {p.display_name && p.display_name !== p.username && <span className="person-username"> @{p.username}</span>}
                </button>
              </div>
              {colorPickerId === p.id && (
                <ColorChoices
                  color={opt.color}
                  onPick={(color, done) => {
                    const opts = upsert(p, color)
                    if (opts) onOptionsChanged(opts)
                    if (done) setColorPickerId(null)
                  }}
                />
              )}
            </div>
          )
        })}
      </div>
      {currentUserId && (
        <div className="select-popover-add">
          <button type="button" className="select-option-label" onClick={() => onChoose(null)}>
            Clear
          </button>
        </div>
      )}
    </Popover>
  )
}

function PersonCell({
  property,
  value,
  onChange,
  onOptionsChanged,
}: {
  property: Property
  value: unknown
  onChange: (value: unknown) => void
  onOptionsChanged: (options: SelectOption[]) => void
}) {
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
  const btnRef = useRef<HTMLButtonElement>(null)
  const userId =
    value && typeof value === 'object' && typeof (value as { user_id?: unknown }).user_id === 'string'
      ? (value as { user_id: string }).user_id
      : null
  const legacyName = typeof value === 'string' && value ? value : null
  const option = userId
    ? (property.options.find((o) => o.id === userId) ?? { id: userId, label: 'Unknown person', color: LEGACY_COLOR })
    : legacyName
      ? { id: 'legacy', label: legacyName, color: LEGACY_COLOR }
      : null

  return (
    <>
      <button
        ref={btnRef}
        type="button"
        className="cell-select-btn"
        onClick={(e) => {
          e.stopPropagation()
          setAnchor(btnRef.current)
        }}
      >
        {option ? <SelectPill option={option} /> : <span className="cell-empty">Empty</span>}
      </button>
      {anchor && (
        <PersonPopover
          property={property}
          currentUserId={userId}
          anchorEl={anchor}
          onChoose={(id) => {
            onChange(id ? { user_id: id } : null)
            setAnchor(null)
          }}
          onOptionsChanged={onOptionsChanged}
          onClose={() => setAnchor(null)}
        />
      )}
    </>
  )
}

// -- Dispatcher ---------------------------------------------------------

export default function PropertyCell({
  property,
  value,
  onChange,
  onOptionsChanged,
}: {
  property: Property
  value: unknown
  onChange: (value: unknown) => void
  onOptionsChanged: (options: SelectOption[]) => void
}) {
  switch (property.type) {
    case 'select':
      return (
        <SelectCell property={property} value={value} onChange={onChange} onOptionsChanged={onOptionsChanged} />
      )
    case 'date':
      return <DateCell value={value} onChange={onChange} />
    case 'number':
      return <NumberCell value={value} onChange={onChange} />
    case 'checkbox':
      return <CheckboxCell value={value} onChange={onChange} />
    case 'person':
      return (
        <PersonCell property={property} value={value} onChange={onChange} onOptionsChanged={onOptionsChanged} />
      )
    case 'text':
    default:
      return <TextCell value={value} onChange={onChange} />
  }
}
