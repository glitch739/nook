// Notion-style status/select color presets, shown as a swatch grid in
// PropertyCell.tsx's select-cell editor (which also offers a custom
// color next to them).
// Pulled into its own file (not exported alongside
// PropertyCell.tsx's components) so that file only exports components,
// which is what keeps Vite's Fast Refresh working for it.
export const SELECT_COLORS = [
  '#94a3b8', '#f87171', '#fb923c', '#fbbf24', '#a3e635',
  '#4ade80', '#2dd4bf', '#38bdf8', '#818cf8', '#c084fc', '#f472b6',
]
