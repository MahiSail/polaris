<template>
  <MSheet
    :model-value="modelValue"
    title="探针管理"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div class="m-stack">
      <div class="m-filter-row">
        <el-input
          v-model="keyword"
          placeholder="搜索探针名称或目标"
          clearable
          style="flex: 1; min-width: 0;"
        />
        <el-button v-if="canWrite" type="primary" @click="openCreate">
          新建探针
        </el-button>
      </div>

      <div v-if="loading && tasks.length === 0" class="m-card m-note">正在加载探针列表...</div>
      <div v-else-if="filteredTasks.length === 0" class="m-card m-note">暂无匹配的探针任务</div>

      <div v-else class="m-list">
        <div v-for="(task, index) in filteredTasks" :key="task.id" class="m-card">
          <div class="m-filter-bar" style="margin-bottom: 6px;">
            <div style="display: flex; align-items: center; gap: 6px; min-width: 0; flex-wrap: wrap;">
              <strong style="font-size: 14px;">{{ task.name }}</strong>
              <el-tag :type="task.auto_join ? 'success' : 'warning'" size="small" effect="light">
                {{ task.auto_join ? '全局默认' : `自定义 (${(task.node_ids || []).length})` }}
              </el-tag>
            </div>
            <span class="m-note">{{ task.interval || 60 }}s</span>
          </div>

          <div class="m-mono-wrap m-note" style="color: var(--sb-text-2); margin-bottom: 10px;">
            {{ task.target }}
          </div>

          <div v-if="canWrite" class="m-filter-row" style="justify-content: flex-end;">
            <el-button
              v-if="scopeFilter === 'all' && !keyword.trim()"
              :disabled="index === 0 || reordering"
              @click="moveTask(task, -1)"
            >
              上移
            </el-button>
            <el-button
              v-if="scopeFilter === 'all' && !keyword.trim()"
              :disabled="index === filteredTasks.length - 1 || reordering"
              @click="moveTask(task, 1)"
            >
              下移
            </el-button>
            <el-button @click="openEdit(task)">编辑</el-button>
            <el-button type="danger" plain @click="removeTask(task)">删除</el-button>
          </div>
        </div>
      </div>
    </div>

    <MSheet
      v-model="formOpen"
      :title="editingId ? '编辑探针任务' : '新建探针任务'"
    >
      <div class="m-stack">
        <div class="m-field">
          <label class="m-field__label">任务名称</label>
          <el-input v-model="form.name" placeholder="例如：广州电信" />
        </div>
        <div class="m-field">
          <label class="m-field__label">探测目标（域名 / IP / host:port）</label>
          <el-input v-model="form.target" placeholder="例如：1.1.1.1 或 example.com:443" />
        </div>
        <div class="m-field">
          <label class="m-field__label">探测间隔（秒）</label>
          <el-input-number v-model="form.interval" :min="5" :max="3600" style="width: 100%;" />
        </div>
        <div class="m-field">
          <label class="m-field__label">生效范围</label>
          <div class="m-filter-row">
            <button
              type="button"
              class="m-pill"
              :class="{ 'is-active': form.auto_join }"
              @click="form.auto_join = true"
            >
              全局默认（全部服务器）
            </button>
            <button
              type="button"
              class="m-pill"
              :class="{ 'is-active': !form.auto_join }"
              @click="form.auto_join = false"
            >
              仅指定服务器
            </button>
          </div>
        </div>

        <div v-if="!form.auto_join" class="m-field">
          <div class="m-filter-bar" style="margin-bottom: 6px;">
            <label class="m-field__label" style="margin: 0;">绑定服务器 ({{ form.node_ids.length }})</label>
            <el-button size="small" text @click="toggleSelectAllNodes">
              {{ form.node_ids.length === nodes.length ? '清空全选' : '全选服务器' }}
            </el-button>
          </div>
          <div class="m-list" style="max-height: min(35dvh, 15rem); overflow-y: auto;">
            <label
              v-for="n in nodes"
              :key="n.id"
              class="m-choice"
              style="display: flex; align-items: center; justify-content: space-between; cursor: pointer;"
            >
              <div style="min-width: 0;">
                <div style="font-size: 13px; font-weight: 500;">{{ n.name }}</div>
                <div class="m-note m-mono-wrap">{{ n.last_observed_ip || n.client_address || '未上报 IP' }}</div>
              </div>
              <el-checkbox
                :model-value="form.node_ids.includes(n.id)"
                @change="toggleNode(n.id)"
              />
            </label>
          </div>
        </div>

        <div class="m-form-foot">
          <el-button style="flex: 1;" @click="formOpen = false">取消</el-button>
          <el-button type="primary" :loading="saving" style="flex: 1;" @click="saveTask">保存</el-button>
        </div>
      </div>
    </MSheet>
  </MSheet>
