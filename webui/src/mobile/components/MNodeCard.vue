<template>
  <article
    class="m-card m-node-card"
    :class="{ 'is-offline': !node.online }"
    tabindex="0"
    @click="$emit('open', node.id)"
    @keydown.enter="$emit('open', node.id)"
  >
    <!-- Row 1: Name, Live Status & Touch Action Trigger -->
    <div class="m-node-card__head">
      <div class="m-node-card__title-wrap">
        <span v-if="showSeparateFlag" class="m-node-card__flag" aria-hidden="true">{{ countryFlag }}</span>
        <h3 class="m-node-card__name">{{ node.name }}</h3>
      </div>

      <div class="m-node-card__head-right" @click.stop>
        <span class="m-pill m-node-status" :class="node.online ? 'm-pill--success' : 'm-pill--info'">
          <span class="m-node-status__dot" />
          {{ node.online ? '在线' : '离线' }}
        </span>
        <button
          v-if="canManage"
          type="button"
          class="m-node-more"
          aria-label="服务器操作"
          @click.stop="$emit('open-actions', node)"
        >
          <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
            <circle cx="5" cy="12" r="2" />
            <circle cx="12" cy="12" r="2" />
            <circle cx="19" cy="12" r="2" />
          </svg>
        </button>
      </div>
    </div>

    <!-- Row 2: Location & System (single crisp baseline) + Separate Capability Tags -->
    <div class="m-node-card__meta">
      <span v-if="locationName" class="m-node-card__loc">{{ locationName }}</span>
      <span v-if="locationName" class="m-node-card__dot">·</span>
      <OsIcon :os="osLabel" class="m-node-card__os-icon" />
      <span class="m-node-card__sys truncate">{{ systemInfoText }}</span>
    </div>

    <div
      v-if="agentUpdateAvailable || singBoxUpdateAvailable || node.ddns_enabled || node.bandwidth"
      class="m-node-card__tags"
    >
      <button
        v-if="canUpgrade && (agentUpdateAvailable || singBoxUpdateAvailable)"
        type="button"
        class="m-node-tag m-node-tag--upgrade is-interactive"
        @click.stop="onUpgradeTap"
      >
        <span class="m-tag-dot" />
        <span>可升级</span>
      </button>
      <span
        v-else-if="agentUpdateAvailable || singBoxUpdateAvailable"
        class="m-node-tag m-node-tag--upgrade"
      >
        <span class="m-tag-dot" />
        <span>可升级</span>
      </span>

      <span v-if="node.ddns_enabled" class="m-node-tag m-node-tag--ddns">
        <span class="m-tag-dot" />
        <span>DDNS</span>
      </span>

      <span v-if="node.bandwidth" class="m-node-tag m-node-tag--bw">
        {{ formatBandwidth(node.bandwidth) }}
      </span>
    </div>

    <!-- Row 3: 2x2 Meter Grid (aligned identical heights; tap Traffic for breakdown) -->
    <div class="m-node-meters">
      <Meter label="CPU" tone="cpu" :pct="cpuPercent" :foot="cpuFootText" />
      <Meter label="内存" tone="mem" :pct="memPercent" :foot="memFootText" />
      <Meter label="硬盘" tone="disk" :pct="diskPercent" :foot="diskFootText" />
      <div
        class="m-traffic-meter"
        :class="{ 'is-tappable': !isShareMode, 'is-open': showTrafficBreakdown }"
        @click.stop="!isShareMode && (showTrafficBreakdown = !showTrafficBreakdown)"
      >
        <Meter
          label="流量"
          tone="net"
          :pct="trafficPercent"
          :foot="trafficFootText"
          :empty="isShareMode ? '已隐藏' : '∞'"
        >
          <template v-if="!isShareMode" #label-extra>
            <span class="m-traffic-hint">
              {{ showTrafficBreakdown ? '收起 ▲' : '明细 ▾' }}
            </span>
          </template>
        </Meter>
      </div>
    </div>

    <!-- Touch-Accessible Traffic Breakdown (replaces desktop hover tooltip) -->
    <div v-if="showTrafficBreakdown && !isShareMode" class="m-traffic-drawer font-mono" @click.stop>
      <div class="m-traffic-drawer__row">
        <span class="m-traffic-drawer__lbl">本月主机流量</span>
        <span class="m-traffic-drawer__val">
          {{ formatBytes(traffic.monthlyTotal) }}
          <small>(↓ {{ formatBytes(traffic.monthlyRx) }} · ↑ {{ formatBytes(traffic.monthlyTx) }})</small>
        </span>
      </div>
      <div v-if="traffic.proxyMonthlyTotal > 0" class="m-traffic-drawer__row">
        <span class="m-traffic-drawer__lbl">本月代理转发</span>
        <span class="m-traffic-drawer__val">
          {{ formatBytes(traffic.proxyMonthlyTotal) }}
          <small>(↓ {{ formatBytes(traffic.proxyMonthlyRx) }} · ↑ {{ formatBytes(traffic.proxyMonthlyTx) }})</small>
        </span>
      </div>
      <div class="m-traffic-drawer__row">
        <span class="m-traffic-drawer__lbl">历史累计</span>
        <span class="m-traffic-drawer__val">
          {{ formatBytes(traffic.lifetimeTotal) }}
          <small>(↓ {{ formatBytes(traffic.lifetimeRx) }} · ↑ {{ formatBytes(traffic.lifetimeTx) }})</small>
        </span>
      </div>
      <div v-if="traffic.quotaBytes > 0" class="m-traffic-drawer__row">
        <span class="m-traffic-drawer__lbl">套餐剩余</span>
        <span class="m-traffic-drawer__val">
          {{ formatBytes(Math.max(0, traffic.quotaBytes - traffic.monthlyTotal)) }}
          <small>({{ trafficPercent ?? 0 }}% 已用)</small>
        </span>
      </div>
    </div>

    <!-- Row 4: 24h Latency & Loss Heat Tracks -->
    <div class="m-node-latency">
      <LatencyPanel :latency="latencyData" />
    </div>

    <!-- Row 5: Expiry Bar -->
    <div class="m-node-expiry">
      <span class="m-node-expiry__lbl">到期</span>
      <span class="m-node-expiry__val font-mono" :class="expiryToneClass">{{ expiryText }}</span>
    </div>

    <!-- Row 6: Consistent Provider Row + Aligned Connections & Price -->
    <dl class="m-node-facts font-mono">
      <div v-if="node.provider" class="m-node-fact m-node-fact--provider">
        <dt>厂商</dt>
        <dd>
          <a
            v-if="node.provider_url"
            :href="node.provider_url"
            target="_blank"
            rel="noopener noreferrer"
            class="m-node-provider-link"
            @click.stop
          >
            {{ node.provider }}
          </a>
          <span v-else>{{ node.provider }}</span>
        </dd>
      </div>
      <div class="m-node-fact">
        <dt>连接</dt>
        <dd class="tnum">
          {{ activeConnections }}
          <small v-if="tcpCount || udpCount" class="m-node-sock">(T:{{ tcpCount }} U:{{ udpCount }})</small>
        </dd>
      </div>
      <div class="m-node-fact">
        <dt>续费</dt>
        <dd>{{ priceText }}</dd>
      </div>
    </dl>

    <!-- Row 7: Live RX / TX Speeds -->
    <div class="m-node-rates">
      <div class="m-node-rate-pill">
        <span class="m-node-rate-lbl is-down">↓ 下行</span>
        <span class="m-node-rate-val tnum font-mono">{{ formatBytes(netRxRate, '/s') }}</span>
      </div>
      <div class="m-node-rate-pill">
        <span class="m-node-rate-lbl is-up">↑ 上行</span>
        <span class="m-node-rate-val tnum font-mono">{{ formatBytes(netTxRate, '/s') }}</span>
      </div>
    </div>
  </article>
