#!/usr/bin/env node
// Phone home-screen icons from the Nook bolt app icon (Nook-bolt-1024.png:
// Apple 1024 grid, 824 px body at 100 px, transparent margin).
// iOS and Android draw their own rounded mask and fill transparency with
// black, so these are the body square cropped full-bleed: no margin, no
// shadow, no own rounding. Writes to frontend/public/:
//   apple-touch-icon.png (180), icon-192.png, icon-512.png
// Needs sharp:  NODE_PATH=<dir with sharp in node_modules> node make-app-icons.js
const sharp = require('sharp')
const path = require('path')

const src = path.join(__dirname, 'Nook-bolt-1024.png')
const out = path.join(__dirname, '../../frontend/public')
// A small inset past the body edge so the corners' outside (transparent)
// never shows; the platform mask rounds the corners anyway.
const INSET = 10
const crop = { left: 100 + INSET, top: 100 + INSET, width: 824 - 2 * INSET, height: 824 - 2 * INSET }

;(async () => {
  for (const [file, size] of [['apple-touch-icon.png', 180], ['icon-192.png', 192], ['icon-512.png', 512]]) {
    await sharp(src)
      .extract(crop)
      .resize(size, size, { kernel: 'lanczos3' })
      .flatten({ background: '#0b0b14' })
      .png()
      .toFile(path.join(out, file))
    console.log(`${file} ${size}x${size}`)
  }
})().catch((e) => {
  console.error(e.message)
  process.exit(1)
})
