import { Extension, type Editor } from '@tiptap/core'
import Image from '@tiptap/extension-image'
import { Plugin, PluginKey, type EditorState } from '@tiptap/pm/state'
import { Decoration, DecorationSet, type EditorView } from '@tiptap/pm/view'
import { uploadPageAttachment, uploadPageImage } from './api'
import { showToast } from './toast'
import { ATTACHMENT_EXTENSIONS, MAX_ATTACHMENT_BYTES, extensionOf } from './fileAttachment'

// Images are scaled down to this width before upload. The editor column
// is well under 1000 CSS px, so 2000 px stays sharp on a 2x Retina
// screen while a full-resolution Mac screenshot (3000+ px) shrinks a lot.
const MAX_WIDTH = 2000
// Files already this small and narrow go up untouched.
const SKIP_BELOW_BYTES = 300 * 1024
const ALLOWED = ['image/png', 'image/jpeg', 'image/webp', 'image/gif']

// shrinkImage re-encodes a pasted image as WebP, scaled to MAX_WIDTH,
// and keeps whichever of the two is smaller. GIFs are left alone so an
// animation isn't flattened to its first frame. Browsers that can't
// encode WebP (older Safari) hand back PNG instead; the size comparison
// quietly keeps the original then.
async function shrinkImage(file: File): Promise<{ blob: Blob; name: string }> {
  if (file.type === 'image/gif') return { blob: file, name: file.name }
  let bitmap: ImageBitmap
  try {
    bitmap = await createImageBitmap(file)
  } catch {
    return { blob: file, name: file.name }
  }
  const scale = Math.min(1, MAX_WIDTH / bitmap.width)
  if (scale === 1 && file.size < SKIP_BELOW_BYTES) {
    bitmap.close()
    return { blob: file, name: file.name }
  }
  const canvas = document.createElement('canvas')
  canvas.width = Math.round(bitmap.width * scale)
  canvas.height = Math.round(bitmap.height * scale)
  const ctx = canvas.getContext('2d')
  if (!ctx) return { blob: file, name: file.name }
  ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
  bitmap.close()
  const blob = await new Promise<Blob | null>((res) => canvas.toBlob(res, 'image/webp', 0.9))
  if (!blob || blob.type !== 'image/webp' || blob.size >= file.size) return { blob: file, name: file.name }
  return { blob, name: file.name.replace(/\.[^.]+$/, '') + '.webp' }
}

// -- upload placeholders -----------------------------------------------------
// While a file uploads, a local-only widget marks where it will land.
// Decorations map through every edit (yours and collaborators'), so the
// image still lands in the right spot if the text around it changes in
// the meantime. They never enter the document, so other users don't see
// a half-finished upload.

const placeholderKey = new PluginKey<DecorationSet>('imageUploadPlaceholder')

type PlaceholderAction = { add: { id: object; pos: number; label?: string } } | { remove: { id: object } }

const placeholderPlugin = new Plugin<DecorationSet>({
  key: placeholderKey,
  state: {
    init: () => DecorationSet.empty,
    apply(tr, set) {
      set = set.map(tr.mapping, tr.doc)
      const action = tr.getMeta(placeholderKey) as PlaceholderAction | undefined
      if (action && 'add' in action) {
        const el = document.createElement('div')
        el.className = 'image-upload-placeholder'
        el.textContent = action.add.label ?? 'Uploading image…'
        set = set.add(tr.doc, [Decoration.widget(action.add.pos, el, { id: action.add.id })])
      } else if (action && 'remove' in action) {
        set = set.remove(set.find(undefined, undefined, (spec) => spec.id === action.remove.id))
      }
      return set
    },
  },
  props: {
    decorations: (state) => placeholderKey.getState(state),
  },
})

function findPlaceholder(state: EditorState, id: object): number | null {
  const found = placeholderKey.getState(state)?.find(undefined, undefined, (spec) => spec.id === id)
  return found && found.length ? found[0].from : null
}

// uploadImages uploads each image file and inserts it at pos (or at the
// cursor). Non-image files are ignored. Failures show an alert rather
// than silently dropping the paste.
export function uploadImages(editor: Editor, pageId: string, files: File[], pos?: number) {
  const view: EditorView = editor.view
  for (const file of files.filter((f) => ALLOWED.includes(f.type))) {
    const id = {}
    const at = pos ?? view.state.selection.from
    view.dispatch(view.state.tr.setMeta(placeholderKey, { add: { id, pos: at } }))
    void (async () => {
      try {
        const { blob, name } = await shrinkImage(file)
        const img = await uploadPageImage(pageId, blob, name || 'image.png')
        if (editor.isDestroyed) return
        const where = findPlaceholder(view.state, id) ?? view.state.selection.from
        // insertContentAt splits a paragraph around the block-level
        // image when the placeholder sits mid-text.
        editor
          .chain()
          .setMeta(placeholderKey, { remove: { id } })
          .insertContentAt(where, { type: 'image', attrs: { src: img.url, alt: file.name || null } })
          .run()
      } catch (err) {
        if (!editor.isDestroyed) view.dispatch(view.state.tr.setMeta(placeholderKey, { remove: { id } }))
        showToast(`Image upload failed: ${err instanceof Error ? err.message : err}`, { error: true })
      }
    })()
  }
}

