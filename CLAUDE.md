# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

这是一个用 Go 语言编写的 JetBrains AI 转 OpenAI 兼容 API 的代理服务器。它使用 Gin 框架，支持将 JetBrains AI 接口转换为 OpenAI 格式，方便与现有 OpenAI 客户端集成，并包含一个用于监控和统计的前端界面。

## 开发命令

### 本地开发
```bash
# 构建可执行文件
go build -o jetbrainsai2api *.go

# 运行服务（默认端口 7860）
./jetbrainsai2api

# 开发模式运行（带调试信息）
GIN_MODE=debug ./jetbrainsai2api

# 生产模式运行
GIN_MODE=release ./jetbrainsai2api

# 运行测试
go test ./...

# 整理依赖
go mod tidy
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
  -e JETBRAINS_JWTS=your-jwt-token \
  jetbrainsai2api

# 使用 docker-compose
docker-compose up -d
```

## 核心架构

### 项目文件结构
- `main.go`: 主程序入口，HTTP 服务器和全局配置初始化
- `routes.go`: API 路由定义和中间件配置
- `handlers.go`: HTTP 请求处理器，包含认证逻辑和工具调用处理
- `jetbrains_api.go`: JetBrains API 交互和账户管理
- `converter.go`: OpenAI 与 JetBrains API 格式转换
- `response_handler.go`: 响应处理，支持流式和非流式输出
- `tools_validator.go`: 工具参数验证和转换，确保 JetBrains API 兼容性
- `models.go`: 数据结构定义
- `config.go`: 配置文件加载（models.json）
- `stats.go`: 统计数据收集和管理
- `storage.go`: 数据持久化存储
- `utils.go`: 工具函数

### 核心组件

- **Go 版本** (`main.go`): 基于高性能 Gin 框架实现，包含请求统计和 Web 监控界面
- **认证与账户管理** (`jetbrains_api.go`, `handlers.go`):
  - 支持多种认证方式： Bearer token、`x-api-key` 头部
  - JetBrains 账户轮询机制，自动处理 JWT 刷新和配额检查
  - 支持静态 JWT 和许可证两种账户模式
- **模型映射系统** (`models.json`, `config.go`):
  - `models.json` 配置文件定义 API 模型 ID 到 JetBrains 内部模型的映射
  - 支持向后兼容的配置格式自动转换
- **API 端点兼容性** (`routes.go`):
  - **OpenAI 兼容**: `/v1/models`、`/v1/chat/completions`
  - 支持流式和非流式响应
  - 内置监控端点：`/api/stats`、`/api/health`
- **工具调用与验证** (`tools_validator.go`, `handlers.go`):
  - 完整支持 OpenAI 工具调用 API (Function Calling)
  - 智能工具参数验证和转换，确保 JetBrains API 兼容性
  - 自动处理复杂 JSON Schema 结构（anyOf/oneOf/allOf）
  - 参数名称规范化和结构优化
  - 强制工具使用机制，提高工具调用成功率
- **消息格式转换** (`converter.go`):
  - 在 OpenAI API 格式和 JetBrains 内部格式之间进行双向转换
  - 支持系统消息、用户消息等多种消息类型
- **监控与统计** (`stats.go`, `static/index.html`):
  - Web 界面统计面板，通过 `/` 访问
  - 实时 QPS 监控、请求成功率统计
  - Token 配额监控和过期预警
  - 统计数据通过 `/api/stats` 端点以 JSON 格式提供
  - 数据持久化存储到 `stats.json`

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

### 日志和监控
- 设置 `GIN_MODE=debug` 启用详细日志
- 访问 `/` 查看实时统计面板
- 使用 `/api/stats` 获取 JSON 格式的统计数据
- 使用 `/api/health` 检查服务健康状态

### 常见问题
- **JWT 过期**: 服务会自动刷新 JWT token，检查许可证配置
- **配额不足**: 查看统计面板中的配额信息，考虑添加更多账户
- **模型不可用**: 检查 `models.json` 中的模型映射配置
- **工具调用失败**: 
  - 检查工具参数名称是否符合规范（最大64字符，仅支持字母数字和 `_.-`）
  - 启用调试模式 `GIN_MODE=debug` 查看详细的工具验证和转换日志
  - 复杂嵌套参数会自动简化，检查转换后的参数结构
  - 确保 `tool_choice` 参数正确设置（支持 "auto", "required", "any" 等）

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
