// Search (ADR 0007, 0009): inside words, attachments by name, e-mail
// preview; with TIKA=1 (scripts/ui-e2e.sh starts a Tika container) also
// the text inside files and a document preview.
import { BASE, session, until } from './lib.mjs'

const tika = process.env.TIKA === '1'

// A one-page PDF whose only text is `text` (enough for Tika to read).
function pdf(text) {
  const stream = `BT /F1 24 Tf 72 720 Td (${text}) Tj ET`
  const objs = [
    '<< /Type /Catalog /Pages 2 0 R >>',
    '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>',
    `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`,
    '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
  ]
  let out = '%PDF-1.4\n'
  const offsets = objs.map((o, i) => {
    const at = out.length
    out += `${i + 1} 0 obj\n${o}\nendobj\n`
    return at
  })
  const xref = out.length
  out += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n` + offsets.map((o) => `${String(o).padStart(10, '0')} 00000 n \n`).join('')
  out += `trailer\n<< /Size ${objs.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`
  return Buffer.from(out, 'latin1')
}

export async function run(type) {
  const s = await session(type, 'search')
  const { page, check, call, ctx } = s
  const upload = async (pageId, name, buffer, mimeType) => {
    const r = await ctx.request.post(`${BASE}/api/pages/${pageId}/attachments`, {
      multipart: { file: { name, mimeType, buffer } },
    })
    if (!r.ok()) throw new Error(`upload ${name}: ${r.status()} ${await r.text()}`)
    return r.json()
  }
  // Search lists an attachment only while its page's text shows it, so
  // each upload is also put on its page as a file block.
  const blocks = {}
  const attach = async (pageId, name, buffer, mimeType) => {
    const att = await upload(pageId, name, buffer, mimeType)
    ;(blocks[pageId] ??= []).push({ type: 'fileAttachment', attrs: { href: att.url, filename: att.filename, size: att.size_bytes } })
    await call.put(`/api/pages/${pageId}`, { content_json: JSON.stringify({ type: 'doc', content: blocks[pageId] }) })
    return att
  }
  const search = async (q) => {
    await page.locator('input.sidebar-search').fill(q)
    await page.waitForTimeout(400) // the box waits for typing to pause
    return page.locator('.search-result').allInnerTexts()
  }

  const doc = await s.mk('Servers')
  await call.put(`/api/pages/${doc.id}`, {
    content_json: JSON.stringify({ type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: `${s.tag} host srvdb01 in rack` }] }] }),
  })
  check('a word found from its middle', (await until(async () => (await search('db01')).some((t) => t.includes(s.name('Servers'))), 5000).catch(() => false)) === true)

  const files = await s.mk('Files')
  await attach(files.id, `${s.tag}-halozati-topologia.zip`, Buffer.from('PK'), 'application/zip')
  const byName = await search('topologia')
  check('an attachment found by its name, under its page', byName.some((t) => t.includes('halozati-topologia.zip')), JSON.stringify(byName))

  const eml =
    'From: Kiss Anna <anna@example.hu>\r\nSubject: Ajanlat ' + s.tag + '\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>Hello <b>Bob</b></p><script>alert(1)</script>'
  const mail = await attach(files.id, 'ajanlat.eml', Buffer.from(eml), 'message/rfc822')
  const view = await ctx.request.get(`${BASE}${mail.url}/view`)
  const html = await view.text()
  check('an e-mail previews, cleaned', view.ok() && html.includes('Kiss Anna') && html.includes('<b>Bob</b>') && !html.includes('<script'))

  await page.locator('input.sidebar-search').fill('')
  await s.open('Files')
  await page.waitForSelector('.file-attachment')
  check('file blocks offer a preview where one exists', (await page.locator('.file-attachment-preview').count()) >= 1)

  if (tika) {
    // A page not opened in the editor yet: its text can still be set
    // through the API (an open one is edited live only).
    const docs = await s.mk('Docs')
    const word = `kiskutya${type}`
    const att = await attach(docs.id, 'leiras.pdf', pdf(`Ez egy ${word} dokumentum`), 'application/pdf')
    const done = await until(async () => (await call.get(`${att.url}/index`)).status === 'done', 60000, 500).catch(() => false)
    check('Tika read the PDF', done === true)
    const byText = await search(word)
    check('a PDF found by its text, with the page', byText.some((t) => t.includes('leiras.pdf') && t.includes('p. 1')), JSON.stringify(byText))
    const rtf = await attach(docs.id, 'ajanlat.rtf', Buffer.from(`{\\rtf1\\ansi\\deff0 {\\fonttbl {\\f0 Times;}} \\f0 Ajanlat szovege ${s.tag}\\par}`), 'application/rtf')
    const docView = await ctx.request.get(`${BASE}${rtf.url}/view`)
    const docHtml = await docView.text()
    check('a document previews through Tika', docView.ok() && docHtml.includes('Ajanlat szovege'), String(docView.status()))
  }
  await s.done()
}
