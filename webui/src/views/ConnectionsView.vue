<script setup>
import { computed, inject, onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CircleClose, Refresh, Search } from '@element-plus/icons-vue'
import { formatBytes, formatDateTime, includesText } from '../format'
import { connectionSnapshots, mergeChainedConnections, subscribeConnections } from '../connections'
import { del } from '../api'
import { isShareMode } from '../share'
import PageHeader from '../components/PageHeader.vue'
import PagedTable from '../components/PagedTable.vue'

const appState = inject('appState')
const loadNodes = inject('loadNodes')
const loading = ref(true)
const selectedNode = ref('')
const selectedOutbound = ref('')
const keyword = ref('')
const closingIds = ref(new Set())
const closingAll = ref(false)
let stopConnections

// sing-box reports the destination as an address, and the sniffed domain
// separately. The console shows one target, written the way an operator would
// type it: the domain when there is one, always with the port.
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

// The user alias is what identifies a connection; the service it came in on
// falls back into place only for a connection that has no alias to show.
function entryLabel(connection) {
  return connection.user || connection.listener_name || '—'
}

const rows = computed(() => {
  const flat = [...connectionSnapshots.value.values()].flatMap((result) => {
    const node = appState.nodes.find((item) => item.id === result.node_id)
    return (result.connections || []).map((connection) => ({
      ...connection,
      node_id: result.node_id,
      node_name: node?.name || result.node_id,
      clock_offset_ms: result.clock_offset_ms || 0,
      // The master resolves the sing-box outbound tag to the egress an operator
      // configured; the raw tag is only shown when that lookup found nothing.
      exit: connection.outbound_name || connection.outbound || connection.chains?.[0] || 'DIRECT',
      entry: entryLabel(connection),
      target: targetLabel(connection),
    }))
  })
  return mergeChainedConnections(flat, appState.nodes).map((row) => ({
    ...row,
    source_display: row.source_ip || row.source || '',
    total_rate: (Number(row.download_rate) || 0) + (Number(row.upload_rate) || 0),
    total_bytes: (Number(row.download) || 0) + (Number(row.upload) || 0),
  }))
})
const collectedAt = computed(() => [...connectionSnapshots.value.values()].map((result) => result.collected_at).filter(Boolean).sort().at(-1) || '')
const outboundOptions = computed(() => [...new Set(rows.value.map((row) => row.exit).filter(Boolean))].sort())
const filteredRows = computed(() => rows.value.filter((row) => {
  if (selectedNode.value && row.node_id !== selectedNode.value && !row.chain_node_ids?.includes(selectedNode.value)) return false
  if (selectedOutbound.value && row.exit !== selectedOutbound.value && !row.chain_exit?.includes(selectedOutbound.value) && !row.exit?.includes(selectedOutbound.value)) return false
  return includesText([
    row.node_name, row.source, row.source_ip, row.source_location, row.target,
    row.entry, row.user, row.listener_name, row.exit, row.network,
  ], keyword.value)
}))
async function load() {
  loading.value = true
  try {
    await loadNodes()
  } finally {
    loading.value = false
  }
}

async function handleCloseConnection(row) {
  if (!row?.node_id || !row?.id) return
  try {
    await ElMessageBox.confirm(`确定要关闭目标为 ${row.target || '该连接'} 的连接吗？`, '提示', {
      confirmButtonText: '确定关闭',
      cancelButtonText: '取消',
      type: 'warning',
      confirmButtonClass: 'el-button--danger',
    })
  } catch {
    return
  }
  closingIds.value.add(row.id)
  try {
    await del(`/nodes/${encodeURIComponent(row.node_id)}/connections/${encodeURIComponent(row.id)}`)
    ElMessage.success('连接已关闭')
    const snapshot = connectionSnapshots.value.get(row.node_id)
    if (snapshot && Array.isArray(snapshot.connections)) {
      snapshot.connections = snapshot.connections.filter((c) => c.id !== row.id)
      connectionSnapshots.value = new Map(connectionSnapshots.value)
    }
  } catch (err) {
    ElMessage.error(err.message || '关闭连接失败')
  } finally {
    closingIds.value.delete(row.id)
  }
}

