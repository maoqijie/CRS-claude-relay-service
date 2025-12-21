package redis

import (
	"testing"
	"time"
)

func TestAPIKeyToMap(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)
	lastUsedAt := now

	apiKey := &APIKey{
		ID:                              "test-id-123",
		Name:                            "Test API Key",
		HashedKey:                       "abc123hash",
		APIKey:                          "abc123hash",
		Limit:                           1000,
		IsActive:                        true,
		CreatedAt:                       now,
		ExpiresAt:                       &expiresAt,
		LastUsedAt:                      &lastUsedAt,
		Description:                     "Test description",
		Permissions:                     []string{"claude", "gemini"},
		AllowedClients:                  []string{"ClaudeCode"},
		ModelBlacklist:                  []string{"claude-3-opus"},
		ConcurrentLimit:                 5,
		ConcurrentRequestQueueEnabled:   true,
		ConcurrentRequestQueueMaxSize:   10,
		ConcurrentRequestQueueTimeoutMs: 5000,
		UserID:                          "user-456",
	}

	result := apiKeyToMap(apiKey)

	// Test required fields
	if result["id"] != "test-id-123" {
		t.Errorf("expected id 'test-id-123', got '%v'", result["id"])
	}
	if result["name"] != "Test API Key" {
		t.Errorf("expected name 'Test API Key', got '%v'", result["name"])
	}
	if result["hashedKey"] != "abc123hash" {
		t.Errorf("expected hashedKey 'abc123hash', got '%v'", result["hashedKey"])
	}

	// Test numeric fields (stored as strings)
	if result["limit"] != "1000" {
		t.Errorf("expected limit '1000', got '%v'", result["limit"])
	}
	if result["concurrentLimit"] != "5" {
		t.Errorf("expected concurrentLimit '5', got '%v'", result["concurrentLimit"])
	}

	// Test boolean fields (stored as strings)
	if result["isActive"] != "true" {
		t.Errorf("expected isActive 'true', got '%v'", result["isActive"])
	}
	if result["concurrentRequestQueueEnabled"] != "true" {
		t.Errorf("expected concurrentRequestQueueEnabled 'true', got '%v'", result["concurrentRequestQueueEnabled"])
	}

	// Test JSON array fields
	if result["permissions"] != `["claude","gemini"]` {
		t.Errorf("expected permissions JSON array, got '%v'", result["permissions"])
	}
	if result["allowedClients"] != `["ClaudeCode"]` {
		t.Errorf("expected allowedClients JSON array, got '%v'", result["allowedClients"])
	}

	// Test optional fields
	if result["userId"] != "user-456" {
		t.Errorf("expected userId 'user-456', got '%v'", result["userId"])
	}
	if result["description"] != "Test description" {
		t.Errorf("expected description 'Test description', got '%v'", result["description"])
	}
}

func TestMapToAPIKey(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	nowStr := now.Format(time.RFC3339)

	data := map[string]string{
		"id":                              "test-id-456",
		"name":                            "Another Key",
		"hashedKey":                       "xyz789hash",
		"apiKey":                          "xyz789hash",
		"limit":                           "500",
		"isActive":                        "true",
		"createdAt":                       nowStr,
		"expiresAt":                       nowStr,
		"lastUsedAt":                      nowStr,
		"description":                     "Some description",
		"permissions":                     `["openai"]`,
		"allowedClients":                  `["GeminiCLI"]`,
		"modelBlacklist":                  `["gpt-4"]`,
		"concurrentLimit":                 "3",
		"concurrentRequestQueueEnabled":   "true",
		"concurrentRequestQueueMaxSize":   "5",
		"concurrentRequestQueueTimeoutMs": "3000",
		"userId":                          "user-789",
	}

	apiKey := mapToAPIKey(data)

	// Test basic fields
	if apiKey.ID != "test-id-456" {
		t.Errorf("expected ID 'test-id-456', got '%s'", apiKey.ID)
	}
	if apiKey.Name != "Another Key" {
		t.Errorf("expected Name 'Another Key', got '%s'", apiKey.Name)
	}
	if apiKey.HashedKey != "xyz789hash" {
		t.Errorf("expected HashedKey 'xyz789hash', got '%s'", apiKey.HashedKey)
	}

	// Test numeric fields
	if apiKey.Limit != 500 {
		t.Errorf("expected Limit 500, got %d", apiKey.Limit)
	}
	if apiKey.ConcurrentLimit != 3 {
		t.Errorf("expected ConcurrentLimit 3, got %d", apiKey.ConcurrentLimit)
	}

	// Test boolean fields
	if !apiKey.IsActive {
		t.Error("expected IsActive true")
	}
	if !apiKey.ConcurrentRequestQueueEnabled {
		t.Error("expected ConcurrentRequestQueueEnabled true")
	}

	// Test array fields
	if len(apiKey.Permissions) != 1 || apiKey.Permissions[0] != "openai" {
		t.Errorf("expected Permissions ['openai'], got %v", apiKey.Permissions)
	}
	if len(apiKey.AllowedClients) != 1 || apiKey.AllowedClients[0] != "GeminiCLI" {
		t.Errorf("expected AllowedClients ['GeminiCLI'], got %v", apiKey.AllowedClients)
	}

	// Test user ID
	if apiKey.UserID != "user-789" {
		t.Errorf("expected UserID 'user-789', got '%s'", apiKey.UserID)
	}
}

