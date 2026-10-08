// Minimal ambient typing for the View Transitions API. Not consistently
// present across TypeScript's bundled lib.dom.d.ts versions yet, and
// only needs the one method we actually call.
interface ViewTransition {
  ready: Promise<void>
  finished: Promise<void>
  updateCallbackDone: Promise<void>
  skipTransition(): void
}

interface Document {
  startViewTransition?(callback: () => void | Promise<void>): ViewTransition
}