</template>

<script setup>
import { computed, inject, ref } from 'vue'
import LatencyPanel from '../../components/LatencyPanel.vue'
import Meter from '../../components/Meter.vue'
import OsIcon from '../../components/OsIcon.vue'
import { formatBandwidth, formatBytes } from '../../format'
import { isShareMode } from '../../share'
import { isNewerVersion } from '../../version'
import { nodeExpiryInfo, nodeFlag, nodeLocationLabel, nodeTrafficStats } from '../utils/nodeHelpers'

const props = defineProps({
  node: { type: Object, required: true },
  metrics: { type: Object, default: () => ({}) },
  live: { type: Object, default: null },
  canManage: { type: Boolean, default: false },
  singBoxLatest: { type: Object, default: () => ({}) },
})

const emit = defineEmits(['open', 'open-actions', 'action'])

const appState = inject('appState', null)
const isAdmin = inject('isAdmin', { value: false })

const showTrafficBreakdown = ref(false)

const canUpgrade = computed(() => props.canManage && Boolean(isAdmin?.value))
const agentUpdateAvailable = computed(() =>
  isNewerVersion(appState?.systemUpdate?.latest_version, props.node.agent_version)
)
const singBoxUpdateAvailable = computed(() => {
  const latest = props.singBoxLatest?.[props.node.architecture]
  return Boolean(latest && props.node.sing_box_version && props.node.sing_box_version !== latest)
})

