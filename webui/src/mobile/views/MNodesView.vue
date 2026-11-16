<template>
  <MPage
    :title="detailNode ? detailNode.name : (isShareMode ? '只读共享节点' : '服务器')"
    :back="isShareMode && !detailNode ? false : true"
    :loading="loading"
    @back="handlePageBack"
  >
    <template #actions>
      <template v-if="!detailNode && !isShareMode">
        <el-button
          v-if="isAdmin && upgradableAgentNodes.length > 0"
          size="small"
          type="warning"
          plain
          @click="upgradeAllAgents"
        >
          升级 Agent ({{ upgradableAgentNodes.length }})
        </el-button>
        <button
          v-if="canWrite"
          type="button"
          class="m-header__btn"
          aria-label="探针管理"
          @click="probeManagerOpen = true"
        >
          探针
        </button>
        <button
          v-if="canWrite"
          type="button"
          class="m-header__btn"
          aria-label="分享服务器"
          @click="shareOpen = true"
        >
          <el-icon><Share /></el-icon>
        </button>
        <el-button
          circle
          size="small"
          :icon="Refresh"
          :loading="refreshing"
          aria-label="刷新"
          @click="load(false)"
        />
      </template>
    </template>

    <!-- 详情子页面 -->
    <MNodeDetailView
      v-if="detailNode"
      :node="detailNode"
      :metrics="metrics[detailNode.id] || {}"
      :live="live.get(detailNode.id) || null"
      :latest-agent-version="appState.systemUpdate?.latest_version || ''"
      :sing-box-latest="singBoxLatest"
      :can-write="canWrite"
      :is-admin="isAdmin"
      @edit="(tab) => openEdit(detailNode, tab)"
      @probe-peers="openPeerProbe(detailNode)"
      @upgrade-agent="upgradeAgent(detailNode)"
      @upgrade-singbox="upgradeSingBox(detailNode)"
      @revoke="revoke(detailNode)"
      @refresh="load(false)"
    />

    <!-- 列表主视图 -->
    <div v-else class="m-stack">
      <SummaryCards v-if="appState.nodes.length" :nodes="appState.nodes" :metrics="metrics" />

      <div v-if="appState.nodes.length" class="m-card" style="padding: 10px 12px;">
        <div class="m-filter-bar" :style="showMap ? 'margin-bottom: 8px;' : ''">
          <span style="font-size: 13px; font-weight: 600;">全球分布视图</span>
          <el-button size="small" text @click="toggleMap">
            {{ showMap ? '收起地图' : '展开地图' }}
          </el-button>
        </div>
        <NodeMap v-if="showMap" :nodes="appState.nodes" :live="live" @open="openDetail" />
      </div>

      <div class="m-stack" style="gap: 8px;">
        <el-input
          v-model="keyword"
          placeholder="搜索服务器名称、IP、系统或厂商"
          clearable
        />

        <MSegmented
          v-model="statusFilter"
          :options="[
            { value: '', label: `全部 (${statusCounts.all})` },
            { value: 'online', label: `在线 (${statusCounts.online})` },
            { value: 'offline', label: `离线 (${statusCounts.offline})` },
          ]"
        />

        <div class="m-sort-row">
          <el-select v-model="sortBy" size="small" style="flex: 1;" aria-label="排序方式">
            <el-option label="按名称排序" value="name" />
            <el-option label="按活跃时间" value="last_seen" />
            <el-option label="按实时连接数" value="conn" />
            <el-option label="按 CPU 占用" value="cpu" />
            <el-option label="按内存占用" value="mem" />
          </el-select>
          <div class="m-filter-row" style="gap: 4px; flex: none;">
            <button
              type="button"
              class="m-pill"
              :class="{ 'is-active': viewMode === 'card' }"
              aria-label="卡片视图"
              @click="viewMode = 'card'"
            >
              卡片
            </button>
            <button
              type="button"
              class="m-pill"
              :class="{ 'is-active': viewMode === 'list' }"
              aria-label="列表视图"
              @click="viewMode = 'list'"
            >
              列表
            </button>
          </div>
        </div>
      </div>

      <!-- Pending Registrations -->
      <template v-if="!isShareMode && filteredPending.length">
        <div class="m-section" style="padding: 4px 2px 0;">
          <span>等待确认的服务器（{{ filteredPending.length }}）</span>
        </div>
        <article v-for="row in filteredPending" :key="row.id" class="m-item m-pending-item">
          <div class="m-item__hit is-static">
            <div class="m-item__head" style="display: flex; align-items: center; gap: 8px;">
              <strong class="m-item__title">{{ row.node_name }}</strong>
              <span class="m-pending-tag">待接入</span>
            </div>
            <div class="m-pending-specs">
              <span class="m-spec-chip">
                <OsIcon :os="parseCapabilities(row.capabilities).os" class="w-3.5 h-3.5 inline-block mr-1" />
                {{ parseCapabilities(row.capabilities).os }} · {{ parseCapabilities(row.capabilities).arch }}
              </span>
              <span v-if="parseCapabilities(row.capabilities).singbox" class="m-spec-chip">
                sing-box {{ parseCapabilities(row.capabilities).singbox }}
              </span>
              <span class="m-spec-chip m-mono-wrap">{{ parseCapabilities(row.capabilities).ip }}</span>
            </div>
            <div class="m-item__meta">提交时间：{{ formatDateTime(row.created_at) }}</div>
          </div>
          <button
            v-if="isAdmin"
            type="button"
            class="m-pending-approve"
            :disabled="approvingIds.has(row.id)"
            @click="approve(row)"
          >
            <el-icon :size="18"><Check /></el-icon>
            <small>{{ approvingIds.has(row.id) ? '处理中' : '接入' }}</small>
          </button>
        </article>
      </template>

      <div class="m-section" style="padding: 4px 2px 0;">
        <span>已接入服务器（{{ filteredNodes.length }}）</span>
      </div>

      <div v-if="!filteredNodes.length" class="m-card m-note">尚未匹配到服务器</div>

      <!-- Card View -->
      <div v-else-if="viewMode === 'card'" class="m-list">
        <article
          v-for="node in shownNodes"
          :key="node.id"
          class="m-node-card"
          @click="openDetail(node)"
        >
          <div class="m-node-head">
            <div class="m-node-hero min-w-0 flex-1">
              <span style="font-size: 18px; line-height: 1;">{{ nodeFlag(node) }}</span>
              <OsIcon :os="node.os_name || node.os" class="m-node-os" />
              <div style="min-width: 0; flex: 1;">
                <h3 class="m-node-title truncate" :title="node.name">{{ node.name }}</h3>
                <div class="m-note truncate" style="font-size: 11px;">
                  {{ nodeLocationLabel(node) || node.os_name || node.os || '—' }}
                  <span v-if="node.architecture"> · {{ node.architecture }}</span>
                </div>
              </div>
            </div>
            <div style="display: flex; align-items: center; gap: 6px; flex: none;">
              <StatusDot :online="node.online" />
              <button
                v-if="!isShareMode"
                type="button"
                class="m-header__btn"
                aria-label="服务器操作"
                @click.stop="openActions(node)"
              >
                <el-icon><MoreFilled /></el-icon>
              </button>
            </div>
          </div>

          <div class="m-node-sub">
            <span
              class="m-mono-wrap"
              style="cursor: pointer; text-decoration: underline dotted;"
              @click.stop="copyAddress(node)"
            >
              {{ node.client_address || node.last_observed_ip || '未设置地址' }}
            </span>
            <span v-if="node.agent_version">v{{ node.agent_version }}</span>
            <span v-if="node.bandwidth">{{ formatBandwidth(node.bandwidth) }}</span>
          </div>

          <!-- Tag Strip: expiry, DDNS, upgrades -->
          <div v-if="!isShareMode" class="m-filter-row" style="gap: 5px;">
            <el-tag
              v-if="nodeExpiryInfo(node)"
              :type="nodeExpiryInfo(node).tone"
              size="small"
              effect="light"
            >
              {{ nodeExpiryInfo(node).label }}
            </el-tag>
            <el-tag v-if="node.ddns_enabled" size="small" type="success" effect="plain">
              DDNS
            </el-tag>
            <el-tag v-if="agentUpdateAvailable(node)" size="small" type="warning" effect="light">
              Agent 可升级
            </el-tag>
            <el-tag v-if="singBoxUpdateAvailable(node)" size="small" type="warning" effect="plain">
              sing-box 可升级
            </el-tag>
          </div>

          <!-- 3 Resource Meters: CPU, Mem, Disk -->
          <div class="m-node-meters" style="grid-template-columns: repeat(3, minmax(0, 1fr));">
            <div class="m-node-meter">
              <div class="m-node-meter-head">
                <span>CPU</span>
                <span>{{ statOf(node, (h) => h.cpu_percent != null ? `${Math.round(h.cpu_percent)}%` : '—') }}</span>
              </div>
              <div class="m-meter-track">
                <div
                  class="m-meter-fill"
                  style="background: var(--sb-chart-1);"
                  :style="{ width: `${meterPct(node, 'cpu')}%` }"
                />
              </div>
            </div>

            <div class="m-node-meter">
              <div class="m-node-meter-head">
                <span>内存</span>
                <span>{{ memText(node) }}</span>
              </div>
              <div class="m-meter-track">
                <div
                  class="m-meter-fill"
                  style="background: var(--sb-chart-2);"
                  :style="{ width: `${meterPct(node, 'mem')}%` }"
                />
              </div>
            </div>

            <div class="m-node-meter">
              <div class="m-node-meter-head">
                <span>硬盘</span>
                <span>{{ diskText(node) }}</span>
              </div>
              <div class="m-meter-track">
                <div
                  class="m-meter-fill"
                  style="background: var(--sb-chart-3);"
                  :style="{ width: `${meterPct(node, 'disk')}%` }"
                />
              </div>
            </div>
          </div>

          <!-- Monthly Traffic & Quota -->
          <div v-if="trafficOf(node)" class="m-stack" style="gap: 4px; padding-top: 2px;">
            <div class="m-filter-bar" style="font-size: 11px; color: var(--sb-text-2);">
              <span>
                本月流量 {{ formatBytes(trafficOf(node).monthlyTotal) }}
                <template v-if="node.traffic_plan"> / {{ node.traffic_plan }}</template>
              </span>
              <span>累计 {{ formatBytes(trafficOf(node).lifetimeTotal) }}</span>
            </div>
            <div v-if="trafficOf(node).quotaPercent !== null" class="m-meter-track">
              <div
                class="m-meter-fill"
                :style="{
                  width: `${Math.min(100, trafficOf(node).quotaPercent)}%`,
                  background: trafficOf(node).quotaPercent >= 90 ? 'var(--sb-danger)' : 'var(--sb-chart-4)',
                }"
              />
            </div>
          </div>

          <!-- Foot: Speed, Latency, Connections -->
          <div class="m-filter-bar" style="font-size: 12px; color: var(--sb-muted);">
            <span class="m-mono-wrap">
              ↓ {{ speedText(node, 'net_rx_rate') }} · ↑ {{ speedText(node, 'net_tx_rate') }}
            </span>
            <span>
              {{ latencyText(node) }} · 连接 {{ connText(node) }}
            </span>
          </div>
        </article>
      </div>

      <!-- Compact List View -->
      <div v-else class="m-list">
        <div
          v-for="node in shownNodes"
          :key="node.id"
          class="m-card"
          style="padding: 10px 12px; cursor: pointer;"
          @click="openDetail(node)"
        >
          <div class="m-filter-bar" style="margin-bottom: 4px;">
            <div style="display: flex; align-items: center; gap: 6px; min-width: 0; flex: 1;">
              <span>{{ nodeFlag(node) }}</span>
              <StatusDot :online="node.online" />
              <strong class="truncate" style="font-size: 14px;">{{ node.name }}</strong>
            </div>
            <div style="display: flex; align-items: center; gap: 6px;">
              <span class="m-note">{{ latencyText(node) }}</span>
              <button
                v-if="!isShareMode"
                type="button"
                class="m-header__btn"
                aria-label="操作"
                @click.stop="openActions(node)"
              >
                <el-icon><MoreFilled /></el-icon>
              </button>
            </div>
          </div>
          <div class="m-filter-bar" style="font-size: 11px; color: var(--sb-muted);">
            <span>CPU {{ statOf(node, (h) => h.cpu_percent != null ? `${Math.round(h.cpu_percent)}%` : '—') }} · 内存 {{ memText(node) }} · 连接 {{ connText(node) }}</span>
            <span>{{ formatBytes(trafficOf(node).monthlyTotal) }}</span>
          </div>
        </div>
      </div>

      <button
        v-if="filteredNodes.length > visible"
        type="button"
        class="m-load-more"
        @click="visible += 20"
      >
        加载更多（还有 {{ filteredNodes.length - visible }} 台）
      </button>
    </div>

    <!-- Quick Action Sheet -->
    <MActionSheet
      v-model="actionsOpen"
      :title="actionTarget?.name"
      :details="actionDetails"
      :actions="sheetActions"
      @select="runAction"
    />

    <!-- Tabbed Edit Sheet -->
    <MSheet v-model="editSheet" :title="editNode ? `配置服务器 · ${editNode.name}` : '编辑服务器'">
      <div class="m-stack">
        <MSegmented
          v-model="editTab"
          :options="[
            { value: 'basic', label: '基本' },
            { value: 'plan', label: '套餐' },
            { value: 'network', label: 'DDNS/IP' },
            { value: 'probes', label: '探针' },
          ]"
        />

        <!-- Tab: basic -->
        <template v-if="editTab === 'basic'">
          <div class="m-field">
            <label class="m-field__label">服务器名称 <em>*</em></label>
            <el-input v-model="editForm.name" maxlength="128" aria-label="服务器名称" placeholder="例如：🇭🇰 香港服务器 01" />
            <div class="m-field__hint">名称开头可写国旗 emoji 或地区关键字，列表自动显示对应地区旗帜。</div>
          </div>

          <div class="m-field">
            <label class="m-field__label">客户端连接域名或 IP 地址</label>
            <el-input v-model="editForm.client_address" aria-label="客户端连接域名或 IP 地址" placeholder="例如：proxy.example.com" />
            <div class="m-field__hint">仅填域名或 IP，不含 http://、端口与路径。</div>
            <div v-if="editNodeAllIPs.length" class="m-filter-row" style="margin-top: 6px;">
              <span class="m-note">点击填入：</span>
              <el-tag
                v-for="ip in editNodeAllIPs"
                :key="ip"
                size="small"
                type="info"
                style="cursor: pointer;"
                @click="editForm.client_address = ip"
              >
                {{ editForm.ip_aliases?.[ip] ? `${editForm.ip_aliases[ip]} (${ip})` : ip }}
              </el-tag>
            </div>
          </div>
        </template>

        <!-- Tab: plan -->
        <template v-else-if="editTab === 'plan'">
          <div class="m-field">
            <label class="m-field__label">计价周期与费用</label>
            <el-select v-model="editForm.price_period" placeholder="计价周期" style="width: 100%; margin-bottom: 8px;">
              <el-option label="未设置" value="" />
              <el-option label="免费" value="free" />
              <el-option label="按天" value="day" />
              <el-option label="按月" value="month" />
              <el-option label="按季" value="quarter" />
              <el-option label="按年" value="year" />
            </el-select>
            <div style="display: flex; gap: 8px;">
              <el-input
                v-model="editForm.price"
                :disabled="editForm.price_period === 'free'"
                placeholder="价格"
                style="flex: 1;"
              />
              <el-select v-model="editForm.currency" style="width: 105px;">
                <el-option label="USD" value="USD" />
                <el-option label="CNY" value="CNY" />
                <el-option label="EUR" value="EUR" />
                <el-option label="CAD" value="CAD" />
              </el-select>
            </div>
          </div>

          <div class="m-field">
            <div class="m-filter-bar" style="margin-bottom: 4px;">
              <label class="m-field__label" style="margin: 0;">到期时间</label>
              <el-button
                size="small"
                text
                @click="editForm.expires_on = 'permanent'"
              >
                设为永久
              </el-button>
            </div>
            <el-input
              v-if="editForm.expires_on === 'permanent'"
              model-value="永久有效 (permanent)"
              readonly
            >
              <template #append>
                <el-button @click="editForm.expires_on = ''">改回日期</el-button>
              </template>
            </el-input>
            <el-date-picker
              v-else
              v-model="editForm.expires_on"
              type="date"
              value-format="YYYY-MM-DD"
              placeholder="选择到期日期"
              clearable
              style="width: 100%;"
            />
          </div>

          <div class="m-field">
            <label class="m-field__label">月流量配额</label>
            <div style="display: flex; gap: 8px;">
              <el-input v-model="editForm.traffic_value" placeholder="例如 1000" style="flex: 1;" />
              <el-select v-model="editForm.traffic_unit" style="width: 100px;">
                <el-option label="MB" value="MB" />
                <el-option label="GB" value="GB" />
                <el-option label="TB" value="TB" />
              </el-select>
            </div>
          </div>

          <div class="m-field">
            <label class="m-field__label">端口带宽上限</label>
            <div style="display: flex; gap: 8px;">
              <el-input v-model="editForm.bandwidth_value" placeholder="例如 1" style="flex: 1;" />
              <el-select v-model="editForm.bandwidth_unit" style="width: 100px;">
                <el-option label="Mbps" value="Mbps" />
                <el-option label="Gbps" value="Gbps" />
              </el-select>
            </div>
          </div>

          <div class="m-field">
            <label class="m-field__label">服务厂商</label>
            <el-input v-model="editForm.provider" placeholder="例如：BandwagonHost / DMIT" style="margin-bottom: 8px;" />
            <el-input v-model="editForm.provider_url" placeholder="厂商控制台 URL（可选）" />
          </div>
        </template>

        <!-- Tab: network -->
        <template v-else-if="editTab === 'network'">
          <div class="m-field">
            <label class="m-field__label">多出口 IP 别名管理</label>
            <div class="m-field__hint" style="margin-bottom: 6px;">自动从服务器网卡获取或手动添加出口 IP 并绑定语义别名。</div>

            <div class="m-filter-row" style="margin-bottom: 8px;">
              <el-input v-model="newCustomEgressIP" placeholder="手动添加 IPv4/IPv6" style="flex: 1;" />
              <el-button size="small" @click="addCustomEgressIP">添加</el-button>
            </div>

            <div v-if="editNodeAllIPs.length" class="m-list">
              <div
                v-for="ip in editNodeAllIPs"
                :key="ip"
                style="background: var(--sb-surface-2); padding: 8px 10px; border-radius: 8px; border: 1px solid var(--sb-line);"
              >
                <div class="m-filter-bar" style="margin-bottom: 4px;">
                  <span class="m-mono-wrap" style="font-size: 12px;">{{ ip }}</span>
                  <el-button
                    v-if="!editNode?.ip_addresses?.includes(ip)"
                    text
                    type="danger"
                    size="small"
                    @click="removeCustomEgressIP(ip)"
                  >
                    移除
                  </el-button>
                </div>
                <el-input
                  v-model="editForm.ip_aliases[ip]"
                  size="small"
                  placeholder="出口别名（例如：电信出口、落地IP）"
                  clearable
                />
              </div>
            </div>
          </div>

          <div class="m-field">
            <div class="m-filter-bar" style="margin-bottom: 8px;">
              <label class="m-field__label" style="margin: 0;">Cloudflare DDNS 自动更新</label>
              <el-switch
                v-model="editForm.ddns_enabled"
                active-text="已开启"
                inactive-text="未开启"
                inline-prompt
              />
            </div>

            <div v-if="editForm.ddns_enabled" class="m-stack" style="gap: 8px;">
              <el-select
                v-model="editForm.ddns_record_ids"
                multiple
                filterable
                collapse-tags
                placeholder="选择绑定域名记录"
                style="width: 100%;"
                :loading="cfLoading"
                @change="syncDDNSDomainsFromSelection"
              >
                <el-option
                  v-for="rec in cfRecords"
                  :key="rec.id"
                  :label="`${rec.type} · ${rec.name}`"
                  :value="rec.id"
                />
              </el-select>

              <div v-if="(editForm.ddns_domains || []).length" class="m-filter-row">
                <el-tag v-for="d in editForm.ddns_domains" :key="d" size="small" type="info">
                  {{ d }}
                </el-tag>
              </div>

              <el-select
                v-model="editForm.ddns_bind_ip"
                filterable
                allow-create
                default-first-option
                clearable
                placeholder="默认同步出口 IP（留空则自动探测）"
                style="width: 100%;"
              >
                <el-option
                  v-for="opt in editDDNSIPOptions"
                  :key="opt.value || 'auto'"
                  :label="opt.label"
                  :value="opt.value"
                />
              </el-select>

              <div
                v-if="(editForm.ddns_record_ids || []).length > 1 || Object.keys(editForm.ddns_record_ips || {}).length > 0"
                class="m-stack"
                style="gap: 8px;"
              >
                <label class="m-field__label" style="margin: 0; font-size: 12px;">分记录独立指定 IP（可选）</label>
                <div
                  v-for="(recId, idx) in editForm.ddns_record_ids"
                  :key="recId"
                  style="padding: 8px; border-radius: 8px; background: var(--sb-surface-2); display: flex; flex-direction: column; gap: 4px;"
                >
                  <span class="m-mono-wrap" style="font-size: 12px;">
                    {{ cfRecordById(recId)?.type || 'DNS' }} · {{ cfRecordById(recId)?.name || editForm.ddns_domains[idx] || recId }}
                  </span>
                  <el-select
                    v-model="editForm.ddns_record_ips[recId]"
                    filterable
                    allow-create
                    default-first-option
                    clearable
                    placeholder="继承默认策略"
                    style="width: 100%;"
                  >
                    <el-option
                      v-for="opt in editDDNSRecordIPOptions(recId)"
                      :key="opt.value"
                      :label="opt.label"
                      :value="opt.value"
                    />
                  </el-select>
                </div>
              </div>
            </div>
          </div>
        </template>

        <!-- Tab: probes -->
        <template v-else-if="editTab === 'probes'">
          <div class="m-card m-card--flat">
            <div class="m-filter-bar" style="margin-bottom: 6px;">
              <strong style="font-size: 13px;">公共默认探针 ({{ publicProbes.length }})</strong>
              <el-button size="small" text @click="probeManagerOpen = true">管理全局探针</el-button>
            </div>
            <div v-if="publicProbes.length === 0" class="m-note">暂无全局默认探针</div>
            <div v-else class="m-filter-row">
              <el-tag v-for="t in publicProbes" :key="t.id" size="small" effect="plain">
                {{ t.name }} ({{ t.target }})
              </el-tag>
            </div>
          </div>

          <div class="m-field">
            <div class="m-filter-bar" style="margin-bottom: 8px;">
              <label class="m-field__label" style="margin: 0;">服务器专属探针 ({{ (editForm.custom_probes || []).length }})</label>
              <el-button size="small" type="primary" plain @click="addCustomProbe">
                + 添加专属探针
              </el-button>
            </div>
            <div v-if="!(editForm.custom_probes || []).length" class="m-card m-card--flat m-note">
              暂无专属探针，点击上方按钮添加仅针对该服务器的探测任务。
            </div>
            <div v-else class="m-list">
              <div
                v-for="(probe, idx) in editForm.custom_probes"
                :key="probe.id || `m-new-${idx}`"
                style="background: var(--sb-surface-2); padding: 10px; border-radius: 8px; border: 1px solid var(--sb-line); display: flex; flex-direction: column; gap: 6px;"
              >
                <div class="m-filter-bar">
                  <strong style="font-size: 12px;">专属探针 #{{ idx + 1 }}</strong>
                  <el-button size="small" type="danger" text @click="removeCustomProbe(idx)">删除</el-button>
                </div>
                <el-input v-model="probe.name" size="small" placeholder="名称，如：香港联通" />
                <el-input v-model="probe.target" size="small" placeholder="目标 IP/域名:端口" />
                <div class="m-filter-bar">
                  <span class="m-note">间隔（秒）</span>
                  <el-input-number v-model="probe.interval" size="small" :min="5" :max="3600" />
                </div>
              </div>
            </div>
          </div>
        </template>
      </div>

      <template #footer>
        <el-button @click="editSheet = false">取消</el-button>
        <el-button
          type="primary"
          :loading="editSaving"
          :disabled="!editForm.name.trim()"
          @click="saveNode"
        >
          保存
        </el-button>
      </template>
    </MSheet>

    <!-- Token Install Sheet -->
    <MSheet v-model="tokenSheet" title="服务器接入信息" full>
      <div class="m-notice m-notice--warning">
        此命令只显示一次，请立即复制。令牌一次性使用，过期后需重新生成。
      </div>
      <div class="m-field">
        <label class="m-field__label">Master 地址（agent 拨号用的主机）</label>
        <el-input v-model="masterHost" placeholder="例如：ctrl.example.com" @change="persistMasterHost" />
      </div>
      <div class="m-field">
        <label class="m-field__label">在目标服务器上以 root 执行</label>
        <pre class="command">{{ installCommand }}</pre>
      </div>
      <p class="m-field__hint">
        令牌有效期至：{{ formatDateTime(expiresAt) }}。命令执行后该服务器会出现在「等待确认的服务器」，需在本页点「接入」批准。
      </p>
      <template #footer>
        <el-button @click="tokenSheet = false">完成</el-button>
        <el-button type="primary" @click="copyInstallCommand">复制</el-button>
      </template>
    </MSheet>

    <MProbeManager v-model="probeManagerOpen" :nodes="appState.nodes" @changed="loadPingTasks" />
    <MShareSheet v-model="shareOpen" page="nodes" />
    <MPeerProbeSheet v-model="peerProbeOpen" :node="peerProbeTarget" />
    <MSheet
      v-model="ddnsHistorySheet"
      :title="ddnsHistoryNode ? `DDNS 变更记录 · ${ddnsHistoryNode.name}` : 'DDNS 变更记录'"
    >
      <MDDNSHistoryList v-if="ddnsHistorySheet" :node-id="ddnsHistoryNode?.id || ''" />
    </MSheet>

    <template v-if="isAdmin && !detailNode && !isShareMode" #fab>
      <button type="button" class="m-fab" aria-label="添加服务器" @click="generateToken">
        <el-icon :size="24"><Plus /></el-icon>
      </button>
    </template>
  </MPage>
