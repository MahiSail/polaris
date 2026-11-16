<template>
  <MPage title="历史连接明细" :back="true" :loading="loading">
    <template #actions>
      <button
        type="button"
        class="m-header__btn"
        aria-label="筛选条件"
        @click="filterOpen = true"
      >
        <el-icon><Filter /></el-icon>
        <span>筛选</span>
      </button>
      <button
        type="button"
        class="m-header__btn"
        :disabled="exporting"
        aria-label="导出 CSV"
        @click="exportCSV"
      >
        <el-icon><Download /></el-icon>
      </button>
      <button
        type="button"
        class="m-header__btn"
        aria-label="刷新记录"
        @click="load(false)"
      >
        <el-icon><Refresh /></el-icon>
      </button>
    </template>

    <div class="m-stack">
      <div class="m-card" style="padding: 10px 12px;">
        <div class="m-filter-row" style="margin-bottom: 8px;">
          <el-input
            v-model="filters.keyword"
            placeholder="搜索目标域名 / IP / 来源IP / 规则"
            clearable
            style="flex: 1; min-width: 0;"
            @keyup.enter="applySearch"
            @clear="applySearch"
          />
          <el-button type="primary" @click="applySearch">搜索</el-button>
        </div>

        <div class="m-filter-bar">
          <div class="m-filter-row" style="gap: 6px;">
            <el-tag
              v-if="filters.ip"
              closable
              size="small"
              type="primary"
              @close="filters.ip = ''; applySearch()"
            >
              来源IP: {{ filters.ip }}
            </el-tag>
            <el-tag
              v-if="filters.user"
              closable
              size="small"
              type="success"
              @close="filters.user = ''; applySearch()"
            >
              接入节点: {{ filters.user }}
            </el-tag>
            <el-tag
              v-if="filters.node_id"
              closable
              size="small"
              type="info"
              @close="filters.node_id = ''; applySearch()"
            >
              服务器: {{ nodeNameOf(filters.node_id) }}
            </el-tag>
            <el-tag
              v-if="filters.network"
              closable
              size="small"
              type="warning"
              @close="filters.network = ''; applySearch()"
            >
              {{ filters.network.toUpperCase() }}
            </el-tag>
            <span v-if="!hasAnyFilter" class="m-note">
              保留最近 7 天 · {{ localTimeZoneLabel() }}
            </span>
          </div>
          <el-button v-if="hasAnyFilter" size="small" text @click="resetFilters">
            清空
          </el-button>
        </div>
      </div>

      <div class="m-filter-bar" style="padding: 0 2px;">
        <span class="m-note">共 {{ total }} 条历史连接</span>
        <button type="button" class="m-pill" @click="filterOpen = true">
          排序：{{ sortLabel }} ({{ filters.order_dir === 'asc' ? '升序' : '降序' }})
        </button>
      </div>

      <div v-if="!loading && records.length === 0" class="m-card m-note">
        暂无匹配的历史连接记录
      </div>

      <div v-else class="m-list">
        <div
          v-for="row in records"
          :key="row.id"
          class="m-card"
          style="cursor: pointer;"
          @click="openDetail(row)"
        >
          <div class="m-filter-bar" style="margin-bottom: 6px;">
            <div style="display: flex; align-items: center; gap: 6px; min-width: 0; flex: 1;">
              <strong class="m-mono-wrap" style="font-size: 14px;">
                {{ row.host || row.destination || '—' }}
              </strong>
              <el-tag size="small" effect="plain">{{ (row.network || 'tcp').toUpperCase() }}</el-tag>
            </div>
            <strong style="font-size: 13px; white-space: nowrap;">
              {{ formatBytes(Number(row.upload || 0) + Number(row.download || 0)) }}
            </strong>
          </div>

          <div class="m-note m-mono-wrap" style="margin-bottom: 4px; color: var(--sb-text-2);">
            来源 {{ row.source_ip }}{{ row.source_port ? `:${row.source_port}` : '' }}
            <span v-if="row.source_location"> · {{ row.source_location }}</span>
          </div>

          <div v-if="row.destination && row.host && row.destination !== row.host" class="m-note m-mono-wrap" style="margin-bottom: 4px;">
            目标 {{ row.destination }}
            <span v-if="row.destination_location"> ({{ row.destination_location }})</span>
          </div>
          <div v-else-if="row.destination_location" class="m-note" style="margin-bottom: 4px;">
            归属地：{{ row.destination_location }}
          </div>

          <div class="m-filter-bar" style="font-size: 11px; color: var(--sb-muted);">
            <span style="min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
              {{ row.node_name || '—' }} · {{ row.user || '未知设备' }} · {{ row.outbound_name || 'direct' }}
            </span>
            <span style="white-space: nowrap;">
              {{ row.closed_at ? formatDuration(row.duration_seconds) : '连接中' }} · {{ formatDateTime(row.started_at) }}
            </span>
          </div>
        </div>
      </div>

      <div v-if="total > pageSize" class="m-pagination">
        <span>第 {{ page }} / {{ totalPages }} 页</span>
        <div style="display: flex; gap: 6px;">
          <el-button :disabled="page <= 1" @click="goPage(page - 1)">上一页</el-button>
          <el-button :disabled="page >= totalPages" @click="goPage(page + 1)">下一页</el-button>
        </div>
      </div>
    </div>

    <!-- Filter & Sort Sheet -->
    <MSheet v-model="filterOpen" title="筛选与排序">
      <div class="m-stack">
        <div class="m-field">
          <label class="m-field__label">来源 IP</label>
          <el-input v-model="filters.ip" placeholder="例如 192.168.1.10" clearable />
        </div>

        <div class="m-field">
          <label class="m-field__label">接入节点 / 设备</label>
          <el-input v-model="filters.user" placeholder="输入接入用户名" clearable />
        </div>

        <div class="m-field">
          <label class="m-field__label">服务器</label>
          <MPicker
            v-model="filters.node_id"
            clearable
            :options="nodePickerOptions"
            title="选择服务器"
            placeholder="全部服务器"
          />
        </div>

        <div class="m-field">
          <label class="m-field__label">网络协议</label>
          <div class="m-filter-row">
            <button
              v-for="opt in networkOptions"
              :key="opt.value || 'all'"
              type="button"
              class="m-pill"
              :class="{ 'is-active': filters.network === opt.value }"
              @click="filters.network = opt.value"
            >
              {{ opt.label }}
            </button>
          </div>
        </div>

        <div class="m-field">
          <label class="m-field__label">出站出口</label>
          <el-input v-model="filters.outbound" placeholder="例如 direct / proxy-us" clearable />
        </div>

        <div class="m-field">
          <label class="m-field__label">快捷时间范围</label>
          <div class="m-filter-row">
            <button
              v-for="preset in timePresets"
              :key="preset.key"
              type="button"
              class="m-pill"
              :class="{ 'is-active': activeTimePreset === preset.key }"
              @click="applyTimePreset(preset.key)"
            >
              {{ preset.label }}
            </button>
          </div>
        </div>

        <div class="m-kv-grid">
          <div class="m-field">
            <label class="m-field__label">开始时间</label>
            <input v-model="filters.startLocal" type="datetime-local" class="m-native-input" />
          </div>
          <div class="m-field">
            <label class="m-field__label">结束时间</label>
            <input v-model="filters.endLocal" type="datetime-local" class="m-native-input" />
          </div>
        </div>

        <div class="m-field">
          <label class="m-field__label">排序方式</label>
          <div class="m-filter-row" style="margin-bottom: 6px;">
            <button
              v-for="opt in sortFields"
              :key="opt.value"
              type="button"
              class="m-pill"
              :class="{ 'is-active': filters.order_by === opt.value }"
              @click="filters.order_by = opt.value"
            >
              {{ opt.label }}
            </button>
          </div>
          <div class="m-filter-row">
            <button
              type="button"
              class="m-pill"
              :class="{ 'is-active': filters.order_dir === 'desc' }"
              @click="filters.order_dir = 'desc'"
            >
              降序
            </button>
            <button
              type="button"
              class="m-pill"
              :class="{ 'is-active': filters.order_dir === 'asc' }"
              @click="filters.order_dir = 'asc'"
            >
              升序
            </button>
          </div>
        </div>

        <div class="m-form-foot">
          <el-button style="flex: 1;" @click="resetFilters">重置</el-button>
          <el-button type="primary" style="flex: 1;" @click="filterOpen = false; applySearch()">应用</el-button>
        </div>
      </div>
    </MSheet>

    <!-- Record Detail Sheet -->
    <MSheet v-model="detailOpen" title="连接记录详情">
      <div v-if="activeRecord" class="m-stack">
        <div class="m-card m-card--flat">
          <div class="m-card__title m-mono-wrap" style="margin-bottom: 4px;">
            {{ activeRecord.host || activeRecord.destination || '—' }}
          </div>
          <div class="m-note m-mono-wrap">记录 ID：{{ activeRecord.id }}</div>
        </div>

        <div class="m-card">
          <div class="m-kv-grid">
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">来源地址</span>
              <span class="m-kv-cell__value m-mono-wrap">
                {{ activeRecord.source_ip }}{{ activeRecord.source_port ? `:${activeRecord.source_port}` : '' }}
              </span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">来源归属地</span>
              <span class="m-kv-cell__value">{{ activeRecord.source_location || '—' }}</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">目标地址</span>
              <span class="m-kv-cell__value m-mono-wrap">{{ activeRecord.destination || '—' }}</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">目标归属地</span>
              <span class="m-kv-cell__value">{{ lookupLocation || activeRecord.destination_location || '—' }}</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">服务器</span>
              <span class="m-kv-cell__value">{{ activeRecord.node_name || '—' }}</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">接入节点</span>
              <span class="m-kv-cell__value">{{ activeRecord.user || '—' }}</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">入站服务</span>
              <span class="m-kv-cell__value">{{ activeRecord.listener_name || '—' }}</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">出站出口</span>
              <span class="m-kv-cell__value">{{ activeRecord.outbound_name || 'direct' }}</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">下行流量</span>
              <span class="m-kv-cell__value">{{ formatBytes(activeRecord.download || 0) }}</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">上行流量</span>
              <span class="m-kv-cell__value">{{ formatBytes(activeRecord.upload || 0) }}</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">开始时间</span>
              <span class="m-kv-cell__value">{{ formatDateTime(activeRecord.started_at) }}</span>
            </div>
            <div class="m-kv-cell">
              <span class="m-kv-cell__label">持续时长</span>
              <span class="m-kv-cell__value">
                {{ activeRecord.closed_at ? formatDuration(activeRecord.duration_seconds) : '连接中' }}
              </span>
            </div>
          </div>
        </div>

        <div class="m-stack" style="gap: 8px;">
          <el-button
            v-if="extractIP(activeRecord.destination || activeRecord.host)"
            :loading="lookingUp"
            style="width: 100%;"
            @click="lookupDestinationIP"
          >
            实时查询目标 IP 归属地
          </el-button>
          <el-button
            v-if="activeRecord.source_ip"
            type="primary"
            plain
            style="width: 100%; margin-left: 0;"
            @click="filterByField('ip', activeRecord.source_ip)"
          >
            仅看此来源 IP ({{ activeRecord.source_ip }})
          </el-button>
          <el-button
            v-if="activeRecord.user"
            style="width: 100%; margin-left: 0;"
            @click="filterByField('user', activeRecord.user)"
          >
            仅看此接入节点 ({{ activeRecord.user }})
          </el-button>
        </div>
      </div>
    </MSheet>
  </MPage>
