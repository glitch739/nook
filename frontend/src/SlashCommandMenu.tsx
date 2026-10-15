import { forwardRef, useImperativeHandle, useState, type ReactNode } from 'react'
import type { SuggestionKeyDownProps, SuggestionProps } from '@tiptap/suggestion'
import type { Editor, Range } from '@tiptap/core'

// One entry in the "/" menu -- run() gets the block-type command that
// would otherwise live on a toolbar button, plus deleteRange() to also
// remove the typed "/query" text itself before applying it.
export interface SlashCommandItem {
  title: string
  subtitle: string
  icon: ReactNode
  run: (editor: Editor, range: Range) => void
}

export interface SlashCommandMenuHandle {
  onKeyDown: (props: SuggestionKeyDownProps) => boolean
}

// The popup rendered by SlashCommand's Suggestion plugin (see
// SlashCommand.tsx). Exposes onKeyDown via a ref so the plugin's
// onKeyDown render hook can forward arrow/enter navigation into this
// component -- the standard pattern for a Tiptap Suggestion-based list,
// same shape as Tiptap's own mention-list example.
const SlashCommandMenu = forwardRef<SlashCommandMenuHandle, SuggestionProps<SlashCommandItem>>(
  function SlashCommandMenu(props, ref) {
    const [selected, setSelected] = useState(0)
    // The filtered item list changes on every keystroke -- always land
    // back on the first (usually most relevant) match rather than an
    // index that may no longer exist, or silently point at something
    // else after the list reshuffles. Adjusted here, during render,
    // rather than via an effect -- there's no external system to
    // synchronize with, just state that needs to track a prop change,
    // which an effect would only do one render late.
    const [trackedItems, setTrackedItems] = useState(props.items)
    if (props.items !== trackedItems) {
      setTrackedItems(props.items)
      setSelected(0)
    }

    useImperativeHandle(ref, () => ({
      onKeyDown: ({ event }) => {
        if (props.items.length === 0) return false
        if (event.key === 'ArrowDown') {
          setSelected((i) => (i + 1) % props.items.length)
          return true
        }
        if (event.key === 'ArrowUp') {
          setSelected((i) => (i - 1 + props.items.length) % props.items.length)
          return true
        }
        if (event.key === 'Enter') {
          const item = props.items[selected]
          if (item) props.command(item)
          return true
        }
        return false
      },
    }))

    if (props.items.length === 0) {
      return (
        <div className="slash-menu">
          <div className="slash-menu-empty">No results</div>
        </div>
      )
    }

    return (
      <div className="slash-menu">
        {props.items.map((item, i) => (
          <button
            type="button"
            key={`${i}-${item.title}`}
            className={i === selected ? 'slash-menu-item active' : 'slash-menu-item'}
            onMouseEnter={() => setSelected(i)}
            onClick={() => props.command(item)}
          >
            <span className="slash-menu-icon">{item.icon}</span>
            <span className="slash-menu-text">
              <span className="slash-menu-title">{item.title}</span>
              <span className="slash-menu-subtitle">{item.subtitle}</span>
            </span>
          </button>
        ))}
      </div>
    )
  },
)

export default SlashCommandMenu
