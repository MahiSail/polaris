<template>
  <MPage :title="currentMeta.title" :back="true">
    <template #actions>
      <el-button
        circle
        size="small"
        :icon="Refresh"
        aria-label="刷新"
        @click="refreshSection"
      />
    </template>

    <div class="m-stack">
      <div class="m-filters" style="padding: 2px 0 4px;">
        <button
          v-for="item in sectionList"
          :key="item.route"
          type="button"
          class="m-pill"
          :class="{ 'is-active': activeSection === item.route }"
          @click="navigate(item.route)"
        >
          {{ item.shortLabel }}
        </button>
      </div>

      <div class="m-card m-card--flat">
        <div class="m-card__title" style="margin-bottom: 4px;">{{ currentMeta.title }}</div>
        <p class="m-note" style="margin: 0;">{{ currentMeta.desc }}</p>
      </div>

      <!-- admin-notify -->
      <div v-if="activeSection === 'admin-notify'" class="m-card">
        <div class="m-card__title" style="margin-bottom: 6px;">邮件通知与告警阈值配置</div>
        <p class="m-note" style="margin: 0 0 12px;">
          前往系统设置的「通知设置」标签页，管理 SMTP / Resend 发信通道、收件人以及 5 项实时告警触发规则。
        </p>
        <el-button type="primary" style="width: 100%;" @click="navigate('settings', 'tab=alerts')">
          打开通知设置
        </el-button>
      </div>

      <!-- admin-data -->
      <template v-else-if="activeSection === 'admin-data'">
        <div class="m-card">
          <div class="m-card__title" style="margin-bottom: 6px;">SQLite 数据库备份与恢复</div>
          <p class="m-note" style="margin: 0 0 12px;">
            下载完整一致性快照备份（polaris-backup.db），或从历史备份文件恢复控制面板数据。
          </p>
          <div class="m-stack" style="gap: 8px;">
            <el-button type="primary" :loading="backingUp" style="width: 100%;" @click="downloadBackup">
              导出数据库备份
            </el-button>
            <input
              ref="restoreInputRef"
              type="file"
              accept=".db,.sqlite,.sqlite3"
              style="display: none;"
              @change="handleRestoreFile"
            />
            <el-button
              v-if="isAdmin"
              :loading="restoring"
              style="width: 100%; margin-left: 0;"
              @click="restoreInputRef?.click()"
            >
              从备份文件恢复数据库
            </el-button>
          </div>
        </div>

        <div class="m-card">
          <div class="m-card__title" style="margin-bottom: 6px;">日志裁剪与存储压缩 (VACUUM)</div>
          <p class="m-note" style="margin: 0 0 12px;">
            清理超过 7 天的延迟探测历史点并执行 SQLite VACUUM 整理，回收磁盘空间。
          </p>
          <el-button
            v-if="isAdmin"
            type="warning"
            plain
            :loading="vacuuming"
            style="width: 100%;"
            @click="runVacuum"
          >
            清理并压缩数据库
          </el-button>
        </div>
      </template>

      <!-- admin-themes -->
      <div v-else-if="activeSection === 'admin-themes'" class="m-card">
        <div class="m-filter-bar" style="margin-bottom: 8px;">
          <div>
            <div class="m-card__title">外观与色彩主题</div>
            <div class="m-note">当前模式：{{ isDark ? '深色模式（黑曜石）' : '浅色模式（微紫灰）' }}</div>
          </div>
          <el-switch :model-value="isDark" @change="() => toggleTheme()" />
        </div>
        <div class="m-filter-row" style="margin-top: 10px;">
          <button
            type="button"
            class="m-pill"
            :class="{ 'is-active': !isDark }"
            @click="isDark && toggleTheme()"
          >
            浅色主题
          </button>
          <button
            type="button"
            class="m-pill"
            :class="{ 'is-active': isDark }"
            @click="!isDark && toggleTheme()"
          >
            深色主题
          </button>
        </div>
      </div>

      <!-- admin-security -->
      <div v-else-if="activeSection === 'admin-security'" class="m-stack">
        <div class="m-card">
          <div class="m-filter-bar" style="margin-bottom: 8px;">
            <div>
              <div class="m-card__title">两步验证 (TOTP / 2FA)</div>
              <div class="m-note">当前账号：{{ appState.username || appState.operator?.username || '—' }}</div>
            </div>
            <el-tag :type="totpEnabled ? 'success' : 'info'" size="small" effect="light">
              {{ totpEnabled ? '已开启' : '未开启' }}
            </el-tag>
          </div>

          <template v-if="!totpEnabled">
            <div v-if="!totpSetup" style="margin-top: 8px;">
              <p class="m-note" style="margin: 0 0 10px;">
                使用 Google Authenticator、1Password 或其他 TOTP 应用绑定动态口令，保护控制台登录安全。
              </p>
              <el-button type="primary" :loading="totpLoading" style="width: 100%;" @click="startTOTPSetup">
                开始配置两步验证
              </el-button>
            </div>

            <div v-else class="m-stack" style="margin-top: 10px;">
              <div v-if="totpQrDataUrl" style="display: flex; justify-content: center; padding: 8px; background: #fff; border-radius: 10px;">
                <img :src="totpQrDataUrl" alt="2FA 二维码" style="width: 180px; height: 180px;" />
              </div>
              <div class="m-card m-card--flat">
                <div class="m-note" style="margin-bottom: 4px;">手动配置密钥：</div>
                <div class="m-mono-wrap" style="font-size: 13px; font-weight: 600;">{{ totpSetup.secret }}</div>
                <el-button size="small" text style="padding: 4px 0 0;" @click="copySecret">复制密钥</el-button>
              </div>
              <div class="m-field">
                <label class="m-field__label">输入 6 位动态验证码</label>
                <el-input v-model="totpCode" placeholder="000000" maxlength="6" />
              </div>
              <div class="m-form-foot">
                <el-button style="flex: 1;" @click="totpSetup = null">取消</el-button>
                <el-button type="primary" :loading="totpLoading" style="flex: 1;" @click="enableTOTP">
                  验证并启用
                </el-button>
              </div>
            </div>
          </template>

          <template v-else>
            <div class="m-stack" style="margin-top: 10px;">
              <p class="m-note" style="margin: 0;">如需停用两步验证，请输入当前账户登录密码确认身份。</p>
              <div class="m-field">
                <label class="m-field__label">登录密码</label>
                <el-input v-model="disablePassword" type="password" show-password placeholder="输入当前登录密码" />
              </div>
              <el-button type="danger" plain :loading="totpLoading" style="width: 100%;" @click="disableTOTP">
                停用两步验证
              </el-button>
            </div>
          </template>
        </div>
      </div>

      <!-- admin-settings -->
      <div v-else-if="activeSection === 'admin-settings'" class="m-card">
        <div class="m-card__title" style="margin-bottom: 6px;">控制面综合设置</div>
        <p class="m-note" style="margin: 0 0 12px;">
          管理操作员账户、通知设置、Reality 伪装域名检测以及高精度 MMDB IP 库。
        </p>
        <el-button type="primary" style="width: 100%;" @click="navigate('settings')">
          前往系统设置
        </el-button>
      </div>

      <!-- admin-update -->
      <div v-else-if="activeSection === 'admin-update'" class="m-card">
        <div class="m-kv-grid" style="margin-bottom: 12px;">
          <div class="m-kv-cell">
            <span class="m-kv-cell__label">当前版本</span>
            <span class="m-kv-cell__value m-mono-wrap">{{ updateInfo.current_version || 'dev' }}</span>
          </div>
          <div class="m-kv-cell">
            <span class="m-kv-cell__label">最新版本</span>
            <span class="m-kv-cell__value m-mono-wrap">{{ updateInfo.latest_version || '未检查' }}</span>
          </div>
          <div class="m-kv-cell">
            <span class="m-kv-cell__label">运行平台</span>
            <span class="m-kv-cell__value">{{ updateInfo.os || '—' }} / {{ updateInfo.architecture || '—' }}</span>
          </div>
          <div class="m-kv-cell">
            <span class="m-kv-cell__label">更新状态</span>
            <span class="m-kv-cell__value">
              {{ updateInfo.update_available ? '发现新版本' : '已是最新' }}
            </span>
          </div>
        </div>

        <div v-if="updateInfo.check_error" class="m-note" style="color: var(--sb-warning); margin-bottom: 10px;">
          检查失败：{{ updateInfo.check_error }}
        </div>

        <div class="m-stack" style="gap: 8px;">
          <el-button :loading="checkingUpdate" style="width: 100%;" @click="checkUpdate">
            检查最新版本
          </el-button>
          <el-button
            v-if="isAdmin && updateInfo.update_available && updateInfo.os === 'linux'"
            type="primary"
            :loading="applyingUpdate"
            style="width: 100%; margin-left: 0;"
            @click="applyUpdate"
          >
            立即更新 Master ({{ updateInfo.latest_version }})
          </el-button>
        </div>
      </div>
    </div>
  </MPage>
