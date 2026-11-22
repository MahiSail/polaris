<script setup>
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Plus } from '@element-plus/icons-vue'
import {
  createListenerModel,
  defaultALPNFor,
  listenerProfileMap,
  listenerProfiles,
  listenerPayload,
  protocolMap,
  securityOptions,
} from '../../protocols'
import MSheet from './MSheet.vue'
import MPicker from './MPicker.vue'

// 接入服务表单。桌面版是一个 1040px 宽的三列弹窗，手机上换成整屏单列，
// 一节一节往下填；模式改成可点选的方块，比下拉好按。
const props = defineProps({
  modelValue: { type: Boolean, required: true },
  listener: { type: Object, default: null },
  template: { type: Object, default: null },
  nodes: { type: Array, default: () => [] },
  listeners: { type: Array, default: () => [] },
  outbounds: { type: Array, default: () => [] },
  allEndpoints: { type: Array, default: () => [] },
  dnsRecords: { type: Array, default: () => [] },
  endpoints: { type: Array, default: () => [] },
  saving: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue', 'save'])

const model = ref(createListenerModel(null, ''))
const accounts = ref([])
const aliasFlags = [
  ['🇭🇰', '香港'], ['🇹🇼', '台湾'], ['🇸🇬', '新加坡'], ['🇯🇵', '日本'],
  ['🇰🇷', '韩国'], ['🇺🇸', '美国'], ['🇬🇧', '英国'], ['🇩🇪', '德国'],
  ['🇫🇷', '法国'], ['🇳🇱', '荷兰'], ['🇨🇦', '加拿大'], ['🇦🇺', '澳大利亚'],
  ['🇪🇪', '爱沙尼亚'], ['🇮🇳', '印度'], ['🇹🇷', '土耳其']
]
const activeFlagPopover = ref(-1)

function setAccountFlag(account, flag) {
  let alias = (account.alias || '').trim()
  // 如果已有其他国旗前缀，先替换
  for (const [f] of aliasFlags) {
    if (alias.startsWith(f)) {
      alias = alias.slice(f.length).trim()
      break
    }
  }
  account.alias = `${flag} ${alias}`.trim()
  activeFlagPopover.value = -1
}

function getAccountFlag(account) {
  const alias = (account.alias || '').trim()
  for (const [f] of aliasFlags) {
    if (alias.startsWith(f)) return f
  }
  return ''
}

const activeAccountIndex = ref(0)

const selectedProfile = computed(() => listenerProfileMap[model.value.profile])
const showReality = computed(() => model.value.security === 'reality')
const currentNode = computed(() => props.nodes.find((node) => node.id === model.value.node_id))
const nodeAddress = computed(() => currentNode.value?.client_address || '')
const nodeOptions = computed(() => props.nodes.map((node) => ({ value: node.id, label: node.name, desc: node.client_address || '未填写客户端连接地址' })))

function getNodeEgressOptions(node) {
  if (!node) return []
  const ips = []
  const seen = new Set()

  const addIP = (ip) => {
    if (ip && typeof ip === 'string') {
      const trimmed = ip.trim()
      if (trimmed && !seen.has(trimmed) && (/^[0-9.]+$/.test(trimmed) || trimmed.includes(':'))) {
        seen.add(trimmed)
        ips.push(trimmed)
      }
    }
  }

  // 1. IP addresses detected by probe
  if (Array.isArray(node.ip_addresses)) {
    for (const ip of node.ip_addresses) addIP(ip)
  }
  // 2. IP aliases configured on this node
  if (node.ip_aliases && typeof node.ip_aliases === 'object') {
    for (const ip of Object.keys(node.ip_aliases)) addIP(ip)
  }
  // 3. IP mappings on this node
  if (node.ip_mappings && typeof node.ip_mappings === 'object') {
    for (const ip of Object.keys(node.ip_mappings)) addIP(ip)
  }
  // 4. Node client address
  addIP(node.client_address)
  // 5. Last observed IP
  addIP(node.last_observed_ip)
  
  const aliases = node.ip_aliases || {}
  return ips.map((ip) => {
    const alias = aliases[ip]
    const ipType = ip.includes(':') ? 'IPv6' : 'IPv4'
    const label = alias ? `${alias} (${ip})` : `${ip} (${ipType})`
    return {
      value: `direct:${ip}`,
      label,
      ip,
      alias,
      type: ipType,
    }
  })
}

const nodeEgressOptions = computed(() => getNodeEgressOptions(currentNode.value))

function defaultEgressValue() {
  return 'direct'
}

function wouldFormCycle(targetEndpointID, currentNodeID, currentListenerID) {
  const visited = new Set([targetEndpointID])
  let currentID = targetEndpointID
  for (let hop = 0; hop < 16; hop++) {
    const ep = (props.allEndpoints || []).find((e) => e.id === currentID)
    if (!ep || !ep.outbound_id || !ep.outbound_id.startsWith('chain:')) break
    const nextID = ep.outbound_id.slice(6)
    if (visited.has(nextID)) return true
    visited.add(nextID)
    const nextEp = (props.allEndpoints || []).find((e) => e.id === nextID)
    if (!nextEp) break
    const nextListener = (props.listeners || []).find((l) => l.id === nextEp.listener_id)
    if (nextListener) {
      if (currentListenerID && nextListener.id === currentListenerID) return true
      if (currentNodeID && nextListener.node_id === currentNodeID) return true
    }
    currentID = nextID
  }
  return false
}

const chainTargets = computed(() => {
  const currentNodeID = model.value.node_id
  const currentListenerID = props.listener?.id || ''
  const supported = new Set(['vless', 'hysteria2'])
  return (props.allEndpoints || []).flatMap((endpoint) => {
    if (currentListenerID && endpoint.listener_id === currentListenerID) return []
    const listener = (props.listeners || []).find((item) => item.id === endpoint.listener_id)
    if (!listener || !supported.has(listener.spec?.protocol)) return []
    if (currentNodeID && listener.node_id === currentNodeID) return []
    if (!listener.enabled || !endpoint.enabled) return []
    if (wouldFormCycle(endpoint.id, currentNodeID, currentListenerID)) return []
    const node = (props.nodes || []).find((item) => item.id === listener.node_id)
    const nodeName = node?.name || listener.node_id
    return [{
      value: `chain:${endpoint.id}`,
      nodeName,
      label: endpoint.alias || endpoint.name,
      desc: `${(protocolMap[listener.spec?.protocol]?.label || listener.spec?.protocol).toUpperCase()} · 端口 ${listener.port}`,
    }]
  })
})

function isOrphanChain(outboundID) {
  if (!outboundID || !outboundID.startsWith('chain:')) return false
  return !chainTargets.value.some((target) => target.value === outboundID)
}

function orphanChainLabel(outboundID) {
  if (!outboundID || !outboundID.startsWith('chain:')) return ''
  const targetID = outboundID.slice(6)
  const targetEndpoint = (props.allEndpoints || []).find((e) => e.id === targetID)
  if (targetEndpoint) {
    const listener = (props.listeners || []).find((item) => item.id === targetEndpoint.listener_id)
    const node = (props.nodes || []).find((item) => item.id === listener?.node_id)
    const nodeName = node?.name || listener?.node_id || '其他服务器'
    const listName = listener?.name || '未知服务'
    const name = targetEndpoint.alias || targetEndpoint.name
    return `${nodeName} · ${listName} · ${name} (已停用或同服务器)`
  }
  return `已删除的节点 (${targetID.slice(0, 8)})`
}

function isOutboundActive(item) {
  if (!item || item.id === 'direct') return false
  if (!item.expires_at) return true
  const expiry = new Date(item.expires_at).getTime()
  return Number.isNaN(expiry) || expiry >= Date.now()
}

function accountOutboundOptions(account) {
  const result = []
  // 1. 直连与出口 IP
  result.push({
    value: 'direct',
    label: '服务器直连 (系统默认出口)',
    desc: '使用服务器系统默认路由与网关出站',
    group: '直连与出口 IP',
  })
  const egressOpts = nodeEgressOptions.value
  for (const opt of egressOpts) {
    result.push({
      value: opt.value,
      label: opt.label,
      desc: opt.alias ? `${opt.type} 本机指定出口 · ${opt.ip}` : `${opt.type} 本机指定出口`,
      group: '直连与出口 IP',
    })
  }
  if (account?.outbound_id?.startsWith('direct:') && !egressOpts.some((o) => o.value === account.outbound_id)) {
    const ip = account.outbound_id.slice(7)
    const alias = currentNode.value?.ip_aliases?.[ip]
    const ipType = ip.includes(':') ? 'IPv6' : 'IPv4'
    const label = alias ? `${alias} (${ip})` : `${ip} (${ipType})`
    result.push({
      value: account.outbound_id,
      label: `${label} (已脱机)`,
      desc: '历史保存的出口 IP 已在服务器网卡脱机',
      group: '直连与出口 IP',
    })
  }
  // 2. 上网出口（配置的出口）
  for (const item of (props.outbounds || []).filter(isOutboundActive)) {
    result.push({
      value: item.id,
      label: item.name + (item.type ? ` (${item.type.toUpperCase()})` : '') + (!item.enabled ? ' [已停用]' : ''),
      desc: item.server ? `${item.type.toUpperCase()} ${item.server}:${item.server_port}` : '配置的上网出口',
      group: '上网出口（配置的出口）',
    })
  }
  // 3. 链式代理（其他服务器节点）
  for (const item of chainTargets.value) {
    result.push({
      value: item.value,
      label: item.label,
      desc: item.desc,
      group: item.nodeName,
    })
  }
  if (account?.outbound_id && isOrphanChain(account.outbound_id)) {
    result.push({
      value: account.outbound_id,
      label: orphanChainLabel(account.outbound_id),
      desc: '已失效的链式代理目标',
      group: '已失效的链式代理',
    })
  }
  return result
}

function recordName(record) {
  return String(record.name || '').replace(/\.$/, '')
}

// 解析到这台服务器的域名：A/AAAA 直接指向它的、CNAME 指到那些名字的，
// 以及服务器本身就用域名接入时的那个域名。
const matchedDomains = computed(() => {
  const address = nodeAddress.value
  if (!address) return []
  const found = new Map()
  const add = (name, proxied) => { if (name && !found.has(name)) found.set(name, proxied) }
  for (const record of props.dnsRecords) {
    if (['A', 'AAAA'].includes(record.type) && record.content === address) add(recordName(record), Boolean(record.proxied))
  }
  if (!/^[0-9.]+$/.test(address) && !address.includes(':')) {
    const own = props.dnsRecords.find((record) => recordName(record) === address)
    add(address, own ? Boolean(own.proxied) : null)
  }
  let grew = true
  while (grew) {
    grew = false
    for (const record of props.dnsRecords) {
      if (record.type !== 'CNAME' || found.has(recordName(record))) continue
      const target = String(record.content || '').replace(/\.$/, '')
      if (target === address || found.has(target)) {
        add(recordName(record), Boolean(record.proxied))
        grew = true
      }
    }
  }
  return [...found].map(([value, proxied]) => ({
    value,
    note: proxied === null ? '' : proxied ? '橙云' : '直连',
  }))
})

function randomHex(byteLength) {
  const bytes = new Uint8Array(byteLength)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('')
}

function randomAccountName() {
  return `user_${randomHex(4)}`
}

// 能猜到的 httpupgrade 路径就是被探测出来的入口，所以路径一律生成而不是手填。
function randomPath() {
  return `/${randomHex(12)}`
}

function setProfile(value) {
  model.value.profile = value
  model.value.tls_alpn = defaultALPNFor(value)
  normalizeProfile()
  if (listenerProfileMap[value]?.transport === 'httpupgrade' && !model.value.transport_path) {
    model.value.transport_path = randomPath()
  }
}

function normalizeProfile() {
  const profile = listenerProfileMap[model.value.profile]
  if (!profile) return
  const protocol = protocolMap[profile.protocol]
  model.value.protocol = profile.protocol
  model.value.network = protocol.network
  model.value.security = profile.security
  model.value.transport_type = profile.transport
  if (!props.listener && !props.template) {
    model.value.name = `${profile.label} 接入服务`
    model.value.port = protocol.defaultPort || 443
  }
}

watch(() => props.modelValue, (open) => {
  if (!open) return
  model.value = createListenerModel(props.listener || props.template, props.nodes[0]?.id || '')
  // 编辑保留客户端正在用的路径；新建和复制各自生成新的。
  if (!props.listener) model.value.transport_path = randomPath()
  // 复制时把原有用户当成新用户带入：去掉 id，保存时会生成各自的新凭据。
  accounts.value = props.listener || props.template
    ? props.endpoints.map((endpoint) => ({
        id: props.listener ? endpoint.id : '',
        name: endpoint.name,
        alias: endpoint.alias || endpoint.name,
        enabled: endpoint.enabled,
        outbound_id: endpoint.outbound_id || 'direct',
      }))
    : []
  if (!accounts.value.length) {
    accounts.value = [{ id: '', name: randomAccountName(), alias: '', enabled: true, outbound_id: 'direct' }]
  }
  activeFlagPopover.value = -1
  normalizeProfile()
})

watch(() => model.value.node_id, (newNodeID) => {
  const targetNode = props.nodes.find((n) => n.id === newNodeID)
  if (!targetNode) return
  const options = getNodeEgressOptions(targetNode)
  for (const account of accounts.value) {
    if (!account.outbound_id) {
      account.outbound_id = 'direct'
    } else if (account.outbound_id.startsWith('direct:') && !options.some((o) => o.value === account.outbound_id)) {
      account.outbound_id = 'direct'
    }
  }
})

function addAccount() {
  accounts.value.push({ id: '', name: randomAccountName(), alias: '', enabled: true, outbound_id: 'direct' })
}

function removeAccount(index) {
  if (accounts.value.length <= 1) return
  accounts.value.splice(index, 1)
  if (activeFlagPopover.value === index) activeFlagPopover.value = -1
}

function save() {
  try {
    if (!selectedProfile.value) throw new Error('请选择接入模式')
    if (!model.value.node_id) throw new Error('请选择服务器')
    if (!model.value.name.trim()) throw new Error('请输入服务名称')
    if (!model.value.connection_domain.trim()) throw new Error('请填写连接域名')
    if (!model.value.port) throw new Error('请输入服务端口')
    if (showReality.value && !model.value.reality_handshake_server) throw new Error('请输入 Reality 目标网站')
    if (selectedProfile.value.transport === 'grpc' && !model.value.transport_service_name) throw new Error('请填写 gRPC 服务名称')
    const names = accounts.value.map((item) => item.name.trim())
    const aliases = accounts.value.map((item) => item.alias.trim())
    if (!names.length || names.some((name) => !name)) throw new Error('请至少保留一个已填写名称的节点')
    if (new Set(names).size !== names.length) throw new Error('同一接入服务中的节点名称不能重复')
    if (aliases.some((alias) => !alias)) throw new Error('请为每个节点填写客户端节点别名')
    if (new Set(aliases).size !== aliases.length) throw new Error('同一接入服务中的客户端节点别名不能重复')
    emit('save', {
      listener: listenerPayload(model.value),
      accounts: accounts.value.map((item) => ({
        id: item.id,
        name: item.name.trim(),
        alias: item.alias.trim(),
        enabled: item.enabled,
        outbound_id: item.outbound_id || 'direct',
      })),
    })
  } catch (error) {
    ElMessage.error(error.message)
  }
}
</script>

<template>
  <MSheet
    :model-value="modelValue"
    :title="listener ? '修改接入服务' : template ? '复制接入服务' : '新建接入服务'"
    full
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div v-if="template" class="m-notice m-notice--info">
      已复制原服务配置，保存前请确认服务器、端口与域名。
    </div>

    <div class="m-section">接入模式</div>
    <button
      v-for="profile in listenerProfiles"
      :key="profile.value"
      type="button"
      class="m-choice"
      :class="{ 'is-active': model.profile === profile.value }"
      @click="setProfile(profile.value)"
    >
      <strong>{{ profile.label }}</strong>
      <span>{{ profile.summary }} · {{ securityOptions[profile.security]?.label }}</span>
    </button>

    <div class="m-section">基础配置</div>
    <div class="m-field">
      <label class="m-field__label">服务器 <em>*</em></label>
      <MPicker v-model="model.node_id" :options="nodeOptions" title="选择服务器" placeholder="选择服务器" />
    </div>
    <div class="m-field">
      <label class="m-field__label">服务名称 <em>*</em></label>
      <el-input v-model="model.name" aria-label="服务名称" placeholder="例如：VLESS gRPC" />
    </div>
    <div class="m-field">
      <label class="m-field__label">服务端口 <em>*</em></label>
      <el-input-number v-model="model.port" :min="1" :max="65535" controls-position="right" aria-label="服务端口" style="width: 100%" />
      <div v-if="listener" class="m-field__hint">端口变更后需同步更新客户端。</div>
    </div>
    <div class="m-field">
      <label class="m-field__label">连接域名 <em>*</em></label>
      <el-input v-model="model.connection_domain" aria-label="连接域名" placeholder="proxy.example.com" />
      <div v-if="matchedDomains.length" class="m-chips">
        <button
          v-for="item in matchedDomains"
          :key="item.value"
          type="button"
          class="m-chip domain"
          @click="model.connection_domain = item.value"
        >{{ item.value }}<small v-if="item.note">{{ item.note }}</small></button>
      </div>
      <div class="m-field__hint">
        <template v-if="!nodeAddress">选择服务器后可选域名。</template>
        <template v-else-if="matchedDomains.length">可选解析到该服务器的域名，也可手动输入。</template>
        <template v-else>没有解析到该服务器的域名，可直接输入。</template>
      </div>
    </div>

    <template v-if="showReality">
      <div class="m-section">Reality</div>
      <div class="m-field">
        <label class="m-field__label">目标网站 <em>*</em></label>
        <el-input v-model="model.reality_handshake_server" aria-label="Reality 目标网站" placeholder="www.microsoft.com" />
      </div>
      <div class="m-field">
        <label class="m-field__label">网站端口</label>
        <el-input-number v-model="model.reality_handshake_port" :min="1" :max="65535" controls-position="right" aria-label="Reality 网站端口" style="width: 100%" />
      </div>
    </template>

    <template v-if="model.transport_type === 'httpupgrade'">
      <div class="m-section">传输配置</div>
      <div class="m-field">
        <label class="m-field__label">请求路径</label>
        <el-input v-model="model.transport_path" readonly aria-label="httpupgrade 请求路径">
          <template #append><el-button @click="model.transport_path = randomPath()">重新生成</el-button></template>
        </el-input>
        <div class="m-field__hint">系统随机生成；重新生成后需同步更新客户端。</div>
      </div>
    </template>

    <template v-if="model.transport_type === 'grpc'">
      <div class="m-section">传输配置</div>
      <div class="m-field">
        <label class="m-field__label">gRPC 服务名称 <em>*</em></label>
        <el-input v-model="model.transport_service_name" aria-label="gRPC 服务名称" placeholder="grpc-service" />
        <div class="m-field__hint">客户端需填写相同名称。</div>
      </div>
    </template>

    <div class="m-section m-section--between">
      <span>节点配置</span>
      <button type="button" class="m-section-action-btn" @click="addAccount">
        <el-icon><Plus /></el-icon>
        <span>添加节点</span>
      </button>
    </div>
    <div class="m-node-list">
      <div
        v-for="(account, index) in accounts"
        :key="account.id || `node-${index}`"
        class="m-node-card"
      >
        <div class="m-node-card__header">
          <div class="m-node-card__title">
            <span class="m-node-num">{{ index + 1 }}</span>
            <span class="m-node-name-preview">{{ account.alias || account.name || `节点 ${index + 1}` }}</span>
          </div>
          <div class="m-node-card__actions">
            <el-switch
              v-model="account.enabled"
              size="small"
              inline-prompt
              active-text="开"
              inactive-text="关"
              aria-label="启用节点"
            />
            <button
              v-if="accounts.length > 1"
              type="button"
              class="m-node-del-icon-btn"
              title="删除该节点"
              aria-label="删除节点"
              @click="removeAccount(index)"
            >
              <el-icon><Delete /></el-icon>
            </button>
          </div>
        </div>

        <div class="m-node-card__body">
          <div class="m-field">
            <label class="m-field__label">客户端节点别名 <em>*</em></label>
            <div class="alias-combo-input">
              <button
                type="button"
                class="alias-flag-badge"
                :class="{ 'is-active': activeFlagPopover === index }"
                title="点击选择地区国旗"
                @click="activeFlagPopover = activeFlagPopover === index ? -1 : index"
              >
                <span class="flag-val">{{ getAccountFlag(account) || '🏳️' }}</span>
                <span class="flag-arrow">▾</span>
              </button>
              <el-input
                v-model="account.alias"
                maxlength="128"
                aria-label="客户端节点别名"
                placeholder="例如：🇭🇰 香港专线 01"
              />
            </div>
            <!-- 国旗轻量平铺选择板 -->
            <div v-if="activeFlagPopover === index" class="flag-grid-selector">
              <button
                v-for="[flag, country] in aliasFlags"
                :key="flag"
                type="button"
                class="flag-grid-item"
                @click="setAccountFlag(account, flag)"
              >
                <span class="flag-icon">{{ flag }}</span>
                <span class="flag-label">{{ country }}</span>
              </button>
            </div>
          </div>

          <div class="m-field">
            <label class="m-field__label">上网出口与链式代理</label>
            <MPicker
              v-model="account.outbound_id"
              :options="accountOutboundOptions(account)"
              title="选择出口或链式节点"
            />
          </div>

          <div class="m-field m-field--auth-id">
            <div class="auth-id-row">
              <label class="m-field__label">节点认证名称 (内部 ID) <em>*</em></label>
            </div>
            <el-input v-model="account.name" aria-label="节点名称" placeholder="例如：user_xxxx" />
          </div>
        </div>
      </div>
    </div>

    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="saving" @click="save">{{ listener ? '保存' : '创建' }}</el-button>
    </template>
  </MSheet>
</template>

<style scoped>
.m-section--between {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.m-section-action-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: var(--sb-accent-soft);
  color: var(--sb-primary);
  border: 1px solid var(--sb-primary-soft);
  border-radius: var(--sb-radius-sm);
  padding: 4px 10px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
}
.m-node-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.m-node-card {
  border: 1px solid var(--sb-line);
  border-radius: var(--sb-radius-md);
  background: var(--sb-surface-1);
  overflow: hidden;
}
.m-node-card__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  background: var(--sb-surface-2);
  border-bottom: 1px solid var(--sb-line);
}
.m-node-card__title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.m-node-num {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border-radius: 4px;
  background: var(--sb-line);
  color: var(--sb-text);
  font-size: 11px;
  font-weight: 600;
}
.m-node-name-preview {
  font-size: 14px;
  font-weight: 600;
  color: var(--sb-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.m-node-card__actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
}
.m-node-del-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: var(--sb-radius-sm);
  border: 1px solid var(--sb-danger-soft, #fca5a5);
  background: var(--sb-danger-soft, #fef2f2);
  color: var(--sb-danger, #ef4444);
  cursor: pointer;
  padding: 0;
  font-size: 14px;
}
.m-node-card__body {
  padding: 14px 14px 4px;
}
.alias-combo-input {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
}
.alias-combo-input :deep(.el-input) {
  flex: 1;
  min-width: 0;
}
.alias-flag-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  height: 38px;
  padding: 0 10px;
  border: 1px solid var(--sb-line-strong);
  border-radius: var(--sb-radius-sm);
  background: var(--sb-surface-2);
  cursor: pointer;
  flex-shrink: 0;
}
.alias-flag-badge.is-active {
  border-color: var(--sb-primary);
  background: var(--sb-accent-soft);
}
.alias-flag-badge .flag-val {
  font-size: 18px;
  line-height: 1;
}
.alias-flag-badge .flag-arrow {
  font-size: 10px;
  color: var(--sb-muted);
}
.flag-grid-selector {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 6px;
  margin-top: 8px;
  padding: 8px;
  border: 1px solid var(--sb-line);
  border-radius: var(--sb-radius-sm);
  background: var(--sb-surface-2);
}
.flag-grid-item {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 6px 8px;
  border: 1px solid var(--sb-line);
  border-radius: var(--sb-radius-sm);
  background: var(--sb-surface-1);
  color: var(--sb-text);
  cursor: pointer;
  font-size: 12px;
}
.flag-grid-item:active {
  background: var(--sb-accent-soft);
  border-color: var(--sb-primary);
}
.flag-grid-item .flag-icon {
  font-size: 16px;
}
.m-field--auth-id {
  margin-top: 4px;
}
.auth-id-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.m-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
}
.m-chip.domain {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border-radius: var(--sb-radius-sm);
  background: var(--sb-surface-2);
  border: 1px solid var(--sb-line-strong);
  color: var(--sb-text);
  font-size: 13px;
  cursor: pointer;
}
.m-chip.domain:hover, .m-chip.domain:active {
  border-color: var(--sb-primary);
  background: var(--sb-accent-soft);
}
.domain small {
  color: var(--sb-primary);
  font-size: 11px;
  font-weight: 500;
  background: var(--sb-surface-1);
  padding: 1px 4px;
  border-radius: 4px;
}
</style>