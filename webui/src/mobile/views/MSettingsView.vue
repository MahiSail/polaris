<script setup>
import { computed, inject, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh, Search, Upload } from '@element-plus/icons-vue'
import { api, post, put, uploadFile } from '../../api'
import { formatDateTime, includesText } from '../../format'
import MPage from '../components/MPage.vue'
import MSegmented from '../components/MSegmented.vue'
import MSheet from '../components/MSheet.vue'
import MPicker from '../components/MPicker.vue'
import MActionSheet from '../components/MActionSheet.vue'

const isAdmin = inject('isAdmin')
const loading = ref(false)

const initialTab = () => {
  const qTab = new URLSearchParams(location.search || '').get('tab')
  if (qTab && ['operators', 'alerts', 'tools', 'ipdb'].includes(qTab)) return qTab
  const hash = location.hash || ''
  if (hash.includes('alerts')) return 'alerts'
  if (hash.includes('tools')) return 'tools'
  if (hash.includes('ipdb')) return 'ipdb'
  return isAdmin.value ? 'operators' : 'tools'
}
const tab = ref(initialTab())
const operators = ref([])
const operatorSheet = ref(false)
const operator = reactive({ username: '', password: '', role: 'operator' })
const operatorKeyword = ref('')
const operatorStatus = ref('')
const actionsOpen = ref(false)
const actionTarget = ref(null)
const roleSheet = ref(false)
const roleTarget = ref(null)
const roleDraft = ref('operator')

// 通知设置相关状态
const alertSettings = reactive({
  bark_server: 'https://api.day.app',
  bark_device_key: '',
  bark_sound: 'minuet.caf',
  bark_group: 'Polaris',
  telegram_enabled: false,
  telegram_server: 'https://api.telegram.org',
  telegram_bot_token: '',
  telegram_chat_id: '',
  traffic_alert_enabled: true,
  traffic_threshold_mbps: 150,
  traffic_duration_sec: 10,
  conn_alert_enabled: true,
  conn_threshold_count: 200,
  single_ip_alert_enabled: true,
  single_ip_threshold_count: 60,
  offline_alert_enabled: true,
  cooldown_minutes: 15,
  scan_alert_enabled: false,
})
const alertSaving = ref(false)
const testSending = ref(false)
const telegramReady = computed(() => Boolean(
  alertSettings.telegram_enabled
  && alertSettings.telegram_bot_token.trim()
  && alertSettings.telegram_chat_id.trim(),
))
const notifierReady = computed(() => Boolean(alertSettings.bark_device_key.trim()) || telegramReady.value)

const soundOptions = [
  { value: 'minuet.caf', label: 'minuet (清脆小步舞曲 - 推荐)', desc: '轻快清爽，推荐日常使用' },
  { value: 'anticipate.caf', label: 'anticipate (轻快期待)', desc: '轻柔提示' },
  { value: 'bell.caf', label: 'bell (经典铃声)', desc: '清脆经典' },
  { value: 'glass.caf', label: 'glass (清澈水滴)', desc: '温和水滴音' },
  { value: 'horn.caf', label: 'horn (警示号角)', desc: '突出警示' },
  { value: 'calypso.caf', label: 'calypso (欢快击鼓)', desc: '节奏欢快' },
  { value: 'telegraph.caf', label: 'telegraph (电报码)', desc: '极简电报滴答声' },
  { value: 'silence.caf', label: 'silence (静音仅震动)', desc: '不发出声音，仅系统震动' },
]

const roles = [
  { value: 'admin', label: '管理员' },
  { value: 'operator', label: '运维人员' },
  { value: 'viewer', label: '只读用户' },
]
const roleNames = Object.fromEntries(roles.map((role) => [role.value, role.label]))
const filteredOperators = computed(() => operators.value.filter((row) => {
  if (operatorStatus.value && String(Boolean(row.enabled)) !== operatorStatus.value) return false
  return includesText([row.username, roleNames[row.role]], operatorKeyword.value)
}))
const tabs = computed(() => (isAdmin.value
  ? [
      { value: 'operators', label: '账户' },
      { value: 'alerts', label: '通知' },
      { value: 'tools', label: '工具' },
      { value: 'ipdb', label: 'IP 库' },
    ]
  : [{ value: 'tools', label: '网络工具' }]))

