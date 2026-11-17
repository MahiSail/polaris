<script setup>
import { computed, inject, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Search } from '@element-plus/icons-vue'
import { writeClipboard } from '../../clipboard'
import { formatBytes, formatDateTime, includesText } from '../../format'
import { connectionSnapshots, mergeChainedConnections, subscribeConnections } from '../../connections'
import MPage from '../components/MPage.vue'
import MPicker from '../components/MPicker.vue'
import MActionSheet from '../components/MActionSheet.vue'

const appState = inject('appState')
const loadNodes = inject('loadNodes')
const loading = ref(true)
const selectedNode = ref('')
const selectedOutbound = ref('')
const keyword = ref('')
// 连接可能有上千条，全画出来手机会卡住；先给一页，看得完再要下一页。
const visible = ref(30)
let stopConnections

// sing-box 把目标地址和嗅探到的域名分开报，这里合成操作者会写的那一种：
// 有域名就用域名，永远带端口。
function splitAddress(address) {
  const text = String(address || '')
  const separator = text.lastIndexOf(':')
  if (separator < 0) return { host: text, port: '' }
  return { host: text.slice(0, separator).replace(/^\[|\]$/g, ''), port: text.slice(separator + 1) }
}

function targetLabel(connection) {
  const address = splitAddress(connection.destination)
  const host = connection.host || address.host
  if (!host) return address.port ? `:${address.port}` : '—'
  return address.port ? `${host}:${address.port}` : host
}

// 认一条连接靠的是用户别名；没有别名可显示时，才退回它进来的那个入站服务。
function entryLabel(connection) {
  return connection.user || connection.listener_name || '—'
}

function startedClock(startedAt) {
  if (!startedAt) return ''
  const text = formatDateTime(startedAt, '')
  if (!text || text === '—') return ''
  const parts = text.split(/\s+/)
  return parts.at(-1) || text
}

// 卡上的脚注：从哪个入站节点进来、从哪个地址来的。服务器名不在这里 ——
// 一条连接落在哪台机器上是筛选用的条件，不是认出这条连接靠的东西。
function sourceLine(row) {
  return [row.entry !== '—' ? row.entry : '', row.source_ip || row.source, row.source_location]
    .filter(Boolean).join(' · ') || '未知来源'
}

function isDirect(exit) {
  if (!exit) return true
  const lower = String(exit).toLowerCase()
  return lower === 'direct' || lower === '直连'
}

const rows = computed(() => {
  const flat = [...connectionSnapshots.value.values()].flatMap((result) => {
    const node = appState.nodes.find((item) => item.id === result.node_id)
    return (result.connections || []).map((connection) => ({
      ...connection,
      node_id: result.node_id,
      node_name: node?.name || result.node_id,
      clock_offset_ms: result.clock_offset_ms || 0,
      // master 会把 sing-box 的出站标签解析成操作者配置的出口，解析不到才显示原始标签。
      exit: connection.outbound_name || connection.outbound || connection.chains?.[0] || 'DIRECT',
      entry: entryLabel(connection),
      target: targetLabel(connection),
    }))
  })
  return mergeChainedConnections(flat, appState.nodes)
})
const collectedAt = computed(() => [...connectionSnapshots.value.values()].map((result) => result.collected_at).filter(Boolean).sort().at(-1) || '')
const nodeOptions = computed(() => [{ value: '', label: '全部服务器' }, ...appState.nodes.map((node) => ({ value: node.id, label: node.name }))])
const outboundOptions = computed(() => [
  { value: '', label: '全部出口' },
  ...[...new Set(rows.value.map((row) => row.exit).filter(Boolean))].sort().map((exit) => ({ value: exit, label: exit })),
])
const filteredRows = computed(() => rows.value.filter((row) => {
  if (selectedNode.value && row.node_id !== selectedNode.value && !row.chain_node_ids?.includes(selectedNode.value)) return false
  if (selectedOutbound.value && row.exit !== selectedOutbound.value && !row.chain_exit?.includes(selectedOutbound.value) && !row.exit?.includes(selectedOutbound.value)) return false
  return includesText([
    row.node_name, row.source, row.source_ip, row.source_location, row.target,
    row.entry, row.user, row.listener_name, row.exit, row.network,
  ], keyword.value)
}))
const shown = computed(() => filteredRows.value.slice(0, visible.value))

