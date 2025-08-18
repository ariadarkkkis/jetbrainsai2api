# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

这是一个用 Go 语言编写的高性能 JetBrains AI 转 OpenAI 兼容 API 的代理服务器。它使用 Gin 框架，支持将 JetBrains AI 接口转换为 OpenAI 格式，方便与现有 OpenAI 客户端集成，并包含一个用于监控和统计的前端界面。

**重要**: 项目已完成重大重构 (v2024.8)，统一使用 ByteDance Sonic JSON 库，提升 JSON 序列化性能 2-5x，并消除了代码重复。

## 开发命令

### 本地开发
```bash
# 下载依赖
go mod download

# 验证依赖
go mod verify

# 整理依赖
go mod tidy

# 构建可执行文件
go build -o jetbrainsai2api *.go

# 直接运行源码（开发时推荐）
go run *.go

# 运行构建后的可执行文件（默认端口 7860）
./jetbrainsai2api

# 开发模式运行（带详细调试信息）
GIN_MODE=debug ./jetbrainsai2api

# 生产模式运行
GIN_MODE=release ./jetbrainsai2api

# 代码格式化
go fmt ./...

# 代码检查
go vet ./...

# 性能分析（启动后访问 http://localhost:6060/debug/pprof/）
# pprof 会在 main.go 中自动启用
```

### 环境配置
```bash
# 复制并编辑环境变量文件
cp .env.example .env
# 编辑 .env 文件，配置必要的 API 密钥和账户信息
```

### Docker 部署
```bash
# 构建 Docker 镜像
docker build -t jetbrainsai2api .

# 运行 Docker 容器
docker run -p 7860:7860 \
  -e TZ=Asia/Shanghai \
  -e CLIENT_API_KEYS=your-api-key \
  -e JETBRAINS_LICENSE_IDS=your-license-id \
  -e JETBRAINS_AUTHORIZATIONS=your-auth-token \
  jetbrainsai2api

# 使用 docker-compose
docker-compose up -d
```

## 核心架构

### 项目文件结构
- `main.go`: 主程序入口，HTTP 服务器初始化、pprof 性能分析、信号处理
- `routes.go`: API 路由定义和中间件配置（CORS、请求超时等）
- `handlers.go`: HTTP 请求处理器，包含认证逻辑和工具调用处理
- `jetbrains_api.go`: JetBrains API 交互和账户管理，JWT 刷新机制
- `converter.go`: OpenAI 与 JetBrains API 格式转换，消息类型处理
- `response_handler.go`: 响应处理，支持流式和非流式输出
- `tools_validator.go`: 工具参数验证和转换，确保 JetBrains API 兼容性
- `image_validator.go`: 图像验证和处理，支持 v8 API 中的媒体消息
- `models.go`: 数据结构定义（请求、响应、账户等模型）
- `config.go`: 配置文件加载（models.json），模型映射管理
- `stats.go`: 统计数据收集和管理，QPS 监控、成功率统计
- `storage.go`: 数据持久化存储，统计数据异步保存
- `cache.go`: LRU 缓存实现，提供消息转换、工具验证和配额查询缓存
- `performance.go`: 性能监控功能，错误率计算和指标收集
- `utils.go`: 工具函数，包含通用辅助方法

### 核心组件

- **HTTP 服务器** (`main.go`): 基于 Gin 框架，集成 pprof 性能分析、优雅停机处理
- **统一 JSON 处理** (`cache.go` `marshalJSON`): 全面使用 ByteDance Sonic 库，提升性能 2-5x
- **账户池管理** (`jetbrains_api.go`): 
  - 多账户负载均衡和故障转移
  - 智能 JWT 刷新（过期前12小时自动刷新）
  - 配额实时监控和账户健康检查
  - 支持静态 JWT 和许可证两种认证模式
- **LRU 缓存系统** (`cache.go`):
  - 消息转换缓存 (10分钟 TTL)
  - 工具验证缓存 (30分钟 TTL) 
  - 配额查询缓存 (1小时 TTL)
  - 可选 Redis 支持，提高分布式性能
- **API 格式转换** (`converter.go`):
  - OpenAI 与 JetBrains API 双向格式转换
  - 支持多种消息类型和复杂参数结构
  - 图像内容验证和处理 (`image_validator.go`)
- **工具调用优化** (`tools_validator.go`):
  - 智能工具参数验证和名称规范化
  - 复杂 JSON Schema 结构自动简化（anyOf/oneOf/allOf）
  - 强制工具使用机制，提高调用成功率
  - **重构亮点**: 统一使用 Sonic 库，移除 encoding/json 依赖
- **性能监控** (`stats.go`, `performance.go`):
  - 实时 QPS、响应时间、成功率统计
  - 错误率计算和异常检测
  - Web 界面监控面板 (`static/index.html`)
  - 统计数据持久化存储 (`storage.go`)

## 重构关键点 (v2024.8)

