# Claude Relay Service - Node.js 到 Go 重构总体方案

## 1. 项目现状分析

### 1.1 代码规模统计

| 目录 | 文件数 | 代码行数 | 说明 |
|------|--------|----------|------|
| src/services/ | 44 | ~38,000 | 核心业务逻辑 |
| src/routes/ | 15+ | ~12,000 | HTTP 路由处理 |
| src/models/ | 3 | ~4,500 | 数据访问层 |
| src/utils/ | 20+ | ~4,000 | 工具函数 |
| src/middleware/ | 3 | ~2,500 | 中间件 |
| src/handlers/ | 1 | ~2,600 | 请求处理器 |
| **总计** | **~120** | **~62,000** | - |

### 1.2 核心模块依赖图

```
┌─────────────────────────────────────────────────────────────────────┐
│                           HTTP Layer                                 │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────────┐   │
│  │ /api/*  │ │/claude/*│ │/gemini/*│ │/openai/*│ │  /admin/*   │   │
│  └────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘ └──────┬──────┘   │
└───────┼──────────┼──────────┼──────────┼───────────────┼───────────┘
        │          │          │          │               │
        ▼          ▼          ▼          ▼               ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        Middleware Layer                              │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐  │
│  │ authenticateApiKey│  │  rateLimiter     │  │  concurrencyCtrl │  │
│  └────────┬─────────┘  └────────┬─────────┘  └────────┬─────────┘  │
└───────────┼─────────────────────┼─────────────────────┼─────────────┘
            │                     │                     │
            ▼                     ▼                     ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         Service Layer                                │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────┐  │
│  │ Relay Services  │  │ Account Services│  │ Scheduler Services  │  │
│  │ ├─claude        │  │ ├─claude        │  │ ├─unifiedClaude     │  │
│  │ ├─gemini        │  │ ├─gemini        │  │ ├─unifiedGemini     │  │
│  │ ├─openai        │  │ ├─openai        │  │ └─unifiedOpenAI     │  │
│  │ ├─bedrock       │  │ ├─bedrock       │  │                     │  │
│  │ └─droid         │  │ └─droid         │  │                     │  │
│  └────────┬────────┘  └────────┬────────┘  └──────────┬──────────┘  │
└───────────┼─────────────────────┼─────────────────────┼─────────────┘
            │                     │                     │
            └─────────────────────┼─────────────────────┘
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                          Data Layer                                  │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                      redis.js (4295行)                       │   │
│  │  ├─ 连接管理 (Connection)                                    │   │
│  │  ├─ API Key 管理 (12个方法)                                  │   │
│  │  ├─ 使用统计 (18个方法)                                      │   │
│  │  ├─ 账户管理 (16个方法, 8种账户类型)                         │   │
│  │  ├─ 会话管理 (12个方法)                                      │   │
│  │  ├─ 并发控制 (12个方法, Lua脚本)                             │   │
│  │  ├─ 分布式锁 (2个方法, Lua脚本)                              │   │
│  │  └─ 请求排队 (12个方法)                                      │   │
│  └─────────────────────────────────────────────────────────────┘   │
│  ┌───────────────────┐  ┌───────────────────────────────────────┐  │
│  │ postgresStore.js  │  │              Redis Server              │  │
│  │   (双写持久化)     │  │           (共享数据层)                 │  │
│  └─────────┬─────────┘  └───────────────────────────────────────┘  │
└────────────┼────────────────────────────────────────────────────────┘
             ▼
┌─────────────────────────────────────────────────────────────────────┐
│                       PostgreSQL (持久化)                            │
└─────────────────────────────────────────────────────────────────────┘
```

### 1.3 大文件清单（重构重点）

| 文件 | 行数 | 复杂度 | 重构优先级 |
|------|------|--------|------------|
| redis.js | 4,295 | 极高 | P0 |
| claudeAccountService.js | 3,235 | 高 | P1 |
| geminiHandlers.js | 2,602 | 高 | P2 |
| usageStats.js (admin) | 2,576 | 中 | P2 |
| claudeRelayService.js | 2,563 | 高 | P1 |
| apiKeys.js (admin) | 2,380 | 中 | P2 |
| apiKeyService.js | 2,133 | 高 | P1 |
| auth.js (middleware) | 2,068 | 高 | P1 |
| unifiedClaudeScheduler.js | 1,825 | 高 | P1 |

