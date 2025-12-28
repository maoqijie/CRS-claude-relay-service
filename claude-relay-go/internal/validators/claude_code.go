package validators

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// ClaudeCodeHeaders Claude Code 请求头常量
const (
	HeaderXSessionID      = "X-Session-Id"
	HeaderXConversationID = "X-Conversation-Id"
	HeaderXRequestID      = "X-Request-Id"
	HeaderXClientVersion  = "X-Client-Version"
	HeaderXModelID        = "X-Model-Id"
	HeaderXToolType       = "X-Tool-Type"
	HeaderXContextHash    = "X-Context-Hash"
)

// ClaudeCodeValidator Claude Code 请求验证器
type ClaudeCodeValidator struct{}

// NewClaudeCodeValidator 创建 Claude Code 验证器
func NewClaudeCodeValidator() *ClaudeCodeValidator {
	return &ClaudeCodeValidator{}
}

// ClaudeCodeRequestInfo Claude Code 请求信息
type ClaudeCodeRequestInfo struct {
	SessionID      string `json:"sessionId,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`
	RequestID      string `json:"requestId,omitempty"`
	ClientVersion  string `json:"clientVersion,omitempty"`
	ModelID        string `json:"modelId,omitempty"`
	ToolType       string `json:"toolType,omitempty"`
	ContextHash    string `json:"contextHash,omitempty"`
	SessionHash    string `json:"sessionHash,omitempty"` // 用于粘性会话
}

// ParseRequestInfo 从请求头解析 Claude Code 请求信息
func (v *ClaudeCodeValidator) ParseRequestInfo(headers http.Header) *ClaudeCodeRequestInfo {
	info := &ClaudeCodeRequestInfo{
		SessionID:      headers.Get(HeaderXSessionID),
		ConversationID: headers.Get(HeaderXConversationID),
		RequestID:      headers.Get(HeaderXRequestID),
		ClientVersion:  headers.Get(HeaderXClientVersion),
		ModelID:        headers.Get(HeaderXModelID),
		ToolType:       headers.Get(HeaderXToolType),
		ContextHash:    headers.Get(HeaderXContextHash),
	}

	// 生成会话哈希（用于粘性会话）
	info.SessionHash = v.GenerateSessionHash(info)

	return info
}

// GenerateSessionHash 生成会话哈希
func (v *ClaudeCodeValidator) GenerateSessionHash(info *ClaudeCodeRequestInfo) string {
	// 优先使用 SessionID
	if info.SessionID != "" {
		return hashString(info.SessionID)
	}

	// 其次使用 ConversationID
	if info.ConversationID != "" {
		return hashString(info.ConversationID)
	}

	// 最后使用 ContextHash
	if info.ContextHash != "" {
		return hashString(info.ContextHash)
	}

	return ""
}

// hashString 计算字符串的 SHA-256 哈希
func hashString(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:16]) // 取前 16 字节
}

// IsClaudeCodeRequest 检查是否是 Claude Code 请求
func (v *ClaudeCodeValidator) IsClaudeCodeRequest(headers http.Header, userAgent string) bool {
	// 检查 User-Agent
	if strings.Contains(strings.ToLower(userAgent), "claude-code") ||
		strings.Contains(strings.ToLower(userAgent), "claudecode") {
		return true
	}

	// 检查特有的请求头
	if headers.Get(HeaderXSessionID) != "" ||
		headers.Get(HeaderXConversationID) != "" ||
		headers.Get(HeaderXContextHash) != "" {
		return true
	}

	return false
}

// ValidateRequest 验证 Claude Code 请求
func (v *ClaudeCodeValidator) ValidateRequest(headers http.Header) *ValidationResult {
	result := &ValidationResult{
		Valid:  true,
		Errors: []string{},
	}

	// Claude Code 请求通常不需要强制验证
	// 这里可以添加自定义验证逻辑

	return result
}

// ValidationResult 验证结果
type ValidationResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

// RequestHeadersValidator 请求头验证器
type RequestHeadersValidator struct {
	requiredHeaders []string
	optionalHeaders []string
}

// NewRequestHeadersValidator 创建请求头验证器
func NewRequestHeadersValidator() *RequestHeadersValidator {
	return &RequestHeadersValidator{
		requiredHeaders: []string{},
		optionalHeaders: []string{},
	}
}

