# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

这是一个用 Go 语言编写的 JetBrains AI 转 OpenAI 兼容 API 的代理服务器。它使用 Gin 框架，支持将 JetBrains AI 接口转换为 OpenAI 格式，方便与现有 OpenAI 客户端集成，并包含一个用于监控和统计的前端界面。

## 开发命令

### Go 版本
```bash
# 构建可执行文件
go build -o jetbrainsai2api *.go

# 运行 Go 版本（默认端口 7860）
./jetbrainsai2api

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

## 核心架构

- **Go 版本** (`main.go`): 基于高性能 Gin 框架实现，包含请求统计和 Web 监控界面。
- **认证与账户管理** (`jetbrains_api.go`, `handlers.go`):
  - 支持多种认证方式： Bearer token、`x-api-key` 头部。
  - JetBrains 账户轮询机制，自动处理 JWT 刷新和配额检查。
- **模型映射系统** (`models.json`, `config.go`):
  - `models.json` 配置文件定义 API 模型 ID 到 JetBrains 内部模型的映射。
  - 支持 Anthropic 风格的模型名称映射。
- **API 端点兼容性** (`routes.go`):
  - **OpenAI 兼容**: `/v1/models`、`/v1/chat/completions`
  - **Anthropic 兼容**: `/v1/messages`
  - 支持流式和非流式响应。
- **消息格式转换** (`converter.go`):
  - 在 OpenAI/Anthropic API 格式和 JetBrains 内部格式之间进行转换。
- **监控与统计** (`stats.go`, `static/index.html`):
  - Web 界面统计面板，通过 `/` 访问。
  - 实时 QPS 监控、请求成功率统计。
  - Token 配额监控和过期预警。
  - 统计数据通过 `/api/stats` 端点以 JSON 格式提供。

## 重要配置文件

### models.json
定义可用模型及其映射关系。

### .env 文件
必须配置的环境变量：
- `CLIENT_API_KEYS`: 客户端 API 密钥（逗号分隔）。
- `JETBRAINS_LICENSE_IDS`: JetBrains 许可证 ID（逗号分隔）。
- `JETBRAINS_AUTHORIZATIONS`: JetBrains 授权 token（逗号分隔）。
- `PORT`: 服务端口（默认 7860）。

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
