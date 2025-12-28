package middleware

import (
	"testing"

	"github.com/catstream/claude-relay-go/internal/config"
)

func TestNewAdminAuthMiddleware_RequiresJWTSecret(t *testing.T) {
	oldCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = oldCfg })

	config.Cfg = nil
	_, err := NewAdminAuthMiddleware(nil)
	if err == nil {
		t.Fatal("expected error when config is not loaded")
	}

	config.Cfg = &config.Config{
		Security: config.SecurityConfig{
			JWTSecret: "   ",
		},
	}
	_, err = NewAdminAuthMiddleware(nil)
	if err == nil {
		t.Fatal("expected error when JWT_SECRET is empty")
	}

	config.Cfg = &config.Config{
		Security: config.SecurityConfig{
			JWTSecret: " test-secret ",
		},
	}
	m, err := NewAdminAuthMiddleware(nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := string(m.jwtSecret); got != "test-secret" {
		t.Fatalf("jwt secret = %q, want %q", got, "test-secret")
	}
}

func TestNewUserAuthMiddleware_RequiresJWTSecret(t *testing.T) {
	oldCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = oldCfg })

	config.Cfg = nil
	_, err := NewUserAuthMiddleware(nil)
	if err == nil {
		t.Fatal("expected error when config is not loaded")
	}

	config.Cfg = &config.Config{
		Security: config.SecurityConfig{
			JWTSecret: "user-secret",
		},
		UserManagement: config.UserManagementConfig{
			Enabled: true,
		},
	}
	m, err := NewUserAuthMiddleware(nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := string(m.jwtSecret); got != "user-secret" {
		t.Fatalf("jwt secret = %q, want %q", got, "user-secret")
	}
	if !m.enabled {
		t.Fatal("expected user management enabled")
	}
}
