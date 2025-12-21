package redis

import (
	"testing"
	"time"
)

func TestUsageStatsStruct(t *testing.T) {
	stats := UsageStats{
		TotalTokens:         10000,
		InputTokens:         3000,
		OutputTokens:        7000,
		CacheCreateTokens:   500,
		CacheReadTokens:     200,
		AllTokens:           10700,
		RequestCount:        50,
		Ephemeral5mTokens:   100,
		Ephemeral1hTokens:   200,
		LongContextRequests: 5,
		TotalCost:           1.5,
	}

	if stats.TotalTokens != 10000 {
		t.Errorf("Expected TotalTokens 10000, got %d", stats.TotalTokens)
	}
	if stats.InputTokens != 3000 {
		t.Errorf("Expected InputTokens 3000, got %d", stats.InputTokens)
	}
	if stats.OutputTokens != 7000 {
		t.Errorf("Expected OutputTokens 7000, got %d", stats.OutputTokens)
	}
	if stats.CacheCreateTokens != 500 {
		t.Errorf("Expected CacheCreateTokens 500, got %d", stats.CacheCreateTokens)
	}
	if stats.CacheReadTokens != 200 {
		t.Errorf("Expected CacheReadTokens 200, got %d", stats.CacheReadTokens)
	}
	if stats.RequestCount != 50 {
		t.Errorf("Expected RequestCount 50, got %d", stats.RequestCount)
	}
	if stats.Ephemeral5mTokens != 100 {
		t.Errorf("Expected Ephemeral5mTokens 100, got %d", stats.Ephemeral5mTokens)
	}
	if stats.TotalCost != 1.5 {
		t.Errorf("Expected TotalCost 1.5, got %f", stats.TotalCost)
	}
}

func TestUsageRecordStruct(t *testing.T) {
	now := time.Now()
	record := UsageRecord{
		Timestamp:         now,
		Model:             "claude-3-5-sonnet-20241022",
		InputTokens:       1000,
		OutputTokens:      2000,
		CacheCreateTokens: 100,
		CacheReadTokens:   50,
		Cost:              0.05,
	}

	if record.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("Expected Model 'claude-3-5-sonnet-20241022', got '%s'", record.Model)
	}
	if record.InputTokens != 1000 {
		t.Errorf("Expected InputTokens 1000, got %d", record.InputTokens)
	}
	if record.OutputTokens != 2000 {
		t.Errorf("Expected OutputTokens 2000, got %d", record.OutputTokens)
	}
	if record.Cost != 0.05 {
		t.Errorf("Expected Cost 0.05, got %f", record.Cost)
	}
}

func TestTokenUsageParamsStruct(t *testing.T) {
	params := TokenUsageParams{
		KeyID:                "key-123",
		AccountID:            "acc-456",
		Model:                "claude-3-5-sonnet",
		InputTokens:          5000,
		OutputTokens:         10000,
		CacheCreateTokens:    500,
		CacheReadTokens:      200,
		Ephemeral5mTokens:    100,
		Ephemeral1hTokens:    300,
		IsLongContextRequest: true,
	}

	if params.KeyID != "key-123" {
		t.Errorf("Expected KeyID 'key-123', got '%s'", params.KeyID)
	}
	if params.AccountID != "acc-456" {
		t.Errorf("Expected AccountID 'acc-456', got '%s'", params.AccountID)
	}
	if params.Model != "claude-3-5-sonnet" {
		t.Errorf("Expected Model 'claude-3-5-sonnet', got '%s'", params.Model)
	}
	if params.InputTokens != 5000 {
		t.Errorf("Expected InputTokens 5000, got %d", params.InputTokens)
	}
	if !params.IsLongContextRequest {
		t.Error("Expected IsLongContextRequest true")
	}
}

func TestNormalizeModelName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple claude model name",
			input:    "claude-3-5-sonnet",
			expected: "claude-3-5-sonnet",
		},
		{
			name:     "claude model with date suffix removed",
			input:    "claude-3-5-sonnet-20241022",
			expected: "claude-3-5-sonnet",
		},
		{
			name:     "claude model with version suffix removed",
			input:    "claude-3-sonnet-v1:0",
			expected: "claude-3-sonnet",
		},
		{
			name:     "non-claude model unchanged",
			input:    "gpt-4-turbo",
			expected: "gpt-4-turbo",
		},
		{
			name:     "non-claude model with version suffix removed",
			input:    "gemini-pro-v1:0",
			expected: "gemini-pro",
		},
		{
			name:     "model with latest suffix removed",
			input:    "gemini-pro:latest",
			expected: "gemini-pro",
		},
		{
			name:     "empty model name",
			input:    "",
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeModelName(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeModelName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseInt64(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{
			name:     "positive number",
			input:    "12345",
			expected: 12345,
		},
		{
			name:     "zero",
			input:    "0",
			expected: 0,
		},
		{
			name:     "negative number",
			input:    "-100",
			expected: -100,
		},
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			name:     "invalid string",
			input:    "abc",
			expected: 0,
		},
		{
			name:     "large number",
			input:    "9223372036854775807",
			expected: 9223372036854775807,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseInt64(tt.input)
			if result != tt.expected {
				t.Errorf("parseInt64(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseFloat64(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected float64
	}{
		{
			name:     "positive float",
			input:    "123.45",
			expected: 123.45,
		},
		{
			name:     "zero",
			input:    "0",
			expected: 0,
		},
		{
			name:     "negative float",
			input:    "-99.99",
			expected: -99.99,
		},
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			name:     "invalid string",
			input:    "xyz",
			expected: 0,
		},
		{
			name:     "integer as float",
			input:    "100",
			expected: 100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseFloat64(tt.input)
			if result != tt.expected {
				t.Errorf("parseFloat64(%q) = %f, want %f", tt.input, result, tt.expected)
			}
		})
	}
}

func TestUsageStatsResultStruct(t *testing.T) {
	totalStats := &UsageStats{
		TotalTokens:  100000,
		InputTokens:  40000,
		OutputTokens: 60000,
		RequestCount: 500,
	}

	stats := UsageStatsResult{
		Total: totalStats,
		Averages: UsageAverages{
			RPM:           10.0,
			TPM:           1000.0,
			DailyRequests: 100.0,
			DailyTokens:   20000.0,
		},
	}

	if stats.Total.TotalTokens != 100000 {
		t.Errorf("Expected Total.TotalTokens 100000, got %d", stats.Total.TotalTokens)
	}
	if stats.Total.RequestCount != 500 {
		t.Errorf("Expected Total.RequestCount 500, got %d", stats.Total.RequestCount)
	}
	if stats.Averages.RPM != 10.0 {
		t.Errorf("Expected Averages.RPM 10.0, got %f", stats.Averages.RPM)
	}
}

func TestUsageAveragesStruct(t *testing.T) {
	averages := UsageAverages{
		RPM:           15.5,
		TPM:           1500.0,
		DailyRequests: 200.0,
		DailyTokens:   30000.0,
	}

	if averages.RPM != 15.5 {
		t.Errorf("Expected RPM 15.5, got %f", averages.RPM)
	}
	if averages.TPM != 1500.0 {
		t.Errorf("Expected TPM 1500.0, got %f", averages.TPM)
	}
	if averages.DailyRequests != 200.0 {
		t.Errorf("Expected DailyRequests 200.0, got %f", averages.DailyRequests)
	}
}
