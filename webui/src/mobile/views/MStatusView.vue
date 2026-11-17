<template>
  <MPage
    :title="selectedNode ? selectedNode.name : '实时节点状态'"
    :back="true"
    :loading="loading"
    @back="handleBack"
  >
    <template #actions>
      <el-button
        v-if="!selectedNode"
        circle
        :icon="Refresh"
        :loading="refreshing"
        aria-label="刷新状态"
        @click="refreshAll"
      />
    </template>

    <!-- 节点详情子视图 -->
    <MNodeDetailView
      v-if="selectedNode"
      :node="selectedNode"
      :metrics="metrics[selectedNode.id] || {}"
      :live="live.get(selectedNode.id) || null"
      :latest-agent-version="appState.systemUpdate?.latest_version || ''"
      :sing-box-latest="singBoxLatest"
      :can-write="false"
      :is-admin="isAdmin"
      @refresh="refreshAll"
    />

    <!-- 实时状态墙 -->
    <div v-else class="m-stack">
      <div v-if="error" class="m-notice m-notice--danger">{{ error }}</div>

      <SummaryCards
        v-if="appState.nodes.length"
        :nodes="mappedNodes"
        :metrics="metrics"
        :live="live"
      />

      <div class="m-stack" style="gap: 8px;">
        <el-input
          v-model="keyword"
          clearable
          placeholder="搜索服务器名称、地区、系统、IP 或 CPU"
        />

        <div class="m-sort-row">
          <MPicker
            v-model="groupBy"
            chip
            :options="groupOptions"
            title="分组方式"
            aria-label="分组方式"
            style="flex: 1;"
          />
          <MPicker
            v-model="sortBy"
            chip
            :options="sortOptions"
            title="排序方式"
            aria-label="排序方式"
            style="flex: 1;"
          />
        </div>

        <div class="m-filter-row" style="justify-content: space-between;">
          <span class="m-count" style="padding: 0;">共 {{ filteredNodes.length }} / {{ mappedNodes.length }} 台节点</span>
          <button
            type="button"
            class="m-pill is-toggle is-active"
            :aria-label="viewMode === 'card' ? '切换为列表视图' : '切换为卡片视图'"
            :title="viewMode === 'card' ? '当前为卡片视图，点击切换为列表视图' : '当前为列表视图，点击切换为卡片视图'"
            @click="viewMode = viewMode === 'card' ? 'list' : 'card'"
          >
            <svg v-if="viewMode === 'card'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
              <rect x="3" y="3" width="7" height="7" />
              <rect x="14" y="3" width="7" height="7" />
              <rect x="14" y="14" width="7" height="7" />
              <rect x="3" y="14" width="7" height="7" />
            </svg>
            <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
              <line x1="8" y1="6" x2="21" y2="6" />
              <line x1="8" y1="12" x2="21" y2="12" />
              <line x1="8" y1="18" x2="21" y2="18" />
              <line x1="3" y1="6" x2="3.01" y2="6" />
              <line x1="3" y1="12" x2="3.01" y2="12" />
              <line x1="3" y1="18" x2="3.01" y2="18" />
            </svg>
            <span>{{ viewMode === 'card' ? '卡片' : '列表' }}</span>
            <span class="m-view-toggle__swap" aria-hidden="true">⇄</span>
          </button>
        </div>
      </div>

      <div v-if="!groups.length && !loading" class="m-empty">尚未匹配到节点</div>

      <template v-for="grp in groups" :key="grp.key">
        <div v-if="grp.label" class="m-section m-filter-bar" style="margin-top: 10px;">
          <span>{{ grp.flag ? `${grp.flag} ` : '' }}{{ grp.label }}</span>
          <span>在线 {{ groupOnline(grp) }} / {{ grp.nodes.length }}</span>
        </div>

        <div v-if="viewMode === 'card'" class="m-cards-flex">
          <MNodeCard
            v-for="node in grp.nodes"
            :key="node.id"
            :node="node"
            :metrics="metrics[node.id] || {}"
            :live="live.get(node.id) || null"
            :can-manage="false"
            :sing-box-latest="singBoxLatest"
            @open="openNodeId = node.id"
          />
        </div>
        <div v-else class="m-list">
          <MNodeRow
            v-for="node in grp.nodes"
            :key="node.id"
            :node="node"
            :metrics="metrics[node.id] || {}"
            :live="live.get(node.id) || null"
            :can-manage="false"
            :sing-box-latest="singBoxLatest"
            @open="openNodeId = node.id"
          />
        </div>
      </template>
    </div>
  </MPage>
