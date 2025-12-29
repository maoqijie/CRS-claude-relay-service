function isModelPassthroughEnabled(apiKeyData) {
  if (!apiKeyData || typeof apiKeyData !== 'object') {
    return false
  }

  const value = apiKeyData.enableModelPassthrough
  if (value === true) {
    return true
  }
  if (typeof value === 'string') {
    return value === 'true'
  }
  return false
}

function getModelOverride(apiKeyData, requestedModel) {
  if (isModelPassthroughEnabled(apiKeyData)) {
    return null
  }

  if (typeof requestedModel !== 'string') {
    return null
  }

  const trimmed = requestedModel.trim()
  return trimmed ? trimmed : null
}

function rewriteAnthropicSseLineModel(line, modelOverride) {
  if (!modelOverride || typeof line !== 'string') {
    return line
  }

  const match = line.match(/^data:\s*/)
  if (!match) {
    return line
  }

  const prefix = match[0]
  const raw = line.slice(prefix.length)
  const jsonStr = raw.trimStart()
  if (!jsonStr || jsonStr === '[DONE]') {
    return line
  }

  try {
    const data = JSON.parse(jsonStr)
    if (!data || typeof data !== 'object') {
      return line
    }

    let changed = false
    if (
      data.message &&
      typeof data.message === 'object' &&
      typeof data.message.model === 'string'
    ) {
      data.message.model = modelOverride
      changed = true
    }

    if (typeof data.model === 'string') {
      data.model = modelOverride
      changed = true
    }

    if (!changed) {
      return line
    }

    return `${prefix}${JSON.stringify(data)}`
  } catch {
    return line
  }
}

module.exports = {
  isModelPassthroughEnabled,
  getModelOverride,
  rewriteAnthropicSseLineModel
}
