package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/catstream/claude-relay-go/internal/pkg/logger"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
	"go.uber.org/zap"
)

// AccountType 账户类型
type AccountType string

const (
	// Claude 账户类型
	AccountTypeClaude        AccountType = "claude"
	AccountTypeClaudeOfficial AccountType = "claude-official"
	AccountTypeClaudeConsole AccountType = "claude-console"
	AccountTypeBedrock       AccountType = "bedrock"
	AccountTypeCCR           AccountType = "ccr"

	// Gemini 账户类型
	AccountTypeGemini    AccountType = "gemini"
	AccountTypeGeminiAPI AccountType = "gemini-api"

	// OpenAI 账户类型
	AccountTypeOpenAI          AccountType = "openai"
	AccountTypeOpenAIResponses AccountType = "openai-responses"
	AccountTypeAzureOpenAI     AccountType = "azure-openai"

	// Droid 账户类型
	AccountTypeDroid AccountType = "droid"
)

// AccountCategory 账户类别
type AccountCategory string

const (
	CategoryClaude AccountCategory = "claude"
	CategoryGemini AccountCategory = "gemini"
	CategoryOpenAI AccountCategory = "openai"
	CategoryDroid  AccountCategory = "droid"
)

// AccountTypeToCategory 账户类型到类别的映射
var AccountTypeToCategory = map[AccountType]AccountCategory{
	AccountTypeClaude:          CategoryClaude,
	AccountTypeClaudeOfficial:  CategoryClaude,
	AccountTypeClaudeConsole:   CategoryClaude,
	AccountTypeBedrock:         CategoryClaude,
	AccountTypeCCR:             CategoryClaude,
	AccountTypeGemini:          CategoryGemini,
	AccountTypeGeminiAPI:       CategoryGemini,
	AccountTypeOpenAI:          CategoryOpenAI,
	AccountTypeOpenAIResponses: CategoryOpenAI,
	AccountTypeAzureOpenAI:     CategoryOpenAI,
	AccountTypeDroid:           CategoryDroid,
}

// AccountTypePriority 账户类型优先级（数值越高优先级越高）
var AccountTypePriority = map[AccountType]int{
	AccountTypeClaude:          100,
	AccountTypeClaudeOfficial:  100,
	AccountTypeClaudeConsole:   90,
	AccountTypeBedrock:         80,
	AccountTypeCCR:             70,
	AccountTypeGemini:          100,
	AccountTypeGeminiAPI:       90,
	AccountTypeOpenAI:          100,
	AccountTypeOpenAIResponses: 90,
	AccountTypeAzureOpenAI:     80,
	AccountTypeDroid:           100,
}

// SelectOptions 账户选择选项
type SelectOptions struct {
	Model                 string        // 请求的模型
	SessionHash           string        // 会话哈希（用于粘性会话）
	APIKeyID              string        // API Key ID
	Permissions           []string      // 允许的权限
	PreferredAccountTypes []AccountType // 优先选择的账户类型
	ExcludeAccountIDs     []string      // 排除的账户 ID
	RequireFeatures       []string      // 需要的功能（如 thinking、vision 等）
}

// SelectResult 账户选择结果
type SelectResult struct {
	Account     map[string]interface{}
	AccountType AccountType
	AccountID   string
	FromSession bool
	Error       error
}

// AccountCandidate 候选账户
type AccountCandidate struct {
	Account     map[string]interface{}
	AccountType AccountType
	AccountID   string
	Priority    int
	Load        float64
	Features    []string
}

// BaseScheduler 基础调度器
type BaseScheduler struct {
	redis                *redis.Client
	sessionMappingPrefix string
	category             AccountCategory
	supportedTypes       []AccountType
}

// NewBaseScheduler 创建基础调度器
func NewBaseScheduler(redisClient *redis.Client, category AccountCategory, supportedTypes []AccountType) *BaseScheduler {
	return &BaseScheduler{
		redis:                redisClient,
		sessionMappingPrefix: fmt.Sprintf("session_mapping:%s:", category),
		category:             category,
		supportedTypes:       supportedTypes,
	}
}

// GetSessionAccount 获取会话绑定的账户
func (s *BaseScheduler) GetSessionAccount(ctx context.Context, sessionHash, model string) *SelectResult {
	session, err := s.redis.GetStickySession(ctx, sessionHash)
	if err != nil || session == nil {
		return nil
	}

	// 验证账户是否仍然可用
	account, err := s.redis.GetAccount(ctx, redis.AccountType(session.AccountType), session.AccountID)
	if err != nil {
		logger.Warn("Session account not available, will select new one",
			zap.String("sessionHash", sessionHash),
			zap.String("accountId", session.AccountID),
			zap.Error(err))
		return nil
	}

	if account == nil {
		return nil
	}

	// 验证账户是否活跃
	if !s.isAccountActive(account) {
		return nil
	}

	// 验证账户是否支持请求的模型
	if model != "" && !s.isModelSupported(account, AccountType(session.AccountType), model) {
		return nil
	}

	// 续期会话
	s.redis.RenewStickySession(ctx, sessionHash, time.Hour)

	logger.Debug("Using session-bound account",
		zap.String("sessionHash", truncateString(sessionHash, 8)),
		zap.String("accountType", session.AccountType),
		zap.String("accountId", session.AccountID))

	return &SelectResult{
		Account:     account,
		AccountType: AccountType(session.AccountType),
		AccountID:   session.AccountID,
		FromSession: true,
	}
}