</template>

<script setup>
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Download, Filter, Refresh } from '@element-plus/icons-vue'
import { api } from '../../api'
import { formatBytes, formatDateTime, formatDuration, localTimeZoneLabel } from '../../format'
import MPage from '../components/MPage.vue'
import MPicker from '../components/MPicker.vue'
import MSheet from '../components/MSheet.vue'

const nodePickerOptions = computed(() => [
  { value: '', label: '全部服务器' },
  ...nodeList.value.map((n) => ({ value: n.id, label: n.name })),
])

const networkOptions = [
  { label: '全部', value: '' },
  { label: 'TCP', value: 'tcp' },
  { label: 'UDP', value: 'udp' },
]

const sortFields = [
  { label: '开始时间', value: 'started_at' },
  { label: '总流量', value: 'total_bytes' },
  { label: '持续时长', value: 'duration_seconds' },
  { label: '来源 IP', value: 'source_ip' },
  { label: '目标主机', value: 'host' },
  { label: '接入节点', value: 'user' },
]

const timePresets = [
  { label: '全部时间', key: '' },
  { label: '今天', key: 'today' },
  { label: '近 3 天', key: '3d' },
  { label: '近 7 天', key: '7d' },
  { label: '近 30 天', key: '30d' },
]

const loading = ref(false)
const exporting = ref(false)
const filterOpen = ref(false)
const detailOpen = ref(false)
const lookingUp = ref(false)
const lookupLocation = ref('')
const activeRecord = ref(null)
const activeTimePreset = ref('')

