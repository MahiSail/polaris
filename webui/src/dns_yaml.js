import YAML from 'yaml'

function defaultDNS() {
  return {
    enable: true,
    ipv6: false,
    enhanced_mode: 'fake-ip',
    fake_ip_range: '198.18.0.1/16',
    fake_ip_filter_mode: 'rule',
    fake_ip_filter: ['MATCH,fake-ip'],
    respect_rules: true,
    default_nameserver: ['223.5.5.5', '119.29.29.29'],
    nameserver: ['https://223.5.5.5/dns-query'],
    proxy_server_nameserver: ['https://223.5.5.5/dns-query'],
    direct_nameserver: ['https://223.5.5.5/dns-query'],
    direct_nameserver_follow_policy: false,
  }
}

function cloneDNS(dns) {
  const base = defaultDNS()
  if (!dns) return base
  return {
    enable: Boolean(dns.enable),
    ipv6: Boolean(dns.ipv6),
    enhanced_mode: dns.enhanced_mode || base.enhanced_mode,
    fake_ip_range: dns.fake_ip_range || base.fake_ip_range,
    fake_ip_filter_mode: dns.fake_ip_filter_mode || base.fake_ip_filter_mode,
    fake_ip_filter: [...(dns.fake_ip_filter || [])],
    respect_rules: Boolean(dns.respect_rules),
    default_nameserver: [...(dns.default_nameserver || [])],
    nameserver: [...(dns.nameserver || [])],
    proxy_server_nameserver: [...(dns.proxy_server_nameserver || [])],
    direct_nameserver: [...(dns.direct_nameserver || [])],
    direct_nameserver_follow_policy: Boolean(dns.direct_nameserver_follow_policy),
  }
}

function toStringArray(val) {
  if (Array.isArray(val)) {
    return val.map((item) => (item === null || item === undefined ? '' : String(item).trim())).filter(Boolean)
  }
  if (typeof val === 'string' && val.trim()) {
    return [val.trim()]
  }
  return []
}

export function parseDNSYAML(text, currentDNS) {
  const base = cloneDNS(currentDNS)
  const trimmed = (text || '').trim()
  if (!trimmed) {
    base.enable = false
    return { success: true, dns: base }
  }

  let parsed
  try {
    parsed = YAML.parse(trimmed)
  } catch (err) {
    return { success: false, error: err.message || 'YAML 解析错误' }
  }

  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return { success: false, error: 'DNS 配置必须是键值映射（Mapping）' }
  }

  const target = (parsed.dns && typeof parsed.dns === 'object' && !Array.isArray(parsed.dns)) ? parsed.dns : parsed
  const getVal = (kebab, snake) => target[kebab] !== undefined ? target[kebab] : target[snake]

  const enableVal = target.enable
  base.enable = enableVal !== undefined ? Boolean(enableVal) : true

  const ipv6Val = getVal('ipv6', 'ipv-6')
  base.ipv6 = ipv6Val !== undefined ? Boolean(ipv6Val) : false

  const enhancedModeVal = getVal('enhanced-mode', 'enhanced_mode')
  if (enhancedModeVal) base.enhanced_mode = String(enhancedModeVal).trim()

  const fakeRangeVal = getVal('fake-ip-range', 'fake_ip_range')
  if (fakeRangeVal) base.fake_ip_range = String(fakeRangeVal).trim()

  const fakeFilterModeVal = getVal('fake-ip-filter-mode', 'fake_ip_filter_mode')
  if (fakeFilterModeVal) base.fake_ip_filter_mode = String(fakeFilterModeVal).trim()

  const fakeFilterVal = getVal('fake-ip-filter', 'fake_ip_filter')
  if (fakeFilterVal !== undefined) base.fake_ip_filter = toStringArray(fakeFilterVal)

  const respectVal = getVal('respect-rules', 'respect_rules')
  base.respect_rules = respectVal !== undefined ? Boolean(respectVal) : true

  const defaultNsVal = getVal('default-nameserver', 'default_nameserver')
  if (defaultNsVal !== undefined) base.default_nameserver = toStringArray(defaultNsVal)

  const nsVal = target.nameserver
  if (nsVal !== undefined) base.nameserver = toStringArray(nsVal)

  const proxyNsVal = getVal('proxy-server-nameserver', 'proxy_server_nameserver')
  if (proxyNsVal !== undefined) base.proxy_server_nameserver = toStringArray(proxyNsVal)

  const directNsVal = getVal('direct-nameserver', 'direct_nameserver')
  if (directNsVal !== undefined) base.direct_nameserver = toStringArray(directNsVal)

  const policyVal = getVal('direct-nameserver-follow-policy', 'direct_nameserver_follow_policy')
  base.direct_nameserver_follow_policy = policyVal !== undefined ? Boolean(policyVal) : false

  return { success: true, dns: base }
}

