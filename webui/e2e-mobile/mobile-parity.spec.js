import { test, expect } from '@playwright/test'

test.use({
  viewport: { width: 390, height: 844 },
  hasTouch: true,
  isMobile: true,
  userAgent:
    'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1',
})

const targetURL = (path = '') => `${process.env.POLARIS_E2E_BASE_URL || ''}${path}`

async function loginMobile(page) {
  const username = process.env.POLARIS_E2E_ADMIN_USERNAME || 'polaris_admin'
  const changedPwd = process.env.POLARIS_E2E_CHANGED_PASSWORD || 'Mobile-E2E-Password-2026!'
  const initialPwd = process.env.POLARIS_E2E_INITIAL_PASSWORD || '123456'

  await page.goto(targetURL('/dashboard'))
  await expect(page.locator('.m-login')).toBeVisible()

  await page.getByLabel('用户名').fill(username)
  await page.getByLabel('密码').fill(changedPwd)
  await page.getByRole('button', { name: '登录', exact: true }).click()

  const loggedInOrChange = await Promise.race([
    page.locator('.m-page').waitFor({ state: 'visible', timeout: 5000 }).then(() => 'ok').catch(() => null),
    page.getByRole('heading', { name: '首次登录需要修改密码', exact: true }).waitFor({ state: 'visible', timeout: 5000 }).then(() => 'change').catch(() => null),
  ])

  if (!loggedInOrChange) {
    await page.getByLabel('密码').fill(initialPwd)
    await page.getByRole('button', { name: '登录', exact: true }).click()
    const needsChange = await page
      .getByRole('heading', { name: '首次登录需要修改密码', exact: true })
      .waitFor({ state: 'visible', timeout: 5000 })
      .then(() => true)
      .catch(() => false)
    if (needsChange) {
      await page.getByPlaceholder('请输入当前密码').fill(initialPwd)
      await page.getByPlaceholder('至少 12 位，不能与当前密码相同').fill(changedPwd)
      await page.getByPlaceholder('请再次输入新密码').fill(changedPwd)
      await page.getByRole('button', { name: '保存', exact: true }).click()
    }
  }
  await expect(page.locator('.m-page')).toBeVisible({ timeout: 10_000 })
}

