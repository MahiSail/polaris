<template>
  <MSheet
    :model-value="modelValue"
    :title="page === 'nodes' ? '分享服务器列表' : '分享运行概览'"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div class="m-stack">
      <div class="m-card m-card--flat">
        <p class="m-note" style="margin: 0;">
          链接无需登录即可查看脱敏后的只读视图；地址、域名、提供商和计费信息会被隐藏，可随时撤销。
        </p>
      </div>

      <div class="m-card">
        <div class="m-card__title" style="margin-bottom: 10px;">有效期设置</div>
        <div class="m-filter-row" style="margin-bottom: 12px;">
          <button
            v-for="opt in ttlOptions"
            :key="opt.value"
            type="button"
            class="m-pill"
            :class="{ 'is-active': expiresIn === opt.value }"
            @click="expiresIn = opt.value"
          >
            {{ opt.label }}
          </button>
        </div>
        <el-button
          type="primary"
          :loading="creating"
          style="width: 100%;"
          @click="createLink"
        >
          生成分享链接
        </el-button>
      </div>

      <div v-if="createdURL" class="m-card" style="border-color: var(--sb-warning);">
        <div class="m-card__title" style="color: var(--sb-warning); margin-bottom: 6px;">
          链接仅本次显示，请立即保存
        </div>
        <div class="m-item__mono m-mono-wrap" style="font-size: 12px; padding: 8px; border-radius: 8px; background: var(--sb-surface-2); margin-bottom: 10px;">
          {{ createdURL }}
        </div>
        <el-button type="warning" plain style="width: 100%;" @click="copyCreated">
          复制分享链接
        </el-button>
      </div>

      <div class="m-section">
        <div class="m-filter-bar">
          <span class="m-section__title" style="padding: 0;">已创建的分享链接 ({{ pageLinks.length }})</span>
          <el-button size="small" text :loading="loading" @click="loadLinks">刷新</el-button>
        </div>

        <div v-if="loading && pageLinks.length === 0" class="m-card m-note">加载中...</div>
        <div v-else-if="pageLinks.length === 0" class="m-card m-note">暂无分享链接</div>
        <div v-else class="m-list">
          <div v-for="item in pageLinks" :key="item.id" class="m-card">
            <div class="m-filter-bar" style="margin-bottom: 6px;">
              <el-tag :type="item.expired ? 'info' : 'success'" size="small" effect="light">
                {{ item.expired ? '已过期' : '有效' }}
              </el-tag>
              <el-button
                v-if="canWrite"
                size="small"
                type="danger"
                text
                :loading="revokingId === item.id"
                @click="revoke(item)"
              >
                撤销
              </el-button>
            </div>
            <div class="m-kv-grid">
              <div class="m-kv-cell">
                <span class="m-kv-cell__label">创建时间</span>
                <span class="m-kv-cell__value">{{ formatDateTime(item.created_at) }}</span>
              </div>
              <div class="m-kv-cell">
                <span class="m-kv-cell__label">到期时间</span>
                <span class="m-kv-cell__value">{{ formatDateTime(item.expires_at) }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </MSheet>
</template>

<script setup>
import { computed, inject, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, del, post } from '../../api'
import { writeClipboard } from '../../clipboard'
import { formatDateTime } from '../../format'
import MSheet from './MSheet.vue'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  page: { type: String, default: 'nodes' },
})

const emit = defineEmits(['update:modelValue'])
const canWrite = inject('canWrite', computed(() => false))

const ttlOptions = [
  { label: '1 小时', value: 3600 },
  { label: '24 小时', value: 86400 },
  { label: '7 天', value: 604800 },
  { label: '30 天', value: 2592000 },
]

const expiresIn = ref(86400)
const creating = ref(false)
const loading = ref(false)
const revokingId = ref('')
const createdURL = ref('')
const links = ref([])

const pageLinks = computed(() =>
  (links.value || []).filter((item) => item && item.page === props.page)
)

async function loadLinks() {
  loading.value = true
  try {
    const res = await api('/shares')
    links.value = res.shares || []
  } catch (err) {
    ElMessage.error(err.message || '加载分享列表失败')
  } finally {
    loading.value = false
  }
}

async function createLink() {
  creating.value = true
  try {
    const res = await post('/shares', {
      page: props.page,
      expires_in: expiresIn.value,
    })
    createdURL.value = res.url || ''
    ElMessage.success('分享链接已生成')
    await loadLinks()
  } catch (err) {
    ElMessage.error(err.message || '生成分享链接失败')
  } finally {
    creating.value = false
  }
}

async function copyCreated() {
  if (!createdURL.value) return
  try {
    await writeClipboard(createdURL.value)
    ElMessage.success('分享链接已复制')
  } catch {
    ElMessage.error('复制失败，请手动长按复制')
  }
}

async function revoke(item) {
  try {
    await ElMessageBox.confirm('确定要撤销这条分享链接吗？撤销后将立即失效。', '撤销分享链接', {
      type: 'warning',
      confirmButtonText: '撤销',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  revokingId.value = item.id
  try {
    await del(`/shares/${item.id}`)
    ElMessage.success('分享链接已撤销')
    await loadLinks()
  } catch (err) {
    ElMessage.error(err.message || '撤销失败')
  } finally {
    revokingId.value = ''
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      createdURL.value = ''
      loadLinks()
    }
  }
)
</script>