async function handleCloseAll() {
  const nodeName = appState.nodes.find((n) => n.id === selectedNode.value)?.name
  const targetDesc = selectedNode.value
    ? `当前服务器（${nodeName || selectedNode.value}）的所有活动连接`
    : '所有服务器上的全部活动连接'
  try {
    await ElMessageBox.confirm(
      `确定要关闭${targetDesc}吗？这会强制切断正在进行的网络会话。`,
      '警告',
      {
        confirmButtonText: '确定全部关闭',
        cancelButtonText: '取消',
        type: 'warning',
        confirmButtonClass: 'el-button--danger',
      }
    )
  } catch {
    return
  }
  closingAll.value = true
  try {
    const path = selectedNode.value
      ? `/nodes/${encodeURIComponent(selectedNode.value)}/connections`
      : '/connections'
    await del(path)
    ElMessage.success('已请求关闭连接')
    if (selectedNode.value) {
      const snap = connectionSnapshots.value.get(selectedNode.value)
      if (snap) snap.connections = []
    } else {
      for (const snap of connectionSnapshots.value.values()) {
        snap.connections = []
      }
    }
    connectionSnapshots.value = new Map(connectionSnapshots.value)
  } catch (err) {
    ElMessage.error(err.message || '关闭连接失败')
  } finally {
    closingAll.value = false
  }
}

// The server answers a new stream with a snapshot straight away, but the hub
// is usually still empty at that point: each node's connections land a few
// hundred milliseconds later. Holding the loading state until something
// arrives — or until the wait is clearly over — keeps "no active connections"
// from flashing past just before the list appears.
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
    if (rows.value.length) settle()
    else settleTimer = setTimeout(settle, 1500)
  }
})
onBeforeUnmount(() => {
  clearTimeout(settleTimer)
  stopConnections?.()
})
</script>

<template>
  <div class="page-shell">
    <PageHeader title="当前连接" />
    <main class="page-content page-content--tight">
      <div class="search-toolbar">
        <el-input v-model="keyword" clearable :prefix-icon="Search" placeholder="搜索 IP、目标、入站节点或出口" class="filter-search" />
        <el-select v-model="selectedNode" clearable placeholder="全部服务器" class="filter-select">
          <el-option v-for="node in appState.nodes" :key="node.id" :label="node.name" :value="node.id" />
        </el-select>
        <el-select v-model="selectedOutbound" clearable placeholder="全部出口" class="filter-select">
          <el-option v-for="exit in outboundOptions" :key="exit" :label="exit" :value="exit" />
        </el-select>
        <span class="toolbar__spacer" />
        <span class="subtle">{{ filteredRows.length }} 条 · {{ formatDateTime(collectedAt, '暂无数据') }}</span>
        <el-tooltip
          v-if="!isShareMode"
          :content="selectedNode ? '关闭当前服务器所有连接' : '关闭所有活动连接'"
          placement="top"
        >
          <el-button
            type="danger"
            link
            :icon="CircleClose"
            :loading="closingAll"
            :disabled="filteredRows.length === 0"
            aria-label="关闭所有连接"
            class="toolbar-close-all-btn"
            @click="handleCloseAll"
          />
        </el-tooltip>
      </div>
      <div class="table-panel">
        <PagedTable :rows="filteredRows" :loading="loading" empty-text="当前没有活动连接">
          <el-table-column label="服务器" prop="node_name" min-width="160" sortable="custom" show-overflow-tooltip />
          <el-table-column label="来源" prop="source_display" min-width="160" sortable="custom" show-overflow-tooltip>
            <template #default="{ row }">
              <div class="mono">{{ row.source_ip || row.source || '—' }}</div>
              <div class="subtle">{{ row.source_location || '未知' }}</div>
            </template>
          </el-table-column>
          <el-table-column label="目标" prop="target" min-width="200" sortable="custom" show-overflow-tooltip>
            <template #default="{ row }">
              <strong>{{ row.target }}</strong>
              <div v-if="row.destination_location" class="subtle" style="font-size: 11px">{{ row.destination_location }}</div>
            </template>
          </el-table-column>
          <el-table-column label="入站节点" prop="entry" min-width="160" sortable="custom" show-overflow-tooltip>
            <template #default="{ row }">
              <div>{{ row.entry }}</div>
              <div class="subtle">{{ (row.network || '').toUpperCase() || '—' }}</div>
            </template>
          </el-table-column>
          <el-table-column label="出口" prop="exit" min-width="220" sortable="custom" class-name="tag-cell" show-overflow-tooltip>
            <template #default="{ row }"><el-tag size="small" effect="plain">{{ row.exit }}</el-tag></template>
          </el-table-column>
          <el-table-column label="实时速率" prop="total_rate" width="130" sortable="custom" show-overflow-tooltip>
            <template #default="{ row }">
              <template v-if="row.has_rates">
                <div class="mono">↓ {{ formatBytes(row.download_rate, '/s') }}</div>
                <div class="mono subtle">↑ {{ formatBytes(row.upload_rate, '/s') }}</div>
              </template>
              <span v-else class="subtle">等待上报</span>
            </template>
          </el-table-column>
          <el-table-column label="累计流量" prop="total_bytes" width="120" sortable="custom" show-overflow-tooltip>
            <template #default="{ row }">
              <div class="mono">↓ {{ formatBytes(row.download) }}</div>
              <div class="mono subtle">↑ {{ formatBytes(row.upload) }}</div>
            </template>
          </el-table-column>
          <el-table-column label="时间" prop="started_at" width="170" sortable="custom" show-overflow-tooltip>
            <template #default="{ row }">{{ formatDateTime(row.started_at) }}</template>
          </el-table-column>
          <el-table-column v-if="!isShareMode" label="操作" width="60" align="center" fixed="right">
            <template #default="{ row }">
              <el-tooltip content="关闭该连接" placement="top">
                <el-button
                  type="danger"
                  link
                  :icon="CircleClose"
                  :loading="closingIds.has(row.id)"
                  aria-label="关闭该连接"
                  class="row-close-btn"
                  @click="handleCloseConnection(row)"
                />
              </el-tooltip>
            </template>
          </el-table-column>
        </PagedTable>
      </div>
    </main>
  </div>