const records = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const nodeList = ref([])

const filters = reactive({
  ip: '',
  node_id: '',
  user: '',
  network: '',
  outbound: '',
  keyword: '',
  startLocal: '',
  endLocal: '',
  order_by: 'started_at',
  order_dir: 'desc',
})

const totalPages = computed(() => Math.max(1, Math.ceil((total.value || 0) / pageSize)))
const sortLabel = computed(() => sortFields.find((s) => s.value === filters.order_by)?.label || '开始时间')
const hasAnyFilter = computed(() =>
  Boolean(
    filters.ip ||
      filters.node_id ||
      filters.user ||
      filters.network ||
      filters.outbound ||
      filters.keyword ||
      filters.startLocal ||
      filters.endLocal
  )
)

function nodeNameOf(id) {
  return nodeList.value.find((n) => n.id === id)?.name || id
}

function toLocalInput(date) {
  const pad = (n) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function applyTimePreset(key) {
  activeTimePreset.value = key
  if (!key) {
    filters.startLocal = ''
    filters.endLocal = ''
    return
  }
  const now = new Date()
  const start = new Date(now)
  if (key === 'today') {
    start.setHours(0, 0, 0, 0)
  } else if (key === '3d') {
    start.setDate(start.getDate() - 3)
  } else if (key === '7d') {
    start.setDate(start.getDate() - 7)
  } else if (key === '30d') {
    start.setDate(start.getDate() - 30)
  }
  filters.startLocal = toLocalInput(start)
  filters.endLocal = toLocalInput(now)
}

function buildQueryParams(includePagination = true) {
  const params = new URLSearchParams()
  if (includePagination) {
    params.set('page', String(page.value))
    params.set('page_size', String(pageSize))
  }
  if (filters.ip.trim()) params.set('ip', filters.ip.trim())
  if (filters.node_id) params.set('node_id', filters.node_id)
  if (filters.user.trim()) params.set('user', filters.user.trim())
  if (filters.network) params.set('network', filters.network)
  if (filters.outbound.trim()) params.set('outbound', filters.outbound.trim())
  if (filters.keyword.trim()) params.set('keyword', filters.keyword.trim())
  if (filters.startLocal) {
    const dt = new Date(filters.startLocal)
    if (!Number.isNaN(dt.getTime())) params.set('start_time', dt.toISOString())
  }
  if (filters.endLocal) {
    const dt = new Date(filters.endLocal)
    if (!Number.isNaN(dt.getTime())) params.set('end_time', dt.toISOString())
  }
  if (filters.order_by) params.set('order_by', filters.order_by)
  if (filters.order_dir) params.set('order_dir', filters.order_dir)
  return params
}

async function load(silent = false) {
  if (!silent) loading.value = true
  try {
    const params = buildQueryParams(true)
    const res = await api(`/devices/connections?${params.toString()}`)
    records.value = res.records || []
    total.value = res.total || 0
  } catch (err) {
    ElMessage.error(err.message || '加载历史连接失败')
  } finally {
    loading.value = false
  }
}

async function loadNodesList() {
  try {
    const res = await api('/nodes')
    nodeList.value = res.nodes || []
  } catch {
    // ignore
  }
}

function syncFromURL() {
  const params = new URLSearchParams(window.location.search)
  filters.ip = params.get('ip') || ''
  filters.node_id = params.get('node_id') || ''
  filters.user = params.get('user') || ''
  filters.network = params.get('network') || ''
  filters.outbound = params.get('outbound') || ''
  filters.keyword = params.get('keyword') || ''
}

function applySearch() {
  page.value = 1
  load(false)
}

function resetFilters() {
  filters.ip = ''
  filters.node_id = ''
  filters.user = ''
  filters.network = ''
  filters.outbound = ''
  filters.keyword = ''
  filters.startLocal = ''
  filters.endLocal = ''
  filters.order_by = 'started_at'
  filters.order_dir = 'desc'
  activeTimePreset.value = ''
  page.value = 1
  filterOpen.value = false
  load(false)
}

function goPage(nextPage) {
  page.value = nextPage
  load(false)
}

function openDetail(row) {
  activeRecord.value = row
  lookupLocation.value = ''
  detailOpen.value = true
}

function extractIP(address) {
  if (!address) return ''
  const raw = String(address).trim()
  if (raw.startsWith('[')) {
    const end = raw.indexOf(']')
    if (end > 1) return raw.slice(1, end)
  }
  const colonIdx = raw.lastIndexOf(':')
  if (colonIdx > 0 && raw.indexOf(':') === colonIdx) {
    const hostPart = raw.slice(0, colonIdx)
    if (/^\d{1,3}(\.\d{1,3}){3}$/.test(hostPart)) return hostPart
  }
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(raw) || raw.includes(':')) return raw
  return ''
}

