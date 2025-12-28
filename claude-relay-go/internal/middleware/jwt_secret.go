package middleware

import (
	"fmt"
	"strings"

	"github.com/catstream/claude-relay-go/internal/config"
)

func requiredJWTSecret() ([]byte, error) {
	if config.Cfg == nil {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}

	secret := strings.TrimSpace(config.Cfg.Security.JWTSecret)
	if secret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}

	return []byte(secret), nil
}
