import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import './themes.css'
import './mobile.css'
import App from './App.tsx'
import { applyStoredThemeEarly } from './theme'
import { enableDragDropTouch } from '@dragdroptouch/drag-drop-touch'
import { enableTouchDragExtras } from './touchDrag'
import { watchStandaloneTitlebar } from './standaloneTitlebar'

applyStoredThemeEarly()
watchStandaloneTitlebar()

// Every reorder in the app (sidebar pages/spaces/groups, tabs, database
// columns) uses HTML5 drag-and-drop, which touch screens don't fire.
// This turns touches into those same drag events. Press-hold mode: a
// drag starts only after holding a draggable item still for a moment,
// so ordinary scrolling and tapping are unaffected. It only listens on
// touch-capable devices.
// pressHoldMargin: moving more than this before the hold completes means
// the finger is scrolling, not picking something up.
enableDragDropTouch(document, document, { isPressHoldMode: true, pressHoldDelayMS: 350, pressHoldMargin: 10 })
enableTouchDragExtras()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
