<template>
  <MSheet
    :model-value="modelValue"
    :title="node ? `线路与丢包排查 · ${node.name}` : '线路与丢包排查'"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div v-if="node" class="m-stack">
      <div class="m-card m-card--flat">
        <div class="m-filter-bar" style="margin-bottom: 8px;">
          <div>
            <div class="m-card__title">{{ node.name }}</div>
            <div class="m-note">{{ result?.source_location || node.location || '来源节点' }}</div>
          </div>
          <el-tag :type="node.online ? 'success' : 'info'" size="small" effect="light">
            {{ node.online ? '在线' : '离线' }}
          </el-tag>
        </div>

        <div class="m-kv-grid" style="margin-bottom: 10px;">
          <div class="m-kv-cell">
            <span class="m-kv-cell__label">公网 IPv4</span>
            <span class="m-kv-cell__value m-mono-wrap">{{ ipv4Addr || '未识别' }}</span>
          </div>
          <div class="m-kv-cell">
            <span class="m-kv-cell__label">公网 IPv6</span>
            <span class="m-kv-cell__value m-mono-wrap">{{ ipv6Addr || '未识别' }}</span>
          </div>
        </div>

        <el-button
          type="primary"
          :loading="probingAll"
          :disabled="!node.online"
          style="width: 100%;"
          @click="runProbeAll"
        >
          一键检测所有互联服务器
        </el-button>
        <div v-if="result?.tested_at" class="m-note" style="margin-top: 6px;">
          最近检测：{{ formatDateTime(result.tested_at) }}
        </div>
      </div>

      <div v-if="probingAll && peers.length === 0" class="m-card m-note">
        正在下发实测脉冲并等待节点回报，请稍候...
      </div>
      <div v-else-if="result && peers.length === 0" class="m-card m-note">
        暂无其他互联服务器可供探测
      </div>

      <div v-else class="m-list">
        <div v-for="peer in peers" :key="peer.target_node_id" class="m-card">
          <div class="m-filter-bar" style="margin-bottom: 8px;">
            <div style="min-width: 0;">
              <div class="m-card__title">{{ peer.target_node_name }}</div>
              <div class="m-note">{{ peer.target_location || '未知归属地' }}</div>
            </div>
            <el-button
              size="small"
              :loading="retestingId === peer.target_node_id"
              :disabled="!node.online"
              @click="retestPeer(peer)"
            >
              重测
            </el-button>
          </div>

          <div class="m-stack" style="gap: 8px;">
            <div class="m-card m-card--flat" style="padding: 10px;">
              <div class="m-filter-bar" style="margin-bottom: 4px;">
                <span style="font-size: 12px; font-weight: 600;">IPv4 {{ peer.ipv4_target || '' }}</span>
                <el-tag :type="statusTagType(peer.ipv4_status)" size="small" effect="light">
                  {{ statusLabel(peer.ipv4_status) }}
                </el-tag>
              </div>
              <div v-if="peer.ipv4_status !== 'none'" class="m-note">
                延迟 {{ formatMs(peer.ipv4_latency_ms) }}
                (最小 {{ formatMs(peer.ipv4_min_latency_ms) }} / 最大 {{ formatMs(peer.ipv4_max_latency_ms) }})
                · 丢包 {{ Number(peer.ipv4_loss_percent || 0).toFixed(1) }}%
                ({{ peer.ipv4_received || 0 }}/{{ peer.ipv4_sent || 0 }})
                <span v-if="peer.ipv4_method"> · {{ peer.ipv4_method }}</span>
              </div>
            </div>

            <div
              v-if="peer.ipv6_target || peer.ipv6_status !== 'none'"
              class="m-card m-card--flat"
              style="padding: 10px;"
            >
              <div class="m-filter-bar" style="margin-bottom: 4px;">
                <span style="font-size: 12px; font-weight: 600;">IPv6 {{ peer.ipv6_target || '' }}</span>
                <el-tag :type="statusTagType(peer.ipv6_status)" size="small" effect="light">
                  {{ statusLabel(peer.ipv6_status) }}
                </el-tag>
              </div>
              <div class="m-note">
                延迟 {{ formatMs(peer.ipv6_latency_ms) }}
                (最小 {{ formatMs(peer.ipv6_min_latency_ms) }} / 最大 {{ formatMs(peer.ipv6_max_latency_ms) }})
                · 丢包 {{ Number(peer.ipv6_loss_percent || 0).toFixed(1) }}%
                ({{ peer.ipv6_received || 0 }}/{{ peer.ipv6_sent || 0 }})
                <span v-if="peer.ipv6_method"> · {{ peer.ipv6_method }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </MSheet>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { post } from '../../api'
import { formatDateTime } from '../../format'
import MSheet from './MSheet.vue'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  node: { type: Object, default: null },
})

const emit = defineEmits(['update:modelValue'])

const probingAll = ref(false)
const retestingId = ref('')
const result = ref(null)

const peers = computed(() => result.value?.peers || [])

const ipv4Addr = computed(() => {
  const ips = props.node?.ip_addresses || []
  const found = ips.find((ip) => ip && !ip.includes(':'))
  if (found) return found
  const obs = props.node?.last_observed_ip || ''
  return obs && !obs.includes(':') ? obs : ''
})

const ipv6Addr = computed(() => {
  const ips = props.node?.ip_addresses || []
  const found = ips.find((ip) => ip && ip.includes(':'))
  if (found) return found
  const obs = props.node?.last_observed_ip || ''
  return obs && obs.includes(':') ? obs : ''
})

function statusTagType(status) {
  if (status === 'ok') return 'success'
  if (status === 'loss') return 'warning'
  if (status === 'unreachable') return 'danger'
  return 'info'
}

function statusLabel(status) {
  if (status === 'ok') return '连通正常'
  if (status === 'loss') return '存在丢包'
  if (status === 'unreachable') return '不可达'
  return '未配置'
}

function formatMs(val) {
  const n = Number(val)
  if (!Number.isFinite(n) || n <= 0) return '—'
  return `${n.toFixed(1)} ms`
}

async function runProbeAll() {
  if (!props.node?.id) return
  probingAll.value = true
  try {
    result.value = await post(`/nodes/${props.node.id}/probe-peers`, {})
    ElMessage.success('互联实测已完成')
  } catch (err) {
    ElMessage.error(err.message || '执行探测失败')
  } finally {
    probingAll.value = false
  }
}

async function retestPeer(peer) {
  if (!props.node?.id || !peer?.target_node_id) return
  retestingId.value = peer.target_node_id
  try {
    const res = await post(
      `/nodes/${props.node.id}/probe-peers?target_node_id=${encodeURIComponent(peer.target_node_id)}`,
      {}
    )
    const updated = res?.peers?.[0]
    if (updated && result.value?.peers) {
      const idx = result.value.peers.findIndex((p) => p.target_node_id === peer.target_node_id)
      if (idx >= 0) result.value.peers[idx] = updated
      if (res.tested_at) result.value.tested_at = res.tested_at
    }
    ElMessage.success(`已重测 ${peer.target_node_name}`)
  } catch (err) {
    ElMessage.error(err.message || '重测失败')
  } finally {
    retestingId.value = ''
  }
}

watch(
  () => [props.modelValue, props.node?.id],
  ([open]) => {
    if (open) {
      result.value = null
      if (props.node?.online) {
        runProbeAll()
      }
    }
  }
)
</script>
