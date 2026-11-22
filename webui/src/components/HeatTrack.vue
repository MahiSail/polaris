<template>
  <div class="heat-track-container" :class="{ 'is-compact': compact }">
    <el-tooltip
      v-for="(slot, i) in normalizedSlots"
      :key="i"
      placement="top"
      effect="dark"
      popper-class="heat-cell-popper"
      :show-after="80"
    >
      <template #content>
        <div class="heat-card">
          <div class="heat-card-header font-mono">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="12" cy="12" r="10" />
              <polyline points="12 6 12 12 16 14" />
            </svg>
            <span>{{ slot.timeStr || '时段统计' }}</span>
          </div>
          <div class="heat-card-body">
            <div class="heat-card-row">
              <span class="heat-indicator-dot" :class="slot.heatClass" />
              <span class="heat-card-label">{{ slot.metricLabel }}</span>
              <span class="heat-card-val font-mono" :class="{ 'is-empty': !slot.hasData }">
                {{ slot.valStr }}
              </span>
            </div>
            <div v-if="slot.probes > 0" class="heat-card-probes font-mono">
              <span class="heat-probe-badge">{{ slot.probes }} 个探测源</span>
            </div>
          </div>
        </div>
      </template>
      <span
        class="heat-cell"
        :class="[
          slot.heatClass,
          { 'slot-gap': i > 0 && i % 6 === 0, 'heat-cell-flat': compact }
        ]"
      />
    </el-tooltip>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  hours: {
    type: Array,
    default: () => [],
  },
  kind: {
    type: String,
    default: 'latency', // 'latency' | 'loss'
  },
  probes: {
    type: Number,
    default: 0,
  },
  compact: {
    type: Boolean,
    default: false,
  },
  rangeHours: {
    type: Number,
    default: 24,
  },
})

const TRACK_LABELS = {
  latency: '延迟',
  loss: '丢包',
}

function getHeatClass(value, kind) {
  if (value === null || value === undefined) return 'heat-muted'
  if (kind === 'latency') {
    if (value < 60) return 'heat-ok'
    if (value < 110) return 'heat-mild'
    if (value < 180) return 'heat-warn'
    if (value < 280) return 'heat-hot'
    return 'heat-bad'
  }
  // loss
  if (value < 0.5) return 'heat-ok'
  if (value < 2) return 'heat-mild'
  if (value < 5) return 'heat-warn'
  if (value < 10) return 'heat-hot'
  return 'heat-bad'
}

function formatSlotTime(ts, rangeHours = 24) {
  if (!ts) return ''
  const d = new Date(ts * 1000)
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const date = String(d.getDate()).padStart(2, '0')
  const hour = String(d.getHours()).padStart(2, '0')
  const minute = String(d.getMinutes()).padStart(2, '0')
  if (rangeHours <= 6) {
    return `${hour}:${minute}`
  }
  return `${month}-${date} ${hour}:00`
}

const normalizedSlots = computed(() => {
  const result = []
  const source = Array.isArray(props.hours) ? props.hours : []
  const range = props.rangeHours || 24
  const count = source.length > 0 ? source.length : (range > 24 ? 28 : 24)

  let items = source
  if (items.length < count) {
    const padCount = count - items.length
    const step = range <= 1 ? 150 : range <= 6 ? 900 : range <= 24 ? 3600 : 21600
    const nowSlot = Math.floor(Date.now() / 1000 / step) * step
    const padding = Array.from({ length: padCount }, (_, idx) => ({
      ts: nowSlot - (count - 1 - idx) * step,
      latency: null,
      loss: null,
    }))
    items = [...padding, ...items]
  }

  for (let i = 0; i < count; i++) {
    const item = items[i] || {}
    const value = props.kind === 'latency' ? item.latency : item.loss
    const timeStr = formatSlotTime(item.ts, range)
    const hasData = value !== null && value !== undefined
    const valStr = !hasData
      ? '无数据'
      : props.kind === 'latency'
        ? `${Math.round(value)} ms`
        : `${Number(value).toFixed(1)}%`
    const probes = props.probes || item.probes || 0
    const probeStr = probes > 1 ? ` (${probes} 个探测)` : ''
    const tooltip = timeStr ? `${timeStr} · ${TRACK_LABELS[props.kind]}: ${valStr}${probeStr}` : '暂无数据'

    result.push({
      value,
      hasData,
      timeStr,
      metricLabel: TRACK_LABELS[props.kind] || '指标',
      valStr,
      probes,
      heatClass: getHeatClass(value, props.kind),
      tooltip,
    })
  }

  return result
})
</script>

<style scoped>
.heat-track-container {
  display: flex;
  align-items: stretch;
  gap: 1.5px;
  width: 100%;
  height: 10px;
  min-width: 0;
}
.heat-track-container.is-compact {
  gap: 1px;
  height: 8px;
}
.heat-cell {
  flex: 1;
  min-width: 0;
  border-radius: 3px;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.28), 0 1px 2px rgba(0, 0, 0, 0.45);
  transition: transform 0.15s ease, filter 0.15s ease;
  cursor: pointer;
}
.heat-cell:hover {
  transform: translateY(-1px);
  filter: saturate(1.2) brightness(1.1);
  z-index: 2;
}
.heat-cell-flat {
  border-radius: 2px;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.18);
}
.slot-gap {
  margin-left: 3px;
}
.is-compact .slot-gap {
  margin-left: 2px;
}
.heat-ok {
  background-color: var(--heat-ok, #10b981);
  background-image: linear-gradient(165deg, rgba(255, 255, 255, 0.3), transparent);
}
.heat-mild {
  background-color: var(--heat-mild, #84cc16);
  background-image: linear-gradient(165deg, rgba(255, 255, 255, 0.3), transparent);
}
.heat-warn {
  background-color: var(--heat-warn, #f59e0b);
  background-image: linear-gradient(165deg, rgba(255, 255, 255, 0.3), transparent);
}
.heat-hot {
  background-color: var(--heat-hot, #f97316);
  background-image: linear-gradient(165deg, rgba(255, 255, 255, 0.3), transparent);
}
.heat-bad {
  background-color: var(--heat-bad, #ef4444);
  background-image: linear-gradient(165deg, rgba(255, 255, 255, 0.3), transparent);
}
.heat-muted {
  background-color: var(--heat-muted, rgba(148, 163, 184, 0.16));
  box-shadow: inset 0 1px 3px rgba(0, 0, 0, 0.4);
}
.heat-muted:hover {
  transform: none;
  filter: none;
}
</style>
