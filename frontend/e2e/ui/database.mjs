// Databases: the row panel in a split, open beside, the board (columns,
// card and column drags by their grips, + New, person grouping) and a
// wide screen's alignment.
import { at, dragByGrip, session, until } from './lib.mjs'

export async function run(type) {
  const s = await session(type, 'db', { width: 2000, height: 900 })
  const { page, check, call } = s
  const { db, status } = await s.statusDb('Tasks')
  const ids = {}
  for (const n of ['A', 'B', 'C']) {
    ids[n] = (await s.mk(n, { parent_page_id: db.id })).id
    await call.put(`/api/pages/${ids[n]}/properties/${status.id}`, { value: { option_id: 'todo' } })
  }
  const owner = await call.post(`/api/pages/${db.id}/properties`, { name: 'Owner', type: 'person' })
  await call.put(`/api/properties/${owner.id}`, { options: [{ id: s.me.id, label: 'Me', color: 'blue' }] })
  const note = await s.mk('Note')
  void note

  // Wide screen: name, meta and toolbar share one left edge.
  await s.open('Tasks')
  await page.waitForSelector('.db-view-bar')
  const lefts = await page.evaluate(() =>
    ['.page-title-bar', '.page-meta', '.db-view-bar'].map((q) => Math.round(document.querySelector(q).getBoundingClientRect().left)),
  )
  check('title, meta and toolbar aligned', Math.max(...lefts) - Math.min(...lefts) <= 2, JSON.stringify(lefts))

  // Row panel inside its pane; open beside reuses the right pane.
  await s.open('Note')
  await page.keyboard.press(type === 'webkit' ? 'Meta+Backslash' : 'Control+Backslash')
  await until(async () => (await page.$$('.pane')).length === 2)
  await page.locator('[data-pane-body]').first().click({ position: { x: 5, y: 5 } })
  await s.open('Tasks')
  await page.locator('.pane').first().locator('.db-row-title-btn').first().click()
  const panel = await until(async () => {
    const [p, r] = await Promise.all([page.locator('.pane').first().boundingBox(), page.locator('.row-detail-panel').boundingBox()])
    return r && Math.abs(r.x + r.width - (p.x + p.width)) <= 1 ? r : null
  }).catch(() => null)
  check('row panel stays in its pane', !!panel)
  for (const i of [0, 1]) {
    if (i) await page.locator('.pane').first().locator('.db-row-title-btn').nth(1).click()
    await page.locator('.row-detail-panel [aria-label="Open beside the table"]').click()
    await page.waitForTimeout(300)
  }
  const tabs = await page.$$eval('.pane', (ps) => ps.map((p) => p.querySelectorAll('.tab').length))
  check('open beside fills one pane', tabs.length === 2 && tabs[1] === 3, JSON.stringify(tabs))
  while ((await page.$$('.pane')).length > 1) await page.locator('.pane').last().locator('.tab-close').first().click()

  // Board by Status.
  await s.open('Tasks')
  await page.locator('.db-view-bar select[aria-label="View"]').selectOption({ label: 'Board: Status' })
  await page.waitForSelector('.db-board-card')
  const cols = () =>
    page.$$eval('.db-board-col:not(.db-board-col-ghost)', (cs) =>
      cs.map((c) => [...c.querySelectorAll('.db-board-card-title')].map((t) => t.textContent.slice(-1)).join('')).join('|'),
    )
  const card = (n) => page.locator('.db-board-card', { hasText: s.name(n) })
  const col = (label) => page.locator('.db-board-col', { hasText: label })
  check('columns and cards', (await cols()) === '|ABC|', await cols())

  await dragByGrip(page, card('C'), '.db-board-grip', await at(card('A'), 0.25))
  check('a card moves within its column', (await until(async () => ((await cols()) === '|CAB|' ? 1 : 0), 3000).catch(() => 0)) === 1, await cols())
  await dragByGrip(page, card('B'), '.db-board-grip', await at(col('Done').locator('.db-board-col-head')))
  await until(async () => (await cols()) === '|CA|B', 3000).catch(() => {})
  const rows = await call.get(`/api/pages/${db.id}/rows`)
  check('into another column sets the value', rows.rows.find((r) => r.id === ids.B).values[status.id]?.option_id === 'done', await cols())
  // Losing the window mid-drag (an app switch) cancels it: nothing sticks.
  await card('A').hover()
  const g = await at(card('A').locator('.db-board-grip'))
  await page.mouse.move(g.x, g.y)
  await page.mouse.down()
  await page.mouse.move(g.x + 40, g.y + 80, { steps: 5 })
  await page.evaluate(() => window.dispatchEvent(new Event('blur')))
  const ghostGone = (await page.locator('.db-board-ghost').count()) === 0
  await page.mouse.up()
  check('focus loss cancels a drag', ghostGone && (await cols()) === '|CA|B', await cols())
  await page.locator('.db-view-bar select[aria-label="View"]').selectOption({ label: 'Table' })
  const table = (await page.$$eval('td.db-td-title', (tds) => tds.map((t) => t.innerText))).map((t) => t.match(/-([ABC])\b/)?.[1]).join('')
  check('the table keeps its own order', table === 'ABC', table)
  await page.locator('.db-view-bar select[aria-label="View"]').selectOption({ label: 'Board: Status' })
  await page.waitForSelector('.db-board-card')

  // Columns move by their header grip, saved for this grouping only.
  const done = col('Done').locator('.db-board-col-head')
  await dragByGrip(page, done, '.db-board-col-grip', await at(page.locator('.db-board-col').first(), 0.1, 0.1))
  const heads = () => page.$$eval('.db-board .db-board-col-head', (h) => h.map((x) => x.innerText.trim().replace(/\s+\d+$/, '')))
  check('a column moves to the front', (await until(async () => ((await heads())[0] === 'Done' ? 1 : 0), 3000).catch(() => 0)) === 1, JSON.stringify(await heads()))
  const saved = (await call.get(`/api/pages/${db.id}/rows`)).board_columns
  check('column order saved for Status only', JSON.stringify(saved) === JSON.stringify({ [status.id]: ['done', '', 'todo'] }), JSON.stringify(saved))

  await col('Todo').locator('.db-board-add').click()
  await page.waitForSelector('.row-detail-panel')
  check('+ New adds a card to its column', (await until(async () => (await col('Todo').locator('.db-board-card').count()) === 3).catch(() => false)) === true)

  await page.locator('.db-view-bar select[aria-label="View"]').selectOption({ label: 'Board: Owner' })
  await page.waitForSelector('.db-board-card')
  await dragByGrip(page, card('A'), '.db-board-grip', await at(col('Me').locator('.db-board-col-head')))
  const assigned = await until(async () => {
    const r = await call.get(`/api/pages/${db.id}/rows`)
    return r.rows.find((x) => x.id === ids.A).values[owner.id]?.user_id === s.me.id
  }).catch(() => false)
  check('person board assigns by drag', assigned === true)

  await page.reload()
  await page.waitForSelector('.db-board-col')
  check('board view remembered', (await page.locator('.db-board-col').count()) > 0)
  await s.done()
}
