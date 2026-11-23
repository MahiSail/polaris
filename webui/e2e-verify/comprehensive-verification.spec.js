import { test, expect } from '@playwright/test'

test('全量页面亲自验证：纯净 URL 无 #、一键升级所有 Agent、桌面端与移动端一致性', async ({ page }) => {
  const consoleErrors = []
  page.on('console', (message) => {
    if (message.type() === 'error' && !message.text().startsWith('Failed to load resource:')) {
      consoleErrors.push(message.text())
    }
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))

  const baseURL = process.env.POLARIS_E2E_BASE_URL
  console.log('Testing against baseURL:', baseURL)

  // 1. 登录并完成首次改密
  await page.goto(baseURL)
  await page.getByLabel('用户名').fill(process.env.POLARIS_E2E_ADMIN_USERNAME)
  await page.getByLabel('密码').fill(process.env.POLARIS_E2E_INITIAL_PASSWORD)
  await page.getByRole('button', { name: '登录', exact: true }).click()

  await expect(page.getByRole('heading', { name: '首次登录需要修改密码', exact: true })).toBeVisible()
  await page.getByPlaceholder('请输入当前密码').fill(process.env.POLARIS_E2E_INITIAL_PASSWORD)
  await page.getByPlaceholder('至少 12 位，不能与当前密码相同').fill(process.env.POLARIS_E2E_CHANGED_PASSWORD)
  await page.getByPlaceholder('请再次输入新密码').fill(process.env.POLARIS_E2E_CHANGED_PASSWORD)
  await page.getByRole('button', { name: '保存', exact: true }).click()

  // 确保已登录进入系统主界面
  await expect(page.locator('.app-layout')).toBeVisible({ timeout: 15000 })
  expect(page.url()).not.toContain('#')

  // 2. 验证旧 Hash 自动规范化跳转（/#/view -> /view）
  console.log('--- 测试旧 Hash URL 自动重定向为无 # 路径 ---')
  await page.goto(baseURL + '/#/nodes')
  await page.waitForTimeout(500)
  expect(page.url()).not.toContain('#')
  expect(page.url()).toBe(baseURL + '/nodes')
  await expect(page.getByRole('heading', { name: '服务器', level: 1 })).toBeVisible()

  await page.goto(baseURL + '/#/subscriptions')
  await page.waitForTimeout(500)
  expect(page.url()).not.toContain('#')
  expect(page.url()).toBe(baseURL + '/subscriptions')
  await expect(page.getByRole('heading', { name: '客户端配置', level: 1 })).toBeVisible()

  // 3. 亲自验证所有桌面端页面的直接访问（Direct Visit）
  const desktopPages = [
    { path: '/nodes', heading: '服务器' },
    { path: '/dashboard', heading: '运行概览' },
    { path: '/inbounds', heading: '接入服务' },
    { path: '/proxy-groups', heading: '代理分组' },
    { path: '/rule-providers', heading: '规则供应商' },
    { path: '/subscriptions', heading: '客户端配置' },
    { path: '/routes', heading: '服务器访问规则' },
    { path: '/outbounds', heading: '上网出口' },
    { path: '/connections', heading: '当前连接' },
    { path: '/devices', heading: '热门设备' },
    { path: '/connection-history', heading: '历史连接明细' },
    { path: '/audit', heading: '操作记录' },
    { path: '/security', heading: '安全中心' },
    { path: '/cloudflare', heading: '域名解析' },
    { path: '/settings', heading: '系统设置' },
    { path: '/admin-settings', heading: '设置' },
    { path: '/admin-update', heading: '更新' },
    { path: '/admin-notify', heading: '通知' },
    { path: '/admin-data', heading: '数据' },
    { path: '/admin-themes', heading: '主题' },
    { path: '/admin-security', heading: '安全' },
  ]

  console.log('--- 验证桌面端直接访问各页面 URL 且绝无 # ---')
  for (const item of desktopPages) {
    await page.goto(baseURL + item.path)
    await page.waitForTimeout(300)
    const currentUrl = page.url()
    expect(currentUrl, `页面 ${item.path} URL 不应包含 #`).not.toContain('#')
    expect(currentUrl).toBe(baseURL + item.path)
    await expect(page.getByRole('heading', { name: item.heading, level: 1 }), `页面 ${item.path} 应渲染对应标题 ${item.heading}`).toBeVisible()
    await expect(page.locator('.page-shell'), `页面 ${item.path} 结构完整`).toHaveCount(1)
  }

  // 4. 验证带查询参数的 URL 无 #
  console.log('--- 验证带查询参数的页面 URL 无 # ---')
  await page.goto(baseURL + '/connection-history?ip=1.1.1.1&protocol=tcp')
  await page.waitForTimeout(300)
  expect(page.url()).toBe(baseURL + '/connection-history?ip=1.1.1.1&protocol=tcp')
  expect(page.url()).not.toContain('#')
  await expect(page.getByRole('heading', { name: '历史连接明细', level: 1 })).toBeVisible()

  // 5. 验证侧边栏点击导航无 #
  console.log('--- 验证侧边栏菜单点击导航无 # ---')
  const sidebarNavigations = [
    { name: '服务器', expectedPath: '/nodes' },
    { name: '运行概览', expectedPath: '/dashboard' },
    { name: '接入服务', expectedPath: '/inbounds' },
    { name: '代理分组', expectedPath: '/proxy-groups' },
    { name: '规则供应商', expectedPath: '/rule-providers' },
    { name: '客户端配置', expectedPath: '/subscriptions' },
    { name: '服务器访问规则', expectedPath: '/routes' },
    { name: '上网出口', expectedPath: '/outbounds' },
    { name: '当前连接', expectedPath: '/connections' },
    { name: '热门设备', expectedPath: '/devices' },
    { name: '历史连接', expectedPath: '/connection-history' },
    { name: '操作记录', expectedPath: '/audit' },
    { name: '安全中心', expectedPath: '/security' },
    { name: '域名解析', expectedPath: '/cloudflare' },
    { name: '系统设置', expectedPath: '/settings' },
  ]

  for (const nav of sidebarNavigations) {
    await page.getByRole('button', { name: nav.name, exact: true }).click()
    await page.waitForTimeout(200)
    expect(page.url(), `点击菜单 ${nav.name} 后 URL 不应包含 #`).not.toContain('#')
    expect(page.url()).toBe(baseURL + nav.expectedPath)
  }

  // 6. 验证服务器页面（/nodes）一键升级所有 Agent 功能
  console.log('--- 验证服务器页面一键升级所有 Agent 功能 ---')

  // 场景 A: 存在待升级节点（8台 demo 节点版本为 1.2.0，最新版为 2026.11.15）时，按钮显示可升级数量
  await page.route('**/api/v1/system/update*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        current_version: '1.2.0',
        latest_version: '2026.11.15',
        update_available: true,
      }),
    })
  })

  await page.goto(baseURL + '/nodes')
  await page.waitForTimeout(500)
  expect(page.url()).toBe(baseURL + '/nodes')
  expect(page.url()).not.toContain('#')

  const upgradeCountBtn = page.getByRole('button', { name: /一键升级所有 Agent \(\d+\)/ })
  await expect(upgradeCountBtn).toBeVisible()

  let upgradeDispatched = false
  await page.route('**/api/v1/nodes/agent/upgrade-all', async (route) => {
    upgradeDispatched = true
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        dispatched: 8,
        skipped: 0,
        failures: [],
        tasks: [
          { id: 'task-batch-1', node_id: 'demo-node-fra-01', type: 'agent.upgrade', status: 'dispatched' },
        ],
      }),
    })
  })

  // 点击触发确认弹窗
  await upgradeCountBtn.click()
  const confirmBox = page.locator('.el-message-box')
  await expect(confirmBox).toBeVisible()
  await expect(confirmBox.getByText('一键升级所有 Agent')).toBeVisible()
  await expect(confirmBox.getByText(/确认将 \d+ 台服务器/)).toBeVisible()

  // 先测试取消
  await confirmBox.getByRole('button', { name: '取消' }).click()
  await expect(confirmBox).not.toBeVisible()
  expect(upgradeDispatched).toBe(false)

  // 再次点击并确认升级
  await upgradeCountBtn.click()
  await expect(confirmBox).toBeVisible()
  await confirmBox.getByRole('button', { name: '开始升级' }).click()
  await page.waitForTimeout(500)
  expect(upgradeDispatched).toBe(true)
  await expect(page.getByText(/已向 \d+ 台服务器下发升级任务/)).toBeVisible()

  // 场景 B: 模拟所有服务器 Agent 均为最新版本，验证提示文案
  console.log('--- 模拟所有 Agent 均已是最新版本时的 UI 表现 ---')
  await page.route('**/api/v1/system/update*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        current_version: '1.2.0',
        latest_version: '1.2.0',
        update_available: false,
      }),
    })
  })
  await page.goto(baseURL + '/nodes')
  await page.waitForTimeout(500)

  const upgradeAllNoCountBtn = page.getByRole('button', { name: /一键升级所有 Agent/ })
  await expect(upgradeAllNoCountBtn).toHaveCount(0)

  // 6.5 验证公共探针与单服务器独立探针 UI 与交互
  console.log('--- 验证公共探针与单服务器独立探针配置 ---')
  await page.getByRole('button', { name: '探针', exact: true }).click()
  const probesDialog = page.locator('.el-dialog').filter({ hasText: '支持配置集群共享的「公共探针」' })
  await expect(probesDialog).toBeVisible()
  await expect(probesDialog.getByText('适用范围')).toBeVisible()

  await probesDialog.getByRole('button', { name: '添加探针' }).click()
  const addProbeDialog = page.locator('.el-dialog.probe-dialog')
  await expect(addProbeDialog).toBeVisible()
  await expect(addProbeDialog.getByText('公共探针（所有服务器共用）')).toBeVisible()
  await expect(addProbeDialog.getByText('单独配置（指定服务器）')).toBeVisible()
  await addProbeDialog.getByPlaceholder(/例如：广西电信/).fill('Cloudflare公共')
  await addProbeDialog.getByPlaceholder(/202\.103\.224\.68:53/).fill('1.1.1.1:443')
  await addProbeDialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(probesDialog.getByText('Cloudflare公共')).toBeVisible()
  await expect(probesDialog.getByText('全部服务器').first()).toBeVisible()
  await probesDialog.locator('.el-dialog__headerbtn').click()
  await expect(probesDialog).not.toBeVisible()

  // 从第一台服务器卡片更多菜单进入「探针配置」
  await page.locator('.openlat-node-card .card-action-btn').first().click()
  await page.getByRole('menuitem', { name: '探针配置' }).click()
  const srvDialog = page.locator('.el-dialog.srv-dialog')
  await expect(srvDialog).toBeVisible()
  await expect(srvDialog.getByRole('tab', { name: /探针配置/ })).toHaveAttribute('aria-selected', 'true')
  await expect(srvDialog.getByText(/服务器自定义探针|服务器特有探针|本机独立探针/)).toBeVisible()

  await srvDialog.getByRole('button', { name: /添加(自定义|特有|独立)探针/ }).click()
  await srvDialog.getByPlaceholder('探针名称（如：广州电信）').fill('本机专属电信探针')
  await srvDialog.getByPlaceholder('目标 host:port（如 1.1.1.1:443）').fill('202.103.224.68:53')
  await page.screenshot({ path: '../artifacts/e2e/verify-node-probes-tab.png' })
  await srvDialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(srvDialog).not.toBeVisible()

  // 再次打开全局探针弹窗确认独立探针已同步并显示适用服务器范围
  await page.getByRole('button', { name: '探针', exact: true }).click()
  await expect(probesDialog).toBeVisible()
  await expect(probesDialog.getByText('本机专属电信探针')).toBeVisible()
  await expect(probesDialog.getByText(/独立探针 \(1\)/)).toBeVisible()
  await page.waitForTimeout(300)
  await page.screenshot({ path: '../artifacts/e2e/verify-global-probes-dialog.png' })
  await probesDialog.locator('.el-dialog__headerbtn').click()
  await expect(probesDialog).not.toBeVisible()

  // 7. 移动端视图完整验证 (Viewport 390x844)
  console.log('--- 验证移动端页面视图与无 # 路由 ---')
  await page.setViewportSize({ width: 390, height: 844 })
  await page.waitForTimeout(400)

  // 移动端各标签页直接访问验证
  const mobilePages = [
    { path: '/nodes' },
    { path: '/dashboard' },
    { path: '/connections' },
    { path: '/subscriptions' },
    { path: '/more' },
  ]

  for (const mPage of mobilePages) {
    await page.goto(baseURL + mPage.path)
    await page.waitForTimeout(300)
    expect(page.url(), `移动端 ${mPage.path} 不应带 #`).not.toContain('#')
    expect(page.url()).toBe(baseURL + mPage.path)
    await expect(page.locator('.m-app')).toBeVisible()
  }

  // 移动端底部导航点击测试
  console.log('--- 验证移动端底部导航点击无 # ---')
  const mobileNavTabs = [
    { label: '概览', expectedPath: '/dashboard' },
    { label: '连接', expectedPath: '/connections' },
    { label: '配置', expectedPath: '/subscriptions' },
    { label: '接入', expectedPath: '/inbounds' },
    { label: '更多', expectedPath: '/more' },
  ]

  for (const tab of mobileNavTabs) {
    await page.locator('.m-app__tab').filter({ hasText: tab.label }).click()
    await page.waitForTimeout(200)
    expect(page.url(), `移动端点击 ${tab.label} 后 URL 不应包含 #`).not.toContain('#')
    expect(page.url()).toBe(baseURL + tab.expectedPath)
  }

  // 移动端通过「更多」页面进入各二级管理功能，验证无 #
  await page.goto(baseURL + '/more')
  const moreSubLinks = [
    { name: '服务器', path: '/nodes' },
    { name: '安全中心', path: '/security' },
    { name: '服务器访问规则', path: '/routes' },
    { name: '上网出口', path: '/outbounds' },
    { name: '代理分组', path: '/proxy-groups' },
    { name: '规则供应商', path: '/rule-providers' },
    { name: '热门设备', path: '/devices' },
    { name: '历史连接', path: '/connection-history' },
    { name: '操作记录', path: '/audit' },
    { name: '域名解析', path: '/cloudflare' },
    { name: '系统设置', path: '/settings' },
  ]

  for (const sub of moreSubLinks) {
    await page.getByRole('button', { name: new RegExp(sub.name) }).first().click()
    await page.waitForTimeout(300)
    expect(page.url(), `移动端进入 ${sub.name} 后 URL 不应包含 #`).not.toContain('#')
    expect(page.url()).toBe(baseURL + sub.path)
    await page.goto(baseURL + '/more')
    await page.waitForTimeout(200)
  }

  // 移动端一键升级 Agent 按钮验证
  console.log('--- 验证移动端一键升级 Agent 按钮 ---')
  await page.route('**/api/v1/system/update*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        current_version: '0.1.0',
        latest_version: '99.0.0',
        update_available: true,
      }),
    })
  })
  await page.goto(baseURL + '/nodes')
  await page.waitForTimeout(400)
  const mobileUpgradeBtn = page.getByRole('button', { name: /升级 Agent \(\d+\)/ })
  await expect(mobileUpgradeBtn).toBeVisible()
  await mobileUpgradeBtn.click()
  const mConfirmBox = page.locator('.el-message-box')
  await expect(mConfirmBox).toBeVisible()
  await mConfirmBox.getByRole('button', { name: '取消' }).click()
  await expect(mConfirmBox).not.toBeVisible()

  // 8. 确保全程控制台无报错
  console.log('Console errors recorded:', consoleErrors)
  expect(consoleErrors).toEqual([])
  console.log('=== 所有页面及一键升级功能亲自验证完全通过！===')
})
