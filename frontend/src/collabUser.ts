// The name and color others see on your cursor while editing together.
// Kept apart from collabProvider.ts (and Yjs) so App.tsx can set it on
// login without the realtime-editing libraries landing in the main
// bundle; collabProvider subscribes when the editor chunk loads.

export interface CollabUser {
  name: string
  color: string
}

const PALETTE = ['#e03e3e', '#d9730d', '#cb912f', '#448361', '#337ea9', '#9065b0', '#c14c8a', '#0f7b6c']

let localUser: CollabUser = { name: 'Anonymous', color: PALETTE[4] }
const listeners = new Set<(user: CollabUser) => void>()

export const getCollabUser = () => localUser

/** Sets the name/color others see on your cursor (color derived from
 * the user id, so it is stable across sessions and devices). */
export function setCollabUser(id: string, name: string) {
  let h = 0
  for (const ch of id) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  localUser = { name, color: PALETTE[h % PALETTE.length] }
  for (const fn of listeners) fn(localUser)
}

export function onCollabUserChange(fn: (user: CollabUser) => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}
