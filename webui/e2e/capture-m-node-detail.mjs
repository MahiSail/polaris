import { chromium } from '@playwright/test'
import fs from 'fs'
import path from 'path'

const artifactDir = 'C:/Users/dom/.gemini/antigravity/brain/1189570f-b006-46b8-bb4a-5ed3792bb944/screenshots'
const TARGET_URL = 'http://127.0.0.1:19670/'

async function run() {
  const browser = await chromium.launch({
    headless: true,
    executablePath: 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
  })
  const ctx = await browser.newContext({
    viewport: { width: 390, height: 844 },
    deviceScaleFactor: 3,
    isMobile: true,
    hasTouch: true,
  })
  const page = await ctx.newPage()
  await page.goto(`${TARGET_URL}#/nodes`, { waitUntil: 'domcontentloaded' })

  // Login
  if (await page.locator('.login-form, .m-login__form').count()) {
    const usernameInput = page.locator('input[autocomplete="username"], input[aria-label="用户名"], .login-form input, .m-login__form input').first()
    await usernameInput.fill('polaris_admin')
    const pwdInput = page.locator('input[type="password"]').first()
    await pwdInput.fill('123456')
    await page.locator('button[type="submit"], button:has-text("登录")').first().click()
    await page.waitForTimeout(1500)
    if (await page.locator('.login-form, .m-login__form').count()) {
      await usernameInput.fill('polaris_admin')
      await pwdInput.fill('Polaris2026!Pass')
      await page.locator('button[type="submit"], button:has-text("登录")').first().click()
      await page.waitForTimeout(1500)
    }
  }

  // Force light mode
  await page.evaluate(() => {
    localStorage.setItem('polaris_theme', 'light')
    document.documentElement.classList.remove('dark')
    document.documentElement.classList.add('light')
  })
  await page.goto(`${TARGET_URL}#/nodes`, { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(1000)

  // Click first node card
  const firstCard = page.locator('.m-node-card').first()
  await firstCard.click()
  await page.waitForTimeout(1200)

  // Scroll to top
  await page.evaluate(() => {
    const body = document.querySelector('.m-page__body')
    if (body) body.scrollTop = 0
    window.scrollTo(0, 0)
  })
  await page.waitForTimeout(500)

  // Capture detail top
  await page.screenshot({
    path: path.join(artifactDir, 'mobile-detail-fixed.png'),
    fullPage: false,
  })
  console.log('Captured mobile-detail-fixed.png')
  await browser.close()
}

run().catch(console.error)
