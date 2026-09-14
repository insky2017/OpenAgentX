import playwright from '/home/sky/.nvm/versions/node/v20.19.4/lib/node_modules/agent-browser/node_modules/playwright-core/index.js'

const binary = '/home/sky/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome'
const state = '/tmp/openagentx-u2-e1-r3-20260905/browser-state.json'

async function main() {
  const browser = await playwright.chromium.launch({ executablePath: binary, headless: true })
  try {
    const context = await browser.newContext({ storageState: state })
    const page = await context.newPage()
    page.setDefaultTimeout(8000)
    await page.goto('http://127.0.0.1:18180/', { waitUntil: 'domcontentloaded', timeout: 10000 })
    await page.locator('nav.bottom button').nth(3).click()
    await page.waitForTimeout(1000)
    const target = page.locator('#network-runtime-target')
    const options = await target.locator('option').evaluateAll(nodes => nodes.map(node => ({ value: node.value, text: node.textContent || '' })))
    const observer = options.filter(option => option.text.includes('e1-observer-agent')).at(0)
    if (!observer) throw new Error('observer unavailable')
    await target.selectOption(observer.value)
    await page.getByRole('radio', { name: '继承 Worker 环境', exact: true }).click()
    const mode = page.locator('.network-mode-test-list')
    const before = await mode.innerText()
    if (!before.includes('流程完成')) await page.getByRole('button', { name: '测试模式', exact: true }).click()
    await page.waitForTimeout(15000)
    const after = await mode.innerText()
    if (after.includes('流程完成')) {
      const publish = page.getByRole('button', { name: '发布模式', exact: true })
      if (await publish.isEnabled()) await publish.click()
      await page.waitForTimeout(5000)
    }
    const body = await page.locator('body').innerText()
    process.stdout.write(JSON.stringify({ classification: 'observer_mode_observed', target: observer.text, mode: after, applied: body.includes('已应用') && body.includes('e1-observer-agent / fixture') }) + '\n')
  } finally {
    await browser.close()
  }
}

main().catch(() => process.stdout.write('{"classification":"observer_mode_failed"}\n'))
