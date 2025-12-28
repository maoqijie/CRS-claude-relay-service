package apikey

import (
	"strings"

	"github.com/catstream/claude-relay-go/internal/pkg/clients"
	"github.com/catstream/claude-relay-go/internal/storage/redis"
)

// Permission 权限常量
const (
	PermissionAll     = "all"
	PermissionClaude  = "claude"
	PermissionGemini  = "gemini"
	PermissionOpenAI  = "openai"
	PermissionDroid   = "droid"
	PermissionBedrock = "bedrock"
	PermissionAzure   = "azure"
)

// ValidPermissions 有效权限列表
var ValidPermissions = []string{
	PermissionAll,
	PermissionClaude,
	PermissionGemini,
	PermissionOpenAI,
	PermissionDroid,
	PermissionBedrock,
	PermissionAzure,
}

// ClientType 客户端类型常量（使用 clients 包的常量保持兼容）
const (
	ClientClaudeCode   = clients.TypeClaudeCode
	ClientGeminiCLI    = clients.TypeGeminiCLI
	ClientCodex        = clients.TypeCodex
	ClientCherryStudio = clients.TypeCherryStudio
	ClientDroidCLI     = clients.TypeDroidCLI
	ClientUnknown      = clients.TypeUnknown
)

// PredefinedClients 预定义客户端列表
var PredefinedClients = clients.PredefinedClients

// PermissionChecker 权限检查器
type PermissionChecker struct {
	apiKey *redis.APIKey
}

// NewPermissionChecker 创建权限检查器
func NewPermissionChecker(apiKey *redis.APIKey) *PermissionChecker {
	return &PermissionChecker{apiKey: apiKey}
}

// HasPermission 检查是否有指定权限
func (pc *PermissionChecker) HasPermission(required string) bool {
	if pc.apiKey == nil {
		return false
	}

	// 未设置权限时默认允许所有
	if len(pc.apiKey.Permissions) == 0 {
		return true
	}

	requiredLower := strings.ToLower(required)
	for _, perm := range pc.apiKey.Permissions {
		permLower := strings.ToLower(perm)
		if permLower == PermissionAll || permLower == requiredLower {
			return true
		}
	}

	return false
}

// HasAnyPermission 检查是否有任意一个权限
func (pc *PermissionChecker) HasAnyPermission(permissions []string) bool {
	for _, perm := range permissions {
		if pc.HasPermission(perm) {
			return true
		}
	}
	return false
}

// HasAllPermissions 检查是否有所有权限
func (pc *PermissionChecker) HasAllPermissions(permissions []string) bool {
	for _, perm := range permissions {
		if !pc.HasPermission(perm) {
			return false
		}
	}
	return true
}

// CanAccessService 检查是否可以访问指定服务
func (pc *PermissionChecker) CanAccessService(service string) bool {
	// 映射服务到权限
	servicePermissionMap := map[string]string{
		"claude":           PermissionClaude,
		"claude-official":  PermissionClaude,
		"claude-console":   PermissionClaude,
		"bedrock":          PermissionBedrock,
		"ccr":              PermissionClaude,
		"gemini":           PermissionGemini,
		"gemini-api":       PermissionGemini,
		"openai":           PermissionOpenAI,
		"openai-responses": PermissionOpenAI,
		"azure-openai":     PermissionAzure,
		"droid":            PermissionDroid,
	}

	required, ok := servicePermissionMap[strings.ToLower(service)]
	if !ok {
		return false
	}

	return pc.HasPermission(required)
}

// GetEffectivePermissions 获取生效的权限列表
func (pc *PermissionChecker) GetEffectivePermissions() []string {
	if pc.apiKey == nil {
		return []string{}
	}

	if len(pc.apiKey.Permissions) == 0 {
		return []string{PermissionAll}
	}

	return pc.apiKey.Permissions
}

