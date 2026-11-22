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

  console.log('Logging in...')
  const usernameInput = page.locator('input[autocomplete="username"], input[aria-label="用户名"], .login-form input, .m-login__form input').first()
  await usernameInput.fill('polaris_admin')
  const pwdInput = page.locator('input[type="password"]').first()
  await pwdInput.fill('123456')
  await page.locator('button[type="submit"], button:has-text("登录")').first().click()
  await page.waitForTimeout(2000)

  // Check if initial login failed due to password change
  if (await page.locator('.login-form, .m-login__form').count()) {
    console.log('Retrying login with Polaris2026!Pass...')
    await usernameInput.fill('polaris_admin')
    await pwdInput.fill('Polaris2026!Pass')
    await page.locator('button[type="submit"], button:has-text("登录")').first().click()
    await page.waitForTimeout(2000)
  }

  // Handle password change card if prompted
  if (await page.locator('.password-card').count()) {
    console.log('Handling required password change...')
    const pwds = page.locator('.password-card input[type="password"]')
    await pwds.nth(0).fill('123456')
    await pwds.nth(1).fill('Polaris2026!Pass')
    await pwds.nth(2).fill('Polaris2026!Pass')
    await page.getByRole('button', { name: '保存', exact: true }).click()
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

  // ─────────────────────────────────────────────
  // 1. DESKTOP LIGHT MODE AUDIT
  // ─────────────────────────────────────────────
  console.log('=== Starting Desktop Light Mode Audit ===')
  const desktopCtx = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 2,
  })
  const dPage = await desktopCtx.newPage()
  await dPage.goto(TARGET_URL, { waitUntil: 'domcontentloaded' })
  await dPage.waitForTimeout(1000)
  await loginUser(dPage)
  await forceLightMode(dPage)
  await dPage.reload({ waitUntil: 'domcontentloaded' })
  await dPage.waitForTimeout(1200)

  async function snapD(name, hashPath, waitMs = 1200) {
    if (hashPath) {
      await dPage.goto(`${TARGET_URL}#/${hashPath}`, { waitUntil: 'domcontentloaded' })
      await dPage.waitForTimeout(waitMs)
    }
    const out = path.join(artifactDir, `desktop-${name}.png`)
    await dPage.screenshot({ path: out, fullPage: false })
    console.log(`[Desktop] Captured: desktop-${name}.png`)
  }

  await snapD('01-dashboard', 'dashboard', 1200)
  await snapD('02-nodes', 'nodes', 1200)

  // 1. Probe Manager Dialog on desktop
  const probeBtn = dPage.locator('button:has-text("探针")').first()
  if (await probeBtn.isVisible()) {
    await probeBtn.click()
    await dPage.waitForTimeout(1000)
    await snapD('02b-probe-dialog', null, 500)
    const closeBtn = dPage.locator('.el-dialog__headerbtn').first()
    if (await closeBtn.isVisible()) await closeBtn.click()
    await dPage.waitForTimeout(500)
  }

  // 2. Node Detail (switch to card view or click first node)
  const nodeItem = dPage.locator('.node-card, .node-row, .openlat-node-card').first()
  if (await nodeItem.isVisible()) {
    await nodeItem.click()
    await dPage.waitForTimeout(1500)
    await snapD('03-node-detail-resources', null, 500)

    // Latency Tab (7-day check)
    const latTab = dPage.locator('.el-tabs__item:has-text("网络延迟"), button:has-text("网络延迟"), .tab:has-text("网络延迟")').first()
    if (await latTab.isVisible()) {
      await latTab.click()
      await dPage.waitForTimeout(1000)
      await snapD('03b-node-detail-latency', null, 500)
    }

    // Facts Tab (6 cards equal height check)
    const factsTab = dPage.locator('.el-tabs__item:has-text("详情"), button:has-text("详情"), .tab:has-text("详情")').first()
    if (await factsTab.isVisible()) {
      await factsTab.click()
      await dPage.waitForTimeout(1000)
      await snapD('03c-node-detail-facts-6cards', null, 500)
    }
  }

  await snapD('04-inbounds', 'inbounds', 1000)
  await snapD('05-proxy-groups', 'proxy-groups', 1000)
  await snapD('06-rule-providers', 'rule-providers', 1000)
  await snapD('07-routes', 'routes', 1000)
  await snapD('08-outbounds', 'outbounds', 1000)
  await snapD('09-subscriptions', 'subscriptions', 1000)

  // Edit Subscription
  const editSubBtn = dPage.locator('.action-column button:has-text("编辑"), .el-table button:has-text("编辑")').first()
  if (await editSubBtn.isVisible()) {
    await editSubBtn.click()
    await dPage.waitForTimeout(1000)
    await snapD('09b-subscription-edit-basic', null, 500)

    const rulesNav = dPage.locator('.form-nav__item:has-text("访问规则")').first()
    if (await rulesNav.isVisible()) {
      await rulesNav.click()
      await dPage.waitForTimeout(800)
      await snapD('09c-subscription-edit-rules', null, 500)
    }
  }

  await snapD('10-connections', 'connections', 1000)
  await snapD('11-devices', 'devices', 1000)
  await snapD('12-connection-history', 'connection-history', 1000)
  await snapD('13-audit', 'audit', 1000)
  await snapD('14-security', 'security', 1000)

  // ─────────────────────────────────────────────
  // 2. MOBILE LIGHT MODE AUDIT
  // ─────────────────────────────────────────────
  console.log('=== Starting Mobile Light Mode Audit ===')
  const mobileCtx = await browser.newContext({
    viewport: { width: 390, height: 844 },
    deviceScaleFactor: 3,
    isMobile: true,
    hasTouch: true,
    userAgent:
      'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1',
  })
  const mPage = await mobileCtx.newPage()
  await mPage.goto(TARGET_URL, { waitUntil: 'domcontentloaded' })
  await mPage.waitForTimeout(1000)
  await loginUser(mPage)
  await forceLightMode(mPage)
  await mPage.reload({ waitUntil: 'domcontentloaded' })
  await mPage.waitForTimeout(1200)

  async function snapM(name, hashPath, waitMs = 1200) {
    if (hashPath) {
      await mPage.goto(`${TARGET_URL}#/${hashPath}`, { waitUntil: 'domcontentloaded' })
      await mPage.waitForTimeout(waitMs)
    }
    const out = path.join(artifactDir, `mobile-${name}.png`)
    await mPage.screenshot({ path: out, fullPage: false })
    console.log(`[Mobile] Captured: mobile-${name}.png`)
  }

  await snapM('01-dashboard', 'dashboard', 1200)
  await snapM('02-nodes', 'nodes', 1200)

  // Probe manager in mobile
  const mProbeBtn = mPage.locator('button:has-text("探针管理")').first()
  if (await mProbeBtn.isVisible()) {
    await mProbeBtn.click()
    await mPage.waitForTimeout(1000)
    await snapM('02b-probe-manager-sheet', null, 500)
    const closeBtn = mPage.locator('.m-sheet__close, button:has-text("完成"), .m-sheet-header button').first()
    if (await closeBtn.isVisible()) await closeBtn.click()
    await mPage.waitForTimeout(500)
  }

  // Click first node card for mobile detail
  const mFirstNode = mPage.locator('.m-node-card, .openlat-node-card').first()
  if (await mFirstNode.isVisible()) {
    await mFirstNode.click()
    await mPage.waitForTimeout(1200)
    await snapM('03-node-detail', null, 500)

    // Latency in mobile detail
    const mLatBtn = mPage.locator('button:has-text("网络延迟"), .m-seg__item:has-text("网络延迟")').first()
    if (await mLatBtn.isVisible()) {
      await mLatBtn.click()
      await mPage.waitForTimeout(800)
      await snapM('03b-node-detail-latency', null, 500)
    }
  }

  // Inbounds in mobile
  await snapM('04-inbounds', 'inbounds', 1200)

  // Open edit listener sheet in mobile
  const mInboundCard = mPage.locator('.m-inbound-card').first()
  if (await mInboundCard.isVisible()) {
    await mInboundCard.click()
    await mPage.waitForTimeout(800)
    const editItem = mPage.locator('button:has-text("编辑"), .m-action-sheet button:has-text("编辑")').first()
    if (await editItem.isVisible()) {
      await editItem.click()
      await mPage.waitForTimeout(1000)
      await snapM('04b-inbounds-edit-sheet', null, 500)
      const cancelBtn = mPage.locator('button:has-text("取消")').first()
      if (await cancelBtn.isVisible()) await cancelBtn.click()
      await mPage.waitForTimeout(500)
    }
  }

  await snapM('05-proxy-groups', 'proxy-groups', 1000)
  await snapM('06-rule-providers', 'rule-providers', 1000)
  await snapM('07-routes', 'routes', 1000)
  await snapM('08-outbounds', 'outbounds', 1000)
  await snapM('09-subscriptions', 'subscriptions', 1000)

  // Edit subscription in mobile
  const mSubItem = mPage.locator('.m-item, .sub-card').first()
  if (await mSubItem.isVisible()) {
    const editBtn = mSubItem.locator('button:has-text("编辑")').first()
    if (await editBtn.isVisible()) {
      await editBtn.click()
      await mPage.waitForTimeout(1000)
      await snapM('09b-subscriptions-edit-mobile', null, 500)

      // Open a rule in subscription mobile edit
      const firstRule = mPage.locator('.rule__body').first()
      if (await firstRule.isVisible()) {
        await firstRule.click()
        await mPage.waitForTimeout(800)
        await snapM('09c-rule-sheet-mobile', null, 500)
        const doneBtn = mPage.locator('button:has-text("完成")').first()
        if (await doneBtn.isVisible()) await doneBtn.click()
        await mPage.waitForTimeout(500)
      }

      const backBtn = mPage.locator('.m-header__back, button:has-text("返回")').first()
      if (await backBtn.isVisible()) {
        await backBtn.click()
        await mPage.waitForTimeout(600)
      }
    }
  }

  await snapM('10-connections', 'connections', 1000)
  await snapM('11-devices', 'devices', 1000)
  await snapM('12-connection-history', 'connection-history', 1000)
  await snapM('13-audit', 'audit', 1000)
  await snapM('14-security', 'security', 1000)
  await snapM('15-settings', 'settings', 1000)
  await snapM('16-more', 'more', 1200)

  await browser.close()
  console.log('=== All visual audit screenshots captured successfully! ===')
}

run().catch((err) => {
  console.error('Audit run failed:', err)
  process.exit(1)
})
