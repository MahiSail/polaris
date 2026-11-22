<template>
  <div class="m-stack">
    <div class="m-card m-card--flat">
      <div class="m-filter-bar" style="margin-bottom: 8px;">
        <div style="display: flex; align-items: center; gap: 10px; min-width: 0; flex: 1;">
          <OsIcon :os="node.os_name || node.os" style="width: 22px; height: 22px; flex: none;" />
          <div style="min-width: 0; flex: 1;">
            <div class="m-card__title" style="display: flex; align-items: center; gap: 6px; flex-wrap: wrap;">
              <span style="min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ node.name }}</span>
              <StatusDot :online="node.online" />
            </div>
            <div class="m-note" style="display: flex; align-items: center; gap: 5px; flex-wrap: wrap;">
              <span>{{ nodeFlag(node) }}</span>
              <span>{{ nodeLocationLabel(node) || node.os_name || node.os || '服务器' }}</span>
              <span v-if="node.architecture">· {{ node.architecture }}</span>
            </div>
          </div>
        </div>
      </div>

      <div class="m-action-strip">
        <el-button v-if="canWrite && !isShareMode" type="primary" plain @click="emit('edit', 'basic')">
          配置
        </el-button>
        <el-button v-if="!isShareMode" @click="emit('probe-peers', node)">
          线路排查
        </el-button>
        <el-button
          v-if="!isShareMode && node.ddns_enabled"
          :loading="syncingDDNS"
          @click="syncDDNS"
        >
          同步 DDNS
        </el-button>
        <el-button v-if="!isShareMode && node.ddns_enabled" @click="ddnsHistoryOpen = true">
          DDNS 记录
        </el-button>
        <el-button
          v-if="isAdmin && !isShareMode && agentUpgradable"
          type="warning"
          @click="emit('upgrade-agent', node)"
        >
          升级 Agent
        </el-button>
        <el-button
          v-if="isAdmin && !isShareMode && singBoxUpgradable"
          type="warning"
          plain
          @click="emit('upgrade-singbox', node)"
        >
          升级 sing-box
        </el-button>
      </div>
    </div>

    <!-- Top 4 KPI summary -->
    <div class="m-kv-grid">
      <div class="m-card" style="padding: 10px 12px;">
        <div class="m-kv-cell__label">CPU 占用</div>
        <div class="m-kv-cell__value" style="font-size: 16px;">{{ cpuPercentText }}</div>
        <div class="m-note">{{ host.cpu_cores ? `${host.cpu_cores} 核` : '—' }}</div>
      </div>
      <div class="m-card" style="padding: 10px 12px;">
        <div class="m-kv-cell__label">内存占用</div>
        <div class="m-kv-cell__value" style="font-size: 16px;">{{ memPercentText }}</div>
        <div class="m-note">{{ host.memory_total ? `${formatBytes(host.memory_used || 0)} / ${formatBytes(host.memory_total)}` : '—' }}</div>
      </div>
      <div class="m-card" style="padding: 10px 12px;">
        <div class="m-kv-cell__label">实时网速</div>
        <div class="m-kv-cell__value" style="font-size: 14px;">↓ {{ formatBytes(host.net_rx_rate || 0, '/s') }}</div>
        <div class="m-note">↑ {{ formatBytes(host.net_tx_rate || 0, '/s') }} · 连接 {{ connCount }}</div>
      </div>
      <div class="m-card" style="padding: 10px 12px;">
        <div class="m-kv-cell__label">网络延迟</div>
        <div class="m-kv-cell__value" style="font-size: 16px;">{{ latencyHeadline }}</div>
        <div class="m-note">{{ latencySubtitle }}</div>
      </div>
    </div>

    <MSegmented
      v-model="tab"
      :options="[
        { value: 'resources', label: '资源' },
        { value: 'latency', label: '网络延迟' },
        { value: 'facts', label: '详情' },
      ]"
    />

    <div v-if="tab !== 'facts'" class="m-filter-bar">
      <div class="m-filter-row">
        <button
          v-for="r in ranges"
          :key="r.hours"
          type="button"
          class="m-pill"
          :class="{ 'is-active': hours === r.hours }"
          @click="hours = r.hours"
        >
          {{ r.label }}
        </button>
      </div>
      <span class="m-note">{{ hours === 168 ? '7 天' : `${hours} 小时` }}</span>
    </div>

    <!-- Tab 1: Resources -->
    <div v-if="tab === 'resources'" class="m-stack">
      <div class="m-card">
        <div class="m-filter-bar" style="margin-bottom: 6px;">
          <strong style="font-size: 13px;">CPU 使用率</strong>
          <span class="m-note">{{ cpuPercentText }}</span>
        </div>
        <TrendChart
          :points="series.cpu"
          name="CPU 使用率"
          color="var(--sb-chart-1)"
          :min="0"
          :max="100"
          :format="(v) => `${Math.round(v)}%`"
          :height="156"
        />
      </div>

      <div class="m-card">
        <div class="m-filter-bar" style="margin-bottom: 6px;">
          <strong style="font-size: 13px;">内存使用 · {{ formatBytes(host.memory_total || 0) }}</strong>
          <span class="m-note">{{ memPercentText }}</span>
        </div>
        <TrendChart
          :points="series.mem"
          name="内存使用"
          color="var(--sb-chart-2)"
          :min="0"
          :max="host.memory_total || undefined"
          :format="(v) => formatBytes(v)"
          :height="156"
        />
      </div>

      <div class="m-card">
        <div class="m-filter-bar" style="margin-bottom: 6px;">
          <strong style="font-size: 13px;">网络速率</strong>
          <span class="m-note">↓ {{ formatBytes(host.net_rx_rate || 0, '/s') }} · ↑ {{ formatBytes(host.net_tx_rate || 0, '/s') }}</span>
        </div>
        <TrendChart
          :series="[
            { name: '下行', points: series.rx, color: 'var(--sb-info)' },
            { name: '上行', points: series.tx, color: 'var(--sb-chart-4)' },
          ]"
          :format="(v) => formatBytes(v, '/s')"
          :height="156"
        />
      </div>

      <div class="m-card">
        <div class="m-filter-bar" style="margin-bottom: 6px;">
          <strong style="font-size: 13px;">硬盘使用 · {{ formatBytes(host.disk_total || 0) }}</strong>
          <span class="m-note">{{ diskPercentText }}</span>
        </div>
        <TrendChart
          :points="series.disk"
          name="硬盘使用"
          color="var(--sb-chart-3)"
          :min="0"
          :max="host.disk_total || undefined"
          :format="(v) => formatBytes(v)"
          :height="156"
        />
      </div>
    </div>

    <!-- Tab 2: Network Latency -->
    <div v-else-if="tab === 'latency'" class="m-stack">
      <LatencyPanel v-if="effectiveLatency" :latency="effectiveLatency" />

      <div v-if="latencyLoading && !probes.length" class="m-card m-note">正在加载延迟历史...</div>
      <div v-else-if="latencyError" class="m-card m-note" style="color: var(--sb-danger);">{{ latencyError }}</div>
      <div v-else-if="!probes.length" class="m-card m-note">该时间段暂无探针延迟历史数据</div>

      <template v-else>
        <div class="m-card">
          <div class="m-filter-bar" style="margin-bottom: 8px;">
            <strong style="font-size: 13px;">探针延迟趋势 ({{ shownProbes.length }})</strong>
            <el-button v-if="canWrite && !isShareMode" size="small" text @click="emit('edit', 'probes')">
              探针配置
            </el-button>
          </div>
          <TrendChart
            v-if="shownProbes.length"
            :series="latencySeries"
            :height="200"
            :area="false"
            :format="(v) => `${Math.round(v)} ms`"
          />
        </div>

        <div class="m-list">
          <div v-for="probe in probes" :key="probe.id" class="m-card">
            <div class="m-filter-bar" style="margin-bottom: 6px;">
              <div style="display: flex; align-items: center; gap: 8px;">
                <el-checkbox
                  :model-value="visibleProbeIds.includes(probe.id)"
                  @change="toggleProbe(probe.id)"
                />
                <span
                  style="width: 10px; height: 10px; border-radius: 999px; display: inline-block;"
                  :style="{ background: probeColor(probe.id) }"
                />
                <strong style="font-size: 13px;">{{ probe.name }}</strong>
              </div>
              <el-button size="small" text @click="focusProbe(probe.id)">
                {{ visibleProbeIds.length === 1 && visibleProbeIds[0] === probe.id ? '显示全部' : '仅看' }}
              </el-button>
            </div>
            <div class="m-kv-grid">
              <div class="m-kv-cell">
                <span class="m-kv-cell__label">平均延迟</span>
                <span class="m-kv-cell__value">{{ probe.avg != null ? `${probe.avg.toFixed(1)} ms` : '—' }}</span>
              </div>
              <div class="m-kv-cell">
                <span class="m-kv-cell__label">抖动 / 丢包</span>
                <span class="m-kv-cell__value">
                  {{ probe.jitter != null ? `${probe.jitter.toFixed(1)} ms` : '—' }} · {{ probe.loss != null ? `${Number(probe.loss).toFixed(1)}%` : '0%' }}
                </span>
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>

    <!-- Tab 3: Facts & Details -->
    <div v-else class="m-stack">
      <section v-for="group in factGroups" :key="group.label" class="m-card">
        <div class="m-filter-bar" style="margin-bottom: 10px;">
          <strong style="font-size: 14px;">{{ group.label }}</strong>
          <div v-if="group.actions?.length" class="m-filter-row">
            <el-button
              v-for="act in group.actions"
              :key="act.label"
              size="small"
              :loading="act.loading"
              @click="act.onClick"
            >
              {{ act.label }}
            </el-button>
          </div>
        </div>

        <div class="m-kv-grid">
          <div v-for="fact in group.facts" :key="fact.label" class="m-kv-cell">
            <span class="m-kv-cell__label">{{ fact.label }}</span>
            <span
              class="m-kv-cell__value"
              :class="{ 'm-mono-wrap': fact.mono }"
              :style="fact.copyable ? 'cursor: pointer; text-decoration: underline dotted;' : ''"
              @click="fact.copyable && copyText(fact.value)"
            >
              {{ fact.value || '—' }}
            </span>
          </div>
        </div>
      </section>

      <div v-if="isAdmin && !isShareMode" style="padding: 4px 0 12px;">
        <el-button type="danger" plain style="width: 100%;" @click="emit('revoke', node)">
          移除服务器
        </el-button>
      </div>
    </div>

    <MSheet v-model="ddnsHistoryOpen" :title="`DDNS 变更记录 · ${node.name}`">
      <MDDNSHistoryList :node-id="node.id" />
    </MSheet>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { api, post } from '../../api'
