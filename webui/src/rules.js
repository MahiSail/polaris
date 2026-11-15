// webui/src/rules.js
export function splitRuleFields(line) {
  const fields = []
  let start = 0
  let depth = 0
  for (let i = 0; i < line.length; i++) {
    const char = line[i]
    if (char === '(') {
      depth++
    } else if (char === ')') {
      depth--
      if (depth < 0) {
        throw new Error('括号不匹配')
      }
    } else if (char === ',' && depth === 0) {
      fields.push(line.slice(start, i).trim())
      start = i + 1
    }
  }
  if (depth !== 0) {
    throw new Error('括号不匹配')
  }
  fields.push(line.slice(start).trim())
  return fields
}

export function parseRawRules(raw) {
  if (!raw || typeof raw !== 'string') return []
  const lines = raw.replace(/\r\n/g, '\n').split('\n')
  const rules = []
  for (let i = 0; i < lines.length; i++) {
    let line = lines[i].trim()
    if (!line || line.startsWith('#')) {
      continue
    }
    if (line.startsWith('-')) {
      line = line.slice(1).trim()
    }
    let parts
    try {
      parts = splitRuleFields(line)
    } catch (e) {
      throw new Error(`第 ${i + 1} 行规则格式错误: ${e.message}`)
    }
    if (parts.length === 2 && parts[0].toUpperCase() === 'MATCH') {
      rules.push({
        type: 'MATCH',
        value: '',
        action: parts[1],
        no_resolve: false,
      })
    } else if (parts.length >= 3) {
      let noResolve = false
      for (const opt of parts.slice(3)) {
        if (opt.toLowerCase() === 'no-resolve') {
          noResolve = true
        } else if (opt) {
          throw new Error(`第 ${i + 1} 行存在不支持的规则选项 "${opt}"`)
        }
      }
      rules.push({
        type: parts[0].toUpperCase(),
        value: parts[1],
        action: parts[2],
        no_resolve: noResolve,
      })
    } else {
      throw new Error(`第 ${i + 1} 行规则不完整: "${line}"`)
    }
  }
  return rules
}

export function formatRule(rule) {
  if (!rule) return ''
  const type = (rule.type || '').trim().toUpperCase()
  const action = (rule.action || '').trim()
  if (type === 'MATCH') {
    return `MATCH,${action}`
  }
  const value = (rule.value || '').trim()
  const parts = [type, value, action]
  if (rule.no_resolve) {
    parts.push('no-resolve')
  }
  return parts.join(',')
}

export function formatRules(rules) {
  if (!Array.isArray(rules) || rules.length === 0) return ''
  return rules.map((r) => formatRule(r)).filter(Boolean).join('\n')
}
