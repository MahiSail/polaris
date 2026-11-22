<template>
  <div
    class="openlat-node-row neu-row rise"
    :class="{ 'is-offline': !node.online }"
    role="button"
    tabindex="0"
    @click="$emit('open', node.id)"
  >
    <!-- Col 1: Identity & System -->
    <div class="row-col col-name">
      <div class="name-line">
        <span class="status-dot" :class="node.online ? 'is-online' : 'is-offline'" />
        <span class="country-flag">{{ countryFlag }}</span>
        <span class="node-name truncate" :title="node.name">{{ node.name }}</span>
        <span
          v-if="agentUpdateAvailable || singBoxUpdateAvailable"
          class="row-tag upgrade-tag shrink-0"
          :title="upgradeTooltip"
        >
          <span class="upgrade-dot" />
          <span>可升级</span>
        </span>
        <span v-if="node.ddns_enabled" class="row-tag ddns-tag" :title="ddnsTooltip">
          <span class="ddns-dot" />
          <span>DDNS</span>
        </span>
      </div>
      <div class="os-line">
        <span v-if="locationName" class="location-tag shrink-0" :title="node.location || locationName">{{ locationName }}</span>
        <span v-if="locationName" class="meta-dot">·</span>
        <OsIcon :os="osLabel" class="os-icon-sm" />
        <span class="system-text truncate">{{ systemInfoText }}</span>
        <template v-if="node.provider">
          <span class="meta-dot">·</span>
          <a
            v-if="node.provider_url"
            :href="node.provider_url"
            target="_blank"
            rel="noopener noreferrer"
            class="row-provider-link truncate"
            @click.stop
          >{{ node.provider }}</a>
          <span v-else class="row-provider-text truncate">{{ node.provider }}</span>
        </template>
        <template v-if="node.bandwidth">
          <span class="meta-dot">·</span>
          <span class="row-bw-text">{{ formatBandwidth(node.bandwidth) }}</span>
        </template>
      </div>
    </div>

    <!-- Col 2: Stacked CPU/RAM/DISK meters -->
    <div class="row-col col-meters">
      <div class="stacked-meter-row">
        <span class="meter-tag">CPU</span>
        <div class="meter-bar-track neu-inset">
          <div
            class="meter-bar-fill bar-cpu"
            :class="{ 'bar-danger': cpuPercent >= 90 }"
            :style="{ width: `${cpuPercent || 0}%` }"
          />
        </div>
        <span class="tnum meter-pct font-mono">{{ cpuPercent !== null ? `${Math.round(cpuPercent)}%` : '—' }}</span>
        <span class="tnum meter-detail font-mono truncate">{{ cpuDetail }}</span>
      </div>

      <div class="stacked-meter-row">
        <span class="meter-tag">内存</span>
        <div class="meter-bar-track neu-inset">
          <div
            class="meter-bar-fill bar-mem"
            :class="{ 'bar-danger': memPercent >= 90 }"
            :style="{ width: `${memPercent || 0}%` }"
          />
        </div>
        <span class="tnum meter-pct font-mono">{{ memPercent !== null ? `${Math.round(memPercent)}%` : '—' }}</span>
        <span class="tnum meter-detail font-mono truncate">{{ memDetail }}</span>
      </div>

      <div class="stacked-meter-row">
        <span class="meter-tag">硬盘</span>
        <div class="meter-bar-track neu-inset">
          <div
            class="meter-bar-fill bar-disk"
            :class="{ 'bar-danger': diskPercent >= 90 }"
            :style="{ width: `${diskPercent || 0}%` }"
          />
        </div>
        <span class="tnum meter-pct font-mono">{{ diskPercent !== null ? `${Math.round(diskPercent)}%` : '—' }}</span>
        <span class="tnum meter-detail font-mono truncate">{{ diskDetail }}</span>
      </div>
    </div>

    <!-- Col 3: Traffic -->
    <el-tooltip
      placement="top"
      effect="dark"
      :show-after="150"
      popper-class="traffic-popper"
      :disabled="isShareMode"
    >
      <template #content>
        <div class="traffic-tip">
          <!-- 本月流量 -->
          <div class="traffic-tip-section">
            <div class="traffic-tip-head">
              <span class="traffic-tip-label">
                <svg class="tip-sec-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <rect x="3" y="4" width="18" height="18" rx="2" ry="2" />
                  <line x1="16" y1="2" x2="16" y2="6" />
                  <line x1="8" y1="2" x2="8" y2="6" />
                  <line x1="3" y1="10" x2="21" y2="10" />
                </svg>
                本月流量
              </span>
              <span class="traffic-tip-val font-mono">{{ trafficText }}</span>
            </div>
            <div class="traffic-tip-badges font-mono">
              <span class="traffic-badge traffic-badge--rx" title="本月下行流量">
                <span class="badge-arrow">↓</span>
                <span>{{ formatBytes(effectiveMonthlyRx) }}</span>
              </span>
              <span class="traffic-badge traffic-badge--tx" title="本月上行流量">
                <span class="badge-arrow">↑</span>
                <span>{{ formatBytes(effectiveMonthlyTx) }}</span>
              </span>
            </div>
          </div>

          <div class="traffic-tip-line" />

          <!-- 累计流量 -->
          <div class="traffic-tip-section">
            <div class="traffic-tip-head">
              <span class="traffic-tip-label">
                <svg class="tip-sec-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <polygon points="12 2 2 7 12 12 22 7 12 2" />
                  <polyline points="2 17 12 22 22 17" />
                  <polyline points="2 12 12 17 22 12" />
                </svg>
                累计流量
              </span>
              <span class="traffic-tip-val font-mono">{{ formatBytes(lifetimeTrafficBytes) }}</span>
            </div>
            <div class="traffic-tip-badges font-mono">
              <span class="traffic-badge traffic-badge--rx" title="累计下行流量">
                <span class="badge-arrow">↓</span>
                <span>{{ formatBytes(effectiveLifetimeRx) }}</span>
              </span>
              <span class="traffic-badge traffic-badge--tx" title="累计上行流量">
                <span class="badge-arrow">↑</span>
                <span>{{ formatBytes(effectiveLifetimeTx) }}</span>
              </span>
            </div>
          </div>

          <!-- 业务代理流量 (sing-box) -->
          <template v-if="hasProxyTraffic && hasNodeTraffic">
            <div class="traffic-tip-line" />
            <div class="traffic-tip-section">
              <div class="traffic-tip-head">
                <span class="traffic-tip-label">
                  <svg class="tip-sec-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <polyline points="16 3 21 3 21 8" />
                    <line x1="4" y1="20" x2="21" y2="3" />
                    <polyline points="21 16 21 21 16 21" />
                    <line x1="15" y1="15" x2="21" y2="21" />
                    <line x1="4" y1="4" x2="9" y2="9" />
                  </svg>
                  业务代理
                </span>
                <span class="traffic-tip-val font-mono">{{ formatBytes(proxyMonthlyTotal) }}</span>
              </div>
              <div class="traffic-tip-badges font-mono">
                <span class="traffic-badge traffic-badge--rx" title="代理下行流量">
                  <span class="badge-arrow">↓</span>
                  <span>{{ formatBytes(proxyMonthlyRx) }}</span>
                </span>
                <span class="traffic-badge traffic-badge--tx" title="代理上行流量">
                  <span class="badge-arrow">↑</span>
                  <span>{{ formatBytes(proxyMonthlyTx) }}</span>
                </span>
              </div>
            </div>
          </template>

          <!-- 套餐配额进度 -->
          <template v-if="trafficLimitBytes > 0">
            <div class="traffic-tip-line" />
            <div class="traffic-tip-section">
              <div class="traffic-tip-head">
                <span class="traffic-tip-label">
                  <svg class="tip-sec-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M21.21 15.89A10 10 0 1 1 8 2.83" />
                    <path d="M22 12A10 10 0 0 0 12 2v10z" />
                  </svg>
                  套餐配额
                </span>
                <span class="traffic-tip-val font-mono" :class="quotaToneClass">{{ trafficPercent ?? 0 }}%</span>
              </div>
              <div class="traffic-meter-track">
                <div
                  class="traffic-meter-bar"
                  :class="quotaToneClass"
                  :style="{ width: `${Math.min(100, trafficPercent || 0)}%` }"
                />
              </div>
              <div class="traffic-quota-sub font-mono">
                <span>{{ formatBytes(trafficUsedBytes) }} / {{ formatBytes(trafficLimitBytes) }}</span>
                <span v-if="trafficLimitBytes > trafficUsedBytes">剩余 {{ formatBytes(trafficLimitBytes - trafficUsedBytes) }}</span>
                <span v-else class="text-danger">已耗尽</span>
              </div>
            </div>
          </template>
        </div>
      </template>
      <div class="row-col col-traffic">
        <span class="col-label">流量</span>
        <span class="tnum traffic-val font-mono">{{ trafficText }}</span>
      </div>
    </el-tooltip>

    <!-- Col 4: Network Rates (Down & Up) -->
    <div class="row-col col-rates">
      <div class="rate-line">
        <span class="arrow-down">&darr;</span>
        <span class="tnum font-mono">{{ formatRate(netRxRate) }}</span>
      </div>
      <div class="rate-line">
        <span class="arrow-up">&uarr;</span>
        <span class="tnum font-mono">{{ formatRate(netTxRate) }}</span>
      </div>
    </div>

    <!-- Col 5: Connections -->
    <div class="row-col col-conns">
      <span class="col-label" title="sing-box 活跃代理连接数">连接</span>
      <span class="tnum font-mono" :title="`sing-box 活跃代理连接: ${activeConnections} (系统底层 TCP: ${tcpCount} · UDP: ${udpCount})`">
        {{ activeConnections }}
      </span>
    </div>

    <!-- Col 6: 24h Dual Mini Heat Tracks -->
    <div class="row-col col-latency">
      <LatencyPanel :latency="latencyData" :compact="true" />
    </div>

    <!-- Col 7: Action Chevron -->
    <div class="row-col col-action">
      <span class="action-chevron">&rsaquo;</span>
    </div>
  </div>
