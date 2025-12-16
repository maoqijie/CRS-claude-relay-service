const CODEX_CLI_UA_REGEX = /^(codex_vscode|codex_cli_rs)\/([\d.]+)/i

function normalizeHeaders(headers = {}) {
  if (!headers || typeof headers !== 'object') {
    return {}
  }

  const normalized = {}
  for (const [key, value] of Object.entries(headers)) {
    if (!key) {
      continue
    }
    normalized[key.toLowerCase()] = Array.isArray(value) ? value[0] : value
  }
  return normalized
}

function toHeaderString(value) {
  if (value === undefined || value === null) {
    return null
  }
  if (typeof value === 'string') {
    return value
  }
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value)
  }
  return null
}

/**
 * 比较版本号
 * @returns {number} -1: v1 < v2, 0: v1 = v2, 1: v1 > v2
 */
function compareVersions(v1, v2) {
  if (typeof v1 !== 'string' || typeof v2 !== 'string') {
    return 0
  }

  const parts1 = v1.split('.').map(Number)
  const parts2 = v2.split('.').map(Number)

  for (let i = 0; i < Math.max(parts1.length, parts2.length); i++) {
    const part1 = parts1[i] || 0
    const part2 = parts2[i] || 0

    if (part1 < part2) {
      return -1
    }
    if (part1 > part2) {
      return 1
    }
  }

  return 0
}

function parseCodexCliUserAgent(userAgent) {
  if (typeof userAgent !== 'string') {
    return null
  }

  const match = userAgent.match(CODEX_CLI_UA_REGEX)
  if (!match) {
    return null
  }

  return {
    client: String(match[1] || '').toLowerCase(),
    version: String(match[2] || '')
  }
}

function isLegacyCodexCliUserAgent(userAgent, minimumVersion = '0.70.0') {
  const info = parseCodexCliUserAgent(userAgent)
  if (!info || !info.version) {
    return false
  }
  return compareVersions(info.version, minimumVersion) < 0
}

/**
 * 仅构造 Codex 上游需要的受控请求头（兼容旧版 Codex CLI 的 header 命名）
 */
function buildCodexUpstreamHeaders(rawHeaders = {}) {
  const incoming = normalizeHeaders(rawHeaders)
  const headers = {}

  const openaiBeta = toHeaderString(incoming['openai-beta'])
  if (openaiBeta) {
    headers['openai-beta'] = openaiBeta
  }

  const originator = toHeaderString(incoming['originator'])
  if (originator) {
    headers['originator'] = originator
  }

  const openaiVersion = toHeaderString(incoming['openai-version'])
  if (openaiVersion) {
    headers['openai-version'] = openaiVersion
  }

  const version = toHeaderString(incoming['version'] ?? incoming['openai-version'])
  if (version) {
    headers['version'] = version
  }

  const sessionId = toHeaderString(
    incoming['session_id'] ?? incoming['x-session-id'] ?? incoming['session-id']
  )
  if (sessionId) {
    headers['session_id'] = sessionId
  }

  return headers
}

module.exports = {
  buildCodexUpstreamHeaders,
  compareVersions,
  isLegacyCodexCliUserAgent,
  parseCodexCliUserAgent
}