</template>

<script setup>
import { computed, inject, onMounted, onUnmounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox, ElNotification } from 'element-plus'
import { Check, MoreFilled, Plus, Refresh, Share } from '@element-plus/icons-vue'
import { performAgentUpgrade, performAllAgentsUpgrade } from '../../agent_upgrade'
import { api, post, put } from '../../api'
import { writeClipboard } from '../../clipboard'
import { connectionSnapshots, subscribeConnections } from '../../connections'
import { buildDDNSIPOptions } from '../../ddns'
import { formatBandwidth, formatBytes, formatDateTime, includesText } from '../../format'
import { subscribeLive } from '../../live'
import { isShareMode, shareAPIPath } from '../../share'
import { isNewerVersion } from '../../version'
import NodeMap from '../../components/NodeMap.vue'
import OsIcon from '../../components/OsIcon.vue'
import StatusDot from '../../components/StatusDot.vue'
import SummaryCards from '../../components/SummaryCards.vue'
import MDDNSHistoryList from '../components/MDDNSHistoryList.vue'
import MPage from '../components/MPage.vue'
import MPeerProbeSheet from '../components/MPeerProbeSheet.vue'
import MProbeManager from '../components/MProbeManager.vue'
import MSegmented from '../components/MSegmented.vue'
import MShareSheet from '../components/MShareSheet.vue'
import MSheet from '../components/MSheet.vue'
import MNodeDetailView from './MNodeDetailView.vue'
import {
  nodeExpiryInfo,
  nodeFlag,
  nodeLocationLabel,
  nodeTrafficStats,
  parseBandwidthPlan,
  parseTrafficPlan,
} from '../utils/nodeHelpers'