</template>

<script setup>
import { computed, inject } from 'vue'
import OsIcon from './OsIcon.vue'
import LatencyPanel from './LatencyPanel.vue'
import { formatBandwidth } from '../format'
import { formatDDNSBindLabel } from '../ddns'
import { isShareMode } from '../share'
import { isNewerVersion } from '../version'

const props = defineProps({
  node: {
    type: Object,
    required: true,
  },
  metrics: {
    type: Object,
    default: () => ({}),
  },
  live: {
    type: Object,
    default: null,
  },
  singBoxLatest: {
    type: Object,
    default: () => ({}),
  },
})

defineEmits(['open'])

const appState = inject('appState', null)

const agentUpdateAvailable = computed(() =>
  isNewerVersion(appState?.systemUpdate?.latest_version, props.node.agent_version),
)

const singBoxUpdateAvailable = computed(() => {
  const latest = props.singBoxLatest?.[props.node.architecture]
  return Boolean(latest && props.node.sing_box_version && props.node.sing_box_version !== latest)
})

const upgradeTooltip = computed(() => {
  if (agentUpdateAvailable.value && singBoxUpdateAvailable.value) {
    return `Agent 与 sing-box 均可升级 (Agent: v${appState?.systemUpdate?.latest_version} · sing-box: ${props.singBoxLatest?.[props.node.architecture]})`
  }
  if (agentUpdateAvailable.value) {
    return `有新版本 Agent 可升级至 v${appState?.systemUpdate?.latest_version}`
  }
  if (singBoxUpdateAvailable.value) {
    return `有新版本 sing-box 可升级至 ${props.singBoxLatest?.[props.node.architecture]}`
  }
  return ''
})

