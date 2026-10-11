import { useRef, useState } from 'react'
import IconPicker from './IconPicker'
import PageIcon from './PageIcon'

// A page/space/row icon that opens IconPicker on click -- the same
// ref-captured-into-state anchor pattern Sidebar.tsx's PageTreeItem and
// TeamspaceHeader each have inline (see IconPicker.tsx's own comment on
// why this is state, not a ref read during render). Pulled out here for
// RowDetailPanel, a third caller that needed the identical behavior --
// Sidebar.tsx's two existing inline copies are left as they are rather
// than churned for this, since they're already working and tested.
export default function EditableIcon({
  icon,
  onChange,
  className = 'page-icon-editable',
  iconClassName = 'page-icon',
  readOnly = false,
}: {
  icon: string
  onChange: (icon: string) => void
  className?: string
  iconClassName?: string
  /** Shows the icon without the picker (a viewer can't change it). */
  readOnly?: boolean
}) {
  const [pickerAnchor, setPickerAnchor] = useState<HTMLSpanElement | null>(null)
  const iconRef = useRef<HTMLSpanElement>(null)

  return (
    <span
      ref={iconRef}
      className={className}
      onClick={(e) => {
        e.stopPropagation()
        if (!readOnly) setPickerAnchor(iconRef.current)
      }}
    >
      <PageIcon icon={icon} className={iconClassName} />
      {pickerAnchor && (
        <IconPicker
          icon={icon}
          anchorEl={pickerAnchor}
          onChange={onChange}
          onClose={() => setPickerAnchor(null)}
        />
      )}
    </span>
  )
}
