const UNIT_MULT = {
  B: 1,
  KB: 1024,
  K: 1024,
  MB: 1024 ** 2,
  M: 1024 ** 2,
  GB: 1024 ** 3,
  G: 1024 ** 3,
  TB: 1024 ** 4,
  T: 1024 ** 4,
}

export function countryCodeToFlag(code) {
  if (!code || typeof code !== 'string' || !/^[A-Za-z]{2}$/.test(code.trim())) return ''
  const offset = 127397
  return String.fromCodePoint(
    ...[...code.trim().toUpperCase()].map((ch) => ch.charCodeAt(0) + offset)
  )
}

export function nodeFlag(node) {
  if (!node) return '🌐'
  const byCode = countryCodeToFlag(node.country || node.country_code)
  if (byCode) return byCode
  const name = String(node.name || '')
  const leading = name.match(/^([\u{1F1E6}-\u{1F1FF}]{2})/u)
  if (leading) return leading[1]
  const upper = name.toUpperCase()
  if (upper.includes('HK') || upper.includes('香港')) return '🇭🇰'
  if (upper.includes('TW') || upper.includes('台湾')) return '🇹🇼'
  if (upper.includes('JP') || upper.includes('日本')) return '🇯🇵'
  if (upper.includes('SG') || upper.includes('新加坡')) return '🇸🇬'
  if (upper.includes('US') || upper.includes('美国')) return '🇺🇸'
  if (upper.includes('KR') || upper.includes('韩国')) return '🇰🇷'
  if (upper.includes('GB') || upper.includes('UK') || upper.includes('英国')) return '🇬🇧'
  if (upper.includes('DE') || upper.includes('德国')) return '🇩🇪'
  if (upper.includes('FR') || upper.includes('法国')) return '🇫🇷'
  if (upper.includes('CA') || upper.includes('加拿大')) return '🇨🇦'
  if (upper.includes('AU') || upper.includes('澳洲')) return '🇦🇺'
  return '🌐'
}

export function nodeLocationLabel(node) {
  if (!node) return ''
  if (node.city && node.country_code) return `${node.country_code} · ${node.city}`
  if (node.location) return node.location
  if (node.city) return node.city
  if (node.country_code) return node.country_code
  return ''
}

export function parseTrafficPlan(raw) {
  if (!raw) return { traffic_value: '', traffic_unit: 'GB' }
  const m = String(raw).trim().match(/^(\d+(?:\.\d+)?)\s*(MB|GB|TB)$/i)
  if (!m) return { traffic_value: String(raw).replace(/[^0-9.]/g, ''), traffic_unit: 'GB' }
  return { traffic_value: m[1], traffic_unit: m[2].toUpperCase() }
}

export function parseBandwidthPlan(raw) {
  if (!raw) return { bandwidth_value: '', bandwidth_unit: 'Gbps' }
  const m = String(raw).trim().match(/^(\d+(?:\.\d+)?)\s*(Mbps|Gbps|M|G)$/i)
  if (!m) {
    return {
      bandwidth_value: String(raw).replace(/[^0-9.]/g, ''),
      bandwidth_unit: 'Gbps',
    }
  }
  const unit = m[2].toUpperCase().startsWith('G') ? 'Gbps' : 'Mbps'
  return { bandwidth_value: m[1], bandwidth_unit: unit }
}

export function parseTrafficQuotaBytes(raw) {
  if (!raw) return 0
  const m = String(raw).trim().match(/^(\d+(?:\.\d+)?)\s*(B|KB|K|MB|M|GB|G|TB|T)?$/i)
  if (!m) return 0
  const num = Number(m[1])
  if (!Number.isFinite(num) || num <= 0) return 0
  const unit = (m[2] || 'GB').toUpperCase()
  return Math.round(num * (UNIT_MULT[unit] || 1024 ** 3))
}

export function hasTrafficFields(map) {
  return Boolean(
    map && (map.monthly_received_bytes !== undefined || map.received_bytes !== undefined)
  )
}

export function nodeTrafficStats(node, report) {
  const hostMetrics = hasTrafficFields(report?.node) ? report.node : null
  const proxyMetrics = hasTrafficFields(report?.proxy) ? report.proxy : null
  const effective = hostMetrics || proxyMetrics || {}

  const monthlyRx = Number(effective.monthly_received_bytes || 0)
  const monthlyTx = Number(effective.monthly_sent_bytes || 0)
  const monthlyTotal = monthlyRx + monthlyTx

  const lifetimeRx = Number(effective.received_bytes || 0)
  const lifetimeTx = Number(effective.sent_bytes || 0)
  const lifetimeTotal = lifetimeRx + lifetimeTx

  const proxyMonthlyRx = Number(proxyMetrics?.monthly_received_bytes || 0)
  const proxyMonthlyTx = Number(proxyMetrics?.monthly_sent_bytes || 0)
  const proxyMonthlyTotal = proxyMonthlyRx + proxyMonthlyTx

  const quotaBytes = parseTrafficQuotaBytes(node?.traffic_plan)
  const quotaPercent =
    quotaBytes > 0 ? Math.min(100, Math.round((monthlyTotal / quotaBytes) * 1000) / 10) : null

  return {
    monthlyRx,
    monthlyTx,
    monthlyTotal,
    lifetimeRx,
    lifetimeTx,
    lifetimeTotal,
    proxyMonthlyRx,
    proxyMonthlyTx,
    proxyMonthlyTotal,
    quotaBytes,
    quotaPercent,
    usingProxyFallback: !hostMetrics && Boolean(proxyMetrics),
  }
}

export function nodeExpiryInfo(node) {
  if (!node) return null
  if (
    node.price_period === 'free' ||
    node.expires_on === 'permanent' ||
    node.expires_on === '2035-01-01' ||
    node.price === '0.00' ||
    node.price === '0'
  ) {
    return { label: '永久免费', tone: 'success', daysLeft: Infinity }
  }
  if (!node.expires_on) return null
  const target = new Date(`${node.expires_on}T23:59:59`)
  if (Number.isNaN(target.getTime())) {
    return { label: node.expires_on, tone: 'info', daysLeft: null }
  }
  const now = new Date()
  const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const targetDay = new Date(target.getFullYear(), target.getMonth(), target.getDate()).getTime()
  const diffDays = Math.round((targetDay - todayStart) / 86400000)
  if (diffDays < 0) return { label: `已到期 (${Math.abs(diffDays)}天)`, tone: 'danger', daysLeft: diffDays }
  if (diffDays === 0) return { label: '今日到期', tone: 'danger', daysLeft: 0 }
  if (diffDays <= 7) return { label: `剩余 ${diffDays} 天`, tone: 'warning', daysLeft: diffDays }
  return { label: `剩余 ${diffDays} 天`, tone: 'info', daysLeft: diffDays }
}
