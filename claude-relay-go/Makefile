.PHONY: build run test clean dev tidy fmt lint help

# 变量
BINARY_NAME=claude-relay-go
BUILD_DIR=./bin
MAIN_PATH=./cmd/relay

# 构建
build:
	@echo "🔨 Building..."
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "✅ Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# 开发模式运行
dev:
	@echo "🚀 Running in development mode..."
	@GO_PORT=8080 go run $(MAIN_PATH)/main.go

# 生产运行
run: build
	@echo "🚀 Running..."
	@$(BUILD_DIR)/$(BINARY_NAME)

# 测试
test:
	@echo "🧪 Running tests..."
	@go test -v ./...

# 清理
clean:
	@echo "🧹 Cleaning..."
	@rm -rf $(BUILD_DIR)
	@go clean

# 依赖整理
tidy:
	@echo "📦 Tidying dependencies..."
	@go mod tidy

# 格式化
fmt:
	@echo "🎨 Formatting code..."
	@go fmt ./...

# 代码检查
lint:
	@echo "🔍 Linting..."
	@golangci-lint run || echo "golangci-lint not installed, skipping..."

# 安装依赖
deps:
	@echo "📦 Installing dependencies..."
	@go mod download

# 帮助
help:
	@echo "Available commands:"
	@echo "  make build  - Build the binary"
	@echo "  make dev    - Run in development mode"
	@echo "  make run    - Build and run"
	@echo "  make test   - Run tests"
	@echo "  make clean  - Clean build artifacts"
	@echo "  make tidy   - Tidy go modules"
	@echo "  make fmt    - Format code"
	@echo "  make lint   - Run linter"
	@echo "  make deps   - Download dependencies"
