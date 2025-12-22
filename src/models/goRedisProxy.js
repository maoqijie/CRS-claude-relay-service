/**
 * Go Redis 代理客户端
 * 通过 HTTP 调用 Go 服务的 Redis API
 */

const http = require('http')
const logger = require('../utils/logger')

class GoRedisProxy {
  constructor() {
    this.baseUrl = process.env.GO_SERVICE_URL || 'http://127.0.0.1:8081'
    this.timeout = parseInt(process.env.GO_SERVICE_TIMEOUT) || 30000
    this.enabled = process.env.GO_REDIS_PROXY_ENABLED === 'true'
    this.isHealthy = false
    this.lastHealthCheck = 0
    this.healthCheckInterval = 30000 // 30秒检查一次
  }

  /**
   * 发送 HTTP 请求到 Go 服务
   */
  async request(method, path, data = null) {
    const url = new URL(path, this.baseUrl)

    return new Promise((resolve, reject) => {
      const options = {
        hostname: url.hostname,
        port: url.port || 8081,
        path: url.pathname + url.search,
        method: method,
        timeout: this.timeout,
        headers: {
          'Content-Type': 'application/json',
        }
      }

      const req = http.request(options, (res) => {
        let body = ''
        res.on('data', chunk => body += chunk)
        res.on('end', () => {
          try {
            const result = JSON.parse(body)
            if (res.statusCode >= 200 && res.statusCode < 300) {
              resolve(result)
            } else {
              reject(new Error(result.error || `HTTP ${res.statusCode}`))
            }
          } catch (e) {
            reject(new Error(`Invalid JSON response: ${body}`))
          }
        })
      })

      req.on('error', reject)
      req.on('timeout', () => {
        req.destroy()
        reject(new Error('Request timeout'))
      })

      if (data) {
        req.write(JSON.stringify(data))
      }
      req.end()
    })
  }

  /**
   * 检查 Go 服务健康状态
   */
  async checkHealth() {
    const now = Date.now()
    if (now - this.lastHealthCheck < this.healthCheckInterval) {
      return this.isHealthy
    }

    try {
      const result = await this.request('GET', '/health')
      this.isHealthy = result.status === 'healthy'
      this.lastHealthCheck = now
      return this.isHealthy
    } catch (error) {
      this.isHealthy = false
      this.lastHealthCheck = now
      logger.debug('Go service health check failed', { error: error.message })
      return false
    }
  }

  /**
   * 是否可用
   */
  async isAvailable() {
    if (!this.enabled) return false
    return await this.checkHealth()
  }

  // ==================== API Key 操作 ====================

  async getApiKey(keyId) {
    return await this.request('GET', `/redis/apikeys/${keyId}`)
  }

  async getApiKeyByHash(hash) {
    return await this.request('GET', `/redis/apikeys/hash/${hash}`)
  }

  async getAllApiKeys(includeDeleted = false) {
    const result = await this.request('GET', `/redis/apikeys?includeDeleted=${includeDeleted}`)
    return result.keys || []
  }

  async getApiKeysPaginated(options = {}) {
    const params = new URLSearchParams()
    if (options.page) params.set('page', options.page)
    if (options.pageSize) params.set('pageSize', options.pageSize)
    if (options.sortBy) params.set('sortBy', options.sortBy)
    if (options.order) params.set('order', options.order)
    if (options.search) params.set('search', options.search)
    if (options.status) params.set('status', options.status)
    if (options.excludeDeleted !== undefined) params.set('excludeDeleted', options.excludeDeleted)

    return await this.request('GET', `/redis/apikeys/paginated?${params.toString()}`)
  }

  async setApiKey(keyData) {
    return await this.request('POST', '/redis/apikeys', keyData)
  }

  async updateApiKeyFields(keyId, updates) {
    return await this.request('PUT', `/redis/apikeys/${keyId}`, updates)
  }