function onUpgradeTap() {
  if (!canUpgrade.value) return
  if (agentUpdateAvailable.value && !singBoxUpdateAvailable.value) {
    emit('action', { command: 'upgradeAgent', node: props.node })
  } else if (singBoxUpdateAvailable.value && !agentUpdateAvailable.value) {
    emit('action', { command: 'upgradeSingBox', node: props.node })
  } else {
    emit('open-actions', props.node)
  }
}

const countryFlag = computed(() => nodeFlag(props.node))
const showSeparateFlag = computed(() => !/^[\u{1F1E6}-\u{1F1FF}]{2}/u.test(String(props.node.name || '').trim()))
const locationName = computed(() => {
  if (props.node.city && props.node.city.trim()) return props.node.city.trim()
  return nodeLocationLabel(props.node)
})

const hostMetrics = computed(() => (props.node.online ? (props.live?.host || props.metrics?.host) : null))
const latencyData = computed(() => props.live?.latency || props.metrics?.latency || null)
const osLabel = computed(() => props.node.os_name || hostMetrics.value?.os_name || props.node.os || '')

const systemInfoText = computed(() => {
  const parts = []
  if (osLabel.value) {
    parts.push(osLabel.value.replace(/\s*\([^)]*\)/g, '').replace(/\s+LTS/i, '').trim())
  }
  const virt = props.node.virt || hostMetrics.value?.virt
  if (virt && virt !== 'none') parts.push(virt)
  if (props.node.arch || props.node.architecture) parts.push(props.node.arch || props.node.architecture)
  return parts.length ? parts.join(' · ') : 'Linux'
})

const cpuPercent = computed(() => (props.node.online && hostMetrics.value ? hostMetrics.value.cpu_percent || 0 : null))
const cpuFootText = computed(() => {
  if (!props.node.online || !hostMetrics.value) return '—'
  const cores = props.node.cpu_cores || hostMetrics.value.cpu_cores || 1
  const load = hostMetrics.value.load_avg
  if (Array.isArray(load) && load.length >= 3) {
    return `${load[0].toFixed(2)} ${load[1].toFixed(2)} ${load[2].toFixed(2)}`
  }
  return `${cores} 核`
})

const memPercent = computed(() => {
  if (!props.node.online || !hostMetrics.value?.memory_total) return null
  return ((hostMetrics.value.memory_used || 0) / hostMetrics.value.memory_total) * 100
})
const memFootText = computed(() => {
  if (!props.node.online || !hostMetrics.value?.memory_total) return '—'
  return `${formatBytes(hostMetrics.value.memory_used || 0)} / ${formatBytes(hostMetrics.value.memory_total)}`
})

const diskPercent = computed(() => {
  if (!props.node.online || !hostMetrics.value?.disk_total) return null
  return ((hostMetrics.value.disk_used || 0) / hostMetrics.value.disk_total) * 100
})
const diskFootText = computed(() => {
  if (!props.node.online || !hostMetrics.value?.disk_total) return '—'
  return `${formatBytes(hostMetrics.value.disk_used || 0)} / ${formatBytes(hostMetrics.value.disk_total)}`
})