const barkReady = computed(() => Boolean(alertSettings.bark_device_key.trim()))
const channelSummary = computed(() => {
  const channels = []
  if (barkReady.value) channels.push('Bark')
  if (telegramReady.value) channels.push('Telegram')
  return channels.length ? `${channels.join(' + ')} 通道已就绪` : '尚未配置推送通道'
})
const enabledRuleCount = computed(() => [
  alertSettings.traffic_alert_enabled,
  alertSettings.conn_alert_enabled,
  alertSettings.single_ip_alert_enabled,
  alertSettings.offline_alert_enabled,
  alertSettings.scan_alert_enabled,
].filter(Boolean).length)

const savedAlertState = ref(null)
const alertSnapshot = () => JSON.stringify(alertSettings)
const isAlertDirty = computed(() => savedAlertState.value !== null && alertSnapshot() !== savedAlertState.value)
function markAlertSaved() {
  savedAlertState.value = alertSnapshot()
}
function revertAlertSettings() {
  if (!savedAlertState.value) return
  Object.assign(alertSettings, JSON.parse(savedAlertState.value))
  ElMessage.info('已撤销未保存的修改')
}

// Reality 检测工具
const realityForm = reactive({ target: 'gateway.icloud.com', port: 443 })
const realityChecking = ref(false)
const realityResult = ref(null)

async function checkReality() {
  const target = (realityForm.target || '').trim()
  if (!target) return ElMessage.warning('请输入待检测的域名')
  realityChecking.value = true
  try {
    const res = await post('/tools/reality-check', {
      target,
      port: Number(realityForm.port) || 443,
    })
    realityResult.value = res?.reality_check || res?.report || res
  } catch (err) {
    ElMessage.error(err?.message || '检测失败')
  } finally {
    realityChecking.value = false
  }
}

// 高精度 MMDB IP 库
const ipdbStatus = reactive({
  mmdb_loaded: false,
  mmdb_path: '',
  mmdb_type: '',
  ip2region_loaded: true,
})
const ipdbDownloadURL = ref('')
const ipdbLoading = ref(false)
const ipReloading = ref(false)
const mmdbUploadInput = ref(null)
const ipTestAddr = ref('8.8.8.8')
const ipTestLoading = ref(false)
const ipTestResult = ref(null)

async function loadIPDBStatus() {
  if (!isAdmin.value) return
  try {
    const res = await api('/ip/database')
    Object.assign(ipdbStatus, res)
  } catch {
    // ignore
  }
}

