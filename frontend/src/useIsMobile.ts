import { useEffect, useState } from 'react'

// Must match the `max-width` breakpoint in mobile.css: at or below it
// the sidebar is an overlay drawer instead of a side-by-side column.
export const MOBILE_QUERY = '(max-width: 768px)'

/** True while the viewport is phone-sized (or a narrow tablet/split
 * view). Follows rotation and window resizes live. */
export function useIsMobile(): boolean {
  const [isMobile, setIsMobile] = useState(() => window.matchMedia(MOBILE_QUERY).matches)
  useEffect(() => {
    const mq = window.matchMedia(MOBILE_QUERY)
    const onChange = () => setIsMobile(mq.matches)
    onChange()
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])
  return isMobile
}