// WithRequiredHeaders 设置必需的请求头
func (v *RequestHeadersValidator) WithRequiredHeaders(headers []string) *RequestHeadersValidator {
	v.requiredHeaders = headers
	return v
}

// WithOptionalHeaders 设置可选的请求头
func (v *RequestHeadersValidator) WithOptionalHeaders(headers []string) *RequestHeadersValidator {
	v.optionalHeaders = headers
	return v
}

// Validate 验证请求头
func (v *RequestHeadersValidator) Validate(headers http.Header) *ValidationResult {
	result := &ValidationResult{
		Valid:  true,
		Errors: []string{},
	}

	// 检查必需的请求头
	for _, header := range v.requiredHeaders {
		if headers.Get(header) == "" {
			result.Valid = false
			result.Errors = append(result.Errors, "Missing required header: "+header)
		}
	}

	return result
}

// ModelValidator 模型验证器
type ModelValidator struct {
	supportedModels map[string]bool
	modelAliases    map[string]string
}

// NewModelValidator 创建模型验证器
func NewModelValidator() *ModelValidator {
	return &ModelValidator{
		supportedModels: make(map[string]bool),
		modelAliases:    make(map[string]string),
	}
}

// WithSupportedModels 设置支持的模型列表
func (v *ModelValidator) WithSupportedModels(models []string) *ModelValidator {
	v.supportedModels = make(map[string]bool)
	for _, model := range models {
		v.supportedModels[strings.ToLower(model)] = true
	}
	return v
}

// WithModelAliases 设置模型别名
func (v *ModelValidator) WithModelAliases(aliases map[string]string) *ModelValidator {
	v.modelAliases = aliases
	return v
}

// ResolveModel 解析模型名称（处理别名）
func (v *ModelValidator) ResolveModel(model string) string {
	// 检查别名
	if resolved, ok := v.modelAliases[strings.ToLower(model)]; ok {
		return resolved
	}
	return model
}

// IsModelSupported 检查模型是否支持
func (v *ModelValidator) IsModelSupported(model string) bool {
	if len(v.supportedModels) == 0 {
		return true // 未配置限制时允许所有
	}

	resolved := v.ResolveModel(model)
	return v.supportedModels[strings.ToLower(resolved)]
}

// IsModelBlacklisted 检查模型是否在黑名单中
func IsModelBlacklisted(model string, blacklist []string) bool {
	if len(blacklist) == 0 {
		return false
	}

	modelLower := strings.ToLower(model)
	for _, blocked := range blacklist {
		blockedLower := strings.ToLower(blocked)

		// 精确匹配
		if blockedLower == modelLower {
			return true
		}

		// 包含匹配
		if strings.Contains(modelLower, blockedLower) {
			return true
		}

		// 通配符匹配
		if strings.HasSuffix(blockedLower, "*") {
			prefix := strings.TrimSuffix(blockedLower, "*")
			if strings.HasPrefix(modelLower, prefix) {
				return true
			}
		}
	}

	return false
}

// DefaultClaudeModels 默认支持的 Claude 模型
var DefaultClaudeModels = []string{
	"claude-sonnet-4-20250514",
	"claude-opus-4-20250514",
	"claude-3-5-sonnet-20241022",
	"claude-3-5-sonnet-latest",
	"claude-3-5-haiku-20241022",
	"claude-3-5-haiku-latest",
	"claude-3-opus-20240229",
	"claude-3-sonnet-20240229",
	"claude-3-haiku-20240307",
}

// DefaultGeminiModels 默认支持的 Gemini 模型
var DefaultGeminiModels = []string{
	"gemini-1.5-pro",
	"gemini-1.5-pro-latest",
	"gemini-1.5-flash",
	"gemini-1.5-flash-latest",
	"gemini-2.0-flash",
	"gemini-2.0-flash-exp",
}

// DefaultOpenAIModels 默认支持的 OpenAI 模型
var DefaultOpenAIModels = []string{
	"gpt-4o",
	"gpt-4o-mini",
	"gpt-4-turbo",
	"gpt-4",
	"gpt-3.5-turbo",
	"o1",
	"o1-mini",
	"o1-preview",
}
