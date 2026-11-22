<script setup>
// 分段控件，代替桌面版的标签页和状态筛选下拉。
// 选项多于三四个时整行横向滑动，不换行。
defineProps({
  modelValue: { type: [String, Number], default: '' },
  // [{ value, label, badge }]
  options: { type: Array, default: () => [] },
})
const emit = defineEmits(['update:modelValue'])
</script>

<template>
  <div class="m-seg" role="tablist">
    <button
      v-for="option in options"
      :key="option.value"
      type="button"
      role="tab"
      class="m-seg__item"
      :class="{ 'is-active': modelValue === option.value }"
      :aria-selected="modelValue === option.value"
      @click="emit('update:modelValue', option.value)"
    >
      {{ option.label }}<i v-if="option.badge !== undefined && option.badge !== ''">{{ option.badge }}</i>
    </button>
  </div>
</template>

<style scoped>
.m-seg {
  display: flex;
  gap: 6px;
  margin-bottom: 12px;
  padding: 0;
  overflow-x: auto;
  background: transparent;
  border: none;
  box-shadow: none;
  scrollbar-width: none;
}
.m-seg::-webkit-scrollbar { display: none; }
.m-seg__item {
  flex: 1 0 auto;
  min-width: 44px;
  min-height: 38px;
  height: 38px;
  padding: 0 14px;
  color: var(--sb-muted);
  background: var(--sb-surface);
  border: 1px solid var(--sb-line-strong);
  border-radius: 8px;
  font: inherit;
  font-size: 13px;
  white-space: nowrap;
  cursor: pointer;
  transition: all 0.15s ease;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-sizing: border-box;
}
.m-seg__item:hover {
  color: var(--sb-text);
  border-color: var(--sb-line-strong);
}
html.dark .m-seg__item {
  background: rgba(255, 255, 255, 0.04);
  border-color: var(--sb-line);
  color: var(--sb-muted);
}
html.dark .m-seg__item.is-active {
  background: rgba(255, 255, 255, 0.16) !important;
  border-color: rgba(255, 255, 255, 0.3) !important;
  color: #F8FAFC !important;
  font-weight: 600;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.35);
}
html.light .m-seg__item.is-active,
html:not(.dark) .m-seg__item.is-active {
  background: #0F172A !important;
  border-color: #0F172A !important;
  color: #FFFFFF !important;
  font-weight: 600;
  box-shadow: 0 1px 3px rgba(15, 23, 42, 0.18);
}
.m-seg__item i { margin-left: 5px; font-style: normal; opacity: .85; font-size: 12px; }
</style>