import { writeClipboard } from '../../clipboard'
import { formatDDNSBindSummary } from '../../ddns'
import { formatBandwidth, formatBytes, formatDateTime, formatUptime } from '../../format'
import { isShareMode } from '../../share'
import { isNewerVersion } from '../../version'
import LatencyPanel from '../../components/LatencyPanel.vue'
import OsIcon from '../../components/OsIcon.vue'
import StatusDot from '../../components/StatusDot.vue'
import TrendChart from '../../components/TrendChart.vue'
import MDDNSHistoryList from '../components/MDDNSHistoryList.vue'
import MSegmented from '../components/MSegmented.vue'
import MSheet from '../components/MSheet.vue'
import { nodeExpiryInfo, nodeFlag, nodeLocationLabel, nodeTrafficStats } from '../utils/nodeHelpers'

const props = defineProps({
  node: { type: Object, required: true },
  metrics: { type: Object, default: () => ({}) },
  live: { type: Object, default: null },
  latestAgentVersion: { type: String, default: '' },
  singBoxLatest: { type: Object, default: () => ({}) },
  canWrite: { type: Boolean, default: false },
  isAdmin: { type: Boolean, default: false },
})

const emit = defineEmits(['edit', 'probe-peers', 'upgrade-agent', 'upgrade-singbox', 'revoke', 'refresh'])

