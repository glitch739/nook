// Runs the UI suites (scripts/ui-e2e.sh starts the server first).
// Usage: node e2e/ui/run.mjs [suite ...]   BROWSERS=chromium,webkit
// Every suite x browser runs at the same time, a few at once; they share
// one server and keep to their own pages.
import { failed } from './lib.mjs'

const all = ['tabs', 'workspace', 'history', 'database']
const suites = process.argv.slice(2).length ? process.argv.slice(2) : all
const browsers = (process.env.BROWSERS ?? 'chromium,webkit').split(',')
const jobs = suites.flatMap((s) => browsers.map((b) => ({ s, b })))
const started = Date.now()

let crashed = 0
async function worker() {
  for (let job = jobs.shift(); job; job = jobs.shift()) {
    const { run } = await import(`./${job.s}.mjs`)
    await run(job.b).catch((e) => {
      crashed++
      console.log(`FAIL ${job.s}-${job.b} crashed: ${e.message.split('\n')[0]}`)
    })
  }
}
await Promise.all(Array.from({ length: Number(process.env.JOBS ?? 3) }, worker))
const bad = failed() + crashed
console.log(`${bad ? `${bad} FAILED` : 'ALL OK'} in ${Math.round((Date.now() - started) / 1000)}s`)
process.exit(bad ? 1 : 0)