const traffic = computed(() => nodeTrafficStats(props.node, props.metrics))
const trafficPercent = computed(() => traffic.value.quotaPercent)
const trafficFootText = computed(() => {
  const used = formatBytes(traffic.value.monthlyTotal)
  if (isShareMode) return `${used} / 已隐藏`
  if (traffic.value.quotaBytes > 0) return `${used} / ${formatBytes(traffic.value.quotaBytes)}`
  return `${used} / ∞`
})

const activeConnections = computed(() => {
  if (!props.node.online) return 0
  if (props.live?.connection_count !== undefined) return props.live.connection_count
  if (Array.isArray(props.metrics?.connections)) return props.metrics.connections.length
  return 0
})
const tcpCount = computed(() => hostMetrics.value?.tcp || 0)
const udpCount = computed(() => hostMetrics.value?.udp || 0)
const netRxRate = computed(() => (props.node.online && hostMetrics.value ? hostMetrics.value.net_rx_rate || 0 : 0))
const netTxRate = computed(() => (props.node.online && hostMetrics.value ? hostMetrics.value.net_tx_rate || 0 : 0))

const expiryInfo = computed(() => {
  if (isShareMode) return { label: '已隐藏', tone: 'muted' }
  return nodeExpiryInfo(props.node) || { label: '永久免费', tone: 'success' }
})
const expiryText = computed(() => expiryInfo.value.label)
const expiryToneClass = computed(() => {
  const t = expiryInfo.value.tone
  if (t === 'danger') return 'is-danger'
  if (t === 'warning') return 'is-warn'
  if (t === 'success') return 'is-ok'
  return ''
})

const priceText = computed(() => {
  if (isShareMode) return '已隐藏'
  const p = props.node.price
  if (!p || p === '0.00' || p === '0' || props.node.price_period === 'free') return '免费'
  const sym = props.node.currency === 'CNY' ? '¥' : props.node.currency === 'EUR' ? '€' : '$'
  const map = { day: '天', month: '月', quarter: '季', year: '年' }
  const per = map[props.node.price_period] || '月'
  return `${sym}${p} / ${per}`
})
</script>

