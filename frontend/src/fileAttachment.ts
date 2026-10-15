import { Node, mergeAttributes } from '@tiptap/core'
import { attachmentPreviewUrl, getFileIndexStatus } from './api'

// A file attached inside a page's text: a block "chip" with a type
// badge, the file name and its size; clicking it downloads the file.
// The bytes live on the server (POST /api/pages/{id}/attachments); the
// document only holds the URL and display details.

// Mirrors assets.attachmentTypes on the server (the server decides; this
// only lets the picker filter and gives an early, friendly error).
export const ATTACHMENT_EXTENSIONS = [
  'pdf', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'odt', 'ods', 'odp', 'rtf', 'eml', 'msg',
  'txt', 'md', 'csv', 'json', 'zip', '7z', 'png', 'jpg', 'jpeg', 'gif', 'webp', 'heic',
  'mp3', 'm4a', 'mp4', 'mov',
]
export const MAX_ATTACHMENT_BYTES = 25 * 1024 * 1024

export function extensionOf(name: string): string {
  const i = name.lastIndexOf('.')
  return i > 0 ? name.slice(i + 1).toLowerCase() : ''
}

export function formatSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return ''
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(bytes < 10 * 1024 ? 1 : 0)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

// Only links to files Nook itself stores are accepted when parsing
// pasted HTML, same rule as NookImage.
export function isNookAttachmentUrl(href: string): boolean {
  return /^\/api\/pages\/[^/]+\/attachments\/[^/]+$/.test(href)
}

export const FileAttachment = Node.create({
  name: 'fileAttachment',
  group: 'block',
  atom: true,
  draggable: true,
  selectable: true,

  addAttributes() {
    return {
      href: { default: '' },
      filename: { default: 'file' },
      size: { default: 0 },
    }
  },

  parseHTML() {
    return [
      {
        tag: 'div[data-type="file-attachment"]',
        getAttrs: (el) => {
          const e = el as HTMLElement
          const href = e.getAttribute('data-href') ?? ''
          if (!isNookAttachmentUrl(href)) return false
          return {
            href,
            filename: e.getAttribute('data-filename') ?? 'file',
            size: Number(e.getAttribute('data-size') ?? 0),
          }
        },
      },
    ]
  },

  renderHTML({ node, HTMLAttributes }) {
    const { href, filename, size } = node.attrs as { href: string; filename: string; size: number }
    return [
      'div',
      mergeAttributes(HTMLAttributes, {
        'data-type': 'file-attachment',
        'data-href': href,
        'data-filename': filename,
        'data-size': String(size),
        class: 'file-attachment',
      }),
      ['a', { href, download: filename }, filename],
    ]
  },

  // A real DOM node view: the chip is not editable text, and a click on
  // it downloads instead of just selecting the node.
  addNodeView() {
    return ({ node }) => {
      const { href, filename, size } = node.attrs as { href: string; filename: string; size: number }
      const dom = document.createElement('div')
      dom.className = 'file-attachment'
      dom.contentEditable = 'false'
      const a = document.createElement('a')
      a.className = 'file-attachment-link'
      a.href = href
      a.download = filename
      a.draggable = false
      const badge = document.createElement('span')
      badge.className = 'file-attachment-badge'
      badge.textContent = (extensionOf(filename) || 'file').slice(0, 4).toUpperCase()
      const name = document.createElement('span')
      name.className = 'file-attachment-name'
      name.textContent = filename
      const meta = document.createElement('span')
      meta.className = 'file-attachment-size'
      meta.textContent = formatSize(size)
      a.append(badge, name, meta)
      dom.append(a)
      // Read it in the browser where it can be (backend preview.go).
      const preview = attachmentPreviewUrl(href, filename, /\.(pdf|png|jpe?g|gif|webp|txt|md|csv)$/i.test(filename))
      if (preview) {
        const view = document.createElement('a')
        view.className = 'file-attachment-preview'
        view.href = preview
        view.target = '_blank'
        view.rel = 'noopener'
        view.textContent = 'Preview'
        dom.append(view)
      }
      // How far the file is in being made searchable (ADR 0009); only
      // said when it is news: still reading, read by OCR, or unreadable.
      if (isNookAttachmentUrl(href)) {
        void getFileIndexStatus(href).then(
          (st) => {
            const note =
              st.status === 'pending' || st.status === 'processing'
                ? 'indexing…'
                : st.status === 'failed' || st.status === 'skipped'
                  ? 'not searchable'
                  : st.used_ocr
                    ? 'OCR'
                    : ''
            if (note) meta.textContent = `${formatSize(size)} · ${note}`
            if (st.error) meta.title = st.error
          },
          () => {},
        )
      }
      return {
        dom,
        // Let the browser handle clicks on the link (download) instead of
        // ProseMirror turning them into a node selection.
        stopEvent: (event) => event.type === 'click' || event.type === 'mousedown',
        ignoreMutation: () => true,
      }
    }
  },
})
