// Desktop web app windows (macOS Safari "Add to Dock", installed PWAs):
// if the page is drawn underneath the window's title bar, the title bar
// takes the clicks and drags in that strip, and the tab bar sits right
// there. In that case the window's outer and inner heights are (almost)
// the same, since no separate title bar is counted. Then the root gets
// .titlebar-overlap and the top of the app moves down by the title
// bar's height (App.css). A window with its own title bar above the page
// (the usual case) has a clear difference and is left alone, as is
// full screen and every touch device.
const TITLEBAR_PX = 28

export function watchStandaloneTitlebar() {
  const standalone =
    window.matchMedia('(display-mode: standalone)').matches ||
    (navigator as Navigator & { standalone?: boolean }).standalone === true
  if (!standalone || window.matchMedia('(hover: none)').matches) return
  const root = document.documentElement
  root.style.setProperty('--titlebar-inset', `${TITLEBAR_PX}px`)
  const update = () => {
    const fullScreen = window.outerHeight >= window.screen.height
    const overlap = !fullScreen && window.outerHeight - window.innerHeight < 8
    root.classList.toggle('titlebar-overlap', overlap)
  }
  update()
  window.addEventListener('resize', update)
}
