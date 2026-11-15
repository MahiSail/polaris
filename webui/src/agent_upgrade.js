import { ElMessage, ElMessageBox } from 'element-plus'
import { post } from './api'
import { waitForTask } from './live'

/**
 * Prompts for confirmation, dispatches an agent upgrade task, and tracks its
 * completion over the live event stream so failures are surfaced immediately.
 */
export async function performAgentUpgrade(node, { refreshList, refreshNodes } = {}) {
  await ElMessageBox.confirm(
    `将“${node.name}”的 agent 升级到最新版本？升级完成后 agent 会自动重启并重新连接，运行中的代理服务不受影响。`,
    '升级 Agent',
    { type: 'warning', confirmButtonText: '开始升级' },
  )
  const task = await post(`/nodes/${node.id}/agent/upgrade`, {})
  ElMessage.success('升级任务已下发，正在等待服务器更新并重启')
  if (!task?.id) return
  waitForTask(task.id, 180000)
    .then(async (done) => {
      if (done.status === 'succeeded') {
        ElMessage.success(done.result_summary || `“${node.name}”的 agent 已更新`)
        if (refreshList) await refreshList().catch(() => {})
        if (refreshNodes) window.setTimeout(() => { refreshNodes().catch(() => {}) }, 3000)
      } else {
        ElMessage.error(done.result_summary || `“${node.name}”的 agent 升级失败`)
      }
    })
    .catch((err) => {
      ElMessage.warning(err?.message || '暂未收到升级结果，请稍后刷新或在“操作记录”中查看')
    })
}

/**
 * Prompts for confirmation and dispatches batch agent upgrade tasks for all specified
 * nodes, tracking their completion over the live event stream.
 */
export async function performAllAgentsUpgrade(nodes, { refreshList, refreshNodes, latestVersion } = {}) {
  const count = nodes?.length || 0
  if (count === 0) {
    ElMessage.info('当前所有服务器的 Agent 均已是最新版本，无需升级')
    return
  }
  const summary = count <= 5
    ? `（${nodes.map((n) => n.name).join('、')}）`
    : `等 ${count} 台服务器`
  await ElMessageBox.confirm(
    `确认将 ${count} 台服务器${summary}的 Agent 升级到最新版本${latestVersion ? ` (v${latestVersion})` : ''}？升级完成后 Agent 会自动重启并重新连接，运行中的代理服务不受影响。`,
    '一键升级所有 Agent',
    { type: 'warning', confirmButtonText: '开始升级' },
  )

  let tasks = []
  try {
    const res = await post('/nodes/agent/upgrade-all', {})
    tasks = res?.tasks || []
    ElMessage.success(`已向 ${res?.dispatched ?? tasks.length} 台服务器下发升级任务，正在等待更新并重启`)
  } catch {
    // Fallback: parallel dispatch per node
    const results = await Promise.allSettled(nodes.map((n) => post(`/nodes/${n.id}/agent/upgrade`, {})))
    tasks = results.filter((r) => r.status === 'fulfilled' && r.value?.id).map((r) => r.value)
    ElMessage.success(`已向 ${tasks.length} 台服务器下发升级任务，正在等待更新并重启`)
  }

  if (!tasks.length) return

  let succeeded = 0
  let failed = 0
  Promise.all(tasks.map((task) =>
    waitForTask(task.id, 180000)
      .then((done) => {
        if (done.status === 'succeeded') succeeded += 1
        else failed += 1
      })
      .catch(() => { failed += 1 })
  )).then(async () => {
    if (succeeded > 0 && failed === 0) {
      ElMessage.success(`所有 Agent 升级完成（${succeeded} 台成功）`)
    } else if (succeeded > 0 && failed > 0) {
      ElMessage.warning(`Agent 升级完成：${succeeded} 台成功，${failed} 台失败`)
    } else if (failed > 0) {
      ElMessage.error(`Agent 升级未完成，请在“操作记录”中查看详情`)
    }
    if (refreshList) await refreshList().catch(() => {})
    if (refreshNodes) window.setTimeout(() => { refreshNodes().catch(() => {}) }, 3000)
  })
}
