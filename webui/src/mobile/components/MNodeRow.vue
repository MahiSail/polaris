<template>
  <article
    class="m-card m-node-row"
    :class="{ 'is-offline': !node.online }"
    tabindex="0"
    @click="$emit('open', node.id)"
    @keydown.enter="$emit('open', node.id)"
  >
    <div class="m-node-row__top">
      <div class="m-node-row__ident">
        <span class="m-item__dot" :class="{ 'is-on': node.online }" />
        <span v-if="showSeparateFlag" class="m-node-row__flag" aria-hidden="true">{{ countryFlag }}</span>
        <strong class="m-node-row__name">{{ node.name }}</strong>
        <span v-if="agentUpdateAvailable || singBoxUpdateAvailable" class="m-node-row__badge is-warn">可升级</span>
        <span v-if="node.ddns_enabled" class="m-node-row__badge is-info">DDNS</span>
      </div>
      <button
        v-if="canManage"
        type="button"
        class="m-node-row__more"
        aria-label="服务器操作"
        @click.stop="$emit('open-actions', node)"
      >
        ···
      </button>
    </div>

    <div class="m-node-row__sub">
      <span v-if="locationName">{{ locationName }}</span>
      <span v-if="locationName">·</span>
      <OsIcon :os="osLabel" class="m-node-row__os" />
      <span class="m-node-row__sys">{{ osLabel || 'Linux' }} {{ node.architecture || '' }}</span>
      <template v-if="node.provider">
        <span>·</span>
        <span>{{ node.provider }}</span>
      </template>
      <template v-if="node.bandwidth">
        <span>·</span>
        <span>{{ formatBandwidth(node.bandwidth) }}</span>
      </template>
    </div>

    <div class="m-node-row__kpis font-mono">
      <div class="m-row-kpi">
        <span>CPU</span>
        <b>{{ cpuText }}</b>
      </div>
      <div class="m-row-kpi">
        <span>内存</span>
        <b>{{ memText }}</b>
      </div>
      <div class="m-row-kpi">
        <span>本月流量</span>
        <b>{{ formatBytes(traffic.monthlyTotal) }}</b>
      </div>
      <div class="m-row-kpi">
        <span>延迟 / 连接</span>
        <b>{{ latencyText }} · {{ activeConnections }}</b>
      </div>
    </div>

    <div class="m-node-row__foot font-mono">
      <span class="m-node-row__rates">
        ↓ {{ formatBytes(netRxRate, '/s') }} · ↑ {{ formatBytes(netTxRate, '/s') }}
      </span>
      <span v-if="!isShareMode" class="m-node-row__expiry" :class="`is-${expiry.tone}`">
        {{ expiry.label }}
      </span>
    </div>
  </article>
</template>

<script setup>
import { computed, inject } from 'vue'
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

defineEmits(['open', 'open-actions'])

const appState = inject('appState', null)

const countryFlag = computed(() => nodeFlag(props.node))
const showSeparateFlag = computed(() => !/^[\u{1F1E6}-\u{1F1FF}]{2}/u.test(String(props.node.name || '').trim()))
const locationName = computed(() => props.node.city?.trim() || nodeLocationLabel(props.node))
const host = computed(() => (props.node.online ? (props.live?.host || props.metrics?.host) : null))
const osLabel = computed(() => props.node.os_name || host.value?.os_name || props.node.os || '')

const agentUpdateAvailable = computed(() =>
  isNewerVersion(appState?.systemUpdate?.latest_version, props.node.agent_version)
)
const singBoxUpdateAvailable = computed(() => {
  const latest = props.singBoxLatest?.[props.node.architecture]
  return Boolean(latest && props.node.sing_box_version && props.node.sing_box_version !== latest)
})

const cpuText = computed(() => (props.node.online && host.value?.cpu_percent != null ? `${Math.round(host.value.cpu_percent)}%` : '—'))
const memText = computed(() => {
  if (!props.node.online || !host.value?.memory_total) return '—'
  return `${Math.round(((host.value.memory_used || 0) / host.value.memory_total) * 100)}%`
})
const traffic = computed(() => nodeTrafficStats(props.node, props.metrics))

const latencyData = computed(() => props.live?.latency || props.metrics?.latency || null)
const latencyText = computed(() => {
  const val = latencyData.value?.average_latency_ms ?? latencyData.value?.avgLatency ?? latencyData.value?.latency
  return val != null ? `${Math.round(val)}ms` : '—'
})
const activeConnections = computed(() => {
  if (!props.node.online) return 0
  if (props.live?.connection_count !== undefined) return props.live.connection_count
  if (Array.isArray(props.metrics?.connections)) return props.metrics.connections.length
  return 0
})

const netRxRate = computed(() => (props.node.online && host.value ? host.value.net_rx_rate || 0 : 0))
const netTxRate = computed(() => (props.node.online && host.value ? host.value.net_tx_rate || 0 : 0))
const expiry = computed(() => nodeExpiryInfo(props.node) || { label: '永久免费', tone: 'success' })
</script>

<style scoped>
.m-node-row {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px;
  cursor: pointer;
}
.m-node-row.is-offline {
  opacity: 0.78;
}
.m-node-row__top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.m-node-row__ident {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  flex: 1;
}
.m-node-row__flag {
  font-size: 15px;
  flex: none;
}
.m-node-row__name {
  font-size: 14.5px;
  font-weight: 650;
  color: var(--sb-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.m-node-row__badge {
  flex: none;
  padding: 1px 6px;
  border-radius: 999px;
  font-size: 10.5px;
  font-weight: 600;
}
.m-node-row__badge.is-warn {
  color: var(--sb-warning);
  background: color-mix(in oklab, var(--sb-warning) 14%, transparent);
}
.m-node-row__badge.is-info {
  color: var(--sb-info, #38bdf8);
  background: color-mix(in oklab, var(--sb-info, #38bdf8) 12%, transparent);
}
.m-node-row__more {
  position: relative;
  width: 36px;
  height: 36px;
  border-radius: 8px;
  border: 1px solid var(--sb-line-strong);
  background: var(--sb-surface-2);
  color: var(--sb-text-2);
  font-weight: 700;
  cursor: pointer;
  flex: none;
}
.m-node-row__more::after {
  content: '';
  position: absolute;
  inset: -4px;
}
.m-node-row__sub {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 11.5px;
  color: var(--sb-muted);
  min-width: 0;
  overflow: hidden;
}
.m-node-row__sub > span:last-child,
.m-node-row__sys {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}
.m-node-row__sub > span:not(.m-node-row__sys) {
  flex: none;
  white-space: nowrap;
}
.m-node-row__os {
  width: 13px;
  height: 13px;
  flex: none;
}
.m-node-row__kpis {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 6px;
  padding: 8px;
  border-radius: 9px;
  background: var(--sb-surface-2);
  border: 1px solid var(--sb-line);
}
.m-row-kpi {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.m-row-kpi span {
  font-size: 10px;
  color: var(--sb-muted);
}
.m-row-kpi b {
  font-size: 12px;
  font-weight: 650;
  color: var(--sb-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.m-node-row__foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 11.5px;
}
.m-node-row__rates {
  color: var(--sb-text-2);
}
.m-node-row__expiry.is-success { color: var(--sb-success); }
.m-node-row__expiry.is-warning { color: var(--sb-warning); }
.m-node-row__expiry.is-danger { color: var(--sb-danger); }
.m-node-row__expiry.is-info { color: var(--sb-muted); }
</style>
