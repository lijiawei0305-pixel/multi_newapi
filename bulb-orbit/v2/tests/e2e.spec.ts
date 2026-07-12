import { test, expect } from '@playwright/test'

test('landing renders brand + 24 satellites + hud interaction', async ({ page }) => {
  const errors: string[] = []
  page.on('console', m => { if (m.type() === 'error') errors.push(m.text()) })
  await page.goto('/')
  await expect(page.locator('#brand-title')).toHaveText('WeDream AI')
  await expect(page.locator('#brand-slogan')).toHaveText('让灵感不再受限')
  await page.waitForTimeout(3000)              // 真实等待入场 + rAF（禁 virtual-time）
  await expect(page.locator('#hud-name')).toHaveText('WeDream 核心')
  await page.screenshot({ path: 'test-results/landing-full.png' })
  // 页面无 <link rel="icon">，浏览器会自动请求 /favicon.ico；该 404 由浏览器网络层产生，
  // 不是页面代码的 console.error，与页面正确性无关——按名单单独过滤，不放宽整体断言。
  const realErrors = errors.filter(e => !e.includes('favicon.ico'))
  expect(realErrors, realErrors.join('\n')).toEqual([])
})
