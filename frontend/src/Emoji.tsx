import { FLUENT_EMOJI } from './fluentEmoji.generated'

// Every image in src/emoji/ (scripts/sync-fluent-emoji.mjs), as its
// built URL: content-hashed under /assets/, so cached for good.
const URLS = import.meta.glob<string>('./emoji/*.webp', { eager: true, query: '?url', import: 'default' })

// An emoji drawn as its Fluent Emoji 3D image, so icons look the same
// on every platform instead of each OS's own emoji font. Anything
// outside the curated set falls back to the plain character.
export default function Emoji({ char }: { char: string }) {
  const file = FLUENT_EMOJI[char]
  const src = file ? URLS[`./emoji/${file}`] : undefined
  if (!src) return <>{char}</>
  return <img className="emoji-img" src={src} alt={char} draggable={false} loading="lazy" decoding="async" />
}