</template>

<script setup>
import { computed, inject, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, del, post, put } from '../../api'
import MSheet from './MSheet.vue'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  nodes: { type: Array, default: () => [] },
})

const emit = defineEmits(['update:modelValue', 'changed'])
const canWrite = inject('canWrite', computed(() => false))

const scopeFilters = [
  { label: '全部', value: 'all' },
  { label: '全局默认', value: 'public' },
  { label: '自定义', value: 'custom' },
]

const loading = ref(false)
const saving = ref(false)
const reordering = ref(false)
const tasks = ref([])
const keyword = ref('')
const scopeFilter = ref('all')

const formOpen = ref(false)
const editingId = ref(0)
const form = reactive({
  name: '',
  target: '',
  interval: 60,
  auto_join: true,
  node_ids: [],
})

const filteredTasks = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  return (tasks.value || []).filter((t) => {
    if (t.auto_join === false) return false
    if (!q) return true
    return (
      String(t.name || '').toLowerCase().includes(q) ||
      String(t.target || '').toLowerCase().includes(q)
    )
  })
})

async function loadTasks() {
  loading.value = true
  try {
    const res = await api('/ping-tasks')
    tasks.value = res.tasks || []
  } catch (err) {
    ElMessage.error(err.message || '加载探针失败')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = 0
  form.name = ''
  form.target = ''
  form.interval = 60
  form.auto_join = true
  form.node_ids = []
  formOpen.value = true
}

function openEdit(task) {
  editingId.value = task.id
  form.name = task.name || ''
  form.target = task.target || ''
  form.interval = Number(task.interval) || 60
  form.auto_join = Boolean(task.auto_join)
  form.node_ids = Array.isArray(task.node_ids) ? [...task.node_ids] : []
  formOpen.value = true
}

function toggleNode(id) {
  const idx = form.node_ids.indexOf(id)
  if (idx >= 0) form.node_ids.splice(idx, 1)
  else form.node_ids.push(id)
}

function toggleSelectAllNodes() {
  if (form.node_ids.length === (props.nodes || []).length) {
    form.node_ids = []
  } else {
    form.node_ids = (props.nodes || []).map((n) => n.id)
  }
}

async function saveTask() {
  if (!form.name.trim() || !form.target.trim()) {
    ElMessage.warning('请填写探针名称与探测目标')
    return
  }
  saving.value = true
  try {
    const payload = {
      name: form.name.trim(),
      target: form.target.trim(),
      interval: Number(form.interval) || 60,
      auto_join: form.auto_join,
      node_ids: form.auto_join ? [] : form.node_ids,
    }
    if (editingId.value) {
      await put(`/ping-tasks/${editingId.value}`, payload)
      ElMessage.success('探针已更新')
    } else {
      await post('/ping-tasks', payload)
      ElMessage.success('探针已创建')
    }
    formOpen.value = false
    await loadTasks()
    emit('changed')
  } catch (err) {
    ElMessage.error(err.message || '保存探针失败')
  } finally {
    saving.value = false
  }
}

async function removeTask(task) {
  try {
    await ElMessageBox.confirm(`确认删除探针「${task.name}」吗？`, '删除探针', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await del(`/ping-tasks/${task.id}`)
    ElMessage.success('探针已删除')
    await loadTasks()
    emit('changed')
  } catch (err) {
    ElMessage.error(err.message || '删除探针失败')
  }
}

async function moveTask(task, delta) {
  const list = [...tasks.value]
  const idx = list.findIndex((t) => t.id === task.id)
  const targetIdx = idx + delta
  if (idx < 0 || targetIdx < 0 || targetIdx >= list.length) return
  const [moved] = list.splice(idx, 1)
  list.splice(targetIdx, 0, moved)
  reordering.value = true
  try {
    await put('/ping-tasks/order', { ids: list.map((t) => t.id) })
    tasks.value = list
    emit('changed')
  } catch (err) {
    ElMessage.error(err.message || '调整排序失败')
  } finally {
    reordering.value = false
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) loadTasks()
  }
)
</script>
