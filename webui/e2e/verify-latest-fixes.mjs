import { chromium } from '@playwright/test'
import fs from 'fs'
import path from 'path'

const artifactDir = 'C:/Users/dom/.gemini/antigravity/brain/1189570f-b006-46b8-bb4a-5ed3792bb944/screenshots'
if (!fs.existsSync(artifactDir)) {
  fs.mkdirSync(artifactDir, { recursive: true })
}

const TARGET_URL = 'http://127.0.0.1:19670/'

async function loginUser(page) {
  const isLoginPage = (await page.locator('.login-form, .m-login__form').count()) > 0
  if (!isLoginPage) return

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

async function forceLightMode(page) {
  await page.evaluate(() => {
    localStorage.setItem('polaris_theme', 'light')
    document.documentElement.classList.remove('dark')
    document.documentElement.classList.add('light')
  })
}

async function run() {
  const browser = await chromium.launch({
    headless: true,
    executablePath: 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
  })

  // 1. DESKTOP
  const desktopCtx = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 2,
  })
  const dPage = await desktopCtx.newPage()
  await dPage.goto(`${TARGET_URL}#/nodes`, { waitUntil: 'domcontentloaded' })
  await loginUser(dPage)
  await forceLightMode(dPage)
  await dPage.goto(`${TARGET_URL}#/nodes`, { waitUntil: 'domcontentloaded' })
  await dPage.waitForTimeout(1200)

  // 截图 1: 桌面端服务器列表顶部 (升级按钮、纯图标视图切换、实时网速仪表盘)
  await dPage.screenshot({
    path: path.join(artifactDir, 'desktop-verify-nodes-top.png'),
    fullPage: false,
  })
  console.log('Captured: desktop-verify-nodes-top.png')

  // 进入第一个节点的详情
  const firstCard = dPage.locator('.openlat-node-card').first()
  if (await firstCard.isVisible()) {
    await firstCard.click()
    await dPage.waitForTimeout(1000)

    // 切换到网络延迟
    const latencyTab = dPage.locator('button:has-text("网络延迟")').first()
    await latencyTab.click()
    await dPage.waitForTimeout(800)

    // 默认或点 1小时
    const btn1h = dPage.locator('.seg-btn:has-text("1 小时")').first()
    if (await btn1h.isVisible()) {
      await btn1h.click()
      await dPage.waitForTimeout(800)
      await dPage.screenshot({
        path: path.join(artifactDir, 'desktop-verify-latency-1h.png'),
        fullPage: false,
      })
      console.log('Captured: desktop-verify-latency-1h.png')
    }

    // 点 6小时
    const btn6h = dPage.locator('.seg-btn:has-text("6 小时")').first()
    if (await btn6h.isVisible()) {
      await btn6h.click()
      await dPage.waitForTimeout(800)
      await dPage.screenshot({
        path: path.join(artifactDir, 'desktop-verify-latency-6h.png'),
        fullPage: false,
      })
      console.log('Captured: desktop-verify-latency-6h.png')
    }

    // 点 7天
    const btn7d = dPage.locator('.seg-btn:has-text("7 天")').first()
    if (await btn7d.isVisible()) {
      await btn7d.click()
      await dPage.waitForTimeout(800)
      await dPage.screenshot({
        path: path.join(artifactDir, 'desktop-verify-latency-7d.png'),
        fullPage: false,
      })
      console.log('Captured: desktop-verify-latency-7d.png')
    }
  }

  // 2. MOBILE
  const mobileCtx = await browser.newContext({
    viewport: { width: 390, height: 844 },
    deviceScaleFactor: 3,
    isMobile: true,
    hasTouch: true,
  })
  const mPage = await mobileCtx.newPage()
  await mPage.goto(`${TARGET_URL}#/nodes`, { waitUntil: 'domcontentloaded' })
  await loginUser(mPage)
  await forceLightMode(mPage)
  await mPage.goto(`${TARGET_URL}#/nodes`, { waitUntil: 'domcontentloaded' })
  await mPage.waitForTimeout(1200)

  // 截图: 移动端服务器列表 (纯图标切换按钮、实时网速图标)
  await mPage.screenshot({
    path: path.join(artifactDir, 'mobile-verify-nodes-top.png'),
    fullPage: false,
  })
  console.log('Captured: mobile-verify-nodes-top.png')

  // 进入节点详情
  const mFirstCard = mPage.locator('.m-node-card').first()
  if (await mFirstCard.isVisible()) {
    await mFirstCard.click()
    await mPage.waitForTimeout(1000)

    // 点网络延迟
    const mLatBtn = mPage.locator('.m-seg__item:has-text("网络延迟")').first()
    if (await mLatBtn.isVisible()) {
      await mLatBtn.click()
      await mPage.waitForTimeout(800)

      // 点 1小时
      const m1h = mPage.locator('.m-pill:has-text("1小时")').first()
      if (await m1h.isVisible()) {
        await m1h.click()
        await mPage.waitForTimeout(800)
        await mPage.screenshot({
          path: path.join(artifactDir, 'mobile-verify-latency-1h.png'),
          fullPage: false,
        })
        console.log('Captured: mobile-verify-latency-1h.png')
      }

      // 点 7天
      const m7d = mPage.locator('.m-pill:has-text("7天")').first()
      if (await m7d.isVisible()) {
        await m7d.click()
        await mPage.waitForTimeout(800)
        await mPage.screenshot({
          path: path.join(artifactDir, 'mobile-verify-latency-7d.png'),
          fullPage: false,
        })
        console.log('Captured: mobile-verify-latency-7d.png')
      }
    }
  }

  await browser.close()
  console.log('Done verify!')
}

run().catch(console.error)
