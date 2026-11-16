<template>
  <div class="m-stack">
    <div v-if="showHeader" class="m-card">
      <div class="m-stack" style="gap: 8px;">
        <div class="m-filter-row">
          <el-input
            v-model="keyword"
            placeholder="搜索域名 / 服务器 / IP"
            clearable
            style="flex: 1; min-width: 0;"
            @keyup.enter="handleFilterChange"
            @clear="handleFilterChange"
          />
          <el-button size="small" :loading="loading" @click="load(false)">刷新</el-button>
          <el-button
            v-if="canWrite && total > 0"
            size="small"
            type="danger"
            plain
            @click="clearAll"
          >
            清空
          </el-button>
        </div>

        <div class="m-filter-row">
          <button
            v-for="opt in statusOptions"
            :key="opt.value"
            type="button"
            class="m-pill"
            :class="{ 'is-active': status === opt.value }"
            @click="status = opt.value; handleFilterChange()"
          >
            {{ opt.label }}
          </button>
          <button
            v-for="opt in triggerOptions"
            :key="opt.value || 'all-trigger'"
            type="button"
            class="m-pill"
            :class="{ 'is-active': triggerType === opt.value }"
            @click="triggerType = opt.value; handleFilterChange()"
          >
            {{ opt.label }}
          </button>
        </div>

        <el-select
          v-if="!nodeId && nodeOptions.length > 0"
          v-model="selectedNodeId"
          placeholder="全部服务器"
          clearable
          style="width: 100%;"
          @change="handleFilterChange"
        >
          <el-option label="全部服务器" value="" />
          <el-option v-for="n in nodeOptions" :key="n.id" :label="n.name" :value="n.id" />
        </el-select>
      </div>
    </div>

    <div v-if="loading && records.length === 0" class="m-card m-note">加载 DDNS 变更记录...</div>
    <div v-else-if="records.length === 0" class="m-card m-note">暂无 DDNS 变更记录</div>

    <div v-else class="m-list">
      <div v-for="row in records" :key="row.id" class="m-card">
        <div class="m-filter-bar" style="margin-bottom: 6px;">
          <div style="display: flex; align-items: center; gap: 6px; min-width: 0; flex-wrap: wrap;">
            <span class="m-card__title m-mono-wrap" style="font-size: 14px;">{{ row.domain }}</span>
            <el-tag size="small" effect="plain">{{ row.record_type || 'A' }}</el-tag>
            <el-tag :type="row.status === 'failed' ? 'danger' : 'success'" size="small" effect="light">
              {{ row.status === 'failed' ? '失败' : '成功' }}
            </el-tag>
          </div>
          <el-button
            v-if="canWrite"
            size="small"
            type="danger"
            text
            @click="deleteOne(row)"
          >
            删除
          </el-button>
        </div>

        <div class="m-mono-wrap" style="font-size: 12px; color: var(--sb-text-2); margin-bottom: 6px;">
          <span>{{ row.old_ip || '无旧记录' }}</span>
          <span style="margin: 0 6px; color: var(--sb-muted);">→</span>
          <strong style="color: var(--sb-text);">{{ row.new_ip || '—' }}</strong>
        </div>

        <div class="m-filter-bar" style="font-size: 11px; color: var(--sb-muted);">
          <span>
            {{ row.node_name || row.node_id || '未知节点' }} ·
            {{ row.trigger_type === 'manual' ? '手动触发' : '自动触发' }}
          </span>
          <span>{{ formatDateTime(row.created_at) }}</span>
        </div>

        <div
          v-if="row.status === 'failed' && row.error_message"
          style="margin-top: 8px; padding: 8px; border-radius: 8px; background: color-mix(in srgb, var(--sb-danger) 10%, transparent); color: var(--sb-danger); font-size: 12px; word-break: break-word;"
        >
          {{ row.error_message }}
        </div>
      </div>
    </div>

    <div v-if="total > pageSize" class="m-pagination">
      <span>共 {{ total }} 条 · 第 {{ page }} / {{ totalPages }} 页</span>
      <div style="display: flex; gap: 6px;">
        <el-button size="small" :disabled="page <= 1" @click="goPage(page - 1)">上一页</el-button>
        <el-button size="small" :disabled="page >= totalPages" @click="goPage(page + 1)">下一页</el-button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, inject, onMounted, onUnmounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, del } from '../../api'
