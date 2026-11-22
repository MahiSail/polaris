<template>
  <MPage title="热门设备" :loading="loading">
    <div class="m-stack">
      <MSegmented
        v-model="range"
        :options="[
          { value: '24h', label: '24 小时' },
          { value: '7d', label: '7 天' },
        ]"
      />
      <div class="m-filter-bar">
        <span class="m-note">保留 7 天 · {{ timeZoneLabel }}</span>
        <button type="button" class="m-pill" @click="navigate('connection-history')">
          历史连接明细
        </button>
      </div>

      <!-- 4 Separate Summary Stats (PC parity) -->
      <div class="devices-stats-grid">
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

          <div class="m-device-head-row">
            <span class="m-note truncate" style="flex: 1 1 auto; min-width: 0;">{{ row.source_location || '归属地未知' }}</span>
            <div v-if="row.user" class="m-device-users">
              <span
                v-for="u in splitUsers(row.user)"
                :key="u"
                role="button"
                tabindex="0"
                class="m-user-badge truncate"
                :title="`筛选接入节点：${u}`"
                @click.stop="filterByUser(u)"
              >
                {{ u }}
              </span>
            </div>
          </div>

          <div class="m-filter-bar" style="font-size: 12px; color: var(--sb-text-2); margin-bottom: 4px;">
            <span class="m-mono-wrap">
              ↓ {{ formatBytes(row.download || 0) }} · ↑ {{ formatBytes(row.upload || 0) }}
            </span>
            <span>{{ row.connection_count || 0 }} 次连接</span>
          </div>

          <div class="m-filter-bar" style="font-size: 11px; color: var(--sb-muted); gap: 8px;">
            <span class="truncate" style="flex: 1; min-width: 0;">{{ (row.node_names || '—').replace(/,\s*/g, ' · ') }}</span>
            <span style="flex: none;">{{ formatDateTime(row.last_seen_at) }}</span>
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

function splitUsers(userStr) {
  if (!userStr) return []
  return String(userStr).split(',').map((s) => s.trim()).filter(Boolean)
}

watch(range, () => {
  load()
})

onMounted(() => {
  load()
})
</script>

<style scoped>
.devices-stats-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.devices-stats-grid > .m-card {
  margin: 0 !important;
  height: 100%;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
}
.m-device-head-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 6px;
  min-width: 0;
}
.m-device-users {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  justify-content: flex-end;
  max-width: 60%;
}
.m-user-badge {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 500;
  color: var(--sb-primary, #0284c7);
  background: var(--sb-accent-soft, #f0f9ff);
  border: 1px solid var(--sb-primary-soft, #bae6fd);
  cursor: pointer;
  max-width: 100%;
}
.m-user-badge:active {
  background: var(--sb-primary-soft, #bae6fd);
}
</style>