function countryCodeToFlag(code) {
  if (!code || code.length !== 2) return '🌐'
  const offset = 127397
  return String.fromCodePoint(...[...code.toUpperCase()].map((c) => c.charCodeAt(0) + offset))
}

function getNodeFlag(name) {
  if (!name) return '🌐'
  const upper = name.toUpperCase()
  if (upper.includes('TW') || upper.includes('台湾')) return '🇹🇼'
  if (upper.includes('HK') || upper.includes('香港')) return '🇭🇰'
  if (upper.includes('SG') || upper.includes('新加坡') || upper.includes('狮城')) return '🇸🇬'
  if (upper.includes('JP') || upper.includes('日本') || upper.includes('东京') || upper.includes('大阪')) return '🇯🇵'
  if (upper.includes('KR') || upper.includes('韩国') || upper.includes('首尔')) return '🇰🇷'
  if (
    upper.includes('US') ||
    upper.includes('美国') ||
    upper.includes('圣何塞') ||
    upper.includes('洛杉矶') ||
    upper.includes('纽约') ||
    upper.includes('西雅图')
  )
    return '🇺🇸'
  if (upper.includes('UK') || upper.includes('英国') || upper.includes('伦敦')) return '🇬🇧'
  if (upper.includes('DE') || upper.includes('德国') || upper.includes('法兰克福')) return '🇩🇪'
  if (upper.includes('FR') || upper.includes('法国') || upper.includes('巴黎')) return '🇫🇷'
  if (upper.includes('CA') || upper.includes('加拿大')) return '🇨🇦'
  if (upper.includes('AU') || upper.includes('澳大利亚') || upper.includes('悉尼')) return '🇦🇺'
  return '🌐'
}