func TestMapToAPIKeyDefaults(t *testing.T) {
	// Test with minimal data - should use defaults
	data := map[string]string{
		"id":   "minimal-id",
		"name": "Minimal Key",
	}

	apiKey := mapToAPIKey(data)

	// Check defaults
	if apiKey.IsActive {
		t.Error("expected IsActive false by default")
	}
	if apiKey.Limit != 0 {
		t.Errorf("expected Limit 0 by default, got %d", apiKey.Limit)
	}
	if len(apiKey.Permissions) != 0 {
		t.Errorf("expected empty Permissions, got %v", apiKey.Permissions)
	}
}

func TestAPIKeyRoundTrip(t *testing.T) {
	// Test that converting to map and back preserves data
	now := time.Now().Truncate(time.Second)

	original := &APIKey{
		ID:              "roundtrip-id",
		Name:            "Round Trip Test",
		HashedKey:       "roundtriphash",
		APIKey:          "roundtriphash",
		Limit:           2000,
		IsActive:        true,
		CreatedAt:       now,
		Description:     "Round trip description",
		Permissions:     []string{"claude", "gemini", "openai"},
		AllowedClients:  []string{"ClaudeCode", "GeminiCLI"},
		ModelBlacklist:  []string{"claude-3-opus", "gpt-4"},
		ConcurrentLimit: 10,
		UserID:          "user-roundtrip",
	}

	// Convert to map
	m := apiKeyToMap(original)

	// Convert interface{} map to string map for mapToAPIKey
	stringMap := make(map[string]string)
	for k, v := range m {
		if s, ok := v.(string); ok {
			stringMap[k] = s
		}
	}

	// Convert back
	result := mapToAPIKey(stringMap)

	// Compare core fields
	if result.ID != original.ID {
		t.Errorf("ID mismatch: got %s, want %s", result.ID, original.ID)
	}
	if result.Name != original.Name {
		t.Errorf("Name mismatch: got %s, want %s", result.Name, original.Name)
	}
	if result.HashedKey != original.HashedKey {
		t.Errorf("HashedKey mismatch: got %s, want %s", result.HashedKey, original.HashedKey)
	}
	if result.Limit != original.Limit {
		t.Errorf("Limit mismatch: got %d, want %d", result.Limit, original.Limit)
	}
	if result.IsActive != original.IsActive {
		t.Errorf("IsActive mismatch: got %v, want %v", result.IsActive, original.IsActive)
	}
	if len(result.Permissions) != len(original.Permissions) {
		t.Errorf("Permissions length mismatch: got %d, want %d", len(result.Permissions), len(original.Permissions))
	}
}

