<template>
  <div class="openlat-toolbar neu glass">
    <!-- Search Box -->
    <div class="search-box-wrap">
      <svg class="search-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="11" cy="11" r="8" />
        <line x1="21" y1="21" x2="16.65" y2="16.65" />
      </svg>
      <input
        type="text"
        :value="query"
        placeholder="搜索名称、地区、系统、CPU…"
        class="neu-track search-input"
        @input="$emit('update:query', $event.target.value)"
      />
      <button
        v-if="query"
        type="button"
        class="clear-btn"
        title="清空"
        @click="$emit('update:query', '')"
      >
        &times;
      </button>
    </div>

    <div class="toolbar-spacer" />

    <!-- Controls right -->
    <div class="controls-right">
      <!-- Group Select -->
      <select
        :value="group"
        class="neu-track select-ctrl"
        @change="$emit('update:group', $event.target.value)"
      >
        <option value="none">不分组</option>
        <option value="country">按地区</option>
        <option value="status">按状态</option>
        <option value="os">按系统</option>
      </select>

      <!-- Sort Select -->
      <select
        :value="sort"
        class="neu-track select-ctrl"
        @change="$emit('update:sort', $event.target.value)"
      >
        <option value="default">默认排序</option>
        <option value="name">按名称</option>
        <option value="cpu">按 CPU</option>
        <option value="mem">按内存</option>
        <option value="traffic">按流量</option>
        <option value="ping">按延迟</option>
      </select>

      <!-- Single-Button View Toggle (Grid <-> List) -->
      <button
        type="button"
        class="neu-inset view-toggle-btn"
        :title="view === 'grid' ? '当前为卡片视图，点击切换为列表视图' : '当前为列表视图，点击切换为卡片视图'"
        :aria-label="view === 'grid' ? '切换为列表视图' : '切换为卡片视图'"
        @click="$emit('update:view', view === 'grid' ? 'list' : 'grid')"
      >
        <svg v-if="view === 'grid'" class="size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
          <rect x="3" y="3" width="7" height="7" />
          <rect x="14" y="3" width="7" height="7" />
          <rect x="14" y="14" width="7" height="7" />
          <rect x="3" y="14" width="7" height="7" />
        </svg>
        <svg v-else class="size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
          <line x1="8" y1="6" x2="21" y2="6" />
          <line x1="8" y1="12" x2="21" y2="12" />
          <line x1="8" y1="18" x2="21" y2="18" />
          <line x1="3" y1="6" x2="3.01" y2="6" />
          <line x1="3" y1="12" x2="3.01" y2="12" />
          <line x1="3" y1="18" x2="3.01" y2="18" />
        </svg>
        <span class="view-toggle-txt">{{ view === 'grid' ? '卡片' : '列表' }}</span>
      </button>
    </div>
  </div>
</template>

<script setup>
defineProps({
  query: {
    type: String,
    default: '',
  },
  group: {
    type: String,
    default: 'none',
  },
  sort: {
    type: String,
    default: 'default',
  },
  view: {
    type: String,
    default: 'grid',
  },
})

defineEmits(['update:query', 'update:group', 'update:sort', 'update:view'])
</script>

<style scoped>
.openlat-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.625rem;
  border-radius: 1rem;
  padding: 0.5rem 0.75rem;
}

.search-box-wrap {
  position: relative;
  flex: 1 1 clamp(12rem, 28%, 22rem);
  min-width: min(12rem, 100%);
  max-width: 100%;
}

.search-icon {
  position: absolute;
  top: 50%;
  left: 0.625rem;
  transform: translateY(-50%);
  width: 0.875rem;
  height: 0.875rem;
  color: var(--sb-muted);
  pointer-events: none;
}

.search-input {
  width: 100%;
  min-height: 2rem;
  padding: 0.25rem 1.75rem 0.25rem 2rem;
  border-radius: 0.5rem;
  font-size: 0.75rem;
  color: var(--sb-text);
  outline: none;
}

.search-input:focus {
  border-color: var(--sb-accent);
}

.clear-btn {
  position: absolute;
  top: 50%;
  right: 0.375rem;
  transform: translateY(-50%);
  display: flex;
  align-items: center;
  justify-content: center;
  width: 1.25rem;
  height: 1.25rem;
  border-radius: 0.25rem;
  border: none;
  background: transparent;
  color: var(--sb-muted);
  font-size: 1rem;
  cursor: pointer;
}

.clear-btn:hover {
  color: var(--sb-text);
}

.toolbar-spacer {
  display: none;
  flex: 1 1 auto;
}

@media (min-width: 640px) {
  .toolbar-spacer {
    display: block;
  }
}

.controls-right {
  display: flex;
  flex: 1 1 auto;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 0.5rem;
}

@media (min-width: 640px) {
  .controls-right {
    flex: 0 1 auto;
    justify-content: flex-start;
  }
}

.select-ctrl {
  flex: 0 1 auto;
  min-width: 0;
  min-height: 2rem;
  padding: 0.25rem 0.625rem;
  border-radius: 0.5rem;
  font-size: 0.75rem;
  color: var(--sb-text);
  outline: none;
  cursor: pointer;
}

.view-toggle-btn {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  min-height: 2rem;
  padding: 0.25rem 0.625rem;
  border-radius: 0.5rem;
  border: 1px solid var(--sb-line);
  color: var(--sb-accent);
  font-size: 0.75rem;
  font-weight: 550;
  cursor: pointer;
  white-space: nowrap;
  transition: all 0.15s ease;
}

.view-toggle-btn:hover {
  color: var(--sb-text);
  border-color: var(--sb-line-strong);
}

.view-toggle-txt {
  line-height: 1;
}
</style>
