// Matching for the command palette.

// Lowercase without accents, so "esk" finds "Esküvő".
export function fold(s: string): string {
  return s.normalize('NFD').replace(/[\u0300-\u036f]/g, '').toLowerCase()
}

// Lower is better; -1 = no match. Prefix beats word start beats
// substring beats a loose in-order match. q must already be folded.
export function score(text: string, q: string): number {
  if (!q) return 0
  const t = fold(text)
  const i = t.indexOf(q)
  if (i === 0) return 0
  if (i > 0) return /[\s\-_/.]/.test(t[i - 1]) ? 1 : 2
  let pos = 0
  for (const ch of q) {
    pos = t.indexOf(ch, pos)
    if (pos === -1) return -1
    pos++
  }
  return 3
}
