import { Extension } from '@tiptap/core'
import Suggestion from '@tiptap/suggestion'
import { ReactRenderer } from '@tiptap/react'
import { pickFiles, pickImages } from './imageUpload'
import SlashCommandMenu, { type SlashCommandItem, type SlashCommandMenuHandle } from './SlashCommandMenu'

// The block types offered by the "/" menu are exactly the ones the
// toolbar already exposes (plus the divider) (Editor.tsx) -- same commands, just a second,
// faster way to reach them without leaving the keyboard. deleteRange
// removes the typed "/query" text itself; toggle*/set* is the same
// chain the matching toolbar button runs.
const ITEMS: SlashCommandItem[] = [
  {
    title: 'Heading',
    subtitle: 'Large subheading (H2)',
    icon: 'H2',
    run: (editor, range) =>
      editor.chain().focus().deleteRange(range).toggleHeading({ level: 2 }).run(),
  },
  {
    title: 'Bulleted list',
    subtitle: 'Simple list',
    icon: '•',
    run: (editor, range) => editor.chain().focus().deleteRange(range).toggleBulletList().run(),
  },
  {
    title: 'Numbered list',
    subtitle: 'Numbered list',
    icon: '1.',
    run: (editor, range) => editor.chain().focus().deleteRange(range).toggleOrderedList().run(),
  },
  {
    title: 'To-do list',
    subtitle: 'Checkable tasks',
    icon: '☑',
    run: (editor, range) => editor.chain().focus().deleteRange(range).toggleTaskList().run(),
  },
  {
    title: 'Quote',
    subtitle: 'Highlighted quote block',
    icon: '❝',
    run: (editor, range) => editor.chain().focus().deleteRange(range).toggleBlockquote().run(),
  },
  {
    title: 'Code block',
    subtitle: 'Formatted code, language selectable',
    icon: '</>',
    run: (editor, range) => editor.chain().focus().deleteRange(range).toggleCodeBlock().run(),
  },
  {
    title: 'Table',
    subtitle: 'Simple 3×3 table with header',
    icon: '▦',
    run: (editor, range) =>
      editor.chain().focus().deleteRange(range).insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run(),
  },
  {
    title: 'Image',
    subtitle: 'Upload an image from your device (or just paste one)',
    icon: '🖼',
    run: (editor, range) => {
      editor.chain().focus().deleteRange(range).run()
      pickImages(editor)
    },
  },
  {
    title: 'File',
    subtitle: 'Attach a PDF, Word, Excel or other file (or just drop one in)',
    icon: '📎',
    run: (editor, range) => {
      editor.chain().focus().deleteRange(range).run()
      pickFiles(editor)
    },
  },
  {
    title: 'Divider',
    subtitle: 'Horizontal divider line',
    icon: '—',
    run: (editor, range) => editor.chain().focus().deleteRange(range).setHorizontalRule().run(),
  },
]

// Extension.create's generic options aren't declared here -- this
// extension has no configurable options of its own, it just registers
// the Suggestion plugin below with a fixed trigger char and item list.
export const SlashCommand = Extension.create({
  name: 'slashCommand',

  addProseMirrorPlugins() {
    return [
      Suggestion<SlashCommandItem>({
        editor: this.editor,
        char: '/',
        startOfLine: false,
        items: ({ query }) =>
          ITEMS.filter((item) => item.title.toLowerCase().includes(query.toLowerCase())),
        command: ({ editor, range, props }) => {
          props.run(editor, range)
        },
        render: () => {
          let component: ReactRenderer<SlashCommandMenuHandle> | null = null
          let unmount: (() => void) | null = null

          return {
            onStart: (props) => {
              component = new ReactRenderer(SlashCommandMenu, {
                props,
                editor: props.editor,
              })
              if (!props.clientRect) return
              unmount = props.mount(component.element as HTMLElement)
            },
            onUpdate: (props) => {
              component?.updateProps(props)
            },
            onKeyDown: (props) => {
              if (props.event.key === 'Escape') {
                unmount?.()
                unmount = null
                return true
              }
              return component?.ref?.onKeyDown(props) ?? false
            },
            onExit: () => {
              unmount?.()
              component?.destroy()
            },
          }
        },
      }),
    ]
  },
})

export default SlashCommand
