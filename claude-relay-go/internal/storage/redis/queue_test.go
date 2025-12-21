package redis

import (
	"strings"
	"testing"
)

func TestQueueStatsStruct(t *testing.T) {
	stats := QueueStats{
		APIKeyID:         "key-123",
		QueueCount:       5,
		Entered:          100,
		Success:          80,
		Timeout:          10,
		Cancelled:        5,
		SocketChanged:    3,
		RejectedOverload: 2,
		AvgWaitMs:        150.5,
		P50WaitMs:        100.0,
		P90WaitMs:        250.0,
		P99WaitMs:        500.0,
	}

	if stats.APIKeyID != "key-123" {
		t.Errorf("Expected APIKeyID 'key-123', got '%s'", stats.APIKeyID)
	}
	if stats.QueueCount != 5 {
		t.Errorf("Expected QueueCount 5, got %d", stats.QueueCount)
	}
	if stats.Entered != 100 {
		t.Errorf("Expected Entered 100, got %d", stats.Entered)
	}
	if stats.Success != 80 {
		t.Errorf("Expected Success 80, got %d", stats.Success)
	}
	if stats.Timeout != 10 {
		t.Errorf("Expected Timeout 10, got %d", stats.Timeout)
	}
	if stats.AvgWaitMs != 150.5 {
		t.Errorf("Expected AvgWaitMs 150.5, got %f", stats.AvgWaitMs)
	}
	if stats.P90WaitMs != 250.0 {
		t.Errorf("Expected P90WaitMs 250.0, got %f", stats.P90WaitMs)
	}
}

func TestGlobalQueueStatsStruct(t *testing.T) {
	stats := GlobalQueueStats{
		TotalQueueCount: 50,
		TotalEntered:    1000,
		TotalSuccess:    800,
		TotalTimeout:    100,
		TotalCancelled:  50,
		GlobalAvgWaitMs: 200.0,
		GlobalP50WaitMs: 150.0,
		GlobalP90WaitMs: 350.0,
		GlobalP99WaitMs: 600.0,
		PerKeyStats: []QueueStats{
			{APIKeyID: "key-1", QueueCount: 10},
			{APIKeyID: "key-2", QueueCount: 20},
		},
	}

	if stats.TotalQueueCount != 50 {
		t.Errorf("Expected TotalQueueCount 50, got %d", stats.TotalQueueCount)
	}
	if stats.TotalEntered != 1000 {
		t.Errorf("Expected TotalEntered 1000, got %d", stats.TotalEntered)
	}
	if stats.GlobalAvgWaitMs != 200.0 {
		t.Errorf("Expected GlobalAvgWaitMs 200.0, got %f", stats.GlobalAvgWaitMs)
	}
	if len(stats.PerKeyStats) != 2 {
		t.Errorf("Expected 2 PerKeyStats, got %d", len(stats.PerKeyStats))
	}
}

func TestCalculateAvg(t *testing.T) {
	tests := []struct {
		name     string
		input    []float64
		expected float64
	}{
		{
			name:     "normal values",
			input:    []float64{10.0, 20.0, 30.0},
			expected: 20.0,
		},
		{
			name:     "single value",
			input:    []float64{50.0},
			expected: 50.0,
		},
		{
			name:     "empty slice",
			input:    []float64{},
			expected: 0,
		},
		{
			name:     "decimal values",
			input:    []float64{1.5, 2.5, 3.0},
			expected: 2.3333333333333335,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateAvg(tt.input)
			if result != tt.expected {
				t.Errorf("calculateAvg() = %f, want %f", result, tt.expected)
			}
		})
	}
}

func TestCalculatePercentile(t *testing.T) {
	tests := []struct {
		name       string
		input      []float64
		percentile float64
		expected   float64
	}{
		{
			name:       "P50 of odd count",
			input:      []float64{10.0, 20.0, 30.0, 40.0, 50.0},
			percentile: 50,
			expected:   30.0,
		},
		{
			name:       "P90 of values",
			input:      []float64{10.0, 20.0, 30.0, 40.0, 50.0, 60.0, 70.0, 80.0, 90.0, 100.0},
			percentile: 90,
			expected:   91.0, // 90th percentile interpolated
		},
		{
			name:       "P99 of values",
			input:      []float64{10.0, 20.0, 30.0, 40.0, 50.0, 60.0, 70.0, 80.0, 90.0, 100.0},
			percentile: 99,
			expected:   99.1, // 99th percentile interpolated
		},
		{
			name:       "empty slice",
			input:      []float64{},
			percentile: 50,
			expected:   0,
		},
		{
			name:       "single value",
			input:      []float64{42.0},
			percentile: 50,
			expected:   42.0,
		},
		{
			name:       "P0 percentile",
			input:      []float64{10.0, 50.0, 100.0},
			percentile: 0,
			expected:   10.0,
		},
		{
			name:       "P100 percentile",
			input:      []float64{10.0, 50.0, 100.0},
			percentile: 100,
			expected:   100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculatePercentile(tt.input, tt.percentile)
			// Allow small floating point difference
			diff := result - tt.expected
			if diff < -0.01 || diff > 0.01 {
				t.Errorf("calculatePercentile() = %f, want %f", result, tt.expected)
			}
		})
	}
}

func TestQueueConstants(t *testing.T) {
	// Test queue-related constants
	if QueueStatsTTL.Hours() != 168 { // 7 days
		t.Errorf("Expected QueueStatsTTL 168 hours (7 days), got %f", QueueStatsTTL.Hours())
	}

	if WaitTimeTTL.Hours() != 24 { // 1 day
		t.Errorf("Expected WaitTimeTTL 24 hours (1 day), got %f", WaitTimeTTL.Hours())
	}

	if QueueTTLBuffer.Seconds() != 30 {
		t.Errorf("Expected QueueTTLBuffer 30 seconds, got %f", QueueTTLBuffer.Seconds())
	}
}

func TestLuaScripts(t *testing.T) {
	// Test that Lua scripts are defined and non-empty
	if luaQueueIncr == "" {
		t.Error("luaQueueIncr script is empty")
	}
	if luaQueueDecr == "" {
		t.Error("luaQueueDecr script is empty")
	}

	// Verify script contains expected commands
	if !strings.Contains(luaQueueIncr, "INCR") {
		t.Error("luaQueueIncr should contain INCR command")
	}
	if !strings.Contains(luaQueueIncr, "EXPIRE") {
		t.Error("luaQueueIncr should contain EXPIRE command")
	}
	if !strings.Contains(luaQueueDecr, "DECR") {
		t.Error("luaQueueDecr should contain DECR command")
	}
	if !strings.Contains(luaQueueDecr, "DEL") {
		t.Error("luaQueueDecr should contain DEL command")
	}
}
