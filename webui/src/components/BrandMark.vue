<script setup>
// Polaris 统一品牌标识：顶级工业美学一体化北极星（B-Final 负空间钻石光孔枢纽）
// 具备 G2 级三次贝塞尔连续平滑收腰、四极端面防次像素虚焦切角（R=0.6px）、内部精密负空间光瞳（R=0.55px 圆角）。
// 基于 currentColor 纯单色语义自适应深浅主题，零脱离悬浮物，100% 向心凝聚。
defineProps({ size: { type: [Number, String], default: 22 } })
</script>

<template>
  <svg :width="size" :height="size" viewBox="0 0 32 32" fill="none" aria-hidden="true" class="polaris-mark">
    <path
      fill-rule="evenodd"
      clip-rule="evenodd"
      d="M 15.35 3.2 C 15.35 2.6 16.65 2.6 16.65 3.2 C 16.65 11.2 20.8 15.35 28.8 15.35 C 29.4 15.35 29.4 16.65 28.8 16.65 C 20.8 16.65 16.65 20.8 16.65 28.8 C 16.65 29.4 15.35 29.4 15.35 28.8 C 15.35 20.8 11.2 16.65 3.2 16.65 C 2.6 16.65 2.6 15.35 3.2 15.35 C 11.2 15.35 15.35 11.2 15.35 3.2 Z M 15.61 13.19 C 15.82 12.98 16.18 12.98 16.39 13.19 L 18.81 15.61 C 19.02 15.82 19.02 16.18 18.81 16.39 L 16.39 18.81 C 16.18 19.02 15.82 19.02 15.61 18.81 L 13.19 16.39 C 12.98 16.18 12.98 15.82 13.19 15.61 Z"
      fill="currentColor"
    />
  </svg>
</template>

<style scoped>
.polaris-mark {
  display: block;
  flex-shrink: 0;
  user-select: none;
}
</style>