  async deleteApiKey(keyId) {
    return await this.request('DELETE', `/redis/apikeys/${keyId}`)
  }

  async getApiKeyStats() {
    return await this.request('GET', '/redis/apikeys/stats')
  }

  // ==================== 成本和使用统计 ====================

  async incrementDailyCost(keyId, amount) {
    return await this.request('POST', `/redis/apikeys/${keyId}/cost/daily`, { amount })
  }

  async getDailyCost(keyId) {
    const result = await this.request('GET', `/redis/apikeys/${keyId}/cost/daily`)
    return result.cost || 0
  }

  async getCostStats(keyId, days = 30) {
    return await this.request('GET', `/redis/apikeys/${keyId}/cost/stats?days=${days}`)
  }

  async incrementTokenUsage(params) {
    return await this.request('POST', '/redis/apikeys/usage', params)
  }

  async getUsageStats(keyId) {
    return await this.request('GET', `/redis/apikeys/${keyId}/usage`)
  }

  // ==================== 并发控制 ====================

  async incrConcurrency(apiKeyId, requestId, leaseSeconds = 600) {
    const result = await this.request('POST', '/redis/concurrency/incr', { apiKeyId, requestId, leaseSeconds })
    return result.count
  }

  async decrConcurrency(apiKeyId, requestId) {
    const result = await this.request('POST', '/redis/concurrency/decr', { apiKeyId, requestId })
    return result.count
  }

  async getConcurrency(apiKeyId) {
    const result = await this.request('GET', `/redis/concurrency/${apiKeyId}`)
    return result.count
  }

  async getConcurrencyStatus(apiKeyId) {
    return await this.request('GET', `/redis/concurrency/${apiKeyId}/status`)
  }

  async getAllConcurrencyStatus() {
    const result = await this.request('GET', '/redis/concurrency/status/all')
    return result.statuses || []
  }

  async refreshConcurrencyLease(apiKeyId, requestId, leaseSeconds = 600) {
    const result = await this.request('POST', '/redis/concurrency/lease/refresh', { apiKeyId, requestId, leaseSeconds })
    return result.refreshed
  }

  async cleanupExpiredConcurrency() {
    return await this.request('POST', '/redis/concurrency/cleanup')
  }

  async forceClearConcurrency(apiKeyId) {
    const result = await this.request('DELETE', `/redis/concurrency/${apiKeyId}/force`)
    return result.cleared
  }

  async forceClearAllConcurrency() {
    return await this.request('DELETE', '/redis/concurrency/force/all')
  }

  // Console 账户并发
  async incrConsoleAccountConcurrency(accountId, requestId, leaseSeconds = 600) {
    const result = await this.request('POST', '/redis/concurrency/console/incr', { accountId, requestId, leaseSeconds })
    return result.count
  }

  async decrConsoleAccountConcurrency(accountId, requestId) {
    const result = await this.request('POST', '/redis/concurrency/console/decr', { accountId, requestId })
    return result.count
  }

  async getConsoleAccountConcurrency(accountId) {
    const result = await this.request('GET', `/redis/concurrency/console/${accountId}`)
    return result.count
  }

  // 并发队列
  async incrConcurrencyQueue(apiKeyId, timeoutMs = 10000) {
    const result = await this.request('POST', '/redis/concurrency/queue/incr', { apiKeyId, timeoutMs })
    return result.count
  }

  async decrConcurrencyQueue(apiKeyId) {
    const result = await this.request('POST', '/redis/concurrency/queue/decr', { apiKeyId })
    return result.count
  }

  async getConcurrencyQueueCount(apiKeyId) {
    const result = await this.request('GET', `/redis/concurrency/queue/${apiKeyId}/count`)
    return result.count
  }

  async clearConcurrencyQueue(apiKeyId) {
    return await this.request('DELETE', `/redis/concurrency/queue/${apiKeyId}`)
  }

  async clearAllConcurrencyQueues() {
    const result = await this.request('DELETE', '/redis/concurrency/queue/all')
    return result.cleared
  }