</template>

<script setup>
import { computed, inject, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import SummaryCards from '../../components/SummaryCards.vue'
import { api } from '../../api'
import { connectionSnapshots, subscribeConnections } from '../../connections'
import { subscribeLive } from '../../live'
import { isShareMode } from '../../share'
import MNodeCard from '../components/MNodeCard.vue'
import MNodeRow from '../components/MNodeRow.vue'
import MPage from '../components/MPage.vue'
import MPicker from '../components/MPicker.vue'
import MNodeDetailView from './MNodeDetailView.vue'
import { countryCodeToFlag } from '../utils/nodeHelpers'

const GROUP_KEY = 'polaris_mobile_status_group'
const SORT_KEY = 'polaris_mobile_status_sort'
const VIEW_KEY = 'polaris_mobile_status_view'

const appState = inject('appState')
const isAdmin = inject('isAdmin')
const loadNodes = inject('loadNodes')
const goBack = inject('goBack', () => {})

const loading = ref(false)
const refreshing = ref(false)
const error = ref('')
const keyword = ref('')
const groupBy = ref(window.localStorage.getItem(GROUP_KEY) || 'none')
const sortBy = ref(window.localStorage.getItem(SORT_KEY) || 'name')
const viewMode = ref(window.localStorage.getItem(VIEW_KEY) || 'card')
const metrics = ref({})
const singBoxLatest = ref({})
const openNodeId = ref('')

const groupOptions = [
  { value: 'none', label: '不分组' },
  { value: 'country', label: '按地区分组' },
  { value: 'status', label: '按在线状态' },
  { value: 'os', label: '按系统分类' },
]

const sortOptions = [
  { value: 'name', label: '按名称排序' },
  { value: 'cpu', label: '按 CPU 占用' },
  { value: 'mem', label: '按内存占用' },
  { value: 'traffic', label: '按月总流量' },
  { value: 'ping', label: '按延迟从低到高' },
]

watch(groupBy, (v) => window.localStorage.setItem(GROUP_KEY, v))
watch(sortBy, (v) => window.localStorage.setItem(SORT_KEY, v))
watch(viewMode, (v) => window.localStorage.setItem(VIEW_KEY, v))

const live = computed(() => connectionSnapshots.value)

const mappedNodes = computed(() =>
  (appState.nodes || []).map((node) => {
    const host = metrics.value[node.id]?.host || {}
    return {
      ...node,
      country: node.country_code || node.country || '',
      os_name: host.os_name || node.os_name || node.os,
      cpu_cores: host.cpu_cores || node.cpu_cores || 0,
      virt: host.virt || '',
    }
  })
)

const selectedNode = computed(() =>
  openNodeId.value ? mappedNodes.value.find((n) => n.id === openNodeId.value) || null : null
)

function osNameOf(node) {
  const raw = node.os_name || node.os || ''
  const first = raw.split(' ')[0].toLowerCase()
  if (!first) return ''
  if (first.includes('ubuntu')) return 'Ubuntu'
  if (first.includes('debian')) return 'Debian'
  if (first.includes('centos')) return 'CentOS'
  if (first.includes('alpine')) return 'AlpineLinux'
  if (first.includes('rocky')) return 'RockyLinux'
  if (first.includes('alma')) return 'AlmaLinux'
  if (first.includes('fedora')) return 'Fedora'
  if (first.includes('arch')) return 'ArchLinux'
  if (first.includes('windows')) return 'Windows'
  if (first.includes('darwin') || first.includes('macos')) return 'macOS'
  return raw.charAt(0).toUpperCase() + raw.slice(1)
}

function statusOf(node) {
  if (node.online) return 'online'
  const host = metrics.value[node.id]?.host
  return host?.cpu_cores || host?.memory_total || node.last_seen_at ? 'offline' : 'pending'
}

function nodeTrafficTotal(node) {
  const totals = metrics.value[node.id]?.node || metrics.value[node.id]?.proxy || {}
  return Number(totals.monthly_received_bytes || 0) + Number(totals.monthly_sent_bytes || 0)
}

function nodeLatencyScore(node) {
  const latency = metrics.value[node.id]?.latency
  if (!latency || latency.none) return Number.POSITIVE_INFINITY
  const value = Number(latency.latency ?? latency.avgLatency ?? latency.average_latency_ms)
  return Number.isFinite(value) ? value : Number.POSITIVE_INFINITY
}

const sortedNodes = computed(() => {
  const list = [...mappedNodes.value]
  switch (sortBy.value) {
    case 'cpu': {
      const cpu = (n) => metrics.value[n.id]?.host?.cpu_percent ?? -1
      return list.sort((a, b) => cpu(b) - cpu(a))
    }
    case 'mem': {
      const memRatio = (n) => {
        const host = metrics.value[n.id]?.host
        return host?.memory_total > 0 ? (host.memory_used || 0) / host.memory_total : -1
      }
      return list.sort((a, b) => memRatio(b) - memRatio(a))
    }
    case 'traffic':
      return list.sort((a, b) => nodeTrafficTotal(b) - nodeTrafficTotal(a))
    case 'ping':
      return list.sort((a, b) => nodeLatencyScore(a) - nodeLatencyScore(b))
    default:
      return list.sort((a, b) => (a.name || '').localeCompare(b.name || '', 'zh-Hans-CN', { numeric: true }))
  }
})

const filteredNodes = computed(() => {
  const terms = keyword.value.trim().toLowerCase().split(/\s+/).filter(Boolean)
  if (!terms.length) return sortedNodes.value
  return sortedNodes.value.filter((node) => {
    const host = metrics.value[node.id]?.host || {}
    const hay = [
      node.name, node.country, node.country_code, node.city, node.location,
      osNameOf(node), node.os, node.architecture, node.agent_version,
      ...(node.ip_addresses || []), node.last_observed_ip, node.provider, node.client_address,
      host.cpu_name,
    ].filter(Boolean).join(' ').toLowerCase()
    return terms.every((t) => hay.includes(t))
  })
})

const groups = computed(() => {
  const nodes = filteredNodes.value
  if (groupBy.value === 'none') return nodes.length ? [{ key: 'all', label: '', nodes }] : []

  const buckets = new Map()
  for (const node of nodes) {
    let meta
    if (groupBy.value === 'country') {
      const code = (node.country_code || node.country || '').trim().toUpperCase()
      meta = code ? { key: code, label: code, flag: countryCodeToFlag(code) } : { key: 'ZZ', label: '未知地区', flag: '🌐' }
    } else if (groupBy.value === 'status') {
      const st = statusOf(node)
      const label = st === 'online' ? '在线' : st === 'offline' ? '离线' : '待接入'
      meta = { key: st, label }
    } else {
      const os = osNameOf(node)
      meta = os ? { key: os, label: os } : { key: 'unknown', label: '等待上报' }
    }
    if (!buckets.has(meta.key)) buckets.set(meta.key, { ...meta, nodes: [] })
    buckets.get(meta.key).nodes.push(node)
  }

  const list = [...buckets.values()]
  if (groupBy.value === 'status') {
    const order = ['online', 'offline', 'pending']
    return list.sort((a, b) => order.indexOf(a.key) - order.indexOf(b.key))
  }
  return list.sort((a, b) => b.nodes.length - a.nodes.length || a.label.localeCompare(b.label, 'zh-Hans-CN'))
})

function groupOnline(grp) {
  return grp.nodes.filter((n) => n.online).length
}

function handleBack() {
  if (openNodeId.value) openNodeId.value = ''
  else goBack('more')
}

async function loadMetrics() {
  try {
    const res = await api('/nodes/metrics')
    metrics.value = Object.fromEntries((res.nodes || []).map((entry) => [entry.node_id, entry.report]))
    error.value = ''
  } catch (e) {
    error.value = e?.message || '加载监控数据失败'
  }
}

async function refreshAll() {
  if (refreshing.value) return
  refreshing.value = true
  if (!mappedNodes.value.length) loading.value = true
  try {
    await Promise.all([loadNodes().catch((e) => { error.value = e?.message || '' }), loadMetrics()])
    if (!isShareMode) {
      const archs = [...new Set((appState.nodes || []).map((n) => n.architecture).filter(Boolean))]
      await Promise.all(archs.map(async (arch) => {
        try {
          const r = await api(`/sing-box/latest?architecture=${encodeURIComponent(arch)}`)
          if (r?.version) singBoxLatest.value[arch] = r.version
        } catch {
          // ignore
        }
      }))
    }
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

let stopLive, stopConns, pollTimer
onMounted(() => {
  refreshAll()
  stopLive = subscribeLive((ev) => {
    if (ev?.kind === 'node') loadMetrics()
  })
  stopConns = subscribeConnections(() => {})
  pollTimer = window.setInterval(loadMetrics, 8000)
})
onBeforeUnmount(() => {
  stopLive?.()
  stopConns?.()
  window.clearInterval(pollTimer)
})
</script>