func TestAPIKeyStruct(t *testing.T) {
	// Test that APIKey struct has all expected fields
	apiKey := APIKey{}

	// These assignments should compile without error
	apiKey.ID = "test"
	apiKey.Name = "name"
	apiKey.HashedKey = "hash"
	apiKey.APIKey = "key"
	apiKey.Limit = 100
	apiKey.UsedToday = 50
	apiKey.IsActive = true
	apiKey.CreatedAt = time.Now()
	apiKey.IsDeleted = false
	apiKey.Description = "desc"
	apiKey.Permissions = []string{"claude"}
	apiKey.AllowedClients = []string{"client"}
	apiKey.ModelBlacklist = []string{"model"}
	apiKey.ConcurrentLimit = 5
	apiKey.RateLimitPerMin = 60
	apiKey.RateLimitPerHour = 1000
	apiKey.ConcurrentRequestQueueEnabled = true
	apiKey.ConcurrentRequestQueueMaxSize = 10
	apiKey.ConcurrentRequestQueueTimeoutMs = 5000
	apiKey.UserID = "user"
	apiKey.Tags = []string{"tag1"}

	// Verify fields are set
	if apiKey.ID != "test" {
		t.Error("Failed to set ID")
	}
	if apiKey.ConcurrentRequestQueueEnabled != true {
		t.Error("Failed to set ConcurrentRequestQueueEnabled")
	}
	if apiKey.RateLimitPerMin != 60 {
		t.Errorf("Failed to set RateLimitPerMin, got %d", apiKey.RateLimitPerMin)
	}
}

func TestAPIKeyPaginatedStruct(t *testing.T) {
	paginated := APIKeyPaginated{
		Keys: []APIKey{
			{ID: "key1", Name: "Key 1"},
			{ID: "key2", Name: "Key 2"},
		},
		Total:      100,
		Page:       1,
		PageSize:   20,
		TotalPages: 5,
	}

	if len(paginated.Keys) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(paginated.Keys))
	}
	if paginated.Total != 100 {
		t.Errorf("Expected Total 100, got %d", paginated.Total)
	}
	if paginated.TotalPages != 5 {
		t.Errorf("Expected TotalPages 5, got %d", paginated.TotalPages)
	}
}

func TestAPIKeyStatsStruct(t *testing.T) {
	stats := APIKeyStats{
		TotalKeys:     100,
		ActiveKeys:    80,
		ExpiredKeys:   10,
		DeletedKeys:   5,
		KeysWithUsers: 70,
	}

	if stats.TotalKeys != 100 {
		t.Errorf("Expected TotalKeys 100, got %d", stats.TotalKeys)
	}
	if stats.ActiveKeys != 80 {
		t.Errorf("Expected ActiveKeys 80, got %d", stats.ActiveKeys)
	}
}

func TestAPIKeyQueryOptions(t *testing.T) {
	isActive := true
	opts := APIKeyQueryOptions{
		Page:           2,
		PageSize:       50,
		IncludeDeleted: true,
		UserID:         "user-123",
		Tags:           []string{"production", "beta"},
		IsActive:       &isActive,
		Search:         "test",
		SortBy:         "createdAt",
		SortOrder:      "desc",
	}

	if opts.Page != 2 {
		t.Errorf("Expected Page 2, got %d", opts.Page)
	}
	if opts.PageSize != 50 {
		t.Errorf("Expected PageSize 50, got %d", opts.PageSize)
	}
	if !opts.IncludeDeleted {
		t.Error("Expected IncludeDeleted true")
	}
	if *opts.IsActive != true {
		t.Error("Expected IsActive true")
	}
}

