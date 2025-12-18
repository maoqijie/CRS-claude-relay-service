package redis

import "time"

// Key 前缀常量 - 与 Node.js 保持完全一致
const (
	// API Key 相关
	PrefixAPIKey        = "apikey:"
	PrefixAPIKeyHashMap = "apikey:hash_map"
	PrefixAPIKeyLegacy  = "api_key:" // 历史兼容

	// 使用统计
	PrefixUsage        = "usage:"
	PrefixUsageDaily   = "usage:daily:"
	PrefixUsageMonthly = "usage:monthly:"
	PrefixUsageHourly  = "usage:hourly:"
	PrefixUsageModel   = "usage:model:"

	// 账户使用统计
	PrefixAccountUsage = "account_usage:"

	// 账户数据
	PrefixClaudeAccount          = "claude:account:"
	PrefixClaudeConsoleAccount   = "claude_console:account:"
	PrefixDroidAccount           = "droid:account:"
	PrefixOpenAIAccount          = "openai:account:"
	PrefixOpenAIResponsesAccount = "openai_responses:account:"
	PrefixGeminiAccount          = "gemini:account:"
	PrefixGeminiAPIAccount       = "gemini_api:account:"
	PrefixBedrockAccount         = "bedrock:account:"
	PrefixAzureOpenAIAccount     = "azure_openai:account:"
	PrefixCCRAccount             = "ccr:account:"

	// 并发控制
	PrefixConcurrency = "concurrency:"

	// 并发请求排队
	PrefixConcurrencyQueue      = "concurrency:queue:"
	PrefixConcurrencyQueueStats = "concurrency:queue:stats:"
	PrefixConcurrencyQueueWait  = "concurrency:queue:wait_times:"

	// 用户消息队列锁
	PrefixUserMsgLock = "user_msg_queue_lock:"
	PrefixUserMsgLast = "user_msg_queue_last:"

	// 会话
	PrefixSession       = "session:"
	PrefixStickySession = "sticky_session:"
	PrefixOAuthSession  = "oauth_session:"

	// 系统
	PrefixSystemMetrics = "system:metrics:minute:"
)

// TTL 常量
const (
	TTLAPIKey          = 365 * 24 * time.Hour // 1年
	TTLUsageDaily      = 32 * 24 * time.Hour  // 32天
	TTLUsageMonthly    = 365 * 24 * time.Hour // 1年
	TTLUsageHourly     = 7 * 24 * time.Hour   // 7天
	TTLQueueStats      = 7 * 24 * time.Hour   // 7天
	TTLWaitTimeSamples = 24 * time.Hour       // 1天
	TTLQueueBuffer     = 30 * time.Second     // 排队缓冲

	TTLSessionDefault = 24 * time.Hour   // 默认会话 TTL
	TTLOAuthSession   = 10 * time.Minute // OAuth 会话
)

// 采样数配置
const (
	WaitTimeSamplesPerKey = 500  // 每 API Key 等待时间样本数
	WaitTimeSamplesGlobal = 2000 // 全局等待时间样本数
)
