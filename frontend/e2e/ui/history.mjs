// Page history (versions, restore) and links between pages (@, [[,
// rename, click, "Linked from").
import { session, until } from './lib.mjs'
import { DatabaseSync } from 'node:sqlite'

// Starts a new version without waiting 10 minutes: the newest one is
// made old in the test database.
function backdateVersions(pageId) {
  const db = new DatabaseSync(process.env.NOOK_DB)
  db.prepare(`UPDATE page_versions SET created_at = '2000-01-01T00:00:00.000Z' WHERE page_id = ?`).run(pageId)
  db.close()
}

export async function run(type) {
  const s = await session(type, 'hist')
  const { page, check, call } = s
  const editor = () => page.locator('.pane .ProseMirror').first()
  const versions = (id) => call.get(`/api/pages/${id}/versions`)

  const doc = await s.mk('Doc')
  await s.open('Doc')
  await editor().click()
  await page.keyboard.type('first draft')
  await until(async () => (await versions(doc.id)).length === 1)
  backdateVersions(doc.id)
  await page.keyboard.type(' and more')
  await until(async () => (await versions(doc.id)).length === 2)
  await page.locator('.page-meta-versions').click()
  await page.waitForSelector('.history-doc')
  const preview = await until(async () => {
    const t = await page.locator('.history-doc').innerText()
    return t.includes('first draft') ? t : ''
  })
  check('older version previewed', !preview.includes('and more'), JSON.stringify(preview))
  await page.locator('.history-footer .settings-primary').click()
  check('restore puts it back', (await until(async () => (await editor().innerText()).trim() === 'first draft').catch(() => false)) === true)

  const target = await s.mk('Target Page')
  await s.mk('Source')
  await page.reload()
  await page.waitForSelector('.sidebar .page-row')
  await s.open('Source')
  await editor().click()
  await page.keyboard.type(`see @${s.name('Target Pa')}`)
  await page.waitForSelector('.slash-menu-item')
  await page.keyboard.press('Enter')
  await page.keyboard.type('and [[Doc')
  await page.waitForSelector('.slash-menu-item')
  await page.keyboard.press('Enter')
  check('@ and [[ insert links', (await page.locator('.pane .page-mention').count()) === 2)
  await until(async () => (await call.get(`/api/pages/${target.id}/backlinks`)).length === 1)

  await call.put(`/api/pages/${target.id}`, { title: s.name('Renamed') })
  await page.reload()
  await page.waitForSelector('.pane .page-mention')
  const mention = page.locator('.pane .page-mention').first()
  check('a link shows the new name', (await until(async () => (await mention.innerText()).includes('Renamed')).catch(() => false)) === true)
  await mention.click()
  await page.waitForSelector('.backlinks button')
  check('click opens it, and it lists the source', (await page.locator('.backlinks button').innerText()).includes('Source'))
  await s.done()
}