// uploadAttachments uploads non-image files and inserts a file chip for
// each. Type and size are checked here first for a quick, clear message;
// the server checks again and has the final word.
export function uploadAttachments(editor: Editor, pageId: string, files: File[], pos?: number) {
  const view: EditorView = editor.view
  for (const file of files) {
    if (!ATTACHMENT_EXTENSIONS.includes(extensionOf(file.name))) {
      showToast(`"${file.name}" can't be attached. Allowed: ${ATTACHMENT_EXTENSIONS.join(', ')}.`, { error: true })
      continue
    }
    if (file.size > MAX_ATTACHMENT_BYTES) {
      showToast(`"${file.name}" is larger than 25 MB.`, { error: true })
      continue
    }
    const id = {}
    const at = pos ?? view.state.selection.from
    view.dispatch(view.state.tr.setMeta(placeholderKey, { add: { id, pos: at, label: `Uploading ${file.name}…` } }))
    void (async () => {
      try {
        const att = await uploadPageAttachment(pageId, file)
        if (editor.isDestroyed) return
        const where = findPlaceholder(view.state, id) ?? view.state.selection.from
        editor
          .chain()
          .setMeta(placeholderKey, { remove: { id } })
          .insertContentAt(where, {
            type: 'fileAttachment',
            attrs: { href: att.url, filename: att.filename, size: att.size_bytes },
          })
          .run()
      } catch (err) {
        if (!editor.isDestroyed) view.dispatch(view.state.tr.setMeta(placeholderKey, { remove: { id } }))
        showToast(`Upload failed: ${err instanceof Error ? err.message : err}`, { error: true })
      }
    })()
  }
}

// uploadFiles sends images down the image path (shown inline) and every
// other file down the attachment path (shown as a file chip).
function uploadFiles(editor: Editor, pageId: string, files: File[], pos?: number) {
  const images = files.filter((f) => ALLOWED.includes(f.type))
  const others = files.filter((f) => !ALLOWED.includes(f.type))
  if (images.length) uploadImages(editor, pageId, images, pos)
  if (others.length) uploadAttachments(editor, pageId, others, pos)
}

// pickFiles opens the OS file picker for attachments (toolbar "File"
// button and the "/file" slash command).
export function pickFiles(editor: Editor) {
  const pageId = (editor.storage as unknown as { imageUpload?: { pageId: string } }).imageUpload?.pageId
  if (!pageId) return
  const input = document.createElement('input')
  input.type = 'file'
  input.accept = ATTACHMENT_EXTENSIONS.map((e) => '.' + e).join(',')
  input.multiple = true
  input.onchange = () => uploadAttachments(editor, pageId, Array.from(input.files ?? []))
  input.click()
}

// pickImages opens the OS file picker (used by the toolbar button and
// the "/image" slash command).
export function pickImages(editor: Editor) {
  const pageId = (editor.storage as unknown as { imageUpload?: { pageId: string } }).imageUpload?.pageId
  if (!pageId) return
  const input = document.createElement('input')
  input.type = 'file'
  input.accept = ALLOWED.join(',')
  input.multiple = true
  input.onchange = () => uploadImages(editor, pageId, Array.from(input.files ?? []))
  input.click()
}

function droppedFiles(list: FileList | null | undefined): File[] {
  return Array.from(list ?? [])
}

// NookImage only accepts images Nook itself serves. A paste from the web
// or from Notion carries <img> tags pointing at other sites; the CSP
// (img-src 'self') would block those and leave broken images behind, so
// they're dropped at parse time instead.
export const NookImage = Image.extend({
  parseHTML() {
    return [
      {
        tag: 'img[src]',
        getAttrs: (el) => ((el as HTMLElement).getAttribute('src') ?? '').startsWith('/api/pages/') ? null : false,
      },
    ]
  },
}).configure({
  inline: false,
  allowBase64: false,
  resize: {
    enabled: true,
    directions: ['bottom-left', 'bottom-right', 'top-left', 'top-right'],
    minWidth: 80,
    minHeight: 40,
    alwaysPreserveAspectRatio: true,
  },
})

// ImageUpload turns pasted and dropped image files into uploaded images.
// Screenshots copied to the clipboard (Cmd+Ctrl+Shift+4 on a Mac) arrive
// as files, so this is what makes "paste a screenshot" work.
export const ImageUpload = Extension.create<{ pageId: string }, { pageId: string }>({
  name: 'imageUpload',
  addOptions: () => ({ pageId: '' }),
  addStorage() {
    return { pageId: this.options.pageId }
  },
  addProseMirrorPlugins() {
    const editor = this.editor
    const pageId = this.options.pageId
    return [
      placeholderPlugin,
      new Plugin({
        key: new PluginKey('imageUploadHandlers'),
        props: {
          handlePaste(_view, event) {
            const files = droppedFiles(event.clipboardData?.files)
            if (!files.length || !editor.isEditable) return false
            event.preventDefault()
            uploadFiles(editor, pageId, files)
            return true
          },
          handleDrop(view, event, _slice, moved) {
            if (moved) return false
            const files = droppedFiles(event.dataTransfer?.files)
            if (!files.length || !editor.isEditable) return false
            event.preventDefault()
            const pos = view.posAtCoords({ left: event.clientX, top: event.clientY })?.pos
            uploadFiles(editor, pageId, files, pos)
            return true
          },
        },
      }),
    ]
  },
})