watch([keyword, selectedNode, selectedOutbound], () => { visible.value = 30 })

// 卡上只放目标、上下行和来源；入站服务、开始时间这些点开这条再看。
const detailOpen = ref(false)
const detailTarget = ref(null)
const details = computed(() => {
  const row = detailTarget.value
  if (!row) return []
  return [
    { label: '目标', value: [row.target, row.destination_location].filter(Boolean).join(' · '), mono: true },
    { label: '服务器', value: row.node_name },
    { label: '入站节点', value: row.entry },
    { label: '出口', value: row.exit },
    { label: '来源', value: [row.source_ip || row.source, row.source_location].filter(Boolean).join(' · ') || '未知', mono: true },
    { label: '网络', value: (row.network || '').toUpperCase() || '—' },
    { label: '实时速率', value: row.has_rates ? `↓ ${formatBytes(row.download_rate, '/s')} · ↑ ${formatBytes(row.upload_rate, '/s')}` : '等待上报' },
    { label: '累计流量', value: `↓ ${formatBytes(row.download)} · ↑ ${formatBytes(row.upload)}` },
    { label: '开始于', value: formatDateTime(row.started_at) },
  ]
})

function openDetail(row) {
  detailTarget.value = row
  detailOpen.value = true
}

const connActions = computed(() => {
  const row = detailTarget.value
  if (!row) return []
  const list = [
    { key: 'copy-target', label: '复制目标地址', hint: row.target },
  ]
  if (row.source_ip || row.source) {
    const ip = row.source_ip || row.source
    list.push({ key: 'filter-ip', label: '仅看此来源 IP', hint: ip })
  }
  if (row.node_id) {
    list.push({ key: 'filter-node', label: '仅看此服务器', hint: row.node_name })
  }
  if (keyword.value || selectedNode.value || selectedOutbound.value) {
    list.push({ key: 'clear-filters', label: '清除筛选条件' })
  }
  return list
})

async function runConnAction(key) {
  const row = detailTarget.value
  if (!row) return
  if (key === 'copy-target') {
    await writeClipboard(row.target || '')
    ElMessage.success('已复制目标地址')
  } else if (key === 'filter-ip') {
    keyword.value = row.source_ip || row.source || ''
  } else if (key === 'filter-node') {
    selectedNode.value = row.node_id
  } else if (key === 'clear-filters') {
    keyword.value = ''
    selectedNode.value = ''
    selectedOutbound.value = ''
  }
}

async function load() {
  loading.value = true
  try {
    await loadNodes()
  } finally {
    loading.value = false
  }
}

// 建流时服务端立刻回一帧快照，但那时 hub 通常还是空的：各节点的连接要再过
// 几百毫秒才到。等到真有数据、或等满兜底时间再收起加载态，否则会先闪一下
// 「当前没有活动连接」再出列表。
let settleTimer
function settle() {
  clearTimeout(settleTimer)
  loading.value = false
}

onMounted(async () => {
  try {
    await loadNodes()
  } finally {
    stopConnections = subscribeConnections(() => { if (rows.value.length) settle() })
    settleTimer = setTimeout(settle, 1500)
  }
})
onBeforeUnmount(() => {
  clearTimeout(settleTimer)
  stopConnections?.()
})
</script>