</template>

<script setup>
import { computed, inject, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import QRCode from 'qrcode'
import { api, post, uploadFile } from '../../api'
import { writeClipboard } from '../../clipboard'
import { isDark as defaultIsDark, toggleTheme as defaultToggleTheme } from '../../theme'
import MPage from '../components/MPage.vue'

const currentView = inject('currentView', ref('admin-data'))
const appState = inject('appState', { operator: null, systemUpdate: {} })
const loadSystemUpdate = inject('loadSystemUpdate', async () => {})
const navigate = inject('navigate', () => {})
const isAdmin = inject('isAdmin', computed(() => false))
const isDark = inject('isDark', defaultIsDark)
const toggleTheme = inject('toggleTheme', defaultToggleTheme)

const sectionList = [
  { route: 'admin-data', shortLabel: '数据管理', title: '数据备份与维护', desc: '管理 SQLite 数据库备份导出、快照恢复与历史探测日志压缩清理。' },
  { route: 'admin-security', shortLabel: '账户安全', title: '账户安全 (2FA)', desc: '为当前操作员配置基于 TOTP 的两步验证保护。' },
  { route: 'admin-themes', shortLabel: '外观主题', title: '外观与主题', desc: '在深色黑曜石与浅色微紫灰之间即时切换控制面板主题。' },
  { route: 'admin-notify', shortLabel: '通知设置', title: '通知与告警管理', desc: '配置邮件发送通道、告警接收人和触发阈值。' },
  { route: 'admin-settings', shortLabel: '系统设置', title: '系统综合设置', desc: '跳转查看操作员、网络工具与 MMDB IP 库配置。' },
  { route: 'admin-update', shortLabel: '版本更新', title: 'Master 版本更新', desc: '检查并应用 Polaris 控制面主程序发布更新。' },
]

const activeSection = computed(() =>
  sectionList.some((s) => s.route === currentView.value) ? currentView.value : 'admin-data'
)
const currentMeta = computed(
  () => sectionList.find((s) => s.route === activeSection.value) || sectionList[0]
)

const backingUp = ref(false)
const restoring = ref(false)
const vacuuming = ref(false)
const restoreInputRef = ref(null)

const totpLoading = ref(false)
const totpSetup = ref(null)
const totpQrDataUrl = ref('')
const totpCode = ref('')
const disablePassword = ref('')
const totpEnabled = computed(() => Boolean(appState.totp_enabled ?? appState.operator?.totp_enabled))

const checkingUpdate = ref(false)
const applyingUpdate = ref(false)
const updateInfo = computed(() => appState.systemUpdate || {})

async function refreshSection() {
  if (activeSection.value === 'admin-update') {
    await checkUpdate()
  } else if (activeSection.value === 'admin-security') {
    try {
      const me = await api('/auth/me')
      if (me) {
        if (typeof me.totp_enabled === 'boolean') appState.totp_enabled = me.totp_enabled
        if (me.operator) appState.operator = me.operator
      }
    } catch {
      // ignore
    }
  }
}

async function downloadBackup() {
  backingUp.value = true
  try {
    const resp = await fetch('/api/v1/db/backup', { credentials: 'include' })
    if (!resp.ok) {
      const text = await resp.text().catch(() => '')
      throw new Error(text || `导出失败 (${resp.status})`)
    }
    const blob = await resp.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'polaris-backup.db'
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
    ElMessage.success('数据库备份已开始下载')
  } catch (err) {
    ElMessage.error(err.message || '导出备份失败')
  } finally {
    backingUp.value = false
  }
}

async function handleRestoreFile(event) {
  const file = event.target?.files?.[0]
  if (event.target) event.target.value = ''
  if (!file) return
  try {
    await ElMessageBox.confirm(
      `确认使用「${file.name}」覆盖并恢复当前控制面板数据库吗？恢复后请刷新检查数据。`,
      '恢复数据库备份',
      { type: 'warning', confirmButtonText: '确认恢复', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  restoring.value = true
  try {
    const formData = new FormData()
    formData.append('backup', file)
    await uploadFile('/db/restore', formData)
    ElMessage.success('数据库已恢复成功')
  } catch (err) {
    ElMessage.error(err.message || '恢复数据库失败')
  } finally {
    restoring.value = false
  }
}

async function runVacuum() {
  try {
    await ElMessageBox.confirm('将清理超过 7 天的探测点并执行 SQLite VACUUM 压缩，是否继续？', '数据库整理', {
      type: 'warning',
      confirmButtonText: '执行',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  vacuuming.value = true
  try {
    const res = await post('/db/vacuum', {})
    ElMessage.success(`整理完成，清理旧探测记录 ${res?.pruned ?? 0} 条`)
  } catch (err) {
    ElMessage.error(err.message || '数据库整理失败')
  } finally {
    vacuuming.value = false
  }
}

async function startTOTPSetup() {
  totpLoading.value = true
  try {
    const res = await post('/auth/2fa/setup', {})
    totpSetup.value = res
    totpCode.value = ''
    totpQrDataUrl.value = res.otpauth_uri ? await QRCode.toDataURL(res.otpauth_uri, { width: 180, margin: 1 }) : ''
  } catch (err) {
    ElMessage.error(err.message || '初始化两步验证失败')
  } finally {
    totpLoading.value = false
  }
}

async function copySecret() {
  if (!totpSetup.value?.secret) return
  try {
    await writeClipboard(totpSetup.value.secret)
    ElMessage.success('密钥已复制')
  } catch {
    ElMessage.error('复制失败')
  }
}

async function enableTOTP() {
  if (!totpCode.value.trim()) {
    ElMessage.warning('请输入 6 位动态验证码')
    return
  }
  totpLoading.value = true
  try {
    await post('/auth/2fa/enable', { code: totpCode.value.trim() })
    ElMessage.success('两步验证已开启')
    totpSetup.value = null
    totpQrDataUrl.value = ''
    totpCode.value = ''
    appState.totp_enabled = true
    const me = await api('/auth/me')
    if (me?.operator) appState.operator = me.operator
  } catch (err) {
    ElMessage.error(err.message || '验证码无效或开启失败')
  } finally {
    totpLoading.value = false
  }
}

async function disableTOTP() {
  if (!disablePassword.value) {
    ElMessage.warning('请输入登录密码')
    return
  }
  totpLoading.value = true
  try {
    await post('/auth/2fa/disable', { password: disablePassword.value })
    ElMessage.success('两步验证已停用')
    disablePassword.value = ''
    appState.totp_enabled = false
    const me = await api('/auth/me')
    if (me?.operator) appState.operator = me.operator
  } catch (err) {
    ElMessage.error(err.message || '停用两步验证失败')
  } finally {
    totpLoading.value = false
  }
}

async function checkUpdate() {
  checkingUpdate.value = true
  try {
    await loadSystemUpdate(true)
    ElMessage.success('版本信息已更新')
  } catch (err) {
    ElMessage.error(err.message || '检查更新失败')
  } finally {
    checkingUpdate.value = false
  }
}

async function applyUpdate() {
  try {
    await ElMessageBox.confirm(
      `确认更新 Master 到 ${updateInfo.value.latest_version} 并重启服务吗？`,
      '应用系统更新',
      { type: 'warning', confirmButtonText: '立即更新', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  applyingUpdate.value = true
  try {
    await post('/system/update/apply', {})
    ElMessage.success('更新任务已触发，Master 正在重启...')
  } catch (err) {
    ElMessage.error(err.message || '执行更新失败')
  } finally {
    applyingUpdate.value = false
  }
}

onMounted(() => {
  if (activeSection.value === 'admin-update') {
    loadSystemUpdate(false).catch(() => {})
  }
})
</script>