### 1.4 部署架构特点

```
                    ┌─────────────────┐
                    │   负载均衡器     │
                    │  (香港/Nginx)   │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
              ▼              ▼              ▼
        ┌─────────┐   ┌─────────┐   ┌─────────┐
        │ 服务器1  │   │ 服务器2  │   │ 服务器N  │
        │ (美国)   │   │ (日本)   │   │  ...    │
        └────┬────┘   └────┬────┘   └────┬────┘
             │              │              │
             └──────────────┼──────────────┘
                            │
                     ┌──────▼──────┐
                     │   Redis     │
                     │ (服务器2)   │
                     └──────┬──────┘
                            │
                     ┌──────▼──────┐
                     │ PostgreSQL  │
                     │ (服务器2)   │
                     └─────────────┘
```

**关键约束：**
- 分布式部署，多服务器共享同一 Redis/PostgreSQL
- 所有服务器使用相同的加密算法
- 零停机迁移要求
- 数据一致性要求高

---

## 2. 重构目标

### 2.1 为什么选择 Go

| 维度 | Node.js 现状 | Go 目标 |
|------|-------------|---------|
| **性能** | 单线程，CPU 密集型任务受限 | 多核并行，原生高性能 |
| **并发** | 事件循环，回调/Promise | Goroutine，CSP 模型 |
| **内存** | V8 堆内存，GC 停顿 | 低内存占用，快速 GC |
| **部署** | 需要 Node.js 运行时 | 单二进制文件 |
| **类型** | 动态类型 (即使有 TS) | 静态类型，编译期检查 |
| **依赖** | node_modules 庞大 | go.mod 精简 |

### 2.2 重构原则

1. **渐进式迁移** - 按模块逐步替换，不做 Big Bang 重写
2. **数据兼容** - Redis Key 结构保持 100% 一致
3. **零停机** - 通过路由分流实现平滑切换
4. **可回滚** - 任何阶段都能快速回退到 Node.js
5. **功能对等** - 先实现功能对等，再优化增强

### 2.3 最终目标架构

```
┌─────────────────────────────────────────────────────────────────┐
│                      Go Relay Service                            │
├─────────────────────────────────────────────────────────────────┤
│  cmd/relay/main.go                    (入口)                     │
├─────────────────────────────────────────────────────────────────┤
│  internal/                                                       │
│  ├── api/                             (HTTP 处理层)              │
│  │   ├── routes/                      (路由定义)                 │
│  │   ├── middleware/                  (中间件)                   │
│  │   └── handlers/                    (请求处理器)               │
│  ├── service/                         (业务逻辑层)               │
│  │   ├── relay/                       (转发服务)                 │
│  │   ├── account/                     (账户服务)                 │
│  │   ├── scheduler/                   (调度服务)                 │
│  │   └── apikey/                      (API Key 服务)            │
│  ├── storage/                         (数据访问层)               │
│  │   ├── redis/                       (Redis 操作)              │
│  │   └── postgres/                    (PostgreSQL 操作)         │
│  ├── config/                          (配置管理)                 │
│  └── pkg/                             (内部公共包)               │
├─────────────────────────────────────────────────────────────────┤
│  pkg/                                 (可导出公共包)              │
│  ├── types/                           (类型定义)                 │
│  └── utils/                           (工具函数)                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## 3. 重构路线图

### 3.1 总体阶段划分

```
阶段0        阶段1         阶段2         阶段3         阶段4        阶段5
准备工作 ──▶ 数据层 ──▶ 核心服务 ──▶ 转发服务 ──▶ 管理后台 ──▶ 完全切换
(1周)       (2-3周)      (3-4周)       (3-4周)       (2-3周)      (1-2周)

[Go骨架]    [redis.go]   [apikey]     [claude]     [admin API] [删除Node]
[配置]      [postgres]   [scheduler]  [gemini]     [dashboard] [优化]
[日志]                   [auth]       [openai]
                                      [bedrock]