<style scoped>
.m-node-card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 10px;
  cursor: pointer;
}
.m-node-card.is-offline {
  opacity: 0.78;
}
.m-node-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-width: 0;
}
.m-node-card__title-wrap {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  flex: 1;
}
.m-node-card__flag {
  font-size: 16px;
  line-height: 1;
  flex: none;
}
.m-node-card__name {
  margin: 0;
  font-size: 15.5px;
  font-weight: 650;
  color: var(--sb-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.m-node-card__head-right {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: none;
}
.m-node-status__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}
.m-node-more {
  position: relative;
  width: 38px;
  height: 38px;
  display: grid;
  place-items: center;
  padding: 0;
  border-radius: 10px;
  border: 1px solid var(--sb-line-strong);
  background: var(--sb-surface-2);
  color: var(--sb-text-2);
  cursor: pointer;
}
.m-node-more::after {
  content: '';
  position: absolute;
  inset: -4px;
}
.m-node-more svg {
  width: 16px;
  height: 16px;
}
.m-node-more:active {
  background: var(--sb-surface-3);
}

.m-node-card__meta {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  color: var(--sb-muted);
  line-height: 1.35;
  min-width: 0;
  overflow: hidden;
}
.m-node-card__loc {
  flex: none;
  color: var(--sb-text-2);
  font-weight: 550;
}
.m-node-card__dot {
  flex: none;
}
.m-node-card__os-icon {
  width: 14px;
  height: 14px;
  flex: none;
}
.m-node-card__sys {
  flex: 1 1 auto;
  min-width: 0;
  color: var(--sb-muted);
}
.m-node-card__tags {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

.m-node-tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 7px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1.3;
  border: 1px solid var(--sb-line);
  background: var(--sb-surface-2);
  color: var(--sb-text-2);
}
.m-node-tag.is-interactive {
  position: relative;
  cursor: pointer;
  min-height: 28px;
}
.m-node-tag.is-interactive::after {
  content: '';
  position: absolute;
  inset: -6px -4px;
}
.m-node-tag--upgrade {
  color: var(--sb-warning);
  background: color-mix(in oklab, var(--sb-warning) 12%, transparent);
  border-color: color-mix(in oklab, var(--sb-warning) 35%, transparent);
}
.m-node-tag--ddns {
  color: var(--sb-info, #38bdf8);
  background: color-mix(in oklab, var(--sb-info, #38bdf8) 10%, transparent);
  border-color: color-mix(in oklab, var(--sb-info, #38bdf8) 28%, transparent);
}
.m-tag-dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: currentColor;
}

.m-node-meters {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.m-node-meters > * {
  flex: 1 1 clamp(8.25rem, calc(50% - 0.25rem), 100%);
  min-width: 0;
}
.m-traffic-meter {
  position: relative;
  display: flex;
  flex-direction: column;
}
.m-traffic-meter.is-tappable {
  cursor: pointer;
}
.m-traffic-hint {
  flex: none;
  font-size: 10px;
  font-weight: 550;
  line-height: 1.3;
  color: var(--sb-muted);
  padding: 1px 5px;
  border-radius: 4px;
  background: var(--sb-surface-2);
  white-space: nowrap;
}

.m-traffic-drawer {
  padding: 9px 11px;
  border-radius: 10px;
  background: var(--sb-surface-2);
  border: 1px solid var(--sb-line);
  display: flex;
  flex-direction: column;
  gap: 5px;
  font-size: 11.5px;
}
.m-traffic-drawer__row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}
.m-traffic-drawer__lbl {
  color: var(--sb-muted);
  flex: none;
}
.m-traffic-drawer__val {
  color: var(--sb-text);
  font-weight: 600;
  text-align: right;
}
.m-traffic-drawer__val small {
  color: var(--sb-muted);
  font-weight: 400;
  font-size: 11px;
}

.m-node-expiry {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 7px 11px;
  border-radius: 10px;
  background: var(--sb-surface-2);
  border: 1px solid var(--sb-line);
  font-size: 12px;
}
.m-node-expiry__lbl {
  color: var(--sb-muted);
}
.m-node-expiry__val {
  font-weight: 600;
  color: var(--sb-text);
}
.m-node-expiry__val.is-ok { color: var(--sb-success); }
.m-node-expiry__val.is-warn { color: var(--sb-warning); }
.m-node-expiry__val.is-danger { color: var(--sb-danger); }

.m-node-facts {
  margin: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  padding: 0 2px;
}
.m-node-fact {
  flex: 1 1 clamp(7.5rem, calc(50% - 0.25rem), 100%);
  min-width: 0;
}
.m-node-fact--provider {
  flex: 1 0 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.625rem;
  padding-bottom: 0.375rem;
  border-bottom: 1px solid var(--sb-line);
}
.m-node-fact--provider dt {
  margin-bottom: 0;
  flex: none;
}
.m-node-fact--provider dd {
  text-align: right;
}
.m-node-fact dt {
  font-size: 11px;
  color: var(--sb-muted);
  margin-bottom: 2px;
}
.m-node-fact dd {
  margin: 0;
  font-size: 12px;
  font-weight: 600;
  color: var(--sb-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.m-node-sock {
  font-size: 10.5px;
  font-weight: 400;
  color: var(--sb-muted);
}
.m-node-provider-link {
  color: var(--sb-text);
  text-decoration: underline;
  text-decoration-color: var(--sb-line-strong);
}

.m-node-rates {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.m-node-rate-pill {
  flex: 1 1 clamp(8rem, calc(50% - 0.25rem), 100%);
  min-width: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.375rem;
  padding: 0.4375rem 0.625rem;
  border-radius: 0.625rem;
  background: var(--sb-surface-2);
  border: 1px solid var(--sb-line);
  font-size: 12px;
}
.m-node-rate-lbl {
  font-weight: 600;
  font-size: 11.5px;
}
.m-node-rate-lbl.is-down { color: var(--sb-chart-2); }
.m-node-rate-lbl.is-up { color: var(--sb-chart-4); }
.m-node-rate-val {
  color: var(--sb-text);
  font-weight: 650;
  font-size: 12px;
}
</style>
