import { useEffect, useState, useSyncExternalStore, type ReactNode } from 'react'
import { EditorContent, ReactNodeViewRenderer, useEditor, type Editor as TiptapEditor } from '@tiptap/react'
import StarterKit from '@tiptap/starter-kit'
import TaskList from '@tiptap/extension-task-list'
import TaskItem from '@tiptap/extension-task-item'
import Placeholder from '@tiptap/extension-placeholder'
import CodeBlockLowlight from '@tiptap/extension-code-block-lowlight'
import { createLowlight, common } from 'lowlight'
import Collaboration from '@tiptap/extension-collaboration'
import CollaborationCaret from '@tiptap/extension-collaboration-caret'
import { TableKit } from '@tiptap/extension-table'
import CodeBlockView from './CodeBlockView'
import { acquireProvider, getCollabUser, type CollabProvider } from './collabProvider'
import SlashCommand from './SlashCommand'
import TableMenu from './TableMenu'
import { ImageUpload, NookImage, pickFiles, pickImages } from './imageUpload'
import { FileAttachment } from './fileAttachment'
import PageMention from './PageMention'
import './Editor.css'

// `common` is lowlight's curated ~35-language bundle (see
// codeLanguages.ts for the matching display list shown in the code
// block's language picker) -- covers everything the picker offers
// without pulling in every highlight.js grammar that exists.
const lowlight = createLowlight(common)

const CodeBlock = CodeBlockLowlight.extend({
  // A plain CodeBlockLowlight renders its own <pre><code> with no room
  // for the language-picker button (CodeBlockView.tsx) -- that button
  // has to live outside the contentEditable code region so clicking it
  // isn't treated as typing, which is exactly what a Tiptap node view
  // is for.
  addNodeView() {
    return ReactNodeViewRenderer(CodeBlockView)
  },
})

// What a document can hold: shared by the live editor below and the
// read-only preview of an older version (HistoryModal.tsx), so both
// read the same schema.
export function contentExtensions(pageId: string) {
  return [
    // codeBlock: false -- CodeBlock above (CodeBlockLowlight + the
    // language-picker node view) replaces StarterKit's plain one.
    // undoRedo: false -- Collaboration brings its own Yjs-aware undo
    // (only undoes *your* changes, never a collaborator's).
    StarterKit.configure({ codeBlock: false, undoRedo: false }),
    CodeBlock.configure({ lowlight }),
    TaskList,
    TaskItem.configure({ nested: true }),
    // Simple tables inside a page's text (not databases): header row,
    // add/remove rows and columns, merge/split, drag to resize
    // columns. Tab moves to the next cell and adds a row at the end.
    TableKit.configure({ table: { resizable: true } }),
    // Images: pasted/dropped files are uploaded (imageUpload.ts) and
    // the document only stores their URL.
    NookImage,
    FileAttachment,
    ImageUpload.configure({ pageId }),
    PageMention,
  ]
}

// The open editors by page, so the history window can restore a version
// through the live one (an ordinary, synced and undoable edit).
const liveEditors = new Map<string, TiptapEditor>()
export const liveEditor = (pageId: string) => liveEditors.get(pageId)

// A per-block drag handle + "turn into" menu (click a handle, retype the
// block) was tried and pulled back out (see DEVLOG) -- it only ever
// positioned one handle per *top-level* ProseMirror node, so a whole
// bullet/task list got exactly one handle for the entire list, not one
// per item, which isn't what "drag to reorder blocks" means in a
// Notion-like editor. Getting that right needs each list item (and
// similar nested content) individually addressable, which is a real
// editor-architecture piece, not a CSS/positioning fix -- worth doing
// properly, hands-on, now that there's real page content to test it
// against (see internal/pages) rather than iterating blind against
// screenshots.

function ToolbarButton({
  onClick,
  active,
  disabled,
  children,
}: {
  onClick: () => void
  active?: boolean
  disabled?: boolean
  children: ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-pressed={active}
      className={active ? 'toolbar-btn active' : 'toolbar-btn'}
    >
      {children}
    </button>
  )
}