  async getQueueStats(apiKeyId) {
    return await this.request('GET', `/redis/concurrency/queue/${apiKeyId}/stats`)
  }

  async getGlobalQueueStats(includePerKey = false) {
    return await this.request('GET', `/redis/concurrency/queue/global/stats?includePerKey=${includePerKey}`)
  }

  async checkQueueHealth(threshold = 0.8, timeoutMs = 10000) {
    return await this.request('GET', `/redis/concurrency/queue/health?threshold=${threshold}&timeoutMs=${timeoutMs}`)
  }

  async recordWaitTime(apiKeyId, waitMs) {
    return await this.request('POST', '/redis/concurrency/queue/wait-time', { apiKeyId, waitMs })
  }

  // ==================== 会话管理 ====================

  async setSession(token, session, ttl = 86400) {
    return await this.request('POST', '/redis/sessions', { token, session, ttl })
  }

  async getSession(token) {
    return await this.request('GET', `/redis/sessions/${token}`)
  }

  async deleteSession(token) {
    return await this.request('DELETE', `/redis/sessions/${token}`)
  }

  async refreshSession(token, ttl = 86400) {
    return await this.request('POST', '/redis/sessions/refresh', { token, ttl })
  }

  // OAuth 会话
  async setOAuthSession(state, session) {
    return await this.request('POST', '/redis/sessions/oauth', { state, session })
  }

  async getOAuthSession(state) {
    return await this.request('GET', `/redis/sessions/oauth/${state}`)
  }

  async deleteOAuthSession(state) {
    return await this.request('DELETE', `/redis/sessions/oauth/${state}`)
  }

  // 粘性会话
  async setStickySession(sessionHash, accountId, accountType, ttl = 3600) {
    return await this.request('POST', '/redis/sessions/sticky', { sessionHash, accountId, accountType, ttl })
  }

  async getStickySession(sessionHash) {
    return await this.request('GET', `/redis/sessions/sticky/${sessionHash}`)
  }

  async getOrCreateStickySession(sessionHash, accountId, accountType, ttl = 3600) {
    return await this.request('POST', '/redis/sessions/sticky/get-or-create', { sessionHash, accountId, accountType, ttl })
  }

  async deleteStickySession(sessionHash) {
    return await this.request('DELETE', `/redis/sessions/sticky/${sessionHash}`)
  }

  async renewStickySession(sessionHash, ttl = 3600) {
    return await this.request('POST', '/redis/sessions/sticky/renew', { sessionHash, ttl })
  }

  async getAllStickySessions() {
    const result = await this.request('GET', '/redis/sessions/sticky/all')
    return result.sessions || []
  }

  async cleanupExpiredStickySessions() {
    const result = await this.request('POST', '/redis/sessions/sticky/cleanup')
    return result.cleaned
  }

  // ==================== 账户管理 ====================

  async getAccount(type, id) {
    return await this.request('GET', `/redis/accounts/${type}/${id}`)
  }

  async getAllAccounts(type) {
    const result = await this.request('GET', `/redis/accounts/${type}`)
    return result.accounts || []
  }

  async getActiveAccounts(type) {
    const result = await this.request('GET', `/redis/accounts/${type}/active`)
    return result.accounts || []
  }

  async setAccount(type, id, data) {
    return await this.request('POST', `/redis/accounts/${type}/${id}`, data)
  }

  async deleteAccount(type, id) {
    return await this.request('DELETE', `/redis/accounts/${type}/${id}`)
  }

  async updateAccountStatus(type, id, status) {
    return await this.request('PUT', `/redis/accounts/${type}/${id}/status`, { status })
  }

  async setAccountError(type, id, errorMsg) {
    return await this.request('POST', `/redis/accounts/${type}/${id}/error`, { errorMsg })
  }

  async clearAccountError(type, id) {
    return await this.request('DELETE', `/redis/accounts/${type}/${id}/error`)
  }