// IsClientAllowed 检查客户端是否允许
func (pc *PermissionChecker) IsClientAllowed(clientType string) bool {
	if pc.apiKey == nil {
		return false
	}

	// 未设置限制时允许所有
	if len(pc.apiKey.AllowedClients) == 0 {
		return true
	}

	clientLower := strings.ToLower(clientType)
	for _, allowed := range pc.apiKey.AllowedClients {
		allowedLower := strings.ToLower(allowed)

		// 通配符
		if allowedLower == "*" || allowedLower == "all" {
			return true
		}

		// 精确匹配
		if allowedLower == clientLower {
			return true
		}

		// 前缀匹配
		if strings.HasSuffix(allowedLower, "*") {
			prefix := strings.TrimSuffix(allowedLower, "*")
			if strings.HasPrefix(clientLower, prefix) {
				return true
			}
		}
	}

	return false
}

// IsModelAllowed 检查模型是否允许
func (pc *PermissionChecker) IsModelAllowed(model string) bool {
	if pc.apiKey == nil {
		return false
	}

	// 未设置黑名单时允许所有
	if len(pc.apiKey.ModelBlacklist) == 0 {
		return true
	}

	modelLower := strings.ToLower(model)
	for _, blocked := range pc.apiKey.ModelBlacklist {
		blockedLower := strings.ToLower(blocked)

		// 精确匹配
		if blockedLower == modelLower {
			return false
		}

		// 包含匹配
		if strings.Contains(modelLower, blockedLower) {
			return false
		}

		// 通配符匹配
		if strings.HasSuffix(blockedLower, "*") {
			prefix := strings.TrimSuffix(blockedLower, "*")
			if strings.HasPrefix(modelLower, prefix) {
				return false
			}
		}
	}

	return true
}

// ValidatePermissions 验证权限列表是否有效
func ValidatePermissions(permissions []string) bool {
	validSet := make(map[string]bool)
	for _, p := range ValidPermissions {
		validSet[strings.ToLower(p)] = true
	}

	for _, perm := range permissions {
		if !validSet[strings.ToLower(perm)] {
			return false
		}
	}

	return true
}

// ParsePermissionsFromString 从逗号分隔的字符串解析权限
func ParsePermissionsFromString(s string) []string {
	if s == "" {
		return []string{}
	}

	parts := strings.Split(s, ",")
	permissions := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			permissions = append(permissions, trimmed)
		}
	}

	return permissions
}

// GetPermissionForRoute 根据路由获取所需权限
func GetPermissionForRoute(path string) string {
	pathLower := strings.ToLower(path)

	// Claude 相关路由
	if strings.HasPrefix(pathLower, "/api/") ||
		strings.HasPrefix(pathLower, "/claude/") ||
		strings.HasPrefix(pathLower, "/v1/messages") {
		return PermissionClaude
	}

	// Gemini 相关路由
	if strings.HasPrefix(pathLower, "/gemini/") {
		return PermissionGemini
	}

	// OpenAI 相关路由
	if strings.HasPrefix(pathLower, "/openai/") {
		// 检查是否是 Claude 转换路由
		if strings.Contains(pathLower, "/claude/") {
			return PermissionClaude
		}
		// 检查是否是 Gemini 转换路由
		if strings.Contains(pathLower, "/gemini/") {
			return PermissionGemini
		}
		return PermissionOpenAI
	}

	// Droid 相关路由
	if strings.HasPrefix(pathLower, "/droid/") {
		return PermissionDroid
	}

	// Azure 相关路由
	if strings.HasPrefix(pathLower, "/azure/") {
		return PermissionAzure
	}

	// 默认需要 all 权限
	return PermissionAll
}

// GetServiceCategory 获取服务类别
func GetServiceCategory(accountType string) string {
	categoryMap := map[string]string{
		"claude":           "claude",
		"claude-official":  "claude",
		"claude-console":   "claude",
		"bedrock":          "claude",
		"ccr":              "claude",
		"gemini":           "gemini",
		"gemini-api":       "gemini",
		"openai":           "openai",
		"openai-responses": "openai",
		"azure-openai":     "openai",
		"droid":            "droid",
	}

	if category, ok := categoryMap[strings.ToLower(accountType)]; ok {
		return category
	}
	return ""
}