const MASTER_HOST_KEY = 'polaris_master_host'
const MAP_OPEN_KEY = 'polaris_mobile_nodes_map_open'
const VIEW_MODE_KEY = 'polaris_mobile_nodes_view_mode'

const appState = inject('appState')
const canWrite = inject('canWrite')
const isAdmin = inject('isAdmin')
const loadNodes = inject('loadNodes')
const goBack = inject('goBack', () => {})

const loading = ref(false)
const refreshing = ref(false)
const keyword = ref('')
const statusFilter = ref(getInitialStatusFilter())
const sortBy = ref('name')
const viewMode = ref(window.localStorage.getItem(VIEW_MODE_KEY) || 'card')
const showMap = ref(window.localStorage.getItem(MAP_OPEN_KEY) !== '0')
const visible = ref(20)

const detailNodeId = ref('')
const pending = ref([])
const approvingIds = ref(new Set())
const metrics = ref({})
const pingTasks = ref([])
const singBoxLatest = ref({})

const actionsOpen = ref(false)
const actionTarget = ref(null)

const editSheet = ref(false)
const editTab = ref('basic')
const editSaving = ref(false)
const editNode = ref(null)
const cfLoading = ref(false)
const cfRecords = ref([])
const newCustomEgressIP = ref('')
const editForm = ref(createEmptyEditForm())