const countryFlag = computed(() => {
  const code = props.node.country || props.node.country_code
  if (code && /^[A-Za-z]{2}$/.test(code)) {
    const f = countryCodeToFlag(code)
    if (f && f !== '🌐') return f
  }
  return getNodeFlag(props.node.name)
})

const locationName = computed(() => {
  if (props.node.city && props.node.city.trim()) return props.node.city.trim()
  if (props.node.location && props.node.location.trim() && props.node.location !== '未知') {
    const parts = props.node.location.trim().split(/\s+/)
    if (parts.length >= 2) return parts[parts.length - 1]
    return props.node.location.trim()
  }
  const code = props.node.country || props.node.country_code
  if (code && /^[A-Za-z]{2}$/.test(code)) {
    try {
      const dn = new Intl.DisplayNames(['zh-CN'], { type: 'region' })
      return dn.of(code.toUpperCase()) || code
    } catch {
      return code
    }
  }
  return ''
})

const osLabel = computed(() => props.node.os_name || hostMetrics.value?.os_name || props.node.os || '')

const systemInfoText = computed(() => {
  const parts = []
  if (osLabel.value) parts.push(osLabel.value)
  const virt = props.node.virt || hostMetrics.value?.virt
  if (virt && virt !== 'none') parts.push(virt)
  if (props.node.arch || props.node.architecture) parts.push(props.node.arch || props.node.architecture)
  return parts.length ? parts.join(' · ') : 'Linux'
})

const hostMetrics = computed(() => props.node.online ? (props.live?.host || props.metrics?.host) : null)
const latencyData = computed(() => props.live?.latency || props.metrics?.latency || null)

const cpuPercent = computed(() => {
  if (!props.node.online || !hostMetrics.value) return null
  return hostMetrics.value.cpu_percent || 0
})

const cpuDetail = computed(() => {
  if (!props.node.online || !hostMetrics.value) return '—'
  const cores = props.node.cpu_cores || hostMetrics.value.cpu_cores || 1
  return `${cores} 核 · 负载 ${((hostMetrics.value.cpu_percent || 0) / 100 * cores).toFixed(2)}`
})

const memPercent = computed(() => {
  if (!props.node.online || !hostMetrics.value) return null
  const total = hostMetrics.value.memory_total || 0
  const used = hostMetrics.value.memory_used || 0
  if (!total) return null
  return (used / total) * 100
})

const memDetail = computed(() => {
  if (!props.node.online || !hostMetrics.value) return '—'
  return `${formatBytes(hostMetrics.value.memory_used || 0)} / ${formatBytes(hostMetrics.value.memory_total || 0)}`
})

const diskPercent = computed(() => {
  if (!props.node.online || !hostMetrics.value) return null
  const total = hostMetrics.value.disk_total || 0
  const used = hostMetrics.value.disk_used || 0
  if (!total) return null
  return (used / total) * 100
})

const diskDetail = computed(() => {
  if (!props.node.online || !hostMetrics.value) return '—'
  return `${formatBytes(hostMetrics.value.disk_used || 0)} / ${formatBytes(hostMetrics.value.disk_total || 0)}`
})

const nodeTraffic = computed(() => props.metrics?.node || null)
const hasNodeTraffic = computed(() => Boolean(
  nodeTraffic.value &&
  (nodeTraffic.value.monthly_received_bytes !== undefined || nodeTraffic.value.received_bytes !== undefined)
))

const proxyTraffic = computed(() => props.metrics?.proxy || null)
const hasProxyTraffic = computed(() => Boolean(
  proxyTraffic.value &&
  (proxyTraffic.value.monthly_received_bytes !== undefined || proxyTraffic.value.received_bytes !== undefined)
))