// Editor is a page's live, collaborative rich-text editor (ADR 0006):
// the content lives in a shared Yjs doc synced by collabProvider, not in
// React state -- there's no save call here at all. initialContent (the
// page's content_json) is only used if this page has never been opened
// in the collaborative editor before, to seed the shared doc.
export default function Editor({ pageId, initialContent }: { pageId: string; initialContent: string }) {
  // Acquired in an effect (not during render) so StrictMode's
  // mount/unmount/mount dance can't leak or prematurely close it.
  const [provider, setProvider] = useState<CollabProvider | null>(null)
  useEffect(() => {
    const p = acquireProvider(pageId, initialContent)
    setProvider(p)
    return () => p.release()
    // initialContent only matters on first-ever open; not a dependency.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pageId])

  if (!provider || provider.pageId !== pageId) return null
  return <CollabEditor key={pageId} provider={provider} />
}

function useProviderState(p: CollabProvider) {
  return useSyncExternalStore(p.subscribe, () => `${p.status}|${p.ready}|${p.readonly}|${p.hasUnsaved}`)
}

interface Peer {
  clientId: number
  name: string
  color: string
}

function usePeers(p: CollabProvider): Peer[] {
  const [peers, setPeers] = useState<Peer[]>([])
  useEffect(() => {
    const update = () => {
      const list: Peer[] = []
      p.awareness.getStates().forEach((state, clientId) => {
        const u = state.user as Partial<Peer> | undefined
        if (clientId !== p.doc.clientID && u)
          list.push({ clientId, name: typeof u.name === 'string' && u.name ? u.name : 'Anonymous', color: typeof u.color === 'string' && u.color ? u.color : '#888' })
      })
      setPeers(list)
    }
    p.awareness.on('change', update)
    update()
    return () => p.awareness.off('change', update)
  }, [p])
  return peers
}

function CollabEditor({ provider }: { provider: CollabProvider }) {
  useProviderState(provider)
  const peers = usePeers(provider)
  const editable = provider.ready && !provider.readonly

  const editor = useEditor({
    editable: false,
    extensions: [
      ...contentExtensions(provider.pageId),
      SlashCommand,
      // An empty paragraph renders with zero visible content otherwise
      // -- indistinguishable from "no block is here at all", which is
      // exactly what made a mostly-empty document look broken/unusable.
      // Notion shows the same kind of hint text for the same reason.
      Placeholder.configure({
        placeholder: 'Write something, or press "/" for commands…',
      }),
      Collaboration.configure({ document: provider.doc, fragment: provider.fragment }),
      // user must be passed here: the extension otherwise broadcasts its
      // own default ({name: null}) over the one collabProvider set.
      CollaborationCaret.configure({ provider, user: getCollabUser() }),
    ],
  })

  useEffect(() => {
    if (editor) provider.setSchema(editor.schema)
  }, [editor, provider])

  useEffect(() => {
    if (!editor) return
    liveEditors.set(provider.pageId, editor)
    return () => {
      if (liveEditors.get(provider.pageId) === editor) liveEditors.delete(provider.pageId)
    }
  }, [editor, provider])

  useEffect(() => {
    if (editor && editor.isEditable !== editable) editor.setEditable(editable)
  }, [editor, editable])

  // Ticking a to-do checkbox must not focus the editor: on a phone that
  // pops up the keyboard just to tick a box. Tiptap's own TaskItem
  // handler focuses the editor before toggling, so this one catches the
  // change first (capture phase on the editor root, which runs before
  // the checkbox's own listener), toggles the item with a plain
  // transaction (still synced to collaborators like any edit) and stops
  // the event there. If the tap still left the editor focused when it
  // wasn't before, that focus is dropped again.
  useEffect(() => {
    if (!editor) return
    const root = editor.view.dom
    let focusedBefore = false

    // Tiptap's task item <li> has no marker of its own; its parent
    // list does (<ul data-type="taskList">).
    function taskItemOf(el: HTMLElement): HTMLLIElement | null {
      const li = el.closest('li')
      return li?.parentElement?.matches('ul[data-type="taskList"]') ? li : null
    }

    function taskCheckbox(target: EventTarget | null): HTMLInputElement | null {
      const el = target as HTMLElement | null
      if (!(el instanceof HTMLInputElement) || el.type !== 'checkbox') return null
      return taskItemOf(el) ? el : null
    }

    function onPointerDown(e: Event) {
      if (taskCheckbox(e.target)) focusedBefore = editor!.view.hasFocus()
    }

    function onChange(e: Event) {
      const box = taskCheckbox(e.target)
      if (!box || !editor) return
      e.stopPropagation()
      if (!editor.isEditable) {
        box.checked = !box.checked
        return
      }
      const li = taskItemOf(box)
      const { state, view } = editor
      let found: number | null = null
      state.doc.descendants((node, pos) => {
        if (found !== null) return false
        if (node.type.name === 'taskItem' && view.nodeDOM(pos) === li) found = pos
        return true
      })
      if (found === null) return
      const node = state.doc.nodeAt(found)!
      view.dispatch(state.tr.setNodeMarkup(found, undefined, { ...node.attrs, checked: box.checked }))
      if (!focusedBefore && view.hasFocus()) (document.activeElement as HTMLElement | null)?.blur()
    }

    root.addEventListener('pointerdown', onPointerDown, true)
    root.addEventListener('change', onChange, true)
    return () => {
      root.removeEventListener('pointerdown', onPointerDown, true)
      root.removeEventListener('change', onChange, true)
    }
  }, [editor])

  if (!editor) return null

  const statusText =
    provider.status === 'online'
      ? provider.hasUnsaved
        ? 'Saving…'
        : provider.readonly
          ? 'Read-only'
          : 'Saved'
      : provider.status === 'connecting'
        ? 'Connecting…'
        : provider.hasUnsaved
          ? 'Offline — changes will be saved once reconnected'
          : 'Offline'

  return (
    <div className="editor-shell">
      {/* Tiptap commands run even when the editor isn't editable, so the
          buttons are disabled on read-only and not-yet-seeded pages. */}
      <div className="toolbar">
        <ToolbarButton
          disabled={!editable}
          onClick={() => editor.chain().focus().toggleBold().run()}
          active={editor.isActive('bold')}
        >
          B
        </ToolbarButton>
        <ToolbarButton
          disabled={!editable}
          onClick={() => editor.chain().focus().toggleItalic().run()}
          active={editor.isActive('italic')}
        >
          I
        </ToolbarButton>
        <ToolbarButton
          disabled={!editable}
          onClick={() => editor.chain().focus().toggleCode().run()}
          active={editor.isActive('code')}
        >
          <code className="toolbar-inline-code">`x`</code>
        </ToolbarButton>
        <ToolbarButton
          disabled={!editable}
          onClick={() => editor.chain().focus().toggleHeading({ level: 2 }).run()}
          active={editor.isActive('heading', { level: 2 })}
        >
          H2
        </ToolbarButton>
        <ToolbarButton
          disabled={!editable}
          onClick={() => editor.chain().focus().toggleBulletList().run()}
          active={editor.isActive('bulletList')}
        >
          • List
        </ToolbarButton>
        <ToolbarButton
          disabled={!editable}
          onClick={() => editor.chain().focus().toggleOrderedList().run()}
          active={editor.isActive('orderedList')}
        >
          1. List
        </ToolbarButton>
        <ToolbarButton
          disabled={!editable}
          onClick={() => editor.chain().focus().toggleTaskList().run()}
          active={editor.isActive('taskList')}
        >
          ☑ To-do
        </ToolbarButton>
        <ToolbarButton
          disabled={!editable}
          onClick={() => editor.chain().focus().toggleBlockquote().run()}
          active={editor.isActive('blockquote')}
        >
          Quote
        </ToolbarButton>
        <ToolbarButton
          disabled={!editable}
          onClick={() => editor.chain().focus().toggleCodeBlock().run()}
          active={editor.isActive('codeBlock')}
        >
          Code
        </ToolbarButton>
        <ToolbarButton
          disabled={!editable}
          onClick={() => editor.chain().focus().insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run()}
        >
          ▦ Table
        </ToolbarButton>
        <ToolbarButton disabled={!editable} onClick={() => pickImages(editor)}>🖼 Image</ToolbarButton>
        <ToolbarButton disabled={!editable} onClick={() => pickFiles(editor)}>📎 File</ToolbarButton>
        <div className="collab-status">
          <div className="collab-peers">
            {peers.map((p) => (
              <span key={p.clientId} className="collab-peer" style={{ background: p.color }} title={p.name}>
                {p.name.trim().charAt(0).toUpperCase() || '?'}
              </span>
            ))}
          </div>
          <span className={`collab-state collab-state-${provider.status}`}>{statusText}</span>
        </div>
      </div>
      <EditorContent editor={editor} className="editor-content" />
      <TableMenu editor={editor} />
    </div>
  )
}
