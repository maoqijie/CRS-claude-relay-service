package redis

import (
	"testing"
	"time"
)

func TestKeyPrefixes(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"API Key前缀", PrefixAPIKey, "apikey:"},
		{"Claude账户前缀", PrefixClaudeAccount, "claude:account:"},
		{"Gemini账户前缀", PrefixGeminiAccount, "gemini:account:"},
		{"使用统计前缀", PrefixUsage, "usage:"},
		{"并发控制前缀", PrefixConcurrency, "concurrency:"},
		{"会话前缀", PrefixSession, "session:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestTTLConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant time.Duration
		expected time.Duration
	}{
		{"API Key TTL", TTLAPIKey, 365 * 24 * time.Hour},
		{"每日使用统计 TTL", TTLUsageDaily, 32 * 24 * time.Hour},
		{"每月使用统计 TTL", TTLUsageMonthly, 365 * 24 * time.Hour},
		{"队列统计 TTL", TTLQueueStats, 7 * 24 * time.Hour},
		{"等待时间样本 TTL", TTLWaitTimeSamples, 24 * time.Hour},
		{"OAuth 会话 TTL", TTLOAuthSession, 10 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestSampleCounts(t *testing.T) {
	if WaitTimeSamplesPerKey != 500 {
		t.Errorf("WaitTimeSamplesPerKey = %v, want 500", WaitTimeSamplesPerKey)
	}

	if WaitTimeSamplesGlobal != 2000 {
		t.Errorf("WaitTimeSamplesGlobal = %v, want 2000", WaitTimeSamplesGlobal)
	}
}