```

### 3.2 详细阶段规划

#### 阶段 0：准备工作 (Week 1)

| 任务 | 说明 | 产出 |
|------|------|------|
| Go 项目初始化 | go mod, 目录结构 | claude-relay-go/ |
| 配置系统 | Viper, 环境变量, 与 Node 兼容 | internal/config/ |
| 日志系统 | Zap, 与现有日志格式兼容 | internal/pkg/logger/ |
| CI/CD 准备 | Makefile, Docker, 测试框架 | 构建脚本 |
| Nginx 配置 | 路由分流准备 | nginx.conf |

#### 阶段 1：数据访问层 (Week 2-4)

| 任务 | 说明 | 产出 |
|------|------|------|
| Redis 客户端 | 连接管理, 通用操作 | storage/redis/client.go |
| API Key 操作 | CRUD, Hash 映射, 分页 | storage/redis/apikey.go |
| 使用统计 | Token 计数, 成本统计 | storage/redis/usage.go |
| 并发控制 | Lua 脚本, 租约机制 | storage/redis/concurrency.go |
| 分布式锁 | SET NX, Lua 释放 | storage/redis/lock.go |
| PostgreSQL | 双写逻辑 | storage/postgres/ |
| **验证点** | 独立运行 Go 服务，读写 Redis 数据 | - |

#### 阶段 2：核心服务层 (Week 5-8)

| 任务 | 说明 | 产出 |
|------|------|------|
| API Key 服务 | 验证, 限流, 权限检查 | service/apikey/ |
| 认证中间件 | authenticateApiKey 完整逻辑 | api/middleware/auth.go |
| 统一调度器 | Claude/Gemini/OpenAI 调度 | service/scheduler/ |
| 账户服务 | 多账户类型管理 | service/account/ |
| **验证点** | Go 处理 /api/v1/models 等只读接口 | - |

#### 阶段 3：转发服务层 (Week 9-12)

| 任务 | 说明 | 产出 |
|------|------|------|
| Claude 转发 | 流式响应, Token 刷新 | service/relay/claude.go |
| Gemini 转发 | Google OAuth | service/relay/gemini.go |
| OpenAI 转发 | 格式转换 | service/relay/openai.go |
| Bedrock 转发 | AWS SDK | service/relay/bedrock.go |
| **验证点** | Go 处理 /api/v1/messages 核心转发 | - |

#### 阶段 4：管理后台 (Week 13-15)

| 任务 | 说明 | 产出 |
|------|------|------|
| Admin API | 账户管理, API Key 管理 | api/routes/admin/ |
| Dashboard | 统计数据接口 | api/handlers/dashboard.go |
| 用户系统 | 用户注册/登录 | service/user/ |
| **验证点** | Go 处理全部 /admin/* 接口 | - |

#### 阶段 5：完全切换 (Week 16-17)

| 任务 | 说明 | 产出 |
|------|------|------|
| 流量全切 | Nginx 全部指向 Go | - |
| 监控确认 | 性能, 错误率, 资源 | - |
| Node 下线 | 删除 Node.js 代码 | - |
| 文档更新 | README, CLAUDE.md | - |

---

## 4. 迁移策略

### 4.1 路由分流方案

```nginx
# /etc/nginx/conf.d/relay.conf

upstream node_backend {
    server 127.0.0.1:3000;
    keepalive 32;
}

upstream go_backend {
    server 127.0.0.1:8080;
    keepalive 64;
}

server {
    listen 80;
    server_name api.fastaicode.top;

    # === 阶段 1: 只读接口先迁移 ===
    # location /api/v1/models {
    #     proxy_pass http://go_backend;
    # }

    # === 阶段 2: 核心转发迁移 ===
    # location /api/v1/messages {
    #     proxy_pass http://go_backend;
    # }

    # === 阶段 3: 全部迁移 ===
    # location / {
    #     proxy_pass http://go_backend;
    # }

    # 默认: Node.js
    location / {
        proxy_pass http://node_backend;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_read_timeout 600s;
    }
}
```

### 4.2 回滚策略

```bash
# 任何阶段出问题，10秒内回滚