const ranges = [
  { label: '1小时', hours: 1 },
  { label: '6小时', hours: 6 },
  { label: '24小时', hours: 24 },
  { label: '7天', hours: 168 },
]

const palette = ['#818CF8', '#38BDF8', '#10B981', '#F59E0B', '#EF4444', '#C084FC', '#14B8A6', '#F472B6']

const tab = ref('resources')
const hours = ref(6)
const metricHistory = ref([])
const pingRaw = ref(null)
const latencyLoading = ref(false)
const latencyError = ref('')
const hiddenProbeIds = ref(new Set())
const syncingDDNS = ref(false)
const ddnsHistoryOpen = ref(false)

const host = computed(() => props.live?.host || props.metrics?.host || {})
const connCount = computed(() => props.live?.connection_count ?? 0)
const effectiveLatency = computed(() => props.live?.latency || props.metrics?.latency || null)

const agentUpgradable = computed(() =>
  Boolean(props.latestAgentVersion && isNewerVersion(props.latestAgentVersion, props.node.agent_version))
)
const singBoxUpgradable = computed(() => {
  const latest = props.singBoxLatest?.[props.node.architecture]
  return Boolean(latest && props.node.sing_box_version && props.node.sing_box_version !== latest)
})

const cpuPercentText = computed(() =>
  props.node.online && host.value.cpu_percent != null ? `${Math.round(host.value.cpu_percent)}%` : '—'
)
const memPercentText = computed(() => {
  if (!props.node.online || !host.value.memory_total) return '—'
  return `${Math.round(((host.value.memory_used || 0) / host.value.memory_total) * 100)}%`
})
const diskPercentText = computed(() => {
  if (!props.node.online || !host.value.disk_total) return '—'
  return `${Math.round(((host.value.disk_used || 0) / host.value.disk_total) * 100)}%`
})
const latencyHeadline = computed(() => {
  const lat = effectiveLatency.value
  const val = lat?.latency ?? lat?.avgLatency
  return val != null ? `${Math.round(val)} ms` : '—'
})
const latencySubtitle = computed(() => {
  const lat = effectiveLatency.value
  if (!lat || lat.none) return '无探测任务'
  const parts = []
  if (lat.probe) parts.push(lat.probe)
  if (lat.jitter != null) parts.push(`抖动 ${Number(lat.jitter).toFixed(1)}ms`)
  if (lat.loss != null) parts.push(`丢包 ${Number(lat.loss).toFixed(1)}%`)
  return parts.join(' · ') || '实时延迟'
})

