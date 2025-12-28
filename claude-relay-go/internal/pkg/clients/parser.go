package clients

import (
	"strings"
)

// ClientType 客户端类型常量
const (
	TypeClaudeCode   = "ClaudeCode"
	TypeGeminiCLI    = "Gemini-CLI"
	TypeCodex        = "Codex"
	TypeCherryStudio = "CherryStudio"
	TypeDroidCLI     = "Droid-CLI"
	TypeCursor       = "Cursor"
	TypeWindsurf     = "Windsurf"
	TypeUnknown      = "Unknown"
)

// PredefinedClients 预定义客户端列表
var PredefinedClients = []string{
	TypeClaudeCode,
	TypeGeminiCLI,
	TypeCodex,
	TypeCherryStudio,
	TypeDroidCLI,
	TypeCursor,
	TypeWindsurf,
}

// ParseClientType 从 User-Agent 解析客户端类型
func ParseClientType(userAgent string) string {
	if userAgent == "" {
		return TypeUnknown
	}

	ua := strings.ToLower(userAgent)

	// Claude Code 客户端
	if strings.Contains(ua, "claude-code") || strings.Contains(ua, "claudecode") {
		return TypeClaudeCode
	}

	// Gemini CLI
	if strings.Contains(ua, "gemini-cli") || strings.Contains(ua, "geminicli") {
		return TypeGeminiCLI
	}

	// Codex
	if strings.Contains(ua, "codex") {
		return TypeCodex
	}

	// Cherry Studio
	if strings.Contains(ua, "cherry-studio") || strings.Contains(ua, "cherrystudio") {
		return TypeCherryStudio
	}

	// Droid CLI
	if strings.Contains(ua, "droid-cli") || strings.Contains(ua, "droidcli") {
		return TypeDroidCLI
	}

	// Cursor
	if strings.Contains(ua, "cursor") {
		return TypeCursor
	}

	// Windsurf
	if strings.Contains(ua, "windsurf") {
		return TypeWindsurf
	}

	return TypeUnknown
}

// IsClientAllowed 检查客户端是否在允许列表中
func IsClientAllowed(allowedClients []string, clientType string) bool {
	if len(allowedClients) == 0 {
		return true // 未设置限制时允许所有
	}

	clientLower := strings.ToLower(clientType)
	for _, allowed := range allowedClients {
		allowedLower := strings.ToLower(allowed)

		// 通配符匹配
		if allowedLower == "*" || allowedLower == "all" {
			return true
		}

		// 精确匹配
		if allowedLower == clientLower {
			return true
		}

		// 前缀匹配（如 "claude*" 匹配 "claudecode"）
		if strings.HasSuffix(allowedLower, "*") {
			prefix := strings.TrimSuffix(allowedLower, "*")
			if strings.HasPrefix(clientLower, prefix) {
				return true
			}
		}
	}

	return false
}

// IsPredefinedClient 检查是否是预定义客户端
func IsPredefinedClient(clientType string) bool {
	for _, predefined := range PredefinedClients {
		if strings.EqualFold(clientType, predefined) {
			return true
		}
	}
	return false
}

// GetClientCategory 获取客户端分类
func GetClientCategory(clientType string) string {
	switch clientType {
	case TypeClaudeCode:
		return "claude"
	case TypeGeminiCLI:
		return "gemini"
	case TypeCodex:
		return "openai"
	case TypeDroidCLI:
		return "droid"
	case TypeCherryStudio:
		return "multi" // 支持多种 API
	case TypeCursor, TypeWindsurf:
		return "ide"
	default:
		return "unknown"
	}
}

// ClientInfo 客户端信息
type ClientInfo struct {
	Type         string `json:"type"`
	Category     string `json:"category"`
	IsPredefined bool   `json:"isPredefined"`
	UserAgent    string `json:"userAgent,omitempty"`
}

// GetClientInfo 获取客户端完整信息
func GetClientInfo(userAgent string) *ClientInfo {
	clientType := ParseClientType(userAgent)
	return &ClientInfo{
		Type:         clientType,
		Category:     GetClientCategory(clientType),
		IsPredefined: IsPredefinedClient(clientType),
		UserAgent:    userAgent,
	}
}