  async setAccountOverloaded(type, id, duration = 300) {
    return await this.request('POST', `/redis/accounts/${type}/${id}/overloaded`, { duration })
  }

  async clearAccountOverloaded(type, id) {
    return await this.request('DELETE', `/redis/accounts/${type}/${id}/overloaded`)
  }

  async getAccountCost(id) {
    const result = await this.request('GET', `/redis/account-cost/${id}`)
    return result.cost || 0
  }

  async getAccountDailyCost(id, date) {
    const params = date ? `?date=${date}` : ''
    const result = await this.request('GET', `/redis/account-cost/${id}/daily${params}`)
    return result.cost || 0
  }

  async incrementAccountCost(id, amount) {
    return await this.request('POST', `/redis/account-cost/${id}`, { amount })
  }

  async incrementAccountUsage(params) {
    return await this.request('POST', '/redis/accounts/usage', params)
  }

  async getSessionWindowUsage(id, windowHours = 1) {
    return await this.request('GET', `/redis/account-cost/${id}/usage/window?windowHours=${windowHours}`)
  }

  async setAccountLock(lockKey, lockValue, ttl = 30) {
    const result = await this.request('POST', '/redis/accounts/lock', { lockKey, lockValue, ttl })
    return result.acquired
  }

  async releaseAccountLock(lockKey, lockValue) {
    const result = await this.request('POST', '/redis/accounts/lock/release', { lockKey, lockValue })
    return result.released
  }

  // ==================== 锁管理 ====================

  async acquireLock(lockKey, ttl = 30000) {
    return await this.request('POST', '/redis/locks/acquire', { lockKey, ttl })
  }

  async releaseLock(lockKey, token) {
    const result = await this.request('POST', '/redis/locks/release', { lockKey, token })
    return result.released
  }

  async extendLock(lockKey, token, ttl = 30000) {
    const result = await this.request('POST', '/redis/locks/extend', { lockKey, token, ttl })
    return result.extended
  }

  async acquireUserMessageLock(accountId, requestId, lockTTLMs = 5000, delayMs = 200) {
    return await this.request('POST', '/redis/locks/user-message/acquire', { accountId, requestId, lockTTLMs, delayMs })
  }

  async releaseUserMessageLock(accountId, requestId) {
    const result = await this.request('POST', '/redis/locks/user-message/release', { accountId, requestId })
    return result.released
  }

  async forceReleaseUserMessageLock(accountId) {
    const result = await this.request('DELETE', `/redis/locks/user-message/${accountId}/force`)
    return result.released
  }

  async getUserMessageQueueStats(accountId) {
    return await this.request('GET', `/redis/locks/user-message/${accountId}/stats`)
  }

  // ==================== 通用操作 ====================

  async get(key) {
    const result = await this.request('GET', `/redis/generic/get/${key}`)
    return result.value
  }

  async set(key, value, expiration = 0) {
    return await this.request('POST', '/redis/generic/set', { key, value, expiration })
  }

  async del(...keys) {
    const result = await this.request('POST', '/redis/generic/del', { keys })
    return result.deleted
  }

  async scanKeys(pattern = '*', count = 1000) {
    const result = await this.request('GET', `/redis/generic/scan?pattern=${encodeURIComponent(pattern)}&count=${count}`)
    return result.keys || []
  }

  async hgetall(key) {
    const result = await this.request('GET', `/redis/generic/hgetall/${key}`)
    return result.values || {}
  }

  async hset(key, values) {
    return await this.request('POST', '/redis/generic/hset', { key, values })
  }

  async dbSize() {
    const result = await this.request('GET', '/redis/generic/dbsize')
    return result.size
  }

  async info() {
    const result = await this.request('GET', '/redis/generic/info')
    return result.info
  }

  async getAllUsedModels() {
    const result = await this.request('GET', '/redis/generic/models')
    return result.models || []
  }
}

module.exports = new GoRedisProxy()