function parseTrafficLimitBytes(plan) {
  if (!plan) return 0
  const m = String(plan).match(/([\d.]+)\s*(TB|GB|MB)?/i)
  if (!m) return 0
  const val = parseFloat(m[1])
  if (isNaN(val) || val <= 0) return 0
  const unit = (m[2] || 'GB').toUpperCase()
  if (unit === 'TB') return val * 1024 * 1024 * 1024 * 1024
  if (unit === 'GB') return val * 1024 * 1024 * 1024
  if (unit === 'MB') return val * 1024 * 1024
  return val * 1024 * 1024 * 1024
}

const trafficLimitBytes = computed(() => parseTrafficLimitBytes(props.node.traffic_plan))

const trafficTotals = computed(() => nodeTraffic.value || proxyTraffic.value || {})

const effectiveMonthlyRx = computed(() => Number(trafficTotals.value.monthly_received_bytes || 0))
const effectiveMonthlyTx = computed(() => Number(trafficTotals.value.monthly_sent_bytes || 0))
const trafficUsedBytes = computed(() => effectiveMonthlyRx.value + effectiveMonthlyTx.value)

const effectiveLifetimeRx = computed(() => Number(trafficTotals.value.received_bytes || 0))
const effectiveLifetimeTx = computed(() => Number(trafficTotals.value.sent_bytes || 0))
const lifetimeTrafficBytes = computed(() => effectiveLifetimeRx.value + effectiveLifetimeTx.value)

const proxyMonthlyRx = computed(() => Number(proxyTraffic.value?.monthly_received_bytes || 0))
const proxyMonthlyTx = computed(() => Number(proxyTraffic.value?.monthly_sent_bytes || 0))
const proxyMonthlyTotal = computed(() => proxyMonthlyRx.value + proxyMonthlyTx.value)

const trafficPercent = computed(() => {
  const limit = trafficLimitBytes.value
  if (!limit || limit <= 0) return null
  const used = trafficUsedBytes.value
  return Math.min(100, Math.round((used / limit) * 100))
})

const quotaToneClass = computed(() => {
  const pct = trafficPercent.value
  if (pct === null || pct === undefined) return ''
  if (pct >= 90) return 'is-danger'
  if (pct >= 75) return 'is-warn'
  return ''
})

const trafficText = computed(() => {
  const p = trafficTotals.value
  const rx = p.monthly_received_bytes !== undefined ? p.monthly_received_bytes : p.received_bytes || 0
  const tx = p.monthly_sent_bytes !== undefined ? p.monthly_sent_bytes : p.sent_bytes || 0
  return formatBytes(Number(rx) + Number(tx))
})

const activeConnections = computed(() => {
  if (!props.node.online) return 0
  if (props.live?.connection_count !== undefined) return props.live.connection_count
  if (Array.isArray(props.metrics?.connections)) return props.metrics.connections.length
  return 0
})

const netRxRate = computed(() => props.node.online && hostMetrics.value ? hostMetrics.value.net_rx_rate || 0 : 0)
const netTxRate = computed(() => props.node.online && hostMetrics.value ? hostMetrics.value.net_tx_rate || 0 : 0)

const tcpCount = computed(() => hostMetrics.value?.tcp || 0)
const udpCount = computed(() => hostMetrics.value?.udp || 0)

const ddnsTooltip = computed(() => {
  if (!props.node.ddns_enabled) return ''
  const domains = (props.node.ddns_domains && props.node.ddns_domains.length ? props.node.ddns_domains : [props.node.ddns_domain]).filter(Boolean)
  const domainText = domains.length ? domains.join(', ') : '未指定域名'
  const bindLabel = formatDDNSBindLabel(props.node)
  const bindText = bindLabel ? ` · 绑定: ${bindLabel}` : ''
  const ipText = props.node.last_observed_ip ? ` (解析 IP: ${props.node.last_observed_ip})` : ''
  return `Cloudflare DDNS: ${domainText}${bindText}${ipText}`
})