test('移动端全功能与 PC 对齐验证：分享/探针/筛选/工具/IP库/历史连接/管理与状态路由', async ({ page, browser }) => {
  const consoleErrors = []
  const httpErrors = []
  let authenticated = false

  page.on('console', (msg) => {
    if (msg.type() === 'error' && !msg.text().startsWith('Failed to load resource:')) consoleErrors.push(msg.text())
  })
  page.on('pageerror', (err) => consoleErrors.push(err.message))
  page.on('response', (res) => {
    const bootstrap401 = !authenticated && res.status() === 401 && (res.url().endsWith('/api/v1/auth/me') || res.url().includes('/api/v1/auth/login'))
    const ignorableSingBox = res.status() === 400 && res.url().includes('/api/v1/sing-box/latest')
    if (res.status() >= 400 && !bootstrap401 && !ignorableSingBox) httpErrors.push(`${res.status()} ${res.url()}`)
  })

  async function expectNoSideways(where) {
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
    expect(overflow, `${where} 出现横向溢出 ${overflow}px`).toBeLessThanOrEqual(0)
  }

  await loginMobile(page)
  authenticated = true

  // 1. 运行概览 + 分享功能 + 只读分享模式无底栏
  await expectNoSideways('概览主页')
  await page.getByRole('button', { name: '分享', exact: true }).click()
  const shareSheet = page.locator('.m-sheet').filter({ hasText: '分享运行概览' })
  await expect(shareSheet).toBeVisible()
  await shareSheet.getByRole('button', { name: '生成分享链接', exact: true }).click()
  const shareURLBox = shareSheet.locator('.m-mono-wrap').first()
  await expect(shareURLBox).toBeVisible()
  const shareURL = (await shareURLBox.innerText()).trim()
  expect(shareURL).toContain('/share/')
  await expectNoSideways('运行概览分享抽屉')

  // 用未登录的手机上下文打开只读分享链接
  const guestCtx = await browser.newContext({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
    userAgent:
      'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1',
  })
  const guestPage = await guestCtx.newPage()
  await guestPage.goto(shareURL)
  await expect(guestPage.locator('.m-page')).toBeVisible()
  await expect(guestPage.locator('.m-app__tabs')).toHaveCount(0)
  const guestOverflow = await guestPage.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(guestOverflow).toBeLessThanOrEqual(0)
  await guestCtx.close()

  // 撤销刚创建的测试分享链接并关闭抽屉
  await shareSheet.getByRole('button', { name: '撤销', exact: true }).first().click()
  await page.getByRole('button', { name: '撤销', exact: true }).last().click()
  await shareSheet.locator('.m-sheet__close').click()

  // 2. 服务器页：汇总卡片、单一地图、移动端原生 MNodeCard / MNodeRow、只读分享指标、排序底部抽屉、探针管理 CRUD、编辑四标签页
  await page.goto(targetURL('/nodes'))
  await expect(page.getByRole('heading', { name: '服务器', exact: true })).toBeVisible()
  await expect(page.locator('.node-map-card')).toHaveCount(1)
  const sortPickerBtn = page.getByRole('button', { name: '排序方式' })
  await expect(sortPickerBtn).toBeVisible()
  await sortPickerBtn.click()
  const sortSheet = page.locator('.m-sheet').filter({ hasText: '排序方式' })
  await expect(sortSheet).toBeVisible()
  await sortSheet.getByRole('button', { name: '按 CPU 占用' }).click()
  await expect(sortSheet).toHaveCount(0)
  await expect(page.getByRole('button', { name: '卡片视图' })).toBeVisible()
  await expect(page.getByRole('button', { name: '列表视图' })).toBeVisible()
  await expectNoSideways('服务器页(含地图与统计卡)')

  const nodeCards = page.locator('.m-node-card')
  await expect(nodeCards.first()).toBeVisible({ timeout: 5000 })
  await expect(nodeCards.first().getByText('24 小时', { exact: true })).toBeVisible()
  await expect(nodeCards.first().getByText('到期', { exact: true })).toBeVisible()
  await expect(nodeCards.first().getByText('厂商', { exact: true })).toBeVisible()
  await expect(nodeCards.first().getByText('续费', { exact: true })).toBeVisible()

  // 触控交互：点流量卡展开明细；点「···」弹出底部操作面板与详情分页
  await nodeCards.first().locator('.m-traffic-meter').click()
  await expect(nodeCards.first().locator('.m-traffic-drawer')).toBeVisible()
  await nodeCards.first().locator('.m-traffic-meter').click()
  await nodeCards.first().getByRole('button', { name: '服务器操作' }).click()
  const nodeActionSheet = page.locator('.m-sheet').last()
  await expect(nodeActionSheet.getByRole('tab', { name: '操作', exact: true })).toBeVisible()
  await nodeActionSheet.getByRole('tab', { name: '详情', exact: true }).click()
  await expect(nodeActionSheet.getByText('本月流量')).toBeVisible()
  await nodeActionSheet.getByRole('button', { name: '关闭', exact: true }).click()

  await page.getByRole('button', { name: '列表视图' }).click()
  await expect(page.locator('.m-node-row').first()).toBeVisible()
  await expectNoSideways('服务器列表视图')
  await page.getByRole('button', { name: '卡片视图' }).click()

  // 服务器只读分享页应正常加载监控指标（不得双倍拼接分享 API 前缀）
  await page.getByRole('button', { name: '分享服务器', exact: true }).click()
  const nodeShareSheet = page.locator('.m-sheet').filter({ hasText: '分享' })
  await expect(nodeShareSheet).toBeVisible()
  await nodeShareSheet.getByRole('button', { name: '生成分享链接', exact: true }).click()
  const nodeShareURL = (await nodeShareSheet.locator('.m-mono-wrap').first().innerText()).trim()
  const nodeShareErrors = []
  const nodeGuestCtx = await browser.newContext({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
  })
  const nodeGuestPage = await nodeGuestCtx.newPage()
  nodeGuestPage.on('response', (res) => {
    if (res.status() >= 400 && !res.url().includes('/sing-box/latest')) {
      nodeShareErrors.push(`${res.status()} ${res.url()}`)
    }
  })
  await nodeGuestPage.goto(nodeShareURL)
  await expect(nodeGuestPage.locator('.m-node-card').first()).toBeVisible({ timeout: 8000 })
  await expect(nodeGuestPage.locator('.m-node-card').first().getByText(/^\d+(\.\d+)?%$/).first()).toBeVisible({ timeout: 8000 })
  await expect(nodeGuestPage.locator('.m-node-card').first().getByText(/^\d+ms$/).first()).toBeVisible({ timeout: 8000 })
  await nodeGuestCtx.close()
  expect(nodeShareErrors, `服务器分享页接口报错：${nodeShareErrors.join(' | ')}`).toEqual([])

  await nodeShareSheet.getByRole('button', { name: '撤销', exact: true }).first().click()
  await page.getByRole('button', { name: '撤销', exact: true }).last().click()
  await nodeShareSheet.locator('.m-sheet__close').click()

  // 验证详情页 (MNodeDetailView)、4 标签页编辑、线路排查入口
  await expect(nodeCards.first()).toBeVisible({ timeout: 5000 })
  await nodeCards.first().click()
  await expect(page.getByRole('button', { name: '线路排查' })).toBeVisible()
  await expect(page.getByRole('tab', { name: '资源', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: '网络延迟', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: '详情', exact: true })).toBeVisible()
  await expectNoSideways('服务器详情页-资源')

  await page.getByRole('tab', { name: '网络延迟', exact: true }).click()
  await expectNoSideways('服务器详情页-网络延迟')
  await page.getByRole('tab', { name: '详情', exact: true }).click()
  await expectNoSideways('服务器详情页-详情')

  await page.getByRole('main').getByRole('button', { name: '配置', exact: true }).click()
  const editSheet = page.locator('.m-sheet').filter({ hasText: '配置服务器' })
  await expect(editSheet).toBeVisible()
  for (const tabLabel of ['基本', '套餐', 'DDNS/IP', '探针']) {
    await editSheet.getByRole('tab', { name: tabLabel, exact: true }).click()
    await expectNoSideways(`服务器编辑-${tabLabel}`)
  }
  await editSheet.getByRole('button', { name: '取消', exact: true }).click()

  await page.getByRole('button', { name: '返回上一页' }).click()
  await expect(page.getByRole('heading', { name: '服务器', exact: true })).toBeVisible()

  // 探针管理 CRUD
  await page.getByRole('button', { name: '探针管理', exact: true }).click()
  const probeSheet = page.locator('.m-sheet').filter({ hasText: '探针管理' })
  await expect(probeSheet).toBeVisible()
  await probeSheet.getByRole('button', { name: '新建探针', exact: true }).click()
  const probeForm = page.locator('.m-sheet').filter({ hasText: '新建探针任务' })
  await probeForm.getByPlaceholder('例如：广州电信').fill('E2E-Probe')
  await probeForm.getByPlaceholder('例如：1.1.1.1 或 example.com:443').fill('1.1.1.1')
  await probeForm.getByRole('button', { name: '保存', exact: true }).click()
  await expect(probeSheet.getByText('E2E-Probe')).toBeVisible()
  await probeSheet.getByRole('button', { name: '删除', exact: true }).first().click()
  await page.getByRole('button', { name: '删除', exact: true }).last().click()
  await expectNoSideways('探针管理抽屉')
  await probeSheet.locator('.m-sheet__close').click()

  // 3. 代理分组策略筛选、上网出口状态筛选、规则供应商自定义代理、路由启用开关、连接刷新
  await page.goto(targetURL('/proxy-groups'))
  await expect(page.getByRole('tab', { name: '全部策略', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: '自动测速', exact: true })).toBeVisible()
  await expectNoSideways('代理分组筛选')

  await page.goto(targetURL('/outbounds'))
  await expect(page.getByRole('tab', { name: '全部状态', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: '启用', exact: true })).toBeVisible()
  await expectNoSideways('上网出口筛选')

  await page.goto(targetURL('/rule-providers'))
  await page.getByRole('button', { name: '新建规则供应商' }).click()
  await expect(page.getByText('支持直接输入自定义节点名')).toBeVisible()
  await page.getByRole('button', { name: /DIRECT/ }).click()
  const proxySheet = page.locator('.m-sheet').filter({ hasText: '选择或输入下载代理' })
  await expect(proxySheet).toBeVisible()
  await proxySheet.getByPlaceholder('搜索或输入自定义值').fill('CustomProxyNode')
  await proxySheet.getByRole('button', { name: /使用自定义值：CustomProxyNode/ }).click()
  await expectNoSideways('规则供应商表单')
  await page.getByRole('button', { name: '取消', exact: true }).click()

  await page.goto(targetURL('/routes'))
  await page.getByRole('button', { name: '新建访问规则' }).click()
  await expect(page.getByText('启用规则')).toBeVisible()
  await expectNoSideways('访问规则表单')
  await page.getByRole('button', { name: '取消', exact: true }).click()

  await page.goto(targetURL('/connections'))
  await page.getByRole('button', { name: '刷新连接', exact: true }).click()
  await expectNoSideways('当前连接')

  // 4. 热门设备 & 历史连接明细独立页面
  await page.goto(targetURL('/devices'))
  await expect(page.getByRole('heading', { name: '热门设备', exact: true })).toBeVisible()
  for (const label of ['历史连接数', '热门设备数 (独立 IP)', '累计下行流量', '累计上行流量']) {
    await expect(page.getByText(label, { exact: true })).toBeVisible()
  }
  await expectNoSideways('热门设备')
  await page.getByRole('button', { name: '历史连接明细', exact: true }).click()
  await expect(page.getByRole('heading', { name: '历史连接明细', exact: true })).toBeVisible()
  expect(page.url()).toContain('/connection-history')
  expect(page.url()).not.toContain('#')
  await expectNoSideways('历史连接明细')
  await page.getByRole('button', { name: '筛选条件', exact: true }).click()
  const chFilter = page.locator('.m-sheet').filter({ hasText: '筛选与排序' })
  await expect(chFilter.getByText('快捷时间范围')).toBeVisible()
  await expectNoSideways('历史连接筛选排序抽屉')
  await chFilter.locator('.m-sheet__close').click()

  // 5. 操作记录三标签页 & 域名解析服务器筛选 + 移动端 DDNS 卡片列表
  await page.goto(targetURL('/audit'))
  await expect(page.getByRole('heading', { name: '操作记录', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: /DDNS 变更/ }).click()
  await expect(page.getByPlaceholder('搜索域名 / 服务器 / IP')).toBeVisible()
  await expectNoSideways('操作记录-DDNS变更')

  await page.goto(targetURL('/cloudflare'))
  await page.getByRole('tab', { name: 'DDNS记录', exact: true }).click()
  await expect(page.locator('.el-table__body')).toHaveCount(0)
  await expectNoSideways('域名解析-DDNS记录')

  // 6. 系统设置直达 ?tab=alerts、脏状态撤销、管理账户状态筛选、网络工具 & 高精度 IP 库
  await page.goto(targetURL('/settings?tab=alerts'))
  await expect(page.getByText(/项告警启用/)).toBeVisible()
  await page.locator('.el-switch').first().click()
  const undoBtn = page.getByRole('button', { name: '撤销', exact: true })
  if (await undoBtn.isVisible().catch(() => false)) {
    await expect(page.getByText('未保存')).toBeVisible()
    await undoBtn.click()
    await expect(page.getByText('未保存')).toHaveCount(0)
  }
  await expectNoSideways('系统设置-通知')

  await page.getByRole('tab', { name: '账户', exact: true }).click()
  await expect(page.getByRole('tab', { name: '停用', exact: true })).toBeVisible()
  await expectNoSideways('系统设置-账户')

  await page.getByRole('tab', { name: '工具', exact: true }).click()
  await expect(page.getByText('Reality 域名伪装检测')).toBeVisible()
  await expectNoSideways('系统设置-网络工具')

  await page.getByRole('tab', { name: 'IP 库', exact: true }).click()
  await expect(page.getByText('IP 归属地数据库状态')).toBeVisible()
  await page.getByPlaceholder('输入 IPv4 / IPv6').fill('8.8.8.8')
  await page.getByRole('button', { name: '查询', exact: true }).click()
  await expect(page.locator('.m-mono-wrap').getByText('8.8.8.8')).toBeVisible()
  await expectNoSideways('系统设置-IP库')

  // 7. 实时节点状态（移动端分组/排序/列表/详情） & 6 个 admin-* 管理路由
  await page.goto(targetURL('/status'))
  await expect(page.getByRole('heading', { name: '实时节点状态' })).toBeVisible()
  await expect(page.locator('.m-node-card').first()).toBeVisible({ timeout: 5000 })
  await page.getByRole('button', { name: '分组方式' }).click()
  const grpSheet = page.locator('.m-sheet').filter({ hasText: '分组方式' })
  await grpSheet.getByRole('button', { name: '按地区分组' }).click()
  await expectNoSideways('实时节点状态看板-分组')
  await page.getByRole('button', { name: '列表视图' }).click()
  await expect(page.locator('.m-node-row').first()).toBeVisible()
  await page.getByRole('button', { name: '卡片视图' }).click()
  await expectNoSideways('实时节点状态看板')

  for (const [path, headingText] of [
    ['/admin-data', '数据备份与维护'],
    ['/admin-security', '账户安全 (2FA)'],
    ['/admin-themes', '外观与主题'],
    ['/admin-notify', '通知与告警管理'],
    ['/admin-settings', '系统综合设置'],
    ['/admin-update', 'Master 版本更新'],
  ]) {
    await page.goto(targetURL(path))
    await expect(page.getByRole('heading', { name: headingText })).toBeVisible()
    await expectNoSideways(path)
  }

  // 切换一次主题并恢复，验证零报错
  await page.goto(targetURL('/admin-themes'))
  await page.getByRole('button', { name: '浅色主题', exact: true }).click()
  await page.getByRole('button', { name: '深色主题', exact: true }).click()

  expect(consoleErrors, `控制台报错：${consoleErrors.join(' | ')}`).toEqual([])
  expect(httpErrors, `接口报错：${httpErrors.join(' | ')}`).toEqual([])
})
