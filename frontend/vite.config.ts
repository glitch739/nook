import { readdirSync, readFileSync } from 'node:fs'
import react from '@vitejs/plugin-react'
import { defineConfig, type Plugin } from 'vite'

// lucide-static icons are served as plain files under a versioned path,
// /assets/lucide-<version>/<name>.svg, instead of going through
// import.meta.glob. The glob put a URL map *and* a lazy-loader map for
// all ~2000 icons into whichever chunk imported it -- the main bundle,
// via PageIcon -- and emitted every icon twice (as .svg and as a .js
// module). Now PageIcon only knows the base path, the full name list
// lives in the lazy IconPicker chunk (virtual:lucide-names), and each
// icon exists once. Under /assets/ they get the immutable cache header
// and the rate-limit exemption; the version in the path keeps that
// safe across lucide upgrades.
const LUCIDE_DIR = 'node_modules/lucide-static/icons'
const LUCIDE_VERSION: string = JSON.parse(readFileSync('node_modules/lucide-static/package.json', 'utf8')).version
const LUCIDE_BASE = `/assets/lucide-${LUCIDE_VERSION}/`

function lucideIcons(): Plugin {
  const names = readdirSync(LUCIDE_DIR)
    .filter((f) => f.endsWith('.svg'))
    .map((f) => f.slice(0, -'.svg'.length))
    .sort()
  const virtualId = 'virtual:lucide-names'
  return {
    name: 'nook-lucide-icons',
    resolveId(id) {
      return id === virtualId ? '\0' + virtualId : undefined
    },
    load(id) {
      return id === '\0' + virtualId ? `export const LUCIDE_NAMES = ${JSON.stringify(names)}` : undefined
    },
    generateBundle() {
      for (const name of names) {
        this.emitFile({
          type: 'asset',
          fileName: `${LUCIDE_BASE.slice(1)}${name}.svg`,
          source: readFileSync(`${LUCIDE_DIR}/${name}.svg`),
        })
      }
    },
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const file = req.url?.startsWith(LUCIDE_BASE) ? req.url.slice(LUCIDE_BASE.length).split('?')[0] : ''
        if (/^[a-z0-9-]+\.svg$/.test(file)) {
          try {
            const body = readFileSync(`${LUCIDE_DIR}/${file}`)
            res.setHeader('Content-Type', 'image/svg+xml')
            res.end(body)
            return
          } catch {
            // fall through to a normal 404
          }
        }
        next()
      })
    },
  }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), lucideIcons()],
  define: {
    __LUCIDE_BASE__: JSON.stringify(LUCIDE_BASE),
  },
  build: {
    // Never inline small assets as base64 into JS: every asset should
    // stay a separately cached file (historically the ~2000 icon SVGs;
    // those are emitted by lucideIcons() above now, but nothing gains
    // from inlining either).
    assetsInlineLimit: 0,
  },
  server: {
    // Bind all interfaces, not just localhost, so the dev server is
    // reachable over the LAN (e.g. http://<server-ip>:5173) — useful
    // while developing over SSH with no local browser on this machine.
    host: '0.0.0.0',
    port: 5173,
    // Proxy API calls to the Go backend so the browser sees everything
    // as same-origin (no CORS config needed, and the session cookie the
    // backend sets is transparently forwarded as if from this origin).
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
