<script setup>
import { computed, ref, watch } from 'vue'

// A table that always shows one page worth of rows and its own pager, so a
// growing list never turns the page into a long scroll. Column definitions
// are passed through unchanged via the default slot.
const props = defineProps({
  rows: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
  pageSize: { type: Number, default: 10 },
  rowKey: { type: String, default: 'id' },
  emptyText: { type: String, default: '暂无数据' },
  defaultSort: { type: Object, default: () => ({}) },
})

const emit = defineEmits(['sort-change'])

const page = ref(1)
const size = ref(props.pageSize)
const total = computed(() => props.rows.length)

const sortProp = ref(props.defaultSort?.prop || '')
const sortOrder = ref(props.defaultSort?.order || '')

function handleSortChange({ column, prop, order }) {
  sortProp.value = prop || ''
  sortOrder.value = order || ''
  page.value = 1
  emit('sort-change', { column, prop, order })
}

const sortedRows = computed(() => {
  if (!sortProp.value || !sortOrder.value) return props.rows
  const multiplier = sortOrder.value === 'descending' ? -1 : 1
  const prop = sortProp.value
  return [...props.rows].sort((a, b) => {
    const va = a[prop]
    const vb = b[prop]
    if (va === vb) return 0
    if (va === null || va === undefined || va === '') return 1
    if (vb === null || vb === undefined || vb === '') return -1
    if (typeof va === 'number' && typeof vb === 'number') {
      return (va - vb) * multiplier
    }
    const sa = String(va)
    const sb = String(vb)
    return sa.localeCompare(sb, 'zh-CN', { numeric: true, sensitivity: 'base' }) * multiplier
  })
})

const pageRows = computed(() => sortedRows.value.slice((page.value - 1) * size.value, page.value * size.value))
// While the first load is still in flight the table is empty, and the mask is
// translucent — leaving the empty text underneath made it look as if the list
// had already been fetched and really was empty.
const emptyText = computed(() => (props.loading ? '' : props.emptyText))

// Filtering or deleting can leave the current page past the end of the list.
watch(total, () => {
  const lastPage = Math.max(1, Math.ceil(total.value / size.value))
  if (page.value > lastPage) page.value = lastPage
})
</script>

<template>
  <div class="paged-table">
    <div class="paged-table__body">
      <el-table
        v-loading="props.loading"
        :data="pageRows"
        :row-key="props.rowKey"
        :empty-text="emptyText"
        :default-sort="props.defaultSort"
        @sort-change="handleSortChange"
      >
        <slot />
      </el-table>
    </div>
    <div class="pagination-bar">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="size"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        background
      />
    </div>
  </div>
</template>

<style scoped>
/* The rows scroll inside their own area so the pager stays reachable no
   matter how tall a page of rows turns out to be — a wrapped cell or a
   larger page size used to push it off the bottom of the screen with no way
   to scroll to it. */
.paged-table { display: flex; flex-direction: column; min-height: 0; }
.paged-table__body { flex: 1 1 auto; min-height: 0; overflow: auto; }
.pagination-bar { flex: 0 0 auto; }
</style>
