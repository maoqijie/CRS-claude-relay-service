// Mock logger to avoid console output during tests
jest.mock('../src/utils/logger', () => ({
  api: jest.fn(),
  warn: jest.fn(),
  error: jest.fn(),
  info: jest.fn(),
  database: jest.fn(),
  security: jest.fn(),
  debug: jest.fn(),
  success: jest.fn()
}))

jest.mock('../src/models/redis', () => ({
  incrementTokenUsage: jest.fn(),
  incrementDailyCost: jest.fn(),
  addUsageRecord: jest.fn(),
  getApiKey: jest.fn(),
  setApiKey: jest.fn(),
  updateApiKeyFields: jest.fn(),
  incrementAccountUsage: jest.fn()
}))

jest.mock('../src/utils/costCalculator', () => ({
  calculateCost: jest.fn()
}))

jest.mock('../src/services/fuelPackService', () => ({
  consumeFuel: jest.fn(),
  refreshWallet: jest.fn()
}))

jest.mock('../src/services/pricingService', () => ({
  pricingData: { initialized: true },
  initialize: jest.fn(),
  calculateCost: jest.fn()
}))

jest.mock('../src/services/billingEventPublisher', () => ({
  publishBillingEvent: jest.fn()
}))

const apiKeyService = require('../src/services/apiKeyService')
const redis = require('../src/models/redis')
const CostCalculator = require('../src/utils/costCalculator')
const fuelPackService = require('../src/services/fuelPackService')
const pricingService = require('../src/services/pricingService')

describe('Fuel pack usage recording', () => {
  beforeEach(() => {
    jest.clearAllMocks()

    redis.incrementTokenUsage.mockResolvedValue(undefined)
    redis.incrementDailyCost.mockResolvedValue(undefined)
    redis.addUsageRecord.mockResolvedValue(undefined)
    redis.updateApiKeyFields.mockResolvedValue(true)
    redis.incrementAccountUsage.mockResolvedValue(undefined)

    fuelPackService.refreshWallet.mockResolvedValue({
      fuelUsed: 0,
      fuelBalance: 10,
      fuelNextExpiresAtMs: Date.now() + 60_000,
      fuelEntries: 1
    })

    fuelPackService.consumeFuel.mockResolvedValue({
      fuelUsed: 1,
      fuelBalance: 9,
      fuelNextExpiresAtMs: Date.now() + 60_000,
      fuelEntries: 1
    })
  })

  it('recordUsage updates lastUsedAt without rewriting full api key data', async () => {
    const now = Date.now()
    redis.getApiKey.mockResolvedValue({
      id: 'key-1',
      apiKey: 'hashed-key',
      fuelBalance: '10',
      fuelNextExpiresAtMs: String(now + 60_000)
    })

    CostCalculator.calculateCost.mockReturnValue({ costs: { total: 5 } })

    const result = await apiKeyService.recordUsage('key-1', 100, 0, 0, 0, 'gpt-test', null)

    expect(fuelPackService.consumeFuel).toHaveBeenCalledWith('key-1', 5)
    expect(redis.updateApiKeyFields).toHaveBeenCalledWith(
      'key-1',
      expect.objectContaining({ lastUsedAt: expect.any(String) })
    )
    expect(redis.setApiKey).not.toHaveBeenCalled()
    expect(result).toEqual({
      totalTokens: 100,
      totalCost: 5,
      fuelCost: 1,
      billableCost: 4
    })
  })

  it('recordUsageWithDetails updates lastUsedAt without rewriting full api key data', async () => {
    const now = Date.now()
    redis.getApiKey.mockResolvedValue({
      id: 'key-2',
      apiKey: 'hashed-key',
      fuelBalance: '10',
      fuelNextExpiresAtMs: String(now + 60_000)
    })

    pricingService.calculateCost.mockReturnValue({
      totalCost: 5,
      inputCost: 5,
      outputCost: 0,
      cacheCreateCost: 0,
      cacheReadCost: 0,
      ephemeral5mCost: 0,
      ephemeral1hCost: 0,
      isLongContextRequest: false
    })

    const result = await apiKeyService.recordUsageWithDetails(
      'key-2',
      { input_tokens: 100, output_tokens: 0, cache_creation_input_tokens: 0, cache_read_input_tokens: 0 },
      'gpt-test',
      null,
      null
    )

    expect(fuelPackService.consumeFuel).toHaveBeenCalledWith('key-2', 5)
    expect(redis.updateApiKeyFields).toHaveBeenCalledWith(
      'key-2',
      expect.objectContaining({ lastUsedAt: expect.any(String) })
    )
    expect(redis.setApiKey).not.toHaveBeenCalled()
    expect(result).toEqual({
      totalTokens: 100,
      totalCost: 5,
      fuelCost: 1,
      billableCost: 4
    })
  })
})