const series = computed(() => ({
  cpu: metricHistory.value.map((p) => ({ ts: p.ts, v: p.cpu })),
  mem: metricHistory.value.map((p) => ({ ts: p.ts, v: p.mem_used })),
  disk: metricHistory.value.map((p) => ({ ts: p.ts, v: p.disk_used })),
  rx: metricHistory.value.map((p) => ({ ts: p.ts, v: p.net_rx })),
  tx: metricHistory.value.map((p) => ({ ts: p.ts, v: p.net_tx })),
}))

const probes = computed(() => {
  if (!pingRaw.value?.ping?.length) return []
  const names = pingRaw.value.probes || {}
  const losses = pingRaw.value.loss || {}
  const byId = new Map()
  for (const pt of pingRaw.value.ping) {
    const id = String(pt.task_id)
    if (!byId.has(id)) {
      byId.set(id, {
        id,
        name: names[id] || id,
        loss: losses[id] ?? null,
        points: [],
      })
    }
    byId.get(id).points.push({ ts: pt.ts, v: pt.latency })
  }
  const result = []
  for (const entry of byId.values()) {
    const vals = entry.points.map((p) => p.v).filter((v) => v != null && Number.isFinite(v))
    const avg = vals.length ? vals.reduce((a, b) => a + b, 0) / vals.length : null
    let jitter = null
    if (vals.length > 1 && avg != null) {
      const diffs = []
      for (let i = 1; i < vals.length; i++) diffs.push(Math.abs(vals[i] - vals[i - 1]))
      jitter = diffs.reduce((a, b) => a + b, 0) / diffs.length
    }
    result.push({ ...entry, avg, jitter })
  }
  return result
})

