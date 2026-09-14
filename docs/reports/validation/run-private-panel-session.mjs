#!/usr/bin/env node

import { chmodSync, closeSync, constants, fstatSync, lstatSync, openSync, readFileSync, realpathSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import playwright from '/home/sky/.nvm/versions/node/v20.19.4/lib/node_modules/agent-browser/node_modules/playwright-core/index.js'

const { chromium } = playwright

const IDENTIFIER = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/
const CHROME = '/home/sky/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome'

function report(classification, detail = '') {
  process.stdout.write(`${JSON.stringify({ classification, detail })}\n`)
}

function privateFile(path) {
  const absolute = resolve(path)
  const parent = lstatSync(dirname(absolute))
  const file = lstatSync(absolute)
  if (!parent.isDirectory() || parent.isSymbolicLink() || (parent.mode & 0o777) !== 0o700 ||
      !file.isFile() || file.isSymbolicLink() || (file.mode & 0o777) !== 0o600 ||
      dirname(realpathSync(absolute)) !== realpathSync(dirname(absolute))) throw new Error('private input rejected')
  const descriptor = openSync(absolute, constants.O_RDONLY | constants.O_NOFOLLOW)
  try {
    const opened = fstatSync(descriptor)
    if (!opened.isFile() || (opened.mode & 0o777) !== 0o600 || opened.dev !== file.dev || opened.ino !== file.ino ||
        opened.size < 1 || opened.size > 4096) throw new Error('private input rejected')
    const value = readFileSync(descriptor)
    if (value.includes(0) || value.includes(10) || value.includes(13)) throw new Error('private input rejected')
    return value
  } finally {
    closeSync(descriptor)
  }
}

function parse() {
  const [url, username, secretFile, stateFile] = process.argv.slice(2)
  if (!url?.startsWith('http://127.0.0.1:') || !IDENTIFIER.test(username || '') || !secretFile?.startsWith('/') || !stateFile?.startsWith('/')) {
    throw new Error('invalid arguments')
  }
  return { url, username, secretFile: resolve(secretFile), stateFile: resolve(stateFile) }
}

async function main() {
  const args = parse()
  const password = privateFile(args.secretFile)
  let browser
  try {
    browser = await chromium.launch({ executablePath: CHROME, headless: true })
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
    await page.goto(args.url, { waitUntil: 'networkidle', timeout: 30_000 })
    const inputs = page.locator('main.login input')
    if (await inputs.count() !== 2) throw new Error('login form unavailable')
    await inputs.nth(0).fill(args.username)
    await inputs.nth(1).fill(password.toString('utf8'))
    await page.locator('main.login button[type="submit"]').click()
    await page.waitForFunction(() => !document.querySelector('main.login'), undefined, { timeout: 30_000 })
    await page.context().storageState({ path: args.stateFile })
    chmodSync(args.stateFile, 0o600)
    const title = await page.title()
    report('success', title.slice(0, 80))
  } finally {
    password.fill(0)
    await browser?.close()
  }
}

main().catch(() => report('failed'))