### JSON 序列化优化
- **问题**: 代码中混用 `encoding/json` 和 `github.com/bytedance/sonic`
- **解决**: 统一使用 `marshalJSON` 函数封装 Sonic 库
- **影响文件**: `tools_validator.go`, `cache.go`, `converter.go`
- **性能提升**: JSON 序列化性能提升 2-5x

### 代码一致性改进
- 消除重复的 JSON 处理逻辑
- 统一错误处理模式
- 移除未使用的导入（如 `encoding/json`）
- 强化类型安全检查

## 重要配置文件

### models.json
定义可用模型及其映射关系，支持以下模型类型：
- **Anthropic Claude**: claude-4-opus, claude-4-sonnet, claude-3-7-sonnet 等
- **Google Gemini**: gemini-2.5-pro, gemini-2.5-flash
- **OpenAI**: o4-mini, o3-mini, o3, o1, gpt-4o, gpt-4.1 系列

格式：`"api_model_id": "jetbrains_internal_model_id"`

### .env 文件
必须配置的环境变量（参考 `.env.example`）：
- `CLIENT_API_KEYS`: 客户端 API 密钥（逗号分隔）
- `JETBRAINS_JWTS`: JWT token（逗号分隔，用于静态 JWT 模式）
- `JETBRAINS_LICENSE_IDS`: JetBrains 许可证 ID（逗号分隔，用于许可证模式）
- `JETBRAINS_AUTHORIZATIONS`: 对应许可证的授权 token（逗号分隔）
- `PORT`: 服务端口（默认 7860）
- `GIN_MODE`: Gin 框架模式（debug/release/test）
- `REDIS_URL`: Redis 连接 URL（可选，用于缓存）

支持三种账户配置模式：
1. **静态 JWT 模式**: 只设置 `JETBRAINS_JWTS`
2. **许可证模式**: 设置 `JETBRAINS_LICENSE_IDS` 和 `JETBRAINS_AUTHORIZATIONS`
3. **混合模式**: 可同时配置多种方式的账户

## 调试和故障排除

### 性能分析和调试工具
- **pprof 性能分析**: 服务启动时自动在端口 6060 启用，访问 `http://localhost:6060/debug/pprof/`
  - CPU 分析: `curl http://localhost:6060/debug/pprof/profile?seconds=30`
  - 内存分析: `go tool pprof http://localhost:6060/debug/pprof/heap`
  - goroutine 分析: `curl http://localhost:6060/debug/pprof/goroutine?debug=1`
- **实时缓存监控**: 通过统计面板查看缓存命中率和性能指标
- **错误率监控**: `performance.go` 提供错误率计算和异常检测
- **JSON 性能监控**: 重构后使用 Sonic 库，可通过 expvar 监控序列化性能

### 日志和监控
- 设置 `GIN_MODE=debug` 启用详细日志
- 访问 `/` 查看实时统计面板
- 使用 `/api/stats` 获取 JSON 格式的统计数据
- 使用 `/api/health` 检查服务健康状态

### 常见问题
- **JSON 序列化错误**: 确保所有文件都使用 `marshalJSON` 函数而非直接调用 `json.Marshal`
- **JWT 过期**: 服务会自动刷新 JWT token（过期前12小时），检查许可证配置
- **配额不足**: 查看统计面板中的配额信息，考虑添加更多账户
- **模型不可用**: 检查 `models.json` 中的模型映射配置
- **缓存问题**: 检查 Redis 连接状态，LRU 缓存会自动降级到内存
- **账户池耗尽**: 所有账户都不可用时，检查账户配置和网络连接
- **工具调用失败**: 
  - 检查工具参数名称是否符合规范（最大64字符，仅支持字母数字和 `_.-`）
  - 启用调试模式 `GIN_MODE=debug` 查看详细的工具验证和转换日志
  - 复杂嵌套参数会自动简化，检查转换后的参数结构
  - 确保 `tool_choice` 参数正确设置（支持 "auto", "required", "any" 等）

### 开发最佳实践
- **JSON 处理**: 始终使用 `marshalJSON` 函数，不要直接调用 `sonic.Marshal`
- **缓存键生成**: 使用现有的缓存键生成函数，确保一致性
- **错误处理**: 遵循项目中统一的错误处理模式
- **性能考虑**: 利用现有的缓存系统，避免重复计算

## 部署命令

### Docker 部署
```bash
# 构建 Docker 镜像
docker build -t jetbrainsai2api .

# 运行 Docker 容器
docker run -p 7860:7860 \
  -e TZ=Asia/Shanghai \
  -e CLIENT_API_KEYS=your-api-key \
  -e JETBRAINS_JWTS=your-jwt-token \
  jetbrainsai2api
```

### HuggingFace Spaces 部署
- Fork 项目到 GitHub
- 在 HuggingFace Spaces 创建新的 Space (使用 Docker SDK)
- 连接 GitHub 仓库并配置环境变量