async function downloadMMDB() {
  try {
    await ElMessageBox.confirm('确定从镜像源在线下载 GeoLite2-City.mmdb 并热加载吗？', '下载 IP 数据库', {
      type: 'info',
      confirmButtonText: '下载',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  ipdbLoading.value = true
  try {
    const body = ipdbDownloadURL.value.trim() ? { url: ipdbDownloadURL.value.trim() } : {}
    const res = await post('/ip/database/download', body)
    if (res?.success) {
      Object.assign(ipdbStatus, res)
      ElMessage.success('MMDB 下载并加载成功')
    } else {
      ElMessage.error(res?.error || '下载失败')
    }
  } catch (err) {
    ElMessage.error(err?.message || '下载 MMDB 失败')
  } finally {
    ipdbLoading.value = false
  }
}

async function reloadIPDB() {
  ipReloading.value = true
  try {
    const res = await post('/ip/database/reload', {})
    if (res?.success) {
      Object.assign(ipdbStatus, res)
      ElMessage.success('MMDB 重新加载成功')
    } else {
      ElMessage.warning(res?.error || '未找到可用 MMDB 文件')
    }
  } catch (err) {
    ElMessage.error(err?.message || '重新加载失败')
  } finally {
    ipReloading.value = false
  }
}

async function onMMDBFileSelected(event) {
  const file = event.target?.files?.[0]
  if (event.target) event.target.value = ''
  if (!file) return
  ipdbLoading.value = true
  try {
    const formData = new FormData()
    formData.append('file', file)
    const res = await uploadFile('/ip/database/upload', formData)
    if (res?.success) {
      Object.assign(ipdbStatus, res)
      ElMessage.success('MMDB 上传并生效成功')
    } else {
      ElMessage.error(res?.error || '上传失败')
    }
  } catch (err) {
    ElMessage.error(err?.message || '上传 MMDB 失败')
  } finally {
    ipdbLoading.value = false
  }
}

async function lookupTestIP() {
  const ip = (ipTestAddr.value || '').trim()
  if (!ip) return ElMessage.warning('请输入要查询的 IP')
  ipTestLoading.value = true
  try {
    ipTestResult.value = await api(`/ip/lookup?ip=${encodeURIComponent(ip)}`)
  } catch (err) {
    ElMessage.error(err?.message || 'IP 查询失败')
  } finally {
    ipTestLoading.value = false
  }
}

function syncTabFromLocation() {
  const qTab = new URLSearchParams(location.search || '').get('tab')
  if (qTab && ['operators', 'alerts', 'tools', 'ipdb'].includes(qTab)) {
    tab.value = !isAdmin.value && qTab !== 'tools' ? 'tools' : qTab
    return
  }
  const hash = location.hash || ''
  if (hash.includes('alerts') && isAdmin.value) tab.value = 'alerts'
  else if (hash.includes('tools')) tab.value = 'tools'
  else if (hash.includes('ipdb') && isAdmin.value) tab.value = 'ipdb'
  else if (hash.includes('operators') && isAdmin.value) tab.value = 'operators'
}

async function load() {
  loading.value = true
  try {
    const [operatorResult, alertsResult] = await Promise.all([
      isAdmin.value ? api('/operators').catch(() => ({ operators: [] })) : Promise.resolve({ operators: [] }),
      isAdmin.value ? api('/alerts/settings').catch(() => ({ settings: null })) : Promise.resolve({ settings: null }),
    ])
    operators.value = operatorResult.operators || []
    if (alertsResult?.settings) {
      Object.assign(alertSettings, alertsResult.settings)
      if (alertSettings.bark_sound && !alertSettings.bark_sound.endsWith('.caf')) {
        alertSettings.bark_sound = alertSettings.bark_sound + '.caf'
      }
    }
    markAlertSaved()
    if (isAdmin.value) await loadIPDBStatus()
  } finally { loading.value = false }
}

async function saveAlertSettings() {
  alertSaving.value = true
  try {
    const res = await put('/alerts/settings', alertSettings)
    if (res?.settings) {
      Object.assign(alertSettings, res.settings)
    }
    markAlertSaved()
    ElMessage.success('通知设置已保存')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '保存失败')
  } finally {
    alertSaving.value = false
  }
}

async function sendTestAlert() {
  if (!notifierReady.value) {
    return ElMessage.warning('请先填写 Bark Device Key，或启用并配置 Telegram')
  }
  testSending.value = true
  try {
    if (isAlertDirty.value) {
      await put('/alerts/settings', alertSettings)
      markAlertSaved()
    }
    const res = await post('/alerts/test', {
      server: alertSettings.bark_server,
      device_key: alertSettings.bark_device_key,
      sound: alertSettings.bark_sound,
      group: alertSettings.bark_group,
      telegram_server: alertSettings.telegram_server,
      telegram_bot_token: alertSettings.telegram_bot_token,
      telegram_chat_id: alertSettings.telegram_chat_id,
    })
    ElMessage.success(res?.message || '测试通知已发出')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '发送测试通知失败')
  } finally {
    testSending.value = false
  }
}

function openOperator() {
  Object.assign(operator, { username: '', password: '', role: 'operator' })
  operatorSheet.value = true
}

async function saveOperator() {
  await post('/operators', operator)
  operatorSheet.value = false
  ElMessage.success('账户已创建，用户首次登录时需要修改初始密码')
  await load()
}

const details = computed(() => {
  const row = actionTarget.value
  if (!row) return []
  return [
    { label: '权限', value: roleNames[row.role] || row.role },
    { label: '状态', value: row.enabled ? '启用' : '停用' },
    { label: '登录方式', value: row.totp_enabled ? '密码 + 两步验证' : '仅密码' },
    { label: '密码', value: row.must_change_password ? '等待本人修改' : '已设置' },
    { label: '最后登录', value: formatDateTime(row.last_login_at, '从未登录') },
  ]
})

function openActions(row) {
  actionTarget.value = row
  actionsOpen.value = true
}

const actions = computed(() => {
  const row = actionTarget.value
  if (!row) return []
  const list = [
    { key: 'role', label: '修改权限', hint: `当前：${roleNames[row.role]}` },
    { key: 'state', label: row.enabled ? '停用账户' : '启用账户' },
    { key: 'password', label: '重置密码' },
  ]
  if (row.totp_enabled) list.push({ key: 'totp', label: '关闭两步验证', danger: true })
  return list
})

function runAction(key) {
  const row = actionTarget.value
  if (key === 'role') return changeRole(row)
  if (key === 'state') return setOperatorState(row)
  if (key === 'password') return resetPassword(row)
  if (key === 'totp') return disableOperatorTOTP(row)
}

function changeRole(row) {
  roleTarget.value = row
  roleDraft.value = row.role
  roleSheet.value = true
}

async function saveRole() {
  await put(`/operators/${roleTarget.value.id}`, { role: roleDraft.value, enabled: roleTarget.value.enabled })
  roleSheet.value = false
  ElMessage.success('权限已修改')
  await load()
}

async function setOperatorState(row) {
  await put(`/operators/${row.id}`, { role: row.role, enabled: !row.enabled })
  await load()
}

async function resetPassword(row) {
  const result = await ElMessageBox.prompt(`为 ${row.username} 设置新的初始密码`, '重置密码', {
    inputType: 'password',
    inputValidator: (value) => value.length >= 12 || '密码至少 12 位',
  })
  await post(`/operators/${row.id}/password`, { password: result.value })
  ElMessage.success('密码已重置，用户下次登录时需要修改密码')
  await load()
}

async function disableOperatorTOTP(row) {
  await ElMessageBox.confirm(`关闭 ${row.username} 的两步验证？`, '关闭两步验证', { type: 'warning' })
  await post(`/operators/${row.id}/totp/reset`, {})
  ElMessage.success('两步验证已关闭')
  await load()
}

onMounted(() => {
  syncTabFromLocation()
  window.addEventListener('popstate', syncTabFromLocation)
  window.addEventListener('hashchange', syncTabFromLocation)
  load()
})

onBeforeUnmount(() => {
  window.removeEventListener('popstate', syncTabFromLocation)
  window.removeEventListener('hashchange', syncTabFromLocation)
})
</script>

<template>
  <MPage title="系统设置" :loading="loading">
    <MSegmented v-model="tab" :options="tabs" />

    <template v-if="tab === 'operators' && isAdmin">
      <div class="m-stack" style="gap: 8px; margin-bottom: 10px;">
        <el-input v-model="operatorKeyword" clearable :prefix-icon="Search" placeholder="搜索用户名或权限" />
        <MSegmented
          v-model="operatorStatus"
          :options="[{ value: '', label: '全部状态' }, { value: 'true', label: '启用' }, { value: 'false', label: '停用' }]"
        />
      </div>
      <div class="m-count">共 {{ filteredOperators.length }} 个账户</div>
      <article v-for="row in filteredOperators" :key="row.id" class="m-item" :class="{ 'is-off': !row.enabled }">
        <button type="button" class="m-item__hit" @click="openActions(row)">
          <div class="m-item__head">
            <span class="m-item__title">{{ row.username }}</span>
            <span v-if="row.must_change_password" class="m-pill m-pill--warning">待改密码</span>
            <span v-if="!row.enabled" class="m-pill m-pill--info">停用</span>
          </div>
          <div class="m-item__stats">
            <span class="m-stat"><b>{{ roleNames[row.role] || row.role }}</b><small>权限</small></span>
            <span class="m-stat"><b>{{ row.totp_enabled ? '两步验证' : '仅密码' }}</b><small>登录方式</small></span>
          </div>
          <div class="m-item__meta">最后登录 {{ formatDateTime(row.last_login_at, '从未登录') }}</div>
        </button>
      </article>
      <div v-if="!filteredOperators.length && !loading" class="m-empty">还没有管理账户</div>
    </template>

    <template v-else-if="tab === 'alerts' && isAdmin">
      <div class="m-sticky-bar m-sticky-bar--top">
        <div class="m-sticky-bar__row">
          <div style="font-size: 12px; color: var(--sb-text-2);">
            <span>{{ channelSummary }}</span>
            <span> · {{ enabledRuleCount }} / 5 项告警启用</span>
          </div>
          <el-tag v-if="isAlertDirty" type="warning" size="small" effect="light">未保存</el-tag>
        </div>
        <div class="m-filter-row" style="justify-content: flex-end;">
          <el-button v-if="isAlertDirty" @click="revertAlertSettings">撤销</el-button>
          <el-button :loading="testSending" :disabled="!notifierReady" @click="sendTestAlert">发送测试</el-button>
          <el-button type="primary" :loading="alertSaving" :disabled="!isAlertDirty" @click="saveAlertSettings">保存</el-button>
        </div>
      </div>
      <!-- Bark 推送配置 -->
      <section class="m-card">
        <div class="m-card__top">
          <span class="m-card__title">Bark 消息推送</span>
          <el-switch v-model="alertSettings.offline_alert_enabled" active-text="离线警报" />
        </div>
        <div class="m-card__note">
          以简洁的 iOS 系统通知推送关键信息，按告警类型自动分组，并严格遵循 iPhone 物理静音开关。
        </div>

        <div class="m-field" style="margin-top: 14px">
          <label class="m-field__label">Bark 服务器地址</label>
          <el-input v-model="alertSettings.bark_server" placeholder="https://api.day.app" clearable />
        </div>

        <div class="m-field">
          <label class="m-field__label">Device Key (设备密钥) <em>*</em></label>
          <el-input v-model="alertSettings.bark_device_key" show-password placeholder="Bark App 中的专属 Key" clearable />
          <div class="m-field__hint">打开手机 Bark App 查看主页生成的 Device Key。</div>
        </div>

        <div class="m-field">
          <label class="m-field__label">默认提示铃声</label>
          <MPicker v-model="alertSettings.bark_sound" :options="soundOptions" title="选择默认提示铃声" />
          <div class="m-field__hint">阻断、离线、爆破等事件会自动使用专属音效（alarm、horn 等）加以区分。</div>
        </div>

        <div class="m-field">
          <label class="m-field__label">推送分组 (Group)</label>
          <el-input v-model="alertSettings.bark_group" placeholder="Polaris" clearable />
        </div>

        <div class="m-card-actions">
          <el-button :loading="testSending" @click="sendTestAlert">测试推送</el-button>
          <el-button type="primary" :loading="alertSaving" @click="saveAlertSettings">保存配置</el-button>
        </div>
      </section>

      <!-- Telegram 推送配置 -->
      <section class="m-card">
        <div class="m-card__top">
          <span class="m-card__title">Telegram 消息推送</span>
          <el-switch v-model="alertSettings.telegram_enabled" />
        </div>
        <div class="m-card__note">
          通过 Telegram 机器人发送富文本告警，包含字段化详情与处理建议，可与 Bark 同时启用。
        </div>

        <div class="m-field" style="margin-top: 14px">
          <label class="m-field__label">Bot API 地址</label>
          <el-input v-model="alertSettings.telegram_server" placeholder="https://api.telegram.org" clearable />
          <div class="m-field__hint">使用官方服务保持默认；自建 Bot API 或反向代理时填写完整地址。</div>
        </div>

        <div class="m-field">
          <label class="m-field__label">Bot Token <em>*</em></label>
          <el-input v-model="alertSettings.telegram_bot_token" show-password placeholder="由 @BotFather 生成的 Token" clearable />
        </div>

        <div class="m-field">
          <label class="m-field__label">Chat ID <em>*</em></label>
          <el-input v-model="alertSettings.telegram_chat_id" placeholder="例如 123456789 或 -1001234567890" clearable />
          <div class="m-field__hint">私聊为数字 ID，群组通常以 -100 开头；公开频道也可填 @频道名。</div>
        </div>
      </section>

      <!-- 瞬时指标与阈值预警 -->
      <section class="m-card">
        <div class="m-card__top">
          <span class="m-card__title">瞬时流量与连接数预警</span>
          <el-switch v-model="alertSettings.traffic_alert_enabled" active-text="监控开启" />
        </div>
        <div class="m-card__note">
          实时监控服务器瞬时带宽突发、连接数激增及单一 IP 突发高并发。
        </div>

        <div class="m-field" style="margin-top: 14px">
          <label class="m-field__label">瞬时带宽突发预警 (Mbps)</label>
          <el-input-number v-model="alertSettings.traffic_threshold_mbps" :min="0" :max="10000" :step="10" style="width: 100%" />
          <div class="m-field__hint">0 表示不限制；持续达到观察窗口即触发告警。</div>
        </div>

        <div class="m-field">
          <label class="m-field__label">带宽突发判定窗口 (秒)</label>
          <el-input-number v-model="alertSettings.traffic_duration_sec" :min="5" :max="300" :step="5" style="width: 100%" />
          <div class="m-field__hint">防止网络瞬间波动误报，推荐 10 ~ 60 秒。</div>
        </div>

        <div class="m-field">
          <label class="m-field__label">服务器瞬时连接总数上限</label>
          <el-input-number v-model="alertSettings.conn_threshold_count" :min="10" :max="50000" :step="50" style="width: 100%" />
          <div class="m-field__hint">全服务器活跃连接总数激增预警。</div>
        </div>

        <div class="m-field">
          <label class="m-field__label">单个 IP 突发并发上限</label>
          <el-input-number v-model="alertSettings.single_ip_threshold_count" :min="5" :max="5000" :step="10" style="width: 100%" />
          <div class="m-field__hint">防范单一来源 IP 大量恶意占用并发。</div>
        </div>

        <div class="m-field m-field--inline" style="padding: 6px 0">
          <label class="m-field__label">
            异常网络扫描预警
            <span class="m-field__hint">探测多端口嗅探、高危服务端口探测与纯 IP 扫段，已彻底豁免正常网页浏览</span>
          </label>
          <el-switch v-model="alertSettings.scan_alert_enabled" />
        </div>

        <div class="m-field">
          <label class="m-field__label">重复报警静默冷却 (分钟)</label>
          <el-input-number v-model="alertSettings.cooldown_minutes" :min="1" :max="1440" :step="5" style="width: 100%" />
          <div class="m-field__hint">同一服务器同一类型的告警在此期间不重复轰炸。</div>
        </div>

        <div class="m-card-actions">
          <el-button type="primary" :loading="alertSaving" @click="saveAlertSettings">保存配置</el-button>
        </div>
      </section>

      <section class="m-card">
        <div class="m-card__top">
          <span class="m-card__title">每日简报</span>
        </div>
        <div class="m-card__note">
          每天 09:00 自动发送昨日简报，包含各服务器网络质量、活跃设备排行、热门访问目标、服务器使用排行与流量汇总。
        </div>
      </section>
    </template>

    <!-- 网络工具 (Reality 检测) -->
    <template v-else-if="tab === 'tools'">
      <section class="m-card">
        <div class="m-card__top">
          <span class="m-card__title">Reality 域名伪装检测</span>
        </div>
        <div class="m-card__note" style="margin-bottom: 10px;">
          在服务端实测目标网站 TLS 1.3、ALPN (h2) 与证书状态，判断是否适合作为 VLESS Reality SNI 伪装目标。
        </div>

        <div class="m-field">
          <label class="m-field__label">待测域名</label>
          <el-input v-model="realityForm.target" placeholder="例如：gateway.icloud.com" clearable />
        </div>
        <div class="m-field">
          <label class="m-field__label">端口</label>
          <el-input-number v-model="realityForm.port" :min="1" :max="65535" style="width: 100%;" />
        </div>
        <el-button type="primary" :loading="realityChecking" style="width: 100%;" @click="checkReality">
          开始检测
        </el-button>

        <div v-if="realityResult" class="m-stack" style="margin-top: 14px;">
          <div class="m-filter-bar">
            <div style="display: flex; align-items: center; gap: 6px; min-width: 0;">
              <strong class="m-mono-wrap">{{ realityResult.target || realityResult.domain }}</strong>
              <el-tag :type="realityResult.compatible ? 'success' : 'danger'" size="small">
                {{ realityResult.compatible ? '可用' : '不建议' }}
              </el-tag>
            </div>
            <span class="m-note">{{ realityResult.latency_ms }} ms</span>
          </div>

          <div class="m-kv-grid">
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">解析 IP</span>
              <span class="m-kv-cell__value m-mono-wrap">{{ realityResult.ip || '—' }}</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">TLS 1.3</span>
              <span class="m-kv-cell__value">{{ realityResult.tls_13 ?? realityResult.tls13 ? '支持' : '不支持' }}</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">协商 ALPN</span>
              <span class="m-kv-cell__value">{{ (realityResult.alpn || []).join(', ') || '无' }}</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">证书剩余天数</span>
              <span class="m-kv-cell__value">{{ realityResult.cert_days_left ?? '—' }} 天</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">证书颁发机构</span>
              <span class="m-kv-cell__value">{{ realityResult.cert_issuer || realityResult.issuer || '—' }}</span>
            </div>
          </div>

          <el-alert
            v-if="realityResult.recommendation || realityResult.verdict"
            :title="realityResult.recommendation || realityResult.verdict"
            :type="realityResult.compatible ? 'success' : 'warning'"
            show-icon
            :closable="false"
          />
          <div v-if="(realityResult.warnings || []).length" class="m-note" style="color: var(--sb-warning);">
            <div v-for="(w, idx) in realityResult.warnings" :key="idx">• {{ w }}</div>
          </div>
        </div>
      </section>
    </template>

    <!-- 高精度 IP 库 (MMDB) -->
    <template v-else-if="tab === 'ipdb' && isAdmin">
      <input
        ref="mmdbUploadInput"
        type="file"
        accept=".mmdb"
        style="display: none;"
        @change="onMMDBFileSelected"
      />

      <section class="m-card">
        <div class="m-card__top" style="margin-bottom: 8px;">
          <span class="m-card__title">IP 归属地数据库状态</span>
          <el-tag :type="ipdbStatus.mmdb_loaded ? 'success' : 'warning'" size="small" effect="light">
            {{ ipdbStatus.mmdb_loaded ? 'MMDB 已加载' : '内置离线库' }}
          </el-tag>
        </div>

        <div class="m-kv-grid" style="margin-bottom: 12px;">
          <div class="m-kv-cell">
            <span class="m-kv-cell__label">数据库类型</span>
            <span class="m-kv-cell__value m-mono-wrap">{{ ipdbStatus.mmdb_type || '—' }}</span>
          </div>
          <div class="m-kv-cell">
            <span class="m-kv-cell__label">ip2region 兜底</span>
            <span class="m-kv-cell__value">{{ ipdbStatus.ip2region_loaded ? '可用' : '未加载' }}</span>
          </div>
          <div class="m-kv-cell" style="grid-column: 1 / -1;">
            <span class="m-kv-cell__label">MMDB 路径</span>
            <span class="m-kv-cell__value m-mono-wrap">{{ ipdbStatus.mmdb_path || '未配置' }}</span>
          </div>
        </div>

        <div class="m-field">
          <label class="m-field__label">下载镜像 URL（留空使用官方默认源）</label>
          <el-input v-model="ipdbDownloadURL" placeholder="https://.../GeoLite2-City.mmdb" clearable />
        </div>

        <div class="m-stack" style="gap: 8px;">
          <el-button type="primary" :loading="ipdbLoading" style="width: 100%;" @click="downloadMMDB">
            在线下载最新 GeoLite2-City.mmdb
          </el-button>
          <div class="m-filter-row">
            <el-button :icon="Upload" :loading="ipdbLoading" style="flex: 1;" @click="mmdbUploadInput?.click()">
              上传 MMDB
            </el-button>
            <el-button :icon="Refresh" :loading="ipReloading" style="flex: 1;" @click="reloadIPDB">
              重新加载
            </el-button>
          </div>
        </div>
      </section>

      <section class="m-card" style="margin-top: 12px;">
        <div class="m-card__top" style="margin-bottom: 8px;">
          <span class="m-card__title">IP 解析测试</span>
        </div>
        <div class="m-filter-row" style="margin-bottom: 10px;">
          <el-input v-model="ipTestAddr" placeholder="输入 IPv4 / IPv6" style="flex: 1;" @keyup.enter="lookupTestIP" />
          <el-button type="primary" :loading="ipTestLoading" @click="lookupTestIP">查询</el-button>
        </div>

        <div v-if="ipTestResult" class="m-stack" style="gap: 6px;">
          <div class="m-filter-bar">
            <strong class="m-mono-wrap">{{ ipTestResult.ip }}</strong>
            <el-tag size="small" effect="plain" :type="ipTestResult.source === 'mmdb' ? 'success' : 'info'">
              {{ ipTestResult.source || 'ip2region' }}
            </el-tag>
          </div>
          <div class="m-kv-grid">
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">归属地</span>
              <span class="m-kv-cell__value">{{ ipTestResult.location || '—' }}</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">国家 / 城市</span>
              <span class="m-kv-cell__value">{{ [ipTestResult.country, ipTestResult.province, ipTestResult.city].filter(Boolean).join(' · ') || '—' }}</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">ISP / ASN</span>
              <span class="m-kv-cell__value">{{ [ipTestResult.isp, ipTestResult.asn].filter(Boolean).join(' · ') || '—' }}</span>
            </div>
          </div>
        </div>
      </section>
    </template>

    <MActionSheet v-model="actionsOpen" :title="actionTarget?.username" :details="details" :actions="actions" @select="runAction" />

    <MSheet v-model="roleSheet" :title="`修改 ${roleTarget?.username || ''} 的权限`">
      <div class="m-field">
        <label class="m-field__label">权限</label>
        <MPicker v-model="roleDraft" :options="roles" title="选择权限" />
      </div>
      <template #footer>
        <el-button @click="roleSheet = false">取消</el-button>
        <el-button type="primary" @click="saveRole">保存</el-button>
      </template>
    </MSheet>

    <MSheet v-model="operatorSheet" title="新建管理账户">
      <div class="m-field">
        <label class="m-field__label">用户名</label>
        <el-input v-model="operator.username" aria-label="用户名" placeholder="3 至 64 位，可用字母、数字、点、下划线和短横线" />
      </div>
      <div class="m-field">
        <label class="m-field__label">初始密码</label>
        <el-input v-model="operator.password" type="password" show-password aria-label="初始密码" />
        <div class="m-field__hint">用户首次登录时必须修改此密码，至少 12 位。</div>
      </div>
      <div class="m-field">
        <label class="m-field__label">权限</label>
        <MPicker v-model="operator.role" :options="roles" title="选择权限" />
      </div>
      <template #footer>
        <el-button @click="operatorSheet = false">取消</el-button>
        <el-button type="primary" :disabled="!operator.username || operator.password.length < 12" @click="saveOperator">创建</el-button>
      </template>
    </MSheet>
    <template v-if="isAdmin && tab === 'operators'" #fab>
      <button type="button" class="m-fab" aria-label="新建管理账户" @click="openOperator">
        <el-icon :size="24"><Plus /></el-icon>
      </button>
    </template>
  </MPage>
</template>

<style scoped>
.wide { width: 100%; height: var(--m-tap); margin: 12px 0 0; }
.m-card-actions {
  display: flex;
  gap: 10px;
  margin-top: 14px;
}
.m-card-actions .el-button {
  flex: 1;
  height: var(--m-tap);
  margin: 0;
}
</style>