// CollectAvailableAccounts 收集可用账户
func (s *BaseScheduler) CollectAvailableAccounts(ctx context.Context, opts SelectOptions) []AccountCandidate {
	var candidates []AccountCandidate

	// 确定要检查的账户类型
	accountTypes := s.supportedTypes
	if len(opts.PreferredAccountTypes) > 0 {
		accountTypes = opts.PreferredAccountTypes
	}

	for _, accountType := range accountTypes {
		// 确保账户类型属于当前调度器的类别
		if cat, ok := AccountTypeToCategory[accountType]; !ok || cat != s.category {
			continue
		}

		accounts, err := s.redis.GetActiveAccounts(ctx, redis.AccountType(accountType))
		if err != nil {
			logger.Warn("Failed to get accounts",
				zap.String("type", string(accountType)),
				zap.Error(err))
			continue
		}

		for _, account := range accounts {
			accountID := s.getAccountID(account)

			// 检查是否在排除列表中
			if contains(opts.ExcludeAccountIDs, accountID) {
				continue
			}

			// 检查账户是否可调度
			if !s.isAccountSchedulable(account) {
				continue
			}

			// 检查账户是否支持模型
			if opts.Model != "" && !s.isModelSupported(account, accountType, opts.Model) {
				continue
			}

			// 检查账户是否过载
			if s.isAccountOverloaded(ctx, accountType, accountID) {
				continue
			}

			// 检查功能要求
			if len(opts.RequireFeatures) > 0 && !s.hasRequiredFeatures(account, opts.RequireFeatures) {
				continue
			}

			candidates = append(candidates, AccountCandidate{
				Account:     account,
				AccountType: accountType,
				AccountID:   accountID,
				Priority:    s.getAccountPriority(accountType, account),
				Load:        s.getAccountLoad(ctx, accountType, accountID),
				Features:    s.getAccountFeatures(account),
			})
		}
	}

	return candidates
}

// SelectBestAccount 选择最优账户
func (s *BaseScheduler) SelectBestAccount(candidates []AccountCandidate) *SelectResult {
	if len(candidates) == 0 {
		return nil
	}

	// 按优先级和负载排序选择最优账户
	best := candidates[0]
	for _, c := range candidates[1:] {
		// 优先级高的优先
		if c.Priority > best.Priority {
			best = c
			continue
		}
		// 优先级相同时，负载低的优先
		if c.Priority == best.Priority && c.Load < best.Load {
			best = c
		}
	}

	return &SelectResult{
		Account:     best.Account,
		AccountType: best.AccountType,
		AccountID:   best.AccountID,
		FromSession: false,
	}
}

// BindSessionAccount 绑定会话账户
func (s *BaseScheduler) BindSessionAccount(ctx context.Context, sessionHash string, accountType AccountType, accountID string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = time.Hour
	}

	err := s.redis.SetStickySession(ctx, sessionHash, accountID, string(accountType), ttl)
	if err != nil {
		return err
	}

	logger.Debug("Bound session to account",
		zap.String("sessionHash", truncateString(sessionHash, 8)),
		zap.String("accountType", string(accountType)),
		zap.String("accountId", accountID))

	return nil
}

// isAccountActive 检查账户是否活跃
func (s *BaseScheduler) isAccountActive(account map[string]interface{}) bool {
	if status, ok := account["status"].(string); ok {
		return status == "active"
	}
	// 默认认为活跃
	return true
}

// isAccountSchedulable 检查账户是否可调度
func (s *BaseScheduler) isAccountSchedulable(account map[string]interface{}) bool {
	// 检查状态
	if !s.isAccountActive(account) {
		return false
	}

	// 检查是否有临时错误
	if _, hasError := account["errorMsg"]; hasError {
		return false
	}

	return true
}

// isModelSupported 检查账户是否支持模型
func (s *BaseScheduler) isModelSupported(account map[string]interface{}, accountType AccountType, model string) bool {
	if model == "" {
		return true
	}

	modelLower := strings.ToLower(model)

	// 根据账户类型检查模型兼容性
	switch accountType {
	case AccountTypeClaude, AccountTypeClaudeOfficial, AccountTypeClaudeConsole:
		return s.isClaudeModelSupported(account, modelLower)
	case AccountTypeBedrock:
		return s.isBedrockModelSupported(modelLower)
	case AccountTypeGemini, AccountTypeGeminiAPI:
		return s.isGeminiModelSupported(modelLower)
	case AccountTypeOpenAI, AccountTypeOpenAIResponses, AccountTypeAzureOpenAI:
		return s.isOpenAIModelSupported(modelLower)
	}

	return true
}

