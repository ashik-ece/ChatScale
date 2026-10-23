// Saves a PNG of the ChatScale Grafana dashboard for a time range, using a
// headless Chrome driven over the DevTools protocol. Usage:
//   node scripts/grafana-screenshot.mjs <out.png> <fromEpochMs> <toEpochMs>
import { spawn } from 'node:child_process'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const [out, from, to] = process.argv.slice(2)
const chromePath = process.env.CHROME ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const grafana = process.env.GRAFANA_URL ?? 'http://localhost:3000'
const auth = Buffer.from(`admin:${process.env.GRAFANA_ADMIN_PASSWORD ?? 'admin'}`).toString('base64')
const port = 9400 + Math.floor(Math.random() * 400)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

const profile = mkdtempSync(join(tmpdir(), 'chatscale-shot-'))
const chrome = spawn(
  chromePath,
  ['--headless=new', '--disable-gpu', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, 'about:blank'],
  { stdio: 'ignore' },
)

try {
  let page
  for (let i = 0; i < 40 && !page; i++) {
    try {
      page = (await (await fetch(`http://localhost:${port}/json`)).json()).find((t) => t.type === 'page')
    } catch {}
    await sleep(250)
  }
  if (!page) throw new Error('Chrome did not start')

  const ws = new WebSocket(page.webSocketDebuggerUrl)
  await new Promise((r) => (ws.onopen = r))
  let id = 0
  const waiting = new Map()
  ws.onmessage = (e) => {
    const m = JSON.parse(e.data)
    waiting.get(m.id)?.(m)
    waiting.delete(m.id)
  }
  const cdp = (method, params = {}) =>
    new Promise((r) => {
      const i = ++id
      waiting.set(i, r)
      ws.send(JSON.stringify({ id: i, method, params }))
    })

  await cdp('Network.enable')
  await cdp('Network.setExtraHTTPHeaders', { headers: { Authorization: `Basic ${auth}` } })
  await cdp('Emulation.setDeviceMetricsOverride', { width: 1500, height: 1000, deviceScaleFactor: 1, mobile: false })
  await cdp('Page.navigate', {
    url: `${grafana}/d/chatscale/chatscale?orgId=1&from=${from}&to=${to}&refresh=&kiosk`,
  })
  await sleep(8000)
  const shot = await cdp('Page.captureScreenshot', { format: 'png' })
  writeFileSync(out, Buffer.from(shot.result.data, 'base64'))
} finally {
  // Chrome keeps writing to its profile until it exits, so wait before cleaning up.
  const exited = new Promise((r) => chrome.once('exit', r))
  chrome.kill()
  await Promise.race([exited, sleep(5000)])
  rmSync(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 })
}
process.exit(0)
