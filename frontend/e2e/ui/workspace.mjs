// Split workspace (Workspace.tsx, layout.ts): drag a tab to split, into
// another pane, resize, drop a sidebar page on an edge, the tab menu,
// reload, closing, Cmd/Ctrl+\, one page in two panes, phone width.
import { at, mouseDrag, session, until } from './lib.mjs'

export async function run(type) {
  const s = await session(type, 'ws')
  const { page, check } = s
  for (const n of ['A', 'B', 'C']) {
    await s.mk(n)
    await s.open(n)
  }
  const panes = () =>
    page.$$eval('.pane', (ps) =>
      ps.map((p) => [...p.querySelectorAll('.tab-title')].map((t) => t.textContent.slice(-1)).join('')).join('|'),
    )
  const tab = (n) => page.locator('.pane .tab', { hasText: s.name(n) }).first()
  const body = (i) => page.locator('[data-pane-body]').nth(i)

  const b0 = await body(0).boundingBox()
  await mouseDrag(page, await at(tab('C')), { x: b0.x + b0.width - 30, y: b0.y + b0.height / 2 })
  check('drag to the right edge splits', (await until(async () => ((await panes()) === 'AB|C' ? 'ok' : ''), 3000).catch(() => '')) === 'ok', await panes())

  await mouseDrag(page, await at(tab('B')), await at(tab('C'), 0.5, 0.1))
  await page.waitForTimeout(300)
  check('drag onto the other pane strip', (await panes()) === 'A|BC', await panes())

  const tabBars = await page.$$eval('.pane .tab-bar', (els) => els.map((e) => Math.round(e.getBoundingClientRect().bottom)))
  check('tab bars line up', new Set(tabBars).size === 1, JSON.stringify(tabBars))

  const before = (await page.locator('.pane').first().boundingBox()).width
  await mouseDrag(page, await at(page.locator('.pane-divider-row')), { x: (await at(page.locator('.pane-divider-row'))).x - 150, y: 400 })
  await page.waitForTimeout(300)
  check('divider resizes', (await page.locator('.pane').first().boundingBox()).width < before - 100)

  // Sidebar rows drag with HTML5 drag-and-drop (their grip).
  const right = await body(1).boundingBox()
  const row = page.locator('.sidebar .page-row', { hasText: s.name('A') })
  await row.hover()
  await row.locator('.page-grip').dragTo(body(1), { targetPosition: { x: right.width / 2, y: right.height - 20 } })
  await page.waitForTimeout(300)
  check('sidebar page on a bottom edge stacks a pane', (await panes()) === 'A|BC|A', await panes())

  await tab('C').click({ button: 'right' })
  const menu = await page.$$eval('.create-menu .create-menu-item', (b) => b.map((x) => x.textContent))
  check('tab menu', menu.includes('Split down') && menu.includes('Close tab'), JSON.stringify(menu))
  await page.keyboard.press('Escape')

  await tab('B').click({ button: 'middle' })
  await page.waitForTimeout(200)
  check('middle click closes', (await panes()) === 'A|C|A', await panes())

  const layout = await panes()
  await page.reload()
  await page.waitForSelector('.pane .tab')
  check('layout survives a reload', (await panes()) === layout, await panes())

  while ((await page.$$('.pane')).length > 1) await page.locator('.pane').last().locator('.tab-close').first().click()
  check('closing tabs collapses panes', (await panes()).split('|').length === 1, await panes())

  // One page in two panes: typing in one shows in the other.
  await s.open('C')
  await page.waitForSelector('.pane .ProseMirror')
  await page.keyboard.press(type === 'webkit' ? 'Meta+Backslash' : 'Control+Backslash')
  await until(async () => (await page.locator('.pane .ProseMirror').count()) === 2)
  await page.locator('.pane .ProseMirror').first().click()
  await page.keyboard.type('hello')
  const synced = await until(async () => (await page.locator('.pane .ProseMirror').nth(1).innerText()).includes('hello'), 4000).catch(() => false)
  check('same page in two panes stays in sync', !!synced)

  await page.setViewportSize({ width: 390, height: 800 })
  check('phone shows one pane', (await until(async () => (await page.$$('.pane')).length === 1).catch(() => false)) === true)
  await s.done()
}