function formatBytes(bytes) {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 1 ? 1 : 0)} ${units[i]}`
}

function formatRate(bps) {
  if (!bps || bps <= 0) return '0 B/s'
  return `${formatBytes(bps)}/s`
}
</script>

<style scoped>
.openlat-node-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.75rem 1rem;
  border-radius: 0.75rem;
  padding: 0.625rem 1rem;
  cursor: pointer;
  transition: transform 0.15s ease, border-color 0.15s ease;
}

.openlat-node-row:hover {
  border-color: var(--sb-accent);
  transform: translateY(-1px);
}

.row-col {
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.col-name {
  flex: 1.4 1 clamp(12rem, 20%, 18rem);
}

.name-line {
  display: flex;
  align-items: center;
  gap: 6px;
}

.row-tag {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 1px 5px;
  border-radius: 4px;
  font-size: 10px;
  line-height: 1.2;
  font-weight: 550;
  flex-shrink: 0;
}

.row-tag.upgrade-tag {
  color: var(--sb-warning, #f59e0b);
  border-color: color-mix(in oklab, var(--sb-warning, #f59e0b) 30%, transparent);
  background: color-mix(in oklab, var(--sb-warning, #f59e0b) 10%, transparent);
  font-weight: 550;
}

.row-tag .upgrade-dot {
  width: 3.5px;
  height: 3.5px;
  border-radius: 50%;
  background: var(--sb-warning, #f59e0b);
}

.row-tag.ddns-tag {
  color: var(--sb-info);
  background: color-mix(in oklab, var(--sb-info) 10%, transparent);
  border: 1px solid color-mix(in oklab, var(--sb-info) 30%, transparent);
}

.row-tag .ddns-dot {
  width: 3.5px;
  height: 3.5px;
  border-radius: 50%;
  background: var(--sb-info);
}

.status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
}

.status-dot.is-online {
  background: var(--sb-success, #10b981);
  box-shadow: 0 0 6px var(--sb-success, #10b981);
}

.status-dot.is-offline {
  background: var(--sb-danger, #ef4444);
}

.country-flag { font-size: 13px; line-height: 1; }
.node-name { font-size: 13px; font-weight: 600; color: var(--sb-text); }

.os-line {
  margin-top: 3px;
  padding-left: 13px;
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 10px;
  color: var(--sb-muted);
  min-width: 0;
}

.os-icon-sm { width: 11px; height: 11px; flex-shrink: 0; }
.meta-dot { opacity: 0.5; flex-shrink: 0; }
.row-provider-link {
  color: var(--sb-muted);
  text-decoration: none;
}
.row-provider-link:hover {
  text-decoration: underline;
  color: var(--sb-accent);
}
.row-provider-text {
  color: var(--sb-muted);
}
.row-bw-text {
  color: var(--sb-muted);
}

.col-meters {
  flex: 1.5 1 clamp(13rem, 22%, 18rem);
  gap: 4px;
}

.stacked-meter-row {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 10px;
}

.meter-tag { flex: 0 0 1.625rem; color: var(--sb-muted); }
.meter-bar-track {
  height: 5px;
  flex: 1 1 3rem;
  min-width: 2rem;
  border-radius: 999px;
  overflow: hidden;
  background: var(--neu-inset-bg, rgba(15, 23, 42, 0.45));
}
.meter-bar-fill { height: 100%; border-radius: 999px; transition: width 0.6s ease; }
.meter-pct { flex: 0 0 2rem; text-align: right; color: var(--sb-text); }
.meter-detail { flex: 0 1 auto; max-width: 6rem; text-align: right; color: var(--sb-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.col-traffic {
  flex: 0.7 1 clamp(5.5rem, 9%, 8rem);
}

.col-rates {
  flex: 0.8 1 clamp(6.25rem, 10%, 9rem);
  gap: 2px;
  font-size: 11px;
}

.rate-line { display: flex; align-items: center; gap: 4px; }
.arrow-down { color: var(--sb-accent); font-weight: bold; }
.arrow-up { color: var(--sb-warning); font-weight: bold; }

.col-conns {
  flex: 0.6 1 clamp(4.75rem, 7%, 7rem);
  font-size: 11px;
}

.col-label { font-size: 10px; color: var(--sb-muted); }

.col-latency {
  flex: 1.2 1 clamp(10.5rem, 18%, 20rem);
}

.col-action {
  flex: none;
  margin-left: auto;
  padding-left: 0.5rem;
}

.action-chevron {
  font-size: 18px;
  color: var(--sb-muted);
}

@media (max-width: 767px) {
  .openlat-node-row {
    gap: 0.5rem 0.625rem;
    padding: 0.75rem 0.8125rem;
    align-items: flex-start;
  }

  .col-name,
  .col-meters,
  .col-latency {
    flex: 1 0 100%;
  }

  .name-line,
  .os-line {
    flex-wrap: wrap;
  }

  .meter-detail {
    max-width: 8.5rem;
  }

  .col-traffic,
  .col-rates,
  .col-conns {
    flex: 1 1 clamp(5.5rem, 30%, 100%);
  }

  .col-action {
    display: none;
  }
}
</style>