export function renderDNSYAML(dns, existingYaml = '') {
  if (!dns) return ''

  const buildPlainObj = () => {
    const obj = {
      enable: Boolean(dns.enable),
      ipv6: Boolean(dns.ipv6),
      'enhanced-mode': dns.enhanced_mode || 'fake-ip',
    }
    if (dns.enhanced_mode === 'fake-ip') {
      obj['fake-ip-range'] = dns.fake_ip_range || '198.18.0.1/16'
      obj['fake-ip-filter-mode'] = dns.fake_ip_filter_mode || 'rule'
      if (dns.fake_ip_filter && dns.fake_ip_filter.length) {
        obj['fake-ip-filter'] = [...dns.fake_ip_filter]
      }
    }
    obj['respect-rules'] = Boolean(dns.respect_rules)
    if (dns.default_nameserver && dns.default_nameserver.length) {
      obj['default-nameserver'] = [...dns.default_nameserver]
    }
    if (dns.nameserver && dns.nameserver.length) {
      obj['nameserver'] = [...dns.nameserver]
    }
    if (dns.proxy_server_nameserver && dns.proxy_server_nameserver.length) {
      obj['proxy-server-nameserver'] = [...dns.proxy_server_nameserver]
    }
    if (dns.direct_nameserver && dns.direct_nameserver.length) {
      obj['direct-nameserver'] = [...dns.direct_nameserver]
    }
    obj['direct-nameserver-follow-policy'] = Boolean(dns.direct_nameserver_follow_policy)
    return obj
  }

  const trimmed = (existingYaml || '').trim()
  if (!trimmed) {
    return YAML.stringify(buildPlainObj()).trim()
  }

  try {
    const doc = YAML.parseDocument(trimmed)
    if (!doc || !doc.contents) {
      return YAML.stringify(buildPlainObj()).trim()
    }

    let root = doc.contents
    if (root && root.get && root.has && root.has('dns')) {
      const nested = root.get('dns')
      if (nested && typeof nested === 'object' && !Array.isArray(nested)) {
        root = nested
      }
    }

    if (!root || typeof root.set !== 'function') {
      return YAML.stringify(buildPlainObj()).trim()
    }

    root.set('enable', Boolean(dns.enable))
    root.set('ipv6', Boolean(dns.ipv6))
    root.set('enhanced-mode', dns.enhanced_mode || 'fake-ip')
    if (dns.enhanced_mode === 'fake-ip') {
      root.set('fake-ip-range', dns.fake_ip_range || '198.18.0.1/16')
      root.set('fake-ip-filter-mode', dns.fake_ip_filter_mode || 'rule')
      if (dns.fake_ip_filter && dns.fake_ip_filter.length) {
        root.set('fake-ip-filter', [...dns.fake_ip_filter])
      } else {
        root.delete('fake-ip-filter')
      }
    } else {
      root.delete('fake-ip-range')
      root.delete('fake-ip-filter-mode')
      root.delete('fake-ip-filter')
    }
    root.set('respect-rules', Boolean(dns.respect_rules))
    if (dns.default_nameserver && dns.default_nameserver.length) {
      root.set('default-nameserver', [...dns.default_nameserver])
    } else {
      root.delete('default-nameserver')
    }
    if (dns.nameserver && dns.nameserver.length) {
      root.set('nameserver', [...dns.nameserver])
    } else {
      root.delete('nameserver')
    }
    if (dns.proxy_server_nameserver && dns.proxy_server_nameserver.length) {
      root.set('proxy-server-nameserver', [...dns.proxy_server_nameserver])
    } else {
      root.delete('proxy-server-nameserver')
    }
    if (dns.direct_nameserver && dns.direct_nameserver.length) {
      root.set('direct-nameserver', [...dns.direct_nameserver])
    } else {
      root.delete('direct-nameserver')
    }
    root.set('direct-nameserver-follow-policy', Boolean(dns.direct_nameserver_follow_policy))

    return doc.toString().trim()
  } catch {
    return YAML.stringify(buildPlainObj()).trim()
  }
}

export { defaultDNS, cloneDNS }
