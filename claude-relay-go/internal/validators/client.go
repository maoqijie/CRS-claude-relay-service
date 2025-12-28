package validators

import (
	"strings"

	"github.com/catstream/claude-relay-go/internal/pkg/clients"
)

// ClientType 客户端类型常量（使用 clients 包保持兼容）
const (
	ClientTypeClaudeCode   = clients.TypeClaudeCode
	ClientTypeGeminiCLI    = clients.TypeGeminiCLI
	ClientTypeCodex        = clients.TypeCodex
	ClientTypeCherryStudio = clients.TypeCherryStudio
	ClientTypeDroidCLI     = clients.TypeDroidCLI
	ClientTypeCursor       = clients.TypeCursor
	ClientTypeWindsurf     = clients.TypeWindsurf
	ClientTypeUnknown      = clients.TypeUnknown
)

// PredefinedClients 预定义客户端列表
var PredefinedClients = clients.PredefinedClients

// ClientValidator 客户端验证器
type ClientValidator struct {
	allowedClients map[string]bool
	blockUnknown   bool
}

// NewClientValidator 创建客户端验证器
func NewClientValidator() *ClientValidator {
	return &ClientValidator{
		allowedClients: make(map[string]bool),
		blockUnknown:   false,
	}
}

// WithAllowedClients 设置允许的客户端列表
func (v *ClientValidator) WithAllowedClients(clientList []string) *ClientValidator {
	v.allowedClients = make(map[string]bool)
	for _, client := range clientList {
		v.allowedClients[strings.ToLower(client)] = true
	}
	return v
}

// WithBlockUnknown 设置是否阻止未知客户端
func (v *ClientValidator) WithBlockUnknown(block bool) *ClientValidator {
	v.blockUnknown = block
	return v
}

// ParseClientType 从 User-Agent 解析客户端类型
func (v *ClientValidator) ParseClientType(userAgent string) string {
	return clients.ParseClientType(userAgent)
}

// IsClientAllowed 检查客户端是否允许
func (v *ClientValidator) IsClientAllowed(clientType string, allowedList []string) bool {
	return clients.IsClientAllowed(allowedList, clientType)
}

// Validate 验证客户端
func (v *ClientValidator) Validate(userAgent string, allowedList []string) (clientType string, allowed bool) {
	clientType = v.ParseClientType(userAgent)

	// 检查是否在允许列表中
	if len(allowedList) > 0 {
		allowed = v.IsClientAllowed(clientType, allowedList)
		return
	}

	// 未设置允许列表
	if v.blockUnknown && clientType == ClientTypeUnknown {
		allowed = false
		return
	}

	allowed = true
	return
}

// IsPredefinedClient 检查是否是预定义客户端
func IsPredefinedClient(clientType string) bool {
	return clients.IsPredefinedClient(clientType)
}

// GetClientCategory 获取客户端分类
func GetClientCategory(clientType string) string {
	return clients.GetClientCategory(clientType)
}

// ClientInfo 客户端信息（使用 clients 包的定义）
type ClientInfo = clients.ClientInfo

// GetClientInfo 获取客户端信息
func GetClientInfo(userAgent string) *ClientInfo {
	return clients.GetClientInfo(userAgent)
}
