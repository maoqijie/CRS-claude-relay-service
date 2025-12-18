package logger

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	// Log 全局日志实例
	Log *zap.Logger
	// Sugar 语法糖日志实例
	Sugar *zap.SugaredLogger
)

// Init 初始化日志系统
func Init(env string, logDir string) error {
	var config zap.Config

	if env == "production" {
		config = zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	} else {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	// 确保日志目录存在
	if logDir != "" {
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return err
		}
		logFile := filepath.Join(logDir, "go-relay.log")
		config.OutputPaths = append(config.OutputPaths, logFile)
	}

	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var err error
	Log, err = config.Build()
	if err != nil {
		return err
	}

	Sugar = Log.Sugar()
	return nil
}

// Sync 刷新日志缓冲
func Sync() {
	if Log != nil {
		_ = Log.Sync()
	}
}

// 便捷方法
func Info(msg string, fields ...zap.Field) {
	Log.Info(msg, fields...)
}

func Error(msg string, fields ...zap.Field) {
	Log.Error(msg, fields...)
}

func Debug(msg string, fields ...zap.Field) {
	Log.Debug(msg, fields...)
}

func Warn(msg string, fields ...zap.Field) {
	Log.Warn(msg, fields...)
}

func Fatal(msg string, fields ...zap.Field) {
	Log.Fatal(msg, fields...)
}

// Database 数据库操作日志 (对应 Node.js 的 logger.database)
func Database(msg string, fields ...zap.Field) {
	Log.Debug(msg, append(fields, zap.String("type", "database"))...)
}