<template>
  <MPage title="当前连接" :loading="loading">
    <template #actions>
      <el-button
        circle
        size="small"
        :icon="Refresh"
        :loading="loading"
        aria-label="刷新连接"
        @click="load"
      />
    </template>
    <div class="m-listbar">
      <el-input v-model="keyword" clearable :prefix-icon="Search" placeholder="搜索 IP、目标、入站节点或出口" />
      <div class="m-filters">
        <MPicker v-model="selectedNode" chip :options="nodeOptions" title="按服务器筛选" placeholder="全部服务器" />
        <MPicker v-model="selectedOutbound" chip :options="outboundOptions" title="按出口筛选" placeholder="全部出口" />
      </div>
    </div>
    <div class="m-count">{{ filteredRows.length }} 条 · 采集于 {{ formatDateTime(collectedAt, '暂无数据') }}</div>

    <article
      v-for="row in shown"
      :key="`${row.node_id}/${row.id}`"
      class="m-conn-card"
      tabindex="0"
      @click="openDetail(row)"
      @keydown.enter="openDetail(row)"
    >
      <!-- Row 1: Host (left, spacious) + Compact Protocol / Hop Tags (right, never wrap-squashing host) -->
      <div class="m-conn-head">
        <div class="m-conn-host font-mono" :title="row.target">{{ row.target }}</div>
        <div class="m-conn-badges">
          <span v-if="row.chain_hop_count > 1" class="m-conn-tag m-conn-tag--chain">{{ row.chain_hop_count }}跳</span>
          <span class="m-conn-tag font-mono">{{ (row.network || 'TCP').toUpperCase() }}</span>
        </div>
      </div>

      <!-- Row 2: Destination Location & Exit Policy (aligned dedicated row, handles ultra-short to ultra-long exits gracefully) -->
      <div class="m-conn-route">
        <span class="m-conn-geo truncate">
          {{ row.destination_location || '归属地未知' }}
        </span>
        <span
          class="m-conn-exit"
          :class="isDirect(row.exit) ? 'is-direct' : 'is-exit'"
          :title="row.exit"
        >
          {{ row.exit }}
        </span>
      </div>

      <!-- Row 3: Symmetric 3-column metrics (arrow inside label so full KB/s & MB/s values never truncate) -->
      <div class="m-conn-rates">
        <div class="rate-col">
          <span class="rate-lbl">↓ 实时下行</span>
          <div class="rate-val font-mono" :class="{ 'is-down-active': row.has_rates && Number(row.download_rate) > 0 }">
            {{ row.has_rates ? formatBytes(row.download_rate, '/s') : '—' }}
          </div>
        </div>
        <div class="rate-col">
          <span class="rate-lbl">↑ 实时上行</span>
          <div class="rate-val font-mono" :class="{ 'is-up-active': row.has_rates && Number(row.upload_rate) > 0 }">
            {{ row.has_rates ? formatBytes(row.upload_rate, '/s') : '—' }}
          </div>
        </div>
        <div class="rate-col">
          <span class="rate-lbl">累计流量</span>
          <div class="rate-val font-mono">
            {{ formatBytes((row.download || 0) + (row.upload || 0)) }}
          </div>
        </div>
      </div>

      <!-- Row 4 & 5: Entry + Server/Chain Tag, Source IP/Geo + Start Time -->
      <div class="m-conn-footer">
        <div class="m-conn-footer__row">
          <span class="source-client truncate">{{ row.entry !== '—' ? row.entry : '未知客户端' }}</span>
          <span class="m-conn-node-tag truncate" :title="row.node_name">{{ row.node_name }}</span>
        </div>
        <div class="m-conn-footer__row m-conn-footer__sub">
          <span class="source-ip-geo truncate">
            <span class="font-mono">{{ row.source_ip || row.source || '—' }}</span>
            <template v-if="row.source_location"> · {{ row.source_location }}</template>
          </span>
          <span v-if="startedClock(row.started_at)" class="source-time font-mono">{{ startedClock(row.started_at) }}</span>
        </div>
      </div>
    </article>

    <div v-if="!filteredRows.length && !loading" class="m-empty">当前没有活动连接</div>
    <button v-if="filteredRows.length > visible" type="button" class="m-load-more" @click="visible += 30">
      加载更多（还有 {{ filteredRows.length - visible }} 条）
    </button>

    <MActionSheet
      v-model="detailOpen"
      :title="detailTarget?.target"
      :details="details"
      :actions="connActions"
      @select="runConnAction"
    />
  </MPage>
