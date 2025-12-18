package types

import "time"

// Response 通用响应结构
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Message string      `json:"message,omitempty"`
}

// HealthResponse 健康检查响应
type HealthResponse struct {
	Status     string            `json:"status"`
	Service    string            `json:"service"`
	Version    string            `json:"version"`
	Timestamp  string            `json:"timestamp"`
	Components map[string]bool   `json:"components"`
	Details    map[string]string `json:"details,omitempty"`
}

// VersionResponse 版本信息响应
type VersionResponse struct {
	Service string `json:"service"`
	Version string `json:"version"`
	Go      string `json:"go"`
}

// CountResponse 计数响应
type CountResponse struct {
	Count   int    `json:"count,omitempty"`
	Total   int    `json:"total,omitempty"`
	Message string `json:"message"`
}

// AccountsCountResponse 账户统计响应
type AccountsCountResponse struct {
	Accounts map[string]int `json:"accounts"`
	Total    int            `json:"total"`
	Message  string         `json:"message"`
}

// RedisInfoResponse Redis 信息响应
type RedisInfoResponse struct {
	DBSize  int64  `json:"dbSize"`
	Message string `json:"message"`
}

// ErrorResponse 错误响应
type ErrorResponse struct {
	Error     string    `json:"error"`
	Message   string    `json:"message,omitempty"`
	Code      string    `json:"code,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// NewSuccessResponse 创建成功响应
func NewSuccessResponse(data interface{}, message string) *Response {
	return &Response{
		Success: true,
		Data:    data,
		Message: message,
	}
}

// NewErrorResponse 创建错误响应
func NewErrorResponse(err string, message string) *Response {
	return &Response{
		Success: false,
		Error:   err,
		Message: message,
	}
}