const tokenSheet = ref(false)
const token = ref('')
const expiresAt = ref('')
const lifetime = 900
const masterPublicKey = ref('')
const masterHost = ref('')
const agentPort = ref(19994)

const probeManagerOpen = ref(false)
const shareOpen = ref(false)
const peerProbeOpen = ref(false)
const peerProbeTarget = ref(null)
const ddnsHistorySheet = ref(false)
const ddnsHistoryNode = ref(null)

let knownPendingIds = null
let pendingTimer = null
let stopLive = null
let stopConnections = null

watch(viewMode, (v) => window.localStorage.setItem(VIEW_MODE_KEY, v))

function toggleMap() {
  showMap.value = !showMap.value
  window.localStorage.setItem(MAP_OPEN_KEY, showMap.value ? '1' : '0')
}

function createEmptyEditForm() {
  return {
    name: '',
    client_address: '',
    ddns_enabled: false,
    ddns_record_id: '',
    ddns_domain: '',
    ddns_record_ids: [],
    ddns_domains: [],
    ddns_bind_ip: '',
    ddns_record_ips: {},
    ip_aliases: {},
    expires_on: '',
    price: '',
    price_period: '',
    currency: 'USD',
    provider: '',
    provider_url: '',
    traffic_value: '',
    traffic_unit: 'GB',
    bandwidth_value: '',
    bandwidth_unit: 'Gbps',
    custom_probes: [],
  }
}