</template>
<style scoped>
.m-conn-card {
  display: block;
  width: 100%;
  padding: 13px 14px 12px;
  margin-bottom: 10px;
  background: var(--sb-panel-solid);
  border: 1px solid var(--sb-line);
  border-radius: var(--m-radius);
  box-shadow: var(--sb-shadow-sm);
  cursor: pointer;
  text-align: left;
  user-select: none;
  transition: transform 0.12s ease, border-color 0.15s ease;
}
.m-conn-card:active {
  transform: scale(0.992);
}
.m-conn-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
  min-width: 0;
}
.m-conn-host {
  flex: 1;
  min-width: 0;
  color: var(--sb-text);
  font-size: 14.5px;
  font-weight: 650;
  line-height: 1.35;
  overflow-wrap: anywhere;
}
.m-conn-badges {
  display: flex;
  align-items: center;
  gap: 4px;
  flex: none;
  white-space: nowrap;
}
.m-conn-route {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-top: 6px;
  min-width: 0;
}
.m-conn-geo {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 11.5px;
  color: var(--sb-muted);
  line-height: 1.35;
}
.m-conn-exit {
  flex: 0 1 auto;
  max-width: 66%;
  padding: 2px 8px;
  border-radius: 7px;
  font-size: 11px;
  font-weight: 600;
  line-height: 17px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  border: 1px solid var(--sb-line);
}
.m-conn-exit.is-direct {
  background: color-mix(in oklab, var(--sb-success) 10%, transparent);
  color: var(--sb-success);
  border-color: color-mix(in oklab, var(--sb-success) 24%, transparent);
}
.m-conn-exit.is-exit {
  background: var(--sb-accent-soft);
  color: var(--sb-text);
  border-color: var(--sb-line-strong);
}
.m-conn-tag {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px 7px;
  border-radius: 7px;
  font-size: 11px;
  font-weight: 600;
  line-height: 17px;
  background: var(--sb-surface-2);
  color: var(--sb-text-2);
  border: 1px solid var(--sb-line);
  white-space: nowrap;
}
.m-conn-tag--chain {
  background: color-mix(in oklab, var(--sb-warning) 14%, transparent);
  color: var(--sb-warning);
  border-color: color-mix(in oklab, var(--sb-warning) 30%, transparent);
}

/* Responsive Flex metrics bar */
.m-conn-rates {
  display: flex;
  flex-wrap: wrap;
  align-items: stretch;
  margin-top: 0.625rem;
  padding: 0.5rem 0.125rem;
  border-radius: 0.625rem;
  background: var(--sb-surface-2);
  border: 1px solid var(--sb-line);
  row-gap: 0.375rem;
}
.rate-col {
  flex: 1 1 clamp(5.5rem, 30%, 100%);
  min-width: 0;
  padding: 0 0.375rem;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.rate-col + .rate-col {
  border-left: 1px solid var(--sb-line);
}
.rate-lbl {
  font-size: 10.5px;
  color: var(--sb-muted);
  line-height: 1.3;
  white-space: nowrap;
}
.rate-val {
  font-size: 11.5px;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.025em;
  color: var(--sb-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  line-height: 1.35;
}
.rate-val.is-down-active {
  color: var(--sb-chart-2);
}
.rate-val.is-up-active {
  color: var(--sb-chart-4);
}

/* Structured 2-row footer */
.m-conn-footer {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 10px;
  padding-top: 9px;
  border-top: 1px solid var(--sb-line);
}
.m-conn-footer__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-width: 0;
}
.source-client {
  flex: 1;
  min-width: 0;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--sb-text-2);
  line-height: 1.35;
}
.m-conn-node-tag {
  flex: 0 1 auto;
  max-width: 54%;
  padding: 2px 7px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 550;
  color: var(--sb-text-2);
  background: var(--sb-surface-2);
  border: 1px solid var(--sb-line);
  line-height: 1.35;
}
.m-conn-footer__sub {
  font-size: 11.5px;
  color: var(--sb-muted);
  line-height: 1.35;
}
.source-ip-geo {
  flex: 1;
  min-width: 0;
}
.source-time {
  flex: none;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  color: var(--sb-muted);
}
</style>
