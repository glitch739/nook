import { Node, mergeAttributes, type Editor } from '@tiptap/core'
import { NodeViewWrapper, ReactNodeViewRenderer, ReactRenderer, type NodeViewProps } from '@tiptap/react'
import Suggestion from '@tiptap/suggestion'
import { PluginKey } from '@tiptap/pm/state'
import PageIcon from './PageIcon'
import SlashCommandMenu, { type SlashCommandItem, type SlashCommandMenuHandle } from './SlashCommandMenu'
import { openLinkedPage, searchPages, usePageNode } from './pageDirectory'

// A link to another page inside the text: "@" or "[[" opens a page
// picker, the chosen page becomes an inline pageMention node. It shows
// the page's current name and icon from the sidebar tree (a rename
// follows everywhere), and a click opens it. label is the name when
// linked: what the Markdown export writes, and the fallback for a page
// this reader can't see. The server reads the ids out of the document
// for the "Linked from" list (backend collab/history.go).

function MentionView({ node }: NodeViewProps) {
  const id = node.attrs.id as string
  const page = usePageNode(id)
  const title = page ? page.title || 'Untitled' : (node.attrs.label as string) || 'Untitled'
  return (
    <NodeViewWrapper
      as="span"
      className={page ? 'page-mention' : 'page-mention page-mention-missing'}
      data-page-mention={id}
      title={page ? `Open “${title}”` : 'This page was removed or is not shared with you'}
      onClick={() => page && openLinkedPage(id)}
    >
      {page && <PageIcon icon={page.icon} className="page-mention-icon" />}
      <span className="page-mention-title">{title}</span>
    </NodeViewWrapper>
  )
}

// One Suggestion plugin per trigger; each needs its own key, and none
// may use the default one, which the "/" menu (SlashCommand.ts) holds.
function picker(editor: Editor, char: string) {
  return Suggestion<SlashCommandItem>({
    editor,
    char,
    pluginKey: new PluginKey(`pageMention${char}`),
    // Page names have spaces; the picker stays open across them only
    // while some page still matches, so "@" in ordinary text lets go.
    allowSpaces: true,
    allow: ({ state, range }) => {
      const query = state.doc.textBetween(range.from, range.to).slice(char.length)
      return !query.includes(' ') || searchPages(query, 1).length > 0
    },
    items: ({ query }) =>
      searchPages(query).map(({ node, space }) => ({
        title: node.title || 'Untitled',
        subtitle: space,
        icon: <PageIcon icon={node.icon} />,
        run: (ed, range) =>
          ed
            .chain()
            .focus()
            .deleteRange(range)
            .insertContent([
              { type: 'pageMention', attrs: { id: node.id, label: node.title || 'Untitled' } },
              { type: 'text', text: ' ' },
            ])
            .run(),
      })),
    command: ({ editor: ed, range, props }) => props.run(ed, range),
    render: () => {
      let component: ReactRenderer<SlashCommandMenuHandle> | null = null
      let unmount: (() => void) | null = null
      return {
        onStart: (props) => {
          component = new ReactRenderer(SlashCommandMenu, { props, editor: props.editor })
          if (!props.clientRect) return
          unmount = props.mount(component.element as HTMLElement)
        },
        onUpdate: (props) => component?.updateProps(props),
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
  })
}

export const PageMention = Node.create({
  name: 'pageMention',
  group: 'inline',
  inline: true,
  atom: true,
  selectable: true,

  addAttributes() {
    return {
      // Written by renderHTML below, not as plain HTML attributes (an
      // id="" would clash with element ids on the page).
      id: { default: null, parseHTML: (el) => el.getAttribute('data-page-mention'), renderHTML: () => ({}) },
      label: { default: '', parseHTML: (el) => el.textContent ?? '', renderHTML: () => ({}) },
    }
  },

  parseHTML() {
    return [{ tag: 'span[data-page-mention]' }]
  },

  renderHTML({ node, HTMLAttributes }) {
    return [
      'span',
      mergeAttributes(HTMLAttributes, { 'data-page-mention': node.attrs.id, class: 'page-mention' }),
      node.attrs.label || 'Untitled',
    ]
  },

  renderText({ node }) {
    return `@${node.attrs.label || 'Untitled'}`
  },

  addNodeView() {
    return ReactNodeViewRenderer(MentionView, { as: 'span' })
  },

  addProseMirrorPlugins() {
    return [picker(this.editor, '@'), picker(this.editor, '[[')]
  },
})

export default PageMention
