package config

import (
	"os"
	"testing"
)

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultVal   string
		envVal       string
		setEnv       bool
		expectedVal  string
	}{
		{
			name:        "环境变量存在",
			key:         "TEST_KEY",
			defaultVal:  "default",
			envVal:      "custom",
			setEnv:      true,
			expectedVal: "custom",
		},
		{
			name:        "环境变量不存在",
			key:         "TEST_KEY_NOT_EXISTS",
			defaultVal:  "default",
			envVal:      "",
			setEnv:      false,
			expectedVal: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				os.Setenv(tt.key, tt.envVal)
				defer os.Unsetenv(tt.key)
			}

			result := getEnv(tt.key, tt.defaultVal)
			if result != tt.expectedVal {
				t.Errorf("getEnv() = %v, want %v", result, tt.expectedVal)
			}
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultVal   int
		envVal       string
		setEnv       bool
		expectedVal  int
	}{
		{
			name:        "有效整数",
			key:         "TEST_INT",
			defaultVal:  8080,
			envVal:      "9000",
			setEnv:      true,
			expectedVal: 9000,
		},
		{
			name:        "无效整数",
			key:         "TEST_INT_INVALID",
			defaultVal:  8080,
			envVal:      "invalid",
			setEnv:      true,
			expectedVal: 8080, // 应返回默认值
		},
		{
			name:        "环境变量不存在",
			key:         "TEST_INT_NOT_EXISTS",
			defaultVal:  8080,
			envVal:      "",
			setEnv:      false,
			expectedVal: 8080,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				os.Setenv(tt.key, tt.envVal)
				defer os.Unsetenv(tt.key)
			}

			result := getEnvInt(tt.key, tt.defaultVal)
			if result != tt.expectedVal {
				t.Errorf("getEnvInt() = %v, want %v", result, tt.expectedVal)
			}
		})
	}
}

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultVal   bool
		envVal       string
		setEnv       bool
		expectedVal  bool
	}{
		{
			name:        "true 值",
			key:         "TEST_BOOL",
			defaultVal:  false,
			envVal:      "true",
			setEnv:      true,
			expectedVal: true,
		},
		{
			name:        "1 值",
			key:         "TEST_BOOL",
			defaultVal:  false,
			envVal:      "1",
			setEnv:      true,
			expectedVal: true,
		},
		{
			name:        "false 值",
			key:         "TEST_BOOL",
			defaultVal:  true,
			envVal:      "false",
			setEnv:      true,
			expectedVal: false,
		},
		{
			name:        "环境变量不存在",
			key:         "TEST_BOOL_NOT_EXISTS",
			defaultVal:  true,
			envVal:      "",
			setEnv:      false,
			expectedVal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				os.Setenv(tt.key, tt.envVal)
				defer os.Unsetenv(tt.key)
			}

			result := getEnvBool(tt.key, tt.defaultVal)
			if result != tt.expectedVal {
				t.Errorf("getEnvBool() = %v, want %v", result, tt.expectedVal)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	// 设置必需的环境变量
	os.Setenv("JWT_SECRET", "test_jwt_secret_32_characters_long")
	os.Setenv("ENCRYPTION_KEY", "test_encryption_key_32_chars_00")
	defer os.Unsetenv("JWT_SECRET")
	defer os.Unsetenv("ENCRYPTION_KEY")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}

	// 验证必需配置已加载
	if cfg.Security.JWTSecret != "test_jwt_secret_32_characters_long" {
		t.Errorf("JWT_SECRET = %v, want test_jwt_secret_32_characters_long", cfg.Security.JWTSecret)
	}

	if cfg.Security.EncryptionKey != "test_encryption_key_32_chars_00" {
		t.Errorf("ENCRYPTION_KEY = %v, want test_encryption_key_32_chars_00", cfg.Security.EncryptionKey)
	}

	// 验证默认值
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %v, want 8080", cfg.Server.Port)
	}

	if cfg.Redis.Host != "127.0.0.1" {
		t.Errorf("Redis.Host = %v, want 127.0.0.1", cfg.Redis.Host)
	}
}

func TestLoadWithoutRequiredConfig(t *testing.T) {
	// 清除必需的环境变量
	os.Unsetenv("JWT_SECRET")
	os.Unsetenv("ENCRYPTION_KEY")

	_, err := Load()
	if err == nil {
		t.Error("Load() should fail without JWT_SECRET")
	}

	// 只设置 JWT_SECRET
	os.Setenv("JWT_SECRET", "test_secret")
	defer os.Unsetenv("JWT_SECRET")

	_, err = Load()
	if err == nil {
		t.Error("Load() should fail without ENCRYPTION_KEY")
	}
}
