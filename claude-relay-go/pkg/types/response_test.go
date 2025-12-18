package types

import (
	"testing"
	"time"
)

func TestNewSuccessResponse(t *testing.T) {
	data := map[string]string{"key": "value"}
	message := "操作成功"

	resp := NewSuccessResponse(data, message)

	if !resp.Success {
		t.Error("Success should be true")
	}

	if resp.Data == nil {
		t.Error("Data should not be nil")
	}

	if resp.Message != message {
		t.Errorf("Message = %v, want %v", resp.Message, message)
	}

	if resp.Error != "" {
		t.Error("Error should be empty")
	}
}

func TestNewErrorResponse(t *testing.T) {
	errMsg := "发生错误"
	message := "请稍后重试"

	resp := NewErrorResponse(errMsg, message)

	if resp.Success {
		t.Error("Success should be false")
	}

	if resp.Error != errMsg {
		t.Errorf("Error = %v, want %v", resp.Error, errMsg)
	}

	if resp.Message != message {
		t.Errorf("Message = %v, want %v", resp.Message, message)
	}

	if resp.Data != nil {
		t.Error("Data should be nil")
	}
}

func TestHealthResponse(t *testing.T) {
	now := time.Now().UTC()
	resp := &HealthResponse{
		Status:    "healthy",
		Service:   "test-service",
		Version:   "1.0.0",
		Timestamp: now.Format(time.RFC3339),
		Components: map[string]bool{
			"redis":    true,
			"postgres": false,
		},
	}

	if resp.Status != "healthy" {
		t.Errorf("Status = %v, want healthy", resp.Status)
	}

	if resp.Components["redis"] != true {
		t.Error("Redis component should be true")
	}

	if resp.Components["postgres"] != false {
		t.Error("Postgres component should be false")
	}
}

func TestVersionResponse(t *testing.T) {
	resp := &VersionResponse{
		Service: "claude-relay-go",
		Version: "0.1.0",
		Go:      "1.24",
	}

	if resp.Service != "claude-relay-go" {
		t.Errorf("Service = %v, want claude-relay-go", resp.Service)
	}

	if resp.Version != "0.1.0" {
		t.Errorf("Version = %v, want 0.1.0", resp.Version)
	}

	if resp.Go != "1.24" {
		t.Errorf("Go = %v, want 1.24", resp.Go)
	}
}

func TestCountResponse(t *testing.T) {
	resp := &CountResponse{
		Count:   10,
		Total:   100,
		Message: "查询成功",
	}

	if resp.Count != 10 {
		t.Errorf("Count = %v, want 10", resp.Count)
	}

	if resp.Total != 100 {
		t.Errorf("Total = %v, want 100", resp.Total)
	}
}

func TestAccountsCountResponse(t *testing.T) {
	resp := &AccountsCountResponse{
		Accounts: map[string]int{
			"claude": 5,
			"gemini": 3,
		},
		Total:   8,
		Message: "统计成功",
	}

	if resp.Total != 8 {
		t.Errorf("Total = %v, want 8", resp.Total)
	}

	if resp.Accounts["claude"] != 5 {
		t.Errorf("Claude account count = %v, want 5", resp.Accounts["claude"])
	}
}

func TestErrorResponse(t *testing.T) {
	now := time.Now()
	resp := &ErrorResponse{
		Error:     "连接失败",
		Message:   "无法连接到数据库",
		Code:      "DB_CONNECTION_ERROR",
		Timestamp: now,
	}

	if resp.Error != "连接失败" {
		t.Errorf("Error = %v, want 连接失败", resp.Error)
	}

	if resp.Code != "DB_CONNECTION_ERROR" {
		t.Errorf("Code = %v, want DB_CONNECTION_ERROR", resp.Code)
	}

	if resp.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}