func TestHasAnyTag(t *testing.T) {
	tests := []struct {
		name       string
		keyTags    []string
		searchTags []string
		expected   bool
	}{
		{
			name:       "matching tag",
			keyTags:    []string{"prod", "beta"},
			searchTags: []string{"beta"},
			expected:   true,
		},
		{
			name:       "no matching tag",
			keyTags:    []string{"prod", "beta"},
			searchTags: []string{"dev"},
			expected:   false,
		},
		{
			name:       "empty key tags",
			keyTags:    []string{},
			searchTags: []string{"prod"},
			expected:   false,
		},
		{
			name:       "empty search tags",
			keyTags:    []string{"prod"},
			searchTags: []string{},
			expected:   false,
		},
		{
			name:       "multiple matches",
			keyTags:    []string{"prod", "beta", "test"},
			searchTags: []string{"beta", "test"},
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasAnyTag(tt.keyTags, tt.searchTags)
			if result != tt.expected {
				t.Errorf("hasAnyTag() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestInterfaceToString(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "string",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "int",
			input:    42,
			expected: "42",
		},
		{
			name:     "int64",
			input:    int64(100),
			expected: "100",
		},
		{
			name:     "bool true",
			input:    true,
			expected: "true",
		},
		{
			name:     "bool false",
			input:    false,
			expected: "false",
		},
		{
			name:     "string slice",
			input:    []string{"a", "b"},
			expected: `["a","b"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := interfaceToString(tt.input)
			if result != tt.expected {
				t.Errorf("interfaceToString() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestNormalizeAPIKeyFieldUpdates_NoHashFields(t *testing.T) {
	updates := map[string]interface{}{
		"limit":    int64(123),
		"isActive": true,
	}

	stringUpdates, newHashValue, hashValueUpdated := normalizeAPIKeyFieldUpdates(updates)

	if hashValueUpdated {
		t.Errorf("expected hashValueUpdated false, got true")
	}
	if newHashValue != "" {
		t.Errorf("expected newHashValue empty, got %q", newHashValue)
	}
	if _, ok := stringUpdates["hashedKey"]; ok {
		t.Errorf("expected no hashedKey in updates, got %v", stringUpdates["hashedKey"])
	}
	if _, ok := stringUpdates["apiKey"]; ok {
		t.Errorf("expected no apiKey in updates, got %v", stringUpdates["apiKey"])
	}
	if v, ok := stringUpdates["limit"].(string); !ok || v != "123" {
		t.Errorf("expected limit '123', got %v", stringUpdates["limit"])
	}
	if v, ok := stringUpdates["isActive"].(string); !ok || v != "true" {
		t.Errorf("expected isActive 'true', got %v", stringUpdates["isActive"])
	}
}

func TestNormalizeAPIKeyFieldUpdates_APIKeySync(t *testing.T) {
	updates := map[string]interface{}{
		"apiKey": "newhash",
	}

	stringUpdates, newHashValue, hashValueUpdated := normalizeAPIKeyFieldUpdates(updates)

	if !hashValueUpdated {
		t.Errorf("expected hashValueUpdated true, got false")
	}
	if newHashValue != "newhash" {
		t.Errorf("expected newHashValue 'newhash', got %q", newHashValue)
	}
	if v, ok := stringUpdates["hashedKey"].(string); !ok || v != "newhash" {
		t.Errorf("expected hashedKey 'newhash', got %v", stringUpdates["hashedKey"])
	}
	if v, ok := stringUpdates["apiKey"].(string); !ok || v != "newhash" {
		t.Errorf("expected apiKey 'newhash', got %v", stringUpdates["apiKey"])
	}
}

func TestNormalizeAPIKeyFieldUpdates_HashedKeyPreferred(t *testing.T) {
	updates := map[string]interface{}{
		"hashedKey": "hashA",
		"apiKey":    "hashB",
	}

	stringUpdates, newHashValue, hashValueUpdated := normalizeAPIKeyFieldUpdates(updates)

	if !hashValueUpdated {
		t.Errorf("expected hashValueUpdated true, got false")
	}
	if newHashValue != "hashA" {
		t.Errorf("expected newHashValue 'hashA', got %q", newHashValue)
	}
	if v, ok := stringUpdates["hashedKey"].(string); !ok || v != "hashA" {
		t.Errorf("expected hashedKey 'hashA', got %v", stringUpdates["hashedKey"])
	}
	if v, ok := stringUpdates["apiKey"].(string); !ok || v != "hashA" {
		t.Errorf("expected apiKey 'hashA', got %v", stringUpdates["apiKey"])
	}
}

func TestNormalizeAPIKeyFieldUpdates_NilHashValue(t *testing.T) {
	updates := map[string]interface{}{
		"hashedKey": nil,
	}

	stringUpdates, newHashValue, hashValueUpdated := normalizeAPIKeyFieldUpdates(updates)

	if !hashValueUpdated {
		t.Errorf("expected hashValueUpdated true, got false")
	}
	if newHashValue != "" {
		t.Errorf("expected newHashValue empty, got %q", newHashValue)
	}
	if v, ok := stringUpdates["hashedKey"].(string); !ok || v != "" {
		t.Errorf("expected hashedKey empty string, got %v", stringUpdates["hashedKey"])
	}
	if v, ok := stringUpdates["apiKey"].(string); !ok || v != "" {
		t.Errorf("expected apiKey empty string, got %v", stringUpdates["apiKey"])
	}
}