import { formatDateTime } from '../../format'
import { subscribeLive } from '../../live'

const props = defineProps({
  nodeId: { type: String, default: '' },
  showHeader: { type: Boolean, default: true },
  nodes: { type: Array, default: null },
})

const emit = defineEmits(['loaded'])
const canWrite = inject('canWrite', computed(() => false))
const statusOptions = [
  { label: '全部状态', value: '' },
  { label: '成功', value: 'succeeded' },
  { label: '失败', value: 'failed' },
]
const triggerOptions = [
  { label: '全部触发', value: '' },
  { label: '自动', value: 'auto' },
  { label: '手动', value: 'manual' },
]

const loading = ref(false)
const records = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = 20

const keyword = ref('')
const status = ref('')
const triggerType = ref('')
const selectedNodeId = ref('')
const fetchedNodes = ref([])
let unsubLive = null

const nodeOptions = computed(() => (props.nodes !== null ? props.nodes : fetchedNodes.value))
const totalPages = computed(() => Math.max(1, Math.ceil((total.value || 0) / pageSize)))

async function loadNodesIfNeeded() {
  if (props.nodeId || props.nodes !== null) return
  try {
    const res = await api('/nodes')
    fetchedNodes.value = res.nodes || []
  } catch {
    // ignore optional filter list error
  }
}

async function load(silent = false) {
  if (!silent) loading.value = true
  try {
    const params = new URLSearchParams()
    params.set('page', String(page.value))
    params.set('page_size', String(pageSize))
    const activeNodeId = props.nodeId || selectedNodeId.value
    if (activeNodeId) params.set('node_id', activeNodeId)
    if (keyword.value.trim()) params.set('keyword', keyword.value.trim())
    if (status.value) params.set('status', status.value)
    if (triggerType.value) params.set('trigger_type', triggerType.value)
    const res = await api(`/ddns/history?${params.toString()}`)
    records.value = res.records || []
    total.value = res.total || 0
    emit('loaded', total.value)
  } catch (err) {
    if (!silent) ElMessage.error(err.message || '加载 DDNS 记录失败')
  } finally {
    if (!silent) loading.value = false
  }
}

function handleFilterChange() {
  page.value = 1
  load(false)
}

function goPage(nextPage) {
  page.value = nextPage
  load(false)
}

async function deleteOne(row) {
  try {
    await ElMessageBox.confirm(`确认删除 ${row.domain} 的这条 DDNS 变更记录吗？`, '删除记录', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await del(`/ddns/history/${row.id}`)
    ElMessage.success('记录已删除')
    await load(false)
  } catch (err) {
    ElMessage.error(err.message || '删除失败')
  }
}

async function clearAll() {
  const msg = props.nodeId
    ? '确定要清空当前服务器的所有 DDNS 变更记录吗？'
    : '确定要清空所有 DDNS 变更记录吗？'
  try {
    await ElMessageBox.confirm(msg, '清空 DDNS 记录', {
      type: 'warning',
      confirmButtonText: '清空',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    const qs = props.nodeId ? `?node_id=${encodeURIComponent(props.nodeId)}` : ''
    await del(`/ddns/history${qs}`)
    ElMessage.success('已清空记录')
    page.value = 1
    await load(false)
  } catch (err) {
    ElMessage.error(err.message || '清空失败')
  }
}

watch(
  () => props.nodeId,
  () => {
    page.value = 1
    load(false)
  }
)

onMounted(() => {
  loadNodesIfNeeded()
  load(false)
  unsubLive = subscribeLive((ev) => {
    const kind = ev?.type || ev?.kind || ''
    if (kind === 'ddns' || kind === 'audit' || ev?.target_type === 'ddns' || ev?.resource === 'ddns') {
      load(true)
    }
  })
})

onUnmounted(() => {
  unsubLive?.()
})

defineExpose({ reload: load })
</script>