</template>

<style scoped>
.toolbar-close-all-btn {
  background: transparent !important;
  border: none !important;
  box-shadow: none !important;
  padding: 0 !important;
  margin: 0 !important;
  height: auto !important;
  width: auto !important;
  color: var(--sb-danger) !important;
  opacity: 0.85;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: opacity 0.15s ease, transform 0.15s ease;
}
.toolbar-close-all-btn :deep(.el-icon) {
  font-size: 19px;
  width: 19px;
  height: 19px;
}
.toolbar-close-all-btn:hover:not(:disabled) {
  background: transparent !important;
  opacity: 1;
  transform: scale(1.15);
}
.toolbar-close-all-btn:active:not(:disabled) {
  background: transparent !important;
  transform: scale(0.95);
}
.toolbar-close-all-btn:disabled,
.toolbar-close-all-btn.is-disabled {
  background: transparent !important;
  border: none !important;
  box-shadow: none !important;
  opacity: 0.35 !important;
  cursor: not-allowed !important;
}
.row-close-btn {
  background: transparent !important;
  border: none !important;
  box-shadow: none !important;
  padding: 0 !important;
  margin: 0 !important;
  height: auto !important;
  width: auto !important;
  color: var(--sb-muted) !important;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: color 0.15s ease, transform 0.15s ease;
}
.row-close-btn :deep(.el-icon) {
  font-size: 16px;
  width: 16px;
  height: 16px;
}
.row-close-btn:hover:not(:disabled) {
  background: transparent !important;
  color: var(--sb-danger) !important;
  text-decoration: none !important;
  transform: scale(1.15);
}
.row-close-btn:active:not(:disabled) {
  background: transparent !important;
  transform: scale(0.95);
}
.row-close-btn:disabled,
.row-close-btn.is-disabled {
  background: transparent !important;
  border: none !important;
  box-shadow: none !important;
  opacity: 0.35 !important;
  cursor: not-allowed !important;
}
:deep(.tag-cell .el-tag) {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: inline-block;
  vertical-align: middle;
}
</style>
