// Tab bar: clicks switch tabs (also with Safari's stray off-window move
// in full screen on a second display), a mouse drag reorders.
import { at, mouseDrag, session } from './lib.mjs'

export async function run(type) {
  const s = await session(type, 'tabs')
  const { page, check } = s
  for (const n of ['A', 'B', 'C']) {
    await s.mk(n)
    await s.open(n)
  }
  const tab = (n) => page.locator('.pane .tab', { hasText: s.name(n) })
  const state = () =>
    page.$$eval('.pane .tab[data-tab-id]', (ts) =>
      ts.map((t) => t.querySelector('.tab-title').textContent.slice(-1) + (t.classList.contains('active') ? '*' : '')).join(' '),
    )
  check('opened', (await state()) === 'A B C*', await state())

  for (const n of ['A', 'B']) {
    const p = await at(tab(n))
    await page.mouse.move(p.x, p.y)
    await page.mouse.down()
    // What Safari sends: one move at a fixed (-776, +2390) offset.
    await page.mouse.move(p.x - 776, p.y + 2390)
    await page.mouse.move(p.x, p.y)
    await page.mouse.up()
    await page.waitForTimeout(150)
  }
  check('clicks switch despite a stray move', (await state()) === 'A B* C', await state())

  await mouseDrag(page, await at(tab('A')), await at(tab('C'), 0.5, 0.95))
  await page.waitForTimeout(300)
  check('drag reorders', (await state()).replace(/\*/g, '') === 'B C A', await state())
  await s.done()
}