function getInitialStatusFilter() {
  const queryStr = location.search ? location.search.slice(1) : location.hash.split('?')[1] || ''
  return new URLSearchParams(queryStr).get('status') || ''
}

function onPopStateFilter() {
  const s = getInitialStatusFilter()
  statusFilter.value = s
}

function parseCapabilities(raw) {
  if (!raw) return { os: '—', arch: '—', singbox: '', ip: '—' }
  try {
    const obj = typeof raw === 'string' ? JSON.parse(raw) : raw
    return {
      os: obj.os_name || obj.os || '—',
      arch: obj.architecture || '—',
      singbox: obj.sing_box_version || '',
      ip: obj.last_observed_ip || obj.client_address || '—',
    }
  } catch {
    return { os: '—', arch: '—', singbox: '', ip: '—' }
  }
}

const live = computed(() => connectionSnapshots.value)

const detailNode = computed(() => {
  if (!detailNodeId.value) return null
  return (appState.nodes || []).find((n) => n.id === detailNodeId.value) || null
})

const statusCounts = computed(() => {
  const nodes = appState.nodes || []
  let online = 0
  let offline = 0
  for (const n of nodes) {
    if (n.online) online++
    else offline++
  }
  return { all: nodes.length, online, offline }
})

const filteredPending = computed(() =>
  pending.value.filter((row) => includesText([row.node_name, row.capabilities], keyword.value))
)

function resourceRatio(used, total) {
  if (!total || total <= 0) return 0
  return (Number(used || 0) / Number(total)) * 100
}

const filteredNodes = computed(() => {
  const list = (appState.nodes || []).filter((n) => {
    if (statusFilter.value === 'online' && !n.online) return false
    if (statusFilter.value === 'offline' && n.online) return false
    return includesText(
      [
        n.name,
        n.client_address,
        n.last_observed_ip,
        n.os,
        n.os_name,
        n.architecture,
        n.provider,
        nodeLocationLabel(n),
      ],
      keyword.value
    )
  })

  return [...list].sort((a, b) => {
    if (sortBy.value === 'last_seen') {
      return (Date.parse(b.last_seen_at) || 0) - (Date.parse(a.last_seen_at) || 0)
    }
    if (sortBy.value === 'conn') {
      return (live.value.get(b.id)?.connection_count || 0) - (live.value.get(a.id)?.connection_count || 0)
    }
    if (sortBy.value === 'cpu') {
      return (metrics.value[b.id]?.host?.cpu_percent || 0) - (metrics.value[a.id]?.host?.cpu_percent || 0)
    }
    if (sortBy.value === 'mem') {
      const ma = resourceRatio(metrics.value[a.id]?.host?.memory_used, metrics.value[a.id]?.host?.memory_total)
      const mb = resourceRatio(metrics.value[b.id]?.host?.memory_used, metrics.value[b.id]?.host?.memory_total)
      return mb - ma
    }
    return (a.name || '').localeCompare(b.name || '', 'zh-Hans-CN', { numeric: true, sensitivity: 'base' })
  })
})

const shownNodes = computed(() => filteredNodes.value.slice(0, visible.value))

const upgradableAgentNodes = computed(() =>
  (appState.nodes || []).filter((n) => n.online && agentUpdateAvailable(n))
)

const publicProbes = computed(() => pingTasks.value.filter((t) => t.auto_join !== false))

const editNodeAllIPs = computed(() => {
  const ips = []
  const seen = new Set()
  const add = (ip) => {
    const val = (ip || '').trim()
    if (val && !seen.has(val)) {
      seen.add(val)
      ips.push(val)
    }
  }
  if (Array.isArray(editNode.value?.ip_addresses)) {
    for (const ip of editNode.value.ip_addresses) add(ip)
  }
  if (editForm.value?.ip_aliases) {
    for (const ip of Object.keys(editForm.value.ip_aliases)) add(ip)
  }
  return ips
})

const editDDNSIPOptions = computed(() =>
  buildDDNSIPOptions(editNode.value, editForm.value.ip_aliases, {
    selectedValues: [editForm.value.ddns_bind_ip],
  })
)