async function lookupDestinationIP() {
  const ip = extractIP(activeRecord.value?.destination || activeRecord.value?.host)
  if (!ip) return
  lookingUp.value = true
  try {
    const res = await api(`/ip/lookup?ip=${encodeURIComponent(ip)}`)
    const parts = [res.location, res.isp, res.asn].filter(Boolean)
    lookupLocation.value = parts.join(' · ') || '未知'
    ElMessage.success(`归属地：${lookupLocation.value}`)
  } catch (err) {
    ElMessage.error(err.message || '查询 IP 归属地失败')
  } finally {
    lookingUp.value = false
  }
}

function filterByField(field, value) {
  filters[field] = value
  detailOpen.value = false
  applySearch()
}

async function exportCSV() {
  exporting.value = true
  try {
    const params = buildQueryParams(false)
    const resp = await fetch(`/api/v1/devices/connections/export?${params.toString()}`, {
      credentials: 'include',
    })
    if (!resp.ok) {
      const text = await resp.text().catch(() => '')
      throw new Error(text || `导出失败 (${resp.status})`)
    }
    const blob = await resp.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `connections_history_${Date.now()}.csv`
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
    ElMessage.success('正在下载历史连接 CSV')
  } catch (err) {
    ElMessage.error(err.message || '导出 CSV 失败')
  } finally {
    exporting.value = false
  }
}

function handlePopState() {
  syncFromURL()
  applySearch()
}

onMounted(() => {
  syncFromURL()
  loadNodesList()
  load(false)
  window.addEventListener('popstate', handlePopState)
})

onUnmounted(() => {
  window.removeEventListener('popstate', handlePopState)
})
</script>
