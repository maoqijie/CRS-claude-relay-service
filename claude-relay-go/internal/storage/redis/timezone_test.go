package redis

import (
	"testing"
	"time"
)

func TestGetDateStringInTimezone(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected string
	}{
		{
			name:     "UTC midnight should be UTC+8 08:00",
			input:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: "2024-01-15",
		},
		{
			name:     "UTC 16:00 should be next day in UTC+8",
			input:    time.Date(2024, 1, 15, 16, 0, 0, 0, time.UTC),
			expected: "2024-01-16",
		},
		{
			name:     "UTC 15:59 should still be same day in UTC+8",
			input:    time.Date(2024, 1, 15, 15, 59, 0, 0, time.UTC),
			expected: "2024-01-15",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getDateStringInTimezone(tt.input)
			if result != tt.expected {
				t.Errorf("getDateStringInTimezone() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetHourInTimezone(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected int
	}{
		{
			name:     "UTC midnight is 08:00 in UTC+8",
			input:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: 8,
		},
		{
			name:     "UTC 12:00 is 20:00 in UTC+8",
			input:    time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			expected: 20,
		},
		{
			name:     "UTC 16:00 is 00:00 next day in UTC+8",
			input:    time.Date(2024, 1, 15, 16, 0, 0, 0, time.UTC),
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getHourInTimezone(tt.input)
			if result != tt.expected {
				t.Errorf("getHourInTimezone() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetMonthStringInTimezone(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected string
	}{
		{
			name:     "Middle of month",
			input:    time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC),
			expected: "2024-03",
		},
		{
			name:     "End of month UTC that becomes next month in UTC+8",
			input:    time.Date(2024, 1, 31, 20, 0, 0, 0, time.UTC),
			expected: "2024-02",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getMonthStringInTimezone(tt.input)
			if result != tt.expected {
				t.Errorf("getMonthStringInTimezone() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetHourStringInTimezone(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected string
	}{
		{
			name:     "Standard time",
			input:    time.Date(2024, 3, 15, 4, 30, 0, 0, time.UTC),
			expected: "2024-03-15:12",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getHourStringInTimezone(tt.input)
			if result != tt.expected {
				t.Errorf("getHourStringInTimezone() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetMinuteTimestamp(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected int64
	}{
		{
			name:     "Round down to minute",
			input:    time.Unix(1704067245, 0), // 2024-01-01 00:00:45
			expected: 1704067200,               // 2024-01-01 00:00:00
		},
		{
			name:     "Already at minute boundary",
			input:    time.Unix(1704067200, 0),
			expected: 1704067200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getMinuteTimestamp(tt.input)
			if result != tt.expected {
				t.Errorf("getMinuteTimestamp() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestTimezoneOffset(t *testing.T) {
	// Verify the default timezone offset is correctly set to 8 hours
	offset := getTimezoneOffset()
	expected := time.Duration(DefaultTimezoneOffset) * time.Hour
	if offset != expected {
		t.Errorf("getTimezoneOffset() = %v, want %v", offset, expected)
	}
}

func TestGetDateInTimezone(t *testing.T) {
	utcTime := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	result := getDateInTimezone(utcTime)

	// Should be 8 hours later
	expected := time.Date(2024, 1, 15, 8, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("getDateInTimezone() = %v, want %v", result, expected)
	}
}