function editDDNSRecordIPOptions(recId) {
  const recType = cfRecordById(recId)?.type || ''
  return buildDDNSIPOptions(editNode.value, editForm.value.ip_aliases, {
    recordType: recType,
    selectedValues: [editForm.value.ddns_record_ips?.[recId]],
  }).filter((o) => o.value)
}

function cfRecordById(recId) {
  return cfRecords.value.find((r) => r.id === recId) || null
}

function hostOf(node) {
  return live.value.get(node.id)?.host || metrics.value[node.id]?.host || null
}

function statOf(node, picker) {
  if (!node.online) return '—'
  const h = hostOf(node)
  return h ? picker(h) : '—'
}

function meterPct(node, kind) {
  if (!node.online) return 0
  const h = hostOf(node)
  if (!h) return 0
  if (kind === 'cpu') return Math.min(100, Math.max(0, Math.round(h.cpu_percent || 0)))
  if (kind === 'mem') return Math.min(100, Math.round(resourceRatio(h.memory_used, h.memory_total)))
  if (kind === 'disk') return Math.min(100, Math.round(resourceRatio(h.disk_used, h.disk_total)))
  return 0
}

function memText(node) {
  const pct = meterPct(node, 'mem')
  return node.online && hostOf(node)?.memory_total ? `${pct}%` : '—'
}

function diskText(node) {
  const pct = meterPct(node, 'disk')
  return node.online && hostOf(node)?.disk_total ? `${pct}%` : '—'
}

function speedText(node, field) {
  if (!node.online) return '0 B/s'
  const h = hostOf(node)
  return formatBytes(h?.[field] || 0, '/s')
}

function latencyText(node) {
  if (!node.online) return '离线'
  const lat = live.value.get(node.id)?.latency || metrics.value[node.id]?.latency
  const val = lat?.latency ?? lat?.avgLatency
  return val != null ? `${Math.round(val)} ms` : '— ms'
}

function connText(node) {
  if (!node.online) return '0'
  return String(live.value.get(node.id)?.connection_count ?? 0)
}

function trafficOf(node) {
  return nodeTrafficStats(node, metrics.value[node.id])
}

function agentUpdateAvailable(node) {
  return isNewerVersion(appState.systemUpdate?.latest_version, node.agent_version)
}

function singBoxUpdateAvailable(node) {
  const latest = singBoxLatest.value[node.architecture]
  return Boolean(latest && node.sing_box_version && node.sing_box_version !== latest)
}

function checkNewPending(list) {
  if (!isAdmin.value) return
  const nextIds = new Set((list || []).map((r) => r.id))
  if (knownPendingIds !== null) {
    for (const item of list || []) {
      if (!knownPendingIds.has(item.id)) {
        ElNotification({
          title: '新服务器申请接入',
          message: `检测到服务器「${item.node_name}」申请接入，请确认批准。`,
          type: 'warning',
          duration: 6000,
        })
      }
    }
  }
  knownPendingIds = nextIds
}

