import { chromium } from '@playwright/test'
import fs from 'fs'
import path from 'path'

const artifactDir = 'C:/Users/dom/.gemini/antigravity/brain/495340ca-a6d6-490f-8159-ee75e82af538/visual_audit'
if (!fs.existsSync(artifactDir)) {
  fs.mkdirSync(artifactDir, { recursive: true })
}

const TARGET_URL = 'http://localhost:19670/'

async function runAudit() {
  const browser = await chromium.launch({
    headless: true,
    executablePath: 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
  })

  // 1. Desktop Context (1280x800)
  const context = await browser.newContext({
    viewport: { width: 1280, height: 800 },
  })
  const page = await context.newPage()

  console.log('Navigating to target URL...')
  await page.goto(TARGET_URL)
  await page.waitForTimeout(1000)

  // 1.1 Desktop Login - Dark
  console.log('Capturing Desktop Login (Dark)...')
  await page.screenshot({ path: path.join(artifactDir, 'desktop_login_dark.png') })

  // 1.2 Desktop Login - Light
  console.log('Capturing Desktop Login (Light)...')
  await page.evaluate(() => {
    document.documentElement.classList.remove('dark')
    document.documentElement.classList.add('light')
  })
  await page.waitForTimeout(500)
  await page.screenshot({ path: path.join(artifactDir, 'desktop_login_light.png') })

  // Restore dark for login
  await page.evaluate(() => {
    document.documentElement.classList.remove('light')
    document.documentElement.classList.add('dark')
  })

  // 1.3 Login to access App layout
  console.log('Logging in...')
  const usernameInput = page.locator('input[autocomplete="username"], .login-form input').first()
  if (await usernameInput.count()) {
    await usernameInput.fill('polaris_admin')
    const pwdInput = page.locator('input[type="password"]').first()
    await pwdInput.fill('123456')
    await page.locator('button[type="submit"], button:has-text("登录")').first().click()
    await page.waitForTimeout(1500)

    // Handle password change if requested
    if (await page.locator('.password-card').count()) {
      console.log('Capturing Password Change Card...')
      await page.screenshot({ path: path.join(artifactDir, 'password_change_card.png') })
      const pwds = page.locator('.password-card input[type="password"]')
      await pwds.nth(0).fill('123456')
      await pwds.nth(1).fill('Polaris2026!Pass')
      await pwds.nth(2).fill('Polaris2026!Pass')
      await page.getByRole('button', { name: '保存', exact: true }).click()
      await page.waitForTimeout(1500)
    }

    // If still at login, retry with new pwd
    if (await page.locator('.login-form').count()) {
      await usernameInput.fill('polaris_admin')
      const pwdInput = page.locator('input[type="password"]').first()
      await pwdInput.fill('Polaris2026!Pass')
      await page.locator('button[type="submit"], button:has-text("登录")').first().click()
      await page.waitForTimeout(1500)
    }
  }

  // 1.4 Desktop Sidebar Expanded (Dark)
  console.log('Capturing Sidebar Expanded (Dark)...')
  await page.waitForTimeout(1000)
  const sidebar = page.locator('.app-sidebar')
  if (await sidebar.count()) {
    await sidebar.screenshot({ path: path.join(artifactDir, 'sidebar_expanded_dark.png') })

    // Test clicking logo does NOT pop up any popover
    console.log('Clicking app-brand to verify no popover appears...')
    await page.locator('.app-brand').click()
    await page.waitForTimeout(500)
    const popoverCount = await page.locator('.update-popover:visible, .update-pop:visible').count()
    console.log(`Popover count after click: ${popoverCount} (expected: 0)`)

    // 1.5 Desktop Sidebar Collapsed (Dark)
    console.log('Toggling sidebar collapse...')
    const toggleBtn = page.locator('.sidebar-toggle-btn')
    if (await toggleBtn.count()) {
      await toggleBtn.click()
      await page.waitForTimeout(600)
      await sidebar.screenshot({ path: path.join(artifactDir, 'sidebar_collapsed_dark.png') })
      // Restore
      await page.locator('.app-brand').click()
      await page.waitForTimeout(600)
    }

    // 1.6 Desktop Sidebar (Light)
    console.log('Capturing Sidebar (Light)...')
    await page.evaluate(() => {
      document.documentElement.classList.remove('dark')
      document.documentElement.classList.add('light')
    })
    await page.waitForTimeout(500)
    await sidebar.screenshot({ path: path.join(artifactDir, 'sidebar_light.png') })
  }

  await context.close()

  // 2. Mobile Context (iPhone 14 / 390x844)
  console.log('Testing Mobile Views...')
  const mContext = await browser.newContext({
    viewport: { width: 390, height: 844 },
    userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 16_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.5 Mobile/15E148 Safari/604.1',
    isMobile: true,
  })
  const mPage = await mContext.newPage()
  await mPage.goto(TARGET_URL)
  await mPage.waitForTimeout(1000)

  // 2.1 Mobile Login - Dark
  console.log('Capturing Mobile Login (Dark)...')
  await mPage.screenshot({ path: path.join(artifactDir, 'mobile_login_dark.png') })

  // 2.2 Mobile Login - Light
  console.log('Capturing Mobile Login (Light)...')
  await mPage.evaluate(() => {
    document.documentElement.classList.remove('dark')
    document.documentElement.classList.add('light')
  })
  await mPage.waitForTimeout(500)
  await mPage.screenshot({ path: path.join(artifactDir, 'mobile_login_light.png') })

  await mContext.close()
  await browser.close()
  console.log('All visual audit screenshots successfully saved!')
}

runAudit().catch(err => {
  console.error('Audit failed:', err)
  process.exit(1)
})