# 1. 修改 Nginx 配置，注释 Go 路由
vim /etc/nginx/conf.d/relay.conf

# 2. 重载 Nginx
nginx -s reload

# 3. 确认流量回到 Node.js
curl -I http://localhost:3000/health
```

### 4.3 数据兼容保证

| 维度 | 策略 |
|------|------|
| **Redis Key** | Go 使用完全相同的 Key 命名规范 |
| **数据类型** | 字符串/Hash/Sorted Set 完全兼容 |
| **加密算法** | AES 加密参数一致 |
| **时区处理** | 使用相同的时区偏移逻辑 |
| **Lua 脚本** | 复用现有脚本，不做修改 |

---

## 5. 技术选型

### 5.1 Go 依赖库

```go
// go.mod
module github.com/yourorg/claude-relay-go

go 1.22

require (
    // Web 框架
    github.com/gin-gonic/gin v1.9.1

    // Redis
    github.com/redis/go-redis/v9 v9.5.1

    // PostgreSQL
    github.com/jackc/pgx/v5 v5.5.5

    // 配置
    github.com/spf13/viper v1.18.2

    // 日志
    go.uber.org/zap v1.27.0

    // HTTP 客户端
    github.com/valyala/fasthttp v1.52.0

    // AWS SDK (Bedrock)
    github.com/aws/aws-sdk-go-v2 v1.25.0

    // JWT
    github.com/golang-jwt/jwt/v5 v5.2.0

    // UUID
    github.com/google/uuid v1.6.0

    // 测试
    github.com/stretchr/testify v1.9.0
)
```

### 5.2 与 Node.js 对应关系

| Node.js | Go |
|---------|-----|
| express | gin |
| ioredis | go-redis |
| pg | pgx |
| winston | zap |
| axios | net/http / fasthttp |
| jsonwebtoken | golang-jwt |
| dotenv | viper |
| bcryptjs | golang.org/x/crypto/bcrypt |

---

## 6. 风险管理

### 6.1 技术风险

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| Lua 脚本行为差异 | 中 | 高 | 编写完整兼容性测试 |
| 流式响应处理差异 | 中 | 高 | 逐字节对比测试 |
| OAuth Token 刷新逻辑 | 低 | 高 | 复用 Node.js 测试用例 |
| 加密算法不一致 | 低 | 极高 | 双向验证测试 |
| 并发竞态条件 | 中 | 中 | 压力测试 + 代码审查 |

### 6.2 运营风险

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| 迁移期间服务中断 | 低 | 高 | 路由级灰度，秒级回滚 |
| 性能回退 | 低 | 中 | 基准测试，持续监控 |
| 功能遗漏 | 中 | 中 | 完整功能清单对照 |
| 团队学习曲线 | 中 | 低 | 文档 + 代码示例 |

---

## 7. 成功指标

### 7.1 功能指标

- [ ] 100% API 接口兼容
- [ ] 100% Redis Key 结构兼容
- [ ] 100% 现有测试用例通过

### 7.2 性能指标

| 指标 | Node.js 基准 | Go 目标 |
|------|-------------|---------|
| QPS (单机) | ~5,000 | >15,000 |
| P99 延迟 | ~50ms | <20ms |
| 内存占用 | ~500MB | <200MB |
| CPU 使用率 | 60% (单核) | 40% (多核) |

### 7.3 运营指标

- [ ] 零停机完成迁移
- [ ] 错误率 < 0.01%
- [ ] 用户无感知

---

## 8. 附录

### 8.1 已创建的 Go 项目结构

```
claude-relay-go/
├── cmd/
│   └── relay/          # 主程序入口
├── internal/
│   ├── config/         # 配置管理
│   ├── storage/
│   │   ├── redis/      # Redis 操作
│   │   │   └── scripts/ # Lua 脚本
│   │   └── postgres/   # PostgreSQL 操作
│   └── utils/          # 内部工具
└── pkg/                # 可导出包
```

### 8.2 相关文档

- [Redis 模块重构详细方案](./01-redis-module.md)
- [第一步实施指南](./01-step1-foundation.md)

---

**文档版本**: v1.0
**创建日期**: 2024-12-18
**维护者**: Claude Relay Team
