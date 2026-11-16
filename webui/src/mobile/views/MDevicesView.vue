<template>
  <MPage title="热门设备" :loading="loading">
    <template #actions>
      <el-button
        circle
        size="small"
        :icon="Refresh"
        :loading="loading"
        aria-label="刷新"
        @click="load"
      />
    </template>

    <div class="m-stack">
      <div class="m-card" style="padding: 10px 12px;">
        <MSegmented
          v-model="range"
          :options="[
            { value: '24h', label: '最近 24 小时' },
            { value: '7d', label: '最近 7 天' },
            { value: '30d', label: '最近 30 天' },
          ]"
        />
        <div class="m-filter-bar" style="margin-top: 8px;">
          <span class="m-note">保留最近 30 天 · {{ timeZoneLabel }}</span>
          <el-button size="small" type="primary" text @click="navigate('connection-history')">
            历史连接明细
          </el-button>
        </div>
      </div>

      <!-- 4 Separate Summary Stats (PC parity) -->
      <div class="m-kv-grid">
        <div class="m-card" style="padding: 10px 12px;">
          <div class="m-kv-cell__label">历史连接数</div>
          <div class="m-kv-cell__value" style="font-size: 16px;">{{ summary.total_connections || 0 }}</div>
        </div>
        <div class="m-card" style="padding: 10px 12px;">
          <div class="m-kv-cell__label">热门设备数 (独立 IP)</div>
          <div class="m-kv-cell__value" style="font-size: 16px;">{{ summary.unique_ips || summary.unique_devices || 0 }}</div>
        </div>
        <div class="m-card" style="padding: 10px 12px;">
          <div class="m-kv-cell__label">累计下行流量</div>
          <div class="m-kv-cell__value" style="font-size: 15px;">{{ formatBytes(summary.total_download || 0) }}</div>
        </div>
        <div class="m-card" style="padding: 10px 12px;">
          <div class="m-kv-cell__label">累计上行流量</div>
          <div class="m-kv-cell__value" style="font-size: 15px;">{{ formatBytes(summary.total_upload || 0) }}</div>
        </div>
      </div>

      <div v-if="!loading && devices.length === 0" class="m-card m-note">
        该时间段暂无设备活动记录
      </div>

      <div v-else class="m-list">
        <div
          v-for="(row, idx) in devices"
          :key="`${row.source_ip}-${row.user || idx}`"
          class="m-card"
          style="cursor: pointer;"
          @click="filterByIP(row.source_ip)"
        >
          <div class="m-filter-bar" style="margin-bottom: 6px;">
            <div style="display: flex; align-items: center; gap: 8px; min-width: 0; flex: 1;">
              <el-tag size="small" effect="plain">#{{ idx + 1 }}</el-tag>
              <strong class="m-mono-wrap" style="font-size: 14px;">{{ row.source_ip }}</strong>
            </div>
            <strong style="font-size: 14px; white-space: nowrap;">
              {{ formatBytes(row.total_bytes || Number(row.upload || 0) + Number(row.download || 0)) }}
            </strong>
          </div>

          <div class="m-filter-bar" style="margin-bottom: 6px;">
            <span class="m-note">{{ row.source_location || '归属地未知' }}</span>
            <el-button
              v-if="row.user"
              size="small"
              text
              type="primary"
              style="padding: 0; height: auto;"
              @click.stop="filterByUser(row.user)"
            >
              接入节点：{{ row.user }}
            </el-button>
          </div>

          <div class="m-filter-bar" style="font-size: 12px; color: var(--sb-text-2); margin-bottom: 4px;">
            <span class="m-mono-wrap">
              ↓ {{ formatBytes(row.download || 0) }} · ↑ {{ formatBytes(row.upload || 0) }}
            </span>
            <span>{{ row.connection_count || 0 }} 次连接</span>
          </div>

          <div class="m-filter-bar" style="font-size: 11px; color: var(--sb-muted);">
            <span class="truncate" style="max-width: 60%;">{{ row.node_names || '—' }}</span>
            <span>{{ formatDateTime(row.last_seen_at) }}</span>
          </div>
        </div>
      </div>
    </div>
  </MPage>
</template>

<script setup>
import { inject, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { api } from '../../api'
import { formatBytes, formatDateTime, localTimeZoneLabel } from '../../format'
import MPage from '../components/MPage.vue'
import MSegmented from '../components/MSegmented.vue'

const navigate = inject('navigate', () => {})
const timeZoneLabel = localTimeZoneLabel()

const range = ref('7d')
const loading = ref(false)
const devices = ref([])
const summary = reactive({
  total_connections: 0,
  unique_devices: 0,
  unique_ips: 0,
  total_upload: 0,
  total_download: 0,
})

async function load() {
  loading.value = true
  try {
    const res = await api(`/devices/popular?range=${encodeURIComponent(range.value)}&limit=20`)
    devices.value = res.devices || []
    Object.assign(summary, {
      total_connections: res.summary?.total_connections || 0,
      unique_devices: res.summary?.unique_devices || 0,
      unique_ips: res.summary?.unique_ips || 0,
      total_upload: res.summary?.total_upload || 0,
      total_download: res.summary?.total_download || 0,
    })
  } catch (err) {
    ElMessage.error(err.message || '加载热门设备失败')
  } finally {
    loading.value = false
  }
}

function filterByIP(ip) {
  if (!ip) return
  navigate('connection-history', `ip=${encodeURIComponent(ip)}`)
}

function filterByUser(user) {
  if (!user) return
  navigate('connection-history', `user=${encodeURIComponent(user)}`)
}

watch(range, () => {
  load()
})

onMounted(() => {
  load()
})
</script>
