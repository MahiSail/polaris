<template>
  <MPage title="实时节点状态" :back="true">
    <div style="margin: 0 -2px;">
      <OpenlatConsole :embedded="true" @logout="logout" />
    </div>
  </MPage>
</template>

<script setup>
import { inject } from 'vue'
import OpenlatConsole from '../../components/OpenlatConsole.vue'
import MPage from '../components/MPage.vue'

const logout = inject('logout', () => {})
</script>