// isClaudeModelSupported 检查 Claude 模型支持
func (s *BaseScheduler) isClaudeModelSupported(account map[string]interface{}, model string) bool {
	// 只支持 Claude 模型
	if !strings.Contains(model, "claude") &&
		!strings.Contains(model, "sonnet") &&
		!strings.Contains(model, "opus") &&
		!strings.Contains(model, "haiku") {
		return false
	}

	// Opus 模型需要检查订阅等级
	if strings.Contains(model, "opus") {
		return s.checkOpusAccess(account, model)
	}

	return true
}

// checkOpusAccess 检查 Opus 模型访问权限
func (s *BaseScheduler) checkOpusAccess(account map[string]interface{}, model string) bool {
	// 获取订阅等级
	subscriptionLevel := ""
	if level, ok := account["subscriptionLevel"].(string); ok {
		subscriptionLevel = strings.ToLower(level)
	}

	// Free 用户不能使用任何 Opus 模型
	if subscriptionLevel == "free" {
		return false
	}

	// Pro 用户只能使用 Opus 4.5+
	if subscriptionLevel == "pro" {
		// opus-4-5 或更新版本
		if strings.Contains(model, "opus-4-5") || strings.Contains(model, "opus-4-20250514") {
			return true
		}
		// 旧版 Opus 需要 Max
		if strings.Contains(model, "opus-4-") {
			return false
		}
	}

	// Max 用户可以使用所有 Opus 版本
	return true
}

// isBedrockModelSupported 检查 Bedrock 模型支持
func (s *BaseScheduler) isBedrockModelSupported(model string) bool {
	// Bedrock 支持特定的 Anthropic 模型
	return strings.Contains(model, "claude") ||
		strings.Contains(model, "anthropic")
}

// isGeminiModelSupported 检查 Gemini 模型支持
func (s *BaseScheduler) isGeminiModelSupported(model string) bool {
	return strings.Contains(model, "gemini") ||
		strings.Contains(model, "palm")
}

// isOpenAIModelSupported 检查 OpenAI 模型支持
func (s *BaseScheduler) isOpenAIModelSupported(model string) bool {
	return strings.Contains(model, "gpt") ||
		strings.Contains(model, "o1") ||
		strings.Contains(model, "o3") ||
		strings.Contains(model, "text-") ||
		strings.Contains(model, "davinci") ||
		strings.Contains(model, "curie")
}

// isAccountOverloaded 检查账户是否过载
func (s *BaseScheduler) isAccountOverloaded(ctx context.Context, accountType AccountType, accountID string) bool {
	key := fmt.Sprintf("overload:%s:%s", accountType, accountID)
	exists, _ := s.redis.Exists(ctx, key)
	return exists
}

// hasRequiredFeatures 检查账户是否有所需功能
func (s *BaseScheduler) hasRequiredFeatures(account map[string]interface{}, required []string) bool {
	features := s.getAccountFeatures(account)
	featureSet := make(map[string]bool)
	for _, f := range features {
		featureSet[strings.ToLower(f)] = true
	}

	for _, req := range required {
		if !featureSet[strings.ToLower(req)] {
			return false
		}
	}

	return true
}

// getAccountID 获取账户 ID
func (s *BaseScheduler) getAccountID(account map[string]interface{}) string {
	if id, ok := account["id"].(string); ok {
		return id
	}
	return ""
}

// getAccountPriority 获取账户优先级
func (s *BaseScheduler) getAccountPriority(accountType AccountType, account map[string]interface{}) int {
	basePriority := AccountTypePriority[accountType]

	// 账户级别的优先级调整
	if priority, ok := account["priority"].(float64); ok {
		return basePriority + int(priority)
	}

	return basePriority
}

// getAccountLoad 获取账户负载
func (s *BaseScheduler) getAccountLoad(ctx context.Context, accountType AccountType, accountID string) float64 {
	concurrency, _ := s.redis.GetConcurrency(ctx, accountID)
	return float64(concurrency)
}

// getAccountFeatures 获取账户支持的功能
func (s *BaseScheduler) getAccountFeatures(account map[string]interface{}) []string {
	if features, ok := account["features"].([]interface{}); ok {
		result := make([]string, 0, len(features))
		for _, f := range features {
			if str, ok := f.(string); ok {
				result = append(result, str)
			}
		}
		return result
	}
	return []string{}
}

// GenerateSessionHash 生成会话哈希
func GenerateSessionHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// truncateString 截断字符串
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// contains 检查切片是否包含元素
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