async function load(silent = false) {
  if (refreshing.value) return
  refreshing.value = true
  if (!silent && !(appState.nodes || []).length) loading.value = true
  try {
    await loadNodes()
    const metricsPath = isShareMode ? shareAPIPath('/nodes/metrics') : '/nodes/metrics'
    const [regRes, metricRes] = await Promise.all([
      isAdmin.value && !isShareMode ? api('/registrations').catch(() => ({ registrations: [] })) : Promise.resolve({ registrations: [] }),
      api(metricsPath).catch(() => ({ nodes: [] })),
    ])
    const regs = regRes.registrations || []
    pending.value = regs
    checkNewPending(regs)
    metrics.value = Object.fromEntries((metricRes.nodes || []).map((entry) => [entry.node_id, entry.report]))
    if (!isShareMode) await loadSingBoxLatest()
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

async function loadSingBoxLatest() {
  const architectures = [...new Set((appState.nodes || []).map((n) => n.architecture).filter(Boolean))]
  await Promise.all(
    architectures.map(async (arch) => {
      try {
        const res = await api(`/sing-box/latest?architecture=${encodeURIComponent(arch)}`)
        if (res?.version) singBoxLatest.value[arch] = res.version
      } catch {
        // ignore offline check errors
      }
    })
  )
}

async function loadPingTasks() {
  if (isShareMode) return
  try {
    const res = await api('/ping-tasks').catch(() => ({ tasks: [] }))
    pingTasks.value = res.tasks || []
  } catch {
    pingTasks.value = []
  }
}

function getCustomProbesForNode(nodeId) {
  if (!nodeId) return []
  return pingTasks.value
    .filter((t) => t.auto_join === false && Array.isArray(t.node_ids) && t.node_ids.includes(nodeId))
    .map((t) => ({ id: t.id, name: t.name, target: t.target, interval: t.interval || 60 }))
}

function openDetail(nodeOrId) {
  const id = typeof nodeOrId === 'string' ? nodeOrId : nodeOrId?.id
  if (!id) return
  detailNodeId.value = id
}

function closeDetail() {
  detailNodeId.value = ''
}

function handlePageBack() {
  if (detailNode.value) {
    closeDetail()
  } else {
    goBack('more')
  }
}

function openActions(node) {
  actionTarget.value = node
  actionsOpen.value = true
}

const actionDetails = computed(() => {
  const n = actionTarget.value
  if (!n) return []
  const exp = nodeExpiryInfo(n)
  return [
    { label: '状态', value: n.online ? '在线' : '离线' },
    { label: '连接地址', value: n.client_address || n.last_observed_ip || '未设置' },
    { label: '地区 / 系统', value: [nodeLocationLabel(n), n.os_name || n.os].filter(Boolean).join(' · ') || '—' },
    { label: '本月流量', value: `${formatBytes(trafficOf(n).monthlyTotal)}${n.traffic_plan ? ` / ${n.traffic_plan}` : ''}` },
    ...(exp ? [{ label: '到期状态', value: exp.label }] : []),
  ]
})

const sheetActions = computed(() => {
  const n = actionTarget.value
  if (!n) return []
  const list = [{ key: 'detail', label: '查看详情与图表', hint: '资源曲线 · 延迟 · 详细参数' }]
  if (canWrite.value) {
    list.push({ key: 'edit', label: '编辑配置', hint: '名称、套餐、DDNS 与地址' })
    list.push({ key: 'edit-probes', label: '探针配置', hint: '查看默认探针与自定义探针' })
  }
  list.push({ key: 'probe-peers', label: '线路与丢包排查', hint: '实测到其他互联服务器的延迟与丢包' })
  if (canWrite.value && n.ddns_enabled) {
    list.push({ key: 'ddns-sync', label: '同步 DDNS', hint: '立即更新 Cloudflare 解析' })
  }
  if (n.ddns_enabled) {
    list.push({ key: 'ddns-history', label: 'DDNS 变更记录' })
  }
  if (isAdmin.value && agentUpdateAvailable(n)) {
    list.push({ key: 'upgrade', label: '升级 Agent', hint: `升级到 v${appState.systemUpdate?.latest_version}` })
  }
  if (isAdmin.value && singBoxUpdateAvailable(n)) {
    list.push({ key: 'upgrade-sing-box', label: '升级 sing-box', hint: `升级到 ${singBoxLatest.value[n.architecture]}` })
  }
  if (isAdmin.value) {
    list.push({ key: 'revoke', label: '移除服务器', danger: true })
  }
  return list
})

async function runAction(key) {
  const node = actionTarget.value
  if (!node) return
  if (key === 'detail') return openDetail(node)
  if (key === 'edit') return openEdit(node, 'basic')
  if (key === 'edit-probes') return openEdit(node, 'probes')
  if (key === 'probe-peers') return openPeerProbe(node)
  if (key === 'ddns-sync') return syncNodeDDNS(node)
  if (key === 'ddns-history') {
    ddnsHistoryNode.value = node
    ddnsHistorySheet.value = true
    return
  }
  if (key === 'upgrade') return upgradeAgent(node)
  if (key === 'upgrade-sing-box') return upgradeSingBox(node)
  if (key === 'revoke') return revoke(node)
}

function openPeerProbe(node) {
  peerProbeTarget.value = node
  peerProbeOpen.value = true
}

async function syncNodeDDNS(node) {
  try {
    const res = await post(`/nodes/${node.id}/ddns/sync`, {})
    ElMessage.success(`DDNS 同步成功${res.ip ? `：${res.ip}` : ''}`)
    await load(true)
  } catch (err) {
    ElMessage.error(err.message || 'DDNS 同步失败')
  }
}

async function copyAddress(node) {
  const addr = node.client_address || node.last_observed_ip
  if (!addr) return
  try {
    await writeClipboard(addr)
    ElMessage.success(`已复制：${addr}`)
  } catch {
    ElMessage.error('复制失败')
  }
}

async function loadCloudflareRecords() {
  cfLoading.value = true
  try {
    const cfSettings = await api('/cloudflare/settings').catch(() => ({ configured: false }))
    if (!cfSettings?.configured) {
      cfRecords.value = []
      return
    }
    const res = await api('/cloudflare/records').catch(() => ({ records: [] }))
    cfRecords.value = (res.records || []).filter((r) => r.type === 'A' || r.type === 'AAAA')
  } finally {
    cfLoading.value = false
  }
}

function syncDDNSDomainsFromSelection(selectedIds) {
  const selected = cfRecords.value.filter((r) => selectedIds.includes(r.id))
  editForm.value.ddns_domains = selected.map((r) => r.name)
  if (selected.length) {
    editForm.value.ddns_record_id = selected[0].id
    editForm.value.ddns_domain = selected[0].name
  } else {
    editForm.value.ddns_record_id = ''
    editForm.value.ddns_domain = ''
  }
}

async function openEdit(node, initialTab = 'basic') {
  editNode.value = node
  const recordIds = node.ddns_record_ids?.length
    ? [...node.ddns_record_ids]
    : node.ddns_record_id
      ? [node.ddns_record_id]
      : []
  const domains = node.ddns_domains?.length
    ? [...node.ddns_domains]
    : node.ddns_domain
      ? [node.ddns_domain]
      : []

  const tPlan = parseTrafficPlan(node.traffic_plan)
  const bwPlan = parseBandwidthPlan(node.bandwidth)

  editForm.value = {
    name: node.name || '',
    client_address: node.client_address || '',
    ddns_enabled: Boolean(node.ddns_enabled),
    ddns_record_id: node.ddns_record_id || '',
    ddns_domain: node.ddns_domain || '',
    ddns_record_ids: recordIds,
    ddns_domains: domains,
    ddns_bind_ip: node.ddns_bind_ip || '',
    ddns_record_ips: node.ddns_record_ips ? { ...node.ddns_record_ips } : {},
    ip_aliases: node.ip_aliases ? { ...node.ip_aliases } : {},
    expires_on: node.expires_on || '',
    price: node.price || '',
    price_period: node.price_period || '',
    currency: node.currency || 'USD',
    provider: node.provider || '',
    provider_url: node.provider_url || '',
    traffic_value: tPlan.traffic_value,
    traffic_unit: tPlan.traffic_unit,
    bandwidth_value: bwPlan.bandwidth_value,
    bandwidth_unit: bwPlan.bandwidth_unit,
    custom_probes: getCustomProbesForNode(node.id),
  }

  editTab.value = typeof initialTab === 'string' && initialTab ? initialTab : 'basic'
  editSheet.value = true
  loadCloudflareRecords()
  loadPingTasks().then(() => {
    if (editNode.value?.id === node.id) {
      editForm.value.custom_probes = getCustomProbesForNode(node.id)
    }
  })
}

function addCustomEgressIP() {
  const ip = (newCustomEgressIP.value || '').trim()
  if (!ip) return
  if (!editForm.value.ip_aliases) editForm.value.ip_aliases = {}
  if (editForm.value.ip_aliases[ip] === undefined) {
    editForm.value.ip_aliases[ip] = ''
  }
  newCustomEgressIP.value = ''
  ElMessage.success(`已添加出口 IP: ${ip}`)
}

function removeCustomEgressIP(ip) {
  if (editForm.value.ip_aliases && ip in editForm.value.ip_aliases) {
    delete editForm.value.ip_aliases[ip]
  }
}

function addCustomProbe() {
  if (!Array.isArray(editForm.value.custom_probes)) editForm.value.custom_probes = []
  editForm.value.custom_probes.push({ id: 0, name: '', target: '', interval: 60 })
}

function removeCustomProbe(idx) {
  if (Array.isArray(editForm.value.custom_probes)) {
    editForm.value.custom_probes.splice(idx, 1)
  }
}

async function saveNode() {
  const customProbes = []
  for (const p of editForm.value.custom_probes || []) {
    const name = (p.name || '').trim()
    const target = (p.target || '').trim()
    if (!name && !target) continue
    if (!name || !target) {
      editTab.value = 'probes'
      ElMessage.warning('请完整填写专属探针的名称和目标地址')
      return
    }
    customProbes.push({
      id: p.id || 0,
      name,
      target,
      interval: Math.max(5, Number(p.interval) || 60),
    })
  }

  editSaving.value = true
  try {
    const trafficPlan = editForm.value.traffic_value
      ? `${editForm.value.traffic_value.trim()} ${editForm.value.traffic_unit}`
      : ''
    const bandwidth = editForm.value.bandwidth_value
      ? `${editForm.value.bandwidth_value.trim()} ${editForm.value.bandwidth_unit}`
      : ''

    await put(`/nodes/${editNode.value.id}`, {
      name: editForm.value.name.trim(),
      client_address: editForm.value.client_address.trim(),
      ddns_enabled: editForm.value.ddns_enabled,
      ddns_record_id: editForm.value.ddns_record_id,
      ddns_domain: editForm.value.ddns_domain,
      ddns_record_ids: editForm.value.ddns_record_ids,
      ddns_domains: editForm.value.ddns_domains,
      ddns_bind_ip: (editForm.value.ddns_bind_ip || '').trim(),
      ddns_record_ips: editForm.value.ddns_record_ips || {},
      ip_aliases: editForm.value.ip_aliases || {},
      expires_on: editForm.value.expires_on,
      price: editForm.value.price_period === 'free' ? '0' : editForm.value.price,
      price_period: editForm.value.price_period,
      currency: editForm.value.currency,
      provider: editForm.value.provider,
      provider_url: editForm.value.provider_url,
      traffic_plan: trafficPlan,
      bandwidth,
      custom_probes: customProbes,
    })

    ElMessage.success('服务器信息已保存')
    editSheet.value = false
    await Promise.all([load(true), loadPingTasks()])
  } catch (err) {
    ElMessage.error(err.message || '保存失败')
  } finally {
    editSaving.value = false
  }
}

const masterAddress = computed(() => {
  const host = masterHost.value.trim().replace(/[^A-Za-z0-9.:_[\]-]/g, '')
  return `${host}:${agentPort.value}`
})

const installCommand = computed(() =>
  [
    'curl -fsSLo install.sh https://raw.githubusercontent.com/liyuwei007036/polaris/main/install.sh',
    ` && sudo env POLARIS_MASTER_ADDRESS='${masterAddress.value}'`,
    ` POLARIS_MASTER_PUBKEY='${masterPublicKey.value}'`,
    ` POLARIS_REGISTRATION_TOKEN='${token.value}'`,
    ' bash install.sh agent',
  ].join('')
)

function persistMasterHost() {
  window.localStorage.setItem(MASTER_HOST_KEY, masterHost.value.trim())
}

async function generateToken() {
  const result = await post('/nodes/registration-tokens', { lifetime_seconds: lifetime })
  token.value = result.token
  expiresAt.value = result.expires_at
  masterPublicKey.value = result.master_public_key || ''
  agentPort.value = result.agent_port || 19994
  masterHost.value = window.localStorage.getItem(MASTER_HOST_KEY) || window.location.hostname
  tokenSheet.value = true
}

async function copyInstallCommand() {
  try {
    await writeClipboard(installCommand.value)
    ElMessage.success('安装命令已复制到剪贴板')
  } catch {
    ElMessage.error('复制失败，请手动选中命令')
  }
}

async function approve(registration) {
  try {
    await ElMessageBox.confirm(
      `允许服务器“${registration.node_name}”接入管理平台？批准后即可远程管理该服务器并下发配置。`,
      '确认服务器接入',
      { confirmButtonText: '确认批准', cancelButtonText: '取消', type: 'warning' }
    )
  } catch {
    return
  }

  approvingIds.value.add(registration.id)
  try {
    await post(`/registrations/${registration.id}/approve`, {})
    ElMessage.success(`已批准 ${registration.node_name} 接入`)
    await load(true)
  } catch (err) {
    ElMessage.error(err?.message || '批准接入失败')
  } finally {
    approvingIds.value.delete(registration.id)
  }
}

async function upgradeAgent(node) {
  try {
    await performAgentUpgrade(node, {
      refreshList: () => load(true),
      refreshNodes: loadNodes,
    })
  } catch (err) {
    if (err !== 'cancel' && err?.message !== 'cancel') {
      ElMessage.error(err?.message || 'Agent 升级失败')
    }
  }
}

async function upgradeAllAgents() {
  if (!upgradableAgentNodes.value.length) {
    ElMessage.info('当前所有服务器的 Agent 均已是最新版本，无需升级')
    return
  }
  try {
    await performAllAgentsUpgrade(upgradableAgentNodes.value, {
      refreshList: () => load(true),
      refreshNodes: loadNodes,
      latestVersion: appState.systemUpdate?.latest_version,
    })
  } catch (err) {
    if (err !== 'cancel' && err?.message !== 'cancel') {
      ElMessage.error(err?.message || '批量升级 Agent 失败')
    }
  }
}

async function upgradeSingBox(node) {
  const targetVersion = singBoxLatest.value[node.architecture] || '最新版本'
  try {
    await ElMessageBox.confirm(
      `确认将“${node.name}”的 sing-box 升级到 ${targetVersion}？`,
      '升级 sing-box',
      { confirmButtonText: '开始升级', cancelButtonText: '取消', type: 'warning' }
    )
  } catch {
    return
  }
  await post(`/nodes/${node.id}/sing-box/install`, {})
  ElMessage.success('升级任务已下发，完成后服务器会上报新的 sing-box 版本')
}

async function revoke(node) {
  try {
    await ElMessageBox.confirm(
      `移除“${node.name}”后，该服务器将无法继续连接管理平台。`,
      '移除服务器',
      { type: 'warning', confirmButtonText: '确认移除', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  await post(`/nodes/${node.id}/revoke`, {})
  ElMessage.success('已移除')
  if (detailNodeId.value === node.id) detailNodeId.value = ''
  await load(false)
}

onMounted(() => {
  load(false)
  loadPingTasks()
  stopConnections = subscribeConnections(() => {})
  window.addEventListener('popstate', onPopStateFilter)
  stopLive = subscribeLive(() => load(true).catch(() => {}))
  if (!isShareMode) {
    pendingTimer = window.setInterval(() => {
      if (isAdmin.value) {
        api('/registrations')
          .then((res) => {
            const regs = res.registrations || []
            pending.value = regs
            checkNewPending(regs)
          })
          .catch(() => {})
      }
    }, 10000)
  }
})

onUnmounted(() => {
  if (pendingTimer) window.clearInterval(pendingTimer)
  stopLive?.()
  stopConnections?.()
  window.removeEventListener('popstate', onPopStateFilter)
})
</script>

<style scoped>
.m-node-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px;
  background: var(--sb-surface);
  border: 1px solid var(--sb-line);
  border-radius: 14px;
  cursor: pointer;
}
.m-node-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.m-node-hero {
  display: flex;
  align-items: center;
  gap: 8px;
}
.m-node-os {
  width: 20px;
  height: 20px;
  flex: none;
}
.m-node-title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--sb-text);
}
.m-node-sub {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 12px;
  color: var(--sb-text-2);
}
.m-node-meters {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}
.m-node-meter {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.m-node-meter-head {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: var(--sb-muted);
}
.m-pending-item {
  border: 1px solid rgba(245, 158, 11, 0.35);
  background: rgba(245, 158, 11, 0.04);
}
.m-pending-tag {
  font-size: 10px;
  font-weight: 600;
  padding: 2px 6px;
  border-radius: 999px;
  background: rgba(245, 158, 11, 0.16);
  color: var(--sb-warning);
}
.m-pending-specs {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
}
.m-spec-chip {
  display: inline-flex;
  align-items: center;
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 6px;
  background: var(--sb-surface-2);
  color: var(--sb-text-2);
}
.m-pending-approve {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  padding: 0 12px;
  border: none;
  border-left: 1px solid var(--sb-line);
  background: rgba(16, 185, 129, 0.08);
  color: var(--sb-success);
  cursor: pointer;
}
.command {
  margin: 0;
  padding: 10px 12px;
  border-radius: 10px;
  background: var(--sb-surface-2);
  border: 1px solid var(--sb-line);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