const visibleProbeIds = computed(() =>
  probes.value.map((p) => p.id).filter((id) => !hiddenProbeIds.value.has(id))
)
const shownProbes = computed(() =>
  probes.value.filter((p) => !hiddenProbeIds.value.has(p.id))
)

function probeColor(id) {
  const idx = probes.value.findIndex((p) => p.id === id)
  return palette[(idx >= 0 ? idx : 0) % palette.length]
}

const latencySeries = computed(() =>
  shownProbes.value.map((p) => ({
    name: p.name,
    color: probeColor(p.id),
    points: p.points,
  }))
)

function toggleProbe(id) {
  const next = new Set(hiddenProbeIds.value)
  if (next.has(id)) next.delete(id)
  else if (next.size + 1 < probes.value.length || probes.value.length === 1) next.add(id)
  hiddenProbeIds.value = next
}

function focusProbe(id) {
  if (visibleProbeIds.value.length === 1 && visibleProbeIds.value[0] === id) {
    hiddenProbeIds.value = new Set()
  } else {
    hiddenProbeIds.value = new Set(probes.value.map((p) => p.id).filter((pid) => pid !== id))
  }
}

const factGroups = computed(() => {
  const n = props.node
  const h = host.value
  const t = nodeTrafficStats(n, props.metrics)
  const exp = nodeExpiryInfo(n)

  const groups = [
    {
      label: '系统与硬件',
      facts: [
        { label: '操作系统', value: h.os_name || n.os_name || n.os },
        { label: 'CPU 架构', value: n.architecture },
        { label: 'CPU 型号', value: h.cpu_name || (h.cpu_cores ? `${h.cpu_cores} 核` : '—') },
        { label: '内核 / 虚拟化', value: [h.kernel, h.virt].filter(Boolean).join(' · ') || '—' },
        { label: '内存', value: h.memory_total ? `${formatBytes(h.memory_used || 0)} / ${formatBytes(h.memory_total)}` : '—' },
        { label: '交换区', value: h.swap_total ? `${formatBytes(h.swap_used || 0)} / ${formatBytes(h.swap_total)}` : '未启用' },
        { label: '硬盘', value: h.disk_total ? `${formatBytes(h.disk_used || 0)} / ${formatBytes(h.disk_total)}` : '—' },
        { label: '系统运行时长', value: h.uptime_seconds ? formatUptime(h.uptime_seconds) : '—' },
      ],
    },
    {
      label: 'Agent 与运行态',
      facts: [
        { label: 'Agent 版本', value: n.agent_version ? `v${n.agent_version}` : '—', mono: true },
        { label: 'sing-box 版本', value: n.sing_box_version || '—', mono: true },
        { label: 'TCP / UDP 连接', value: h.tcp != null ? `${h.tcp} / ${h.udp ?? 0}` : `${connCount.value}` },
        { label: '系统负载', value: Array.isArray(h.load_avg) ? h.load_avg.map((v) => Number(v || 0).toFixed(2)).join(' / ') : '—' },
        { label: '进程数', value: h.procs != null ? String(h.procs) : '—' },
        { label: '最后心跳', value: formatDateTime(n.last_seen_at) },
      ],
    },
    {
      label: '网络与地址',
      facts: [
        { label: '客户端连接地址', value: n.client_address || '未设置', mono: true, copyable: Boolean(n.client_address) },
        { label: '当前观测公网 IP', value: n.last_observed_ip || '—', mono: true, copyable: Boolean(n.last_observed_ip) },
        { label: '归属地', value: nodeLocationLabel(n) || '—' },
        { label: '网卡上报 IP', value: (n.ip_addresses || []).join(', ') || '—', mono: true, copyable: Boolean(n.ip_addresses?.length) },
      ],
    },
  ]

  const currencyMap = { USD: '$', CNY: '¥', EUR: '€', CAD: 'C$' }
  const periodMap = { day: '天', month: '月', quarter: '季', year: '年' }
  const priceText = n.price_period === 'free' || n.price === '0' || n.price === '0.00'
    ? '免费'
    : n.price
      ? `${currencyMap[n.currency || 'USD'] || `${n.currency} `}${n.price} / ${periodMap[n.price_period || 'month'] || n.price_period}`
      : '未设置'
  const quotaText = t.quotaBytes > 0 ? formatBytes(t.quotaBytes) : n.traffic_plan

  if (!isShareMode) {
    groups.push({
      label: '服务商与流量套餐',
      facts: [
        { label: '服务商', value: n.provider || '未设置' },
        { label: '到期状态', value: exp ? `${exp.label}${n.expires_on && n.expires_on !== 'permanent' ? ` (${n.expires_on})` : ''}` : '未设置' },
        { label: '套餐费用', value: priceText },
        { label: '带宽限制', value: n.bandwidth ? formatBandwidth(n.bandwidth) : '不限' },
        { label: '本月流量 / 配额', value: `${formatBytes(t.monthlyTotal)}${quotaText ? ` / ${quotaText}` : ''}` },
        { label: '本月明细 (↓/↑)', value: `↓ ${formatBytes(t.monthlyRx)} · ↑ ${formatBytes(t.monthlyTx)}` },
        { label: '历史累计流量', value: formatBytes(t.lifetimeTotal) },
        { label: '代理转发月流量', value: formatBytes(t.proxyMonthlyTotal) },
      ],
    })

    const domains = (n.ddns_domains?.length ? n.ddns_domains : [n.ddns_domain]).filter(Boolean)
    groups.push({
      label: 'Cloudflare DDNS',
      actions: n.ddns_enabled
        ? [
            { label: syncingDDNS.value ? '同步中...' : '立即同步', loading: syncingDDNS.value, onClick: syncDDNS },
            { label: '变更记录', loading: false, onClick: () => { ddnsHistoryOpen.value = true } },
          ]
        : [],
      facts: [
        { label: 'DDNS 状态', value: n.ddns_enabled ? '已启用' : '未启用' },
        { label: '绑定域名', value: domains.join(', ') || '未绑定', mono: true },
        { label: '出口策略', value: n.ddns_enabled ? formatDDNSBindSummary(n) : '—', mono: true },
        { label: '当前解析 IP', value: n.last_observed_ip || '等待检测', mono: true },
      ],
    })

    const aliasEntries = Object.entries(n.ip_aliases || {})
    if (aliasEntries.length || (n.ip_addresses || []).length > 1) {
      groups.push({
        label: '出口 IP 别名',
        facts: aliasEntries.length
          ? aliasEntries.map(([ip, alias]) => ({ label: alias || '默认出口', value: ip, mono: true, copyable: true }))
          : (n.ip_addresses || []).map((ip, idx) => ({ label: `出口 #${idx + 1}`, value: ip, mono: true, copyable: true })),
      })
    }
  }

  return groups
})

async function copyText(text) {
  if (!text) return
  try {
    await writeClipboard(text)
    ElMessage.success(`已复制：${text}`)
  } catch {
    ElMessage.error('复制失败')
  }
}

async function syncDDNS() {
  syncingDDNS.value = true
  try {
    const res = await post(`/nodes/${props.node.id}/ddns/sync`, {})
    ElMessage.success(`DDNS 同步成功${res.ip ? `：${res.ip}` : ''}`)
    emit('refresh')
  } catch (err) {
    ElMessage.error(err.message || 'DDNS 同步失败')
  } finally {
    syncingDDNS.value = false
  }
}

async function loadHistory() {
  if (!props.node?.id) return
  latencyLoading.value = true
  latencyError.value = ''
  try {
    const basePath = `/nodes/${props.node.id}/history`
    const [mRes, pRes] = await Promise.all([
      api(`${basePath}?hours=${hours.value}`),
      api(`${basePath}?hours=${hours.value}&series=ping&points=240`),
    ])
    metricHistory.value = mRes.metrics || []
    pingRaw.value = pRes
  } catch (err) {
    latencyError.value = err.message || '读取历史数据失败'
  } finally {
    latencyLoading.value = false
  }
}

function refreshAll() {
  emit('refresh')
  loadHistory()
}

watch([() => props.node?.id, hours], () => {
  loadHistory()
})

onMounted(() => {
  loadHistory()
})
</script>
