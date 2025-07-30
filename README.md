# JetBrains AI to OpenAI API Bridge

一个用 Go 语言编写的 JetBrains AI 转 OpenAI 兼容 API 的代理服务器。它使用 Gin 框架，支持将 JetBrains AI 接口转换为 OpenAI 格式，方便与现有 OpenAI 客户端集成，并包含一个用于监控和统计的前端界面。

## 功能特性

- **OpenAI 兼容 API**: 支持 `/v1/models` 和 `/v1/chat/completions` 端点
- **多种认证方式**: 支持 Bearer token 和 `x-api-key` 头部认证
- **账户轮询机制**: 自动处理 JWT 刷新和配额检查
- **模型映射系统**: 通过 `models.json` 配置文件灵活映射模型
- **工具调用支持**: 完整支持 OpenAI 工具调用 API，自动验证和转换工具参数
- **智能工具验证**: 自动验证工具参数名称和结构，确保 JetBrains API 兼容性
- **参数转换优化**: 智能简化复杂嵌套参数，支持 anyOf/oneOf/allOf 等复杂 JSON Schema
- **实时监控**: Web 界面统计面板，QPS 监控和配额预警
- **流式响应**: 支持流式和非流式输出
- **数据持久化**: 统计数据自动保存到 `stats.json`

## 支持的模型

- **Anthropic Claude**: claude-4-opus, claude-4-sonnet, claude-3-7-sonnet 等
- **Google Gemini**: gemini-2.5-pro, gemini-2.5-flash
- **OpenAI**: o4-mini, o3-mini, o3, o1, gpt-4o, gpt-4.1 系列

## 快速开始

### 环境配置

1. 复制环境变量文件：
```bash
cp .env.example .env
```

2. 编辑 `.env` 文件，配置必要的 API 密钥和账户信息：
```bash
CLIENT_API_KEYS=your-api-key
JETBRAINS_JWTS=your-jwt-token
# 或者使用许可证模式
JETBRAINS_LICENSE_IDS=your-license-id
JETBRAINS_AUTHORIZATIONS=your-auth-token
```

### 运行服务

#### Go 版本
```bash
# 构建可执行文件
go build -o jetbrainsai2api *.go

# 运行服务（默认端口 7860）
./jetbrainsai2api

# 开发模式运行（带调试信息）
GIN_MODE=debug ./jetbrainsai2api
```

#### Docker 部署
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

## API 使用

### 获取模型列表
```bash
curl -H "Authorization: Bearer your-api-key" \
  http://localhost:7860/v1/models
```

### 聊天补全
```bash
curl -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-4-sonnet",
    "messages": [{"role": "user", "content": "Hello!"}]
  }' \
  http://localhost:7860/v1/chat/completions
```

### 工具调用 (Function Calling)
```bash
curl -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-4-sonnet",
    "messages": [{"role": "user", "content": "What is the weather like in Beijing?"}],
    "tools": [{
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get current weather information",
        "parameters": {
          "type": "object",
          "properties": {
            "location": {"type": "string", "description": "City name"}
          },
          "required": ["location"]
        }
      }
    }],
    "tool_choice": "auto"
  }' \
  http://localhost:7860/v1/chat/completions
```

## 监控和统计

- **Web 界面**: 访问 `http://localhost:7860/` 查看实时统计面板
- **API 统计**: `GET /api/stats` 获取 JSON 格式的统计数据
- **健康检查**: `GET /api/health` 检查服务状态

## 配置文件

### models.json
定义可用模型及其映射关系：
```json
{
  "claude-4-sonnet": "anthropic/claude-4-sonnet",
  "gemini-2.5-pro": "google/gemini-2.5-pro"
}
```

### 环境变量
- `CLIENT_API_KEYS`: 客户端 API 密钥（逗号分隔）
- `JETBRAINS_JWTS`: JWT token（逗号分隔，静态 JWT 模式）
- `JETBRAINS_LICENSE_IDS`: JetBrains 许可证 ID（逗号分隔）
- `JETBRAINS_AUTHORIZATIONS`: 对应许可证的授权 token（逗号分隔）
- `PORT`: 服务端口（默认 7860）
- `GIN_MODE`: Gin 框架模式（debug/release/test）
- `REDIS_URL`: Redis 连接 URL（可选）

## 开发

### 运行测试
```bash
go test ./...
```

### 整理依赖
```bash
go mod tidy
```

## 部署

### HuggingFace Spaces
1. Fork 项目到 GitHub
2. 在 HuggingFace Spaces 创建新的 Space (使用 Docker SDK)
3. 连接 GitHub 仓库并配置环境变量

## 工具调用特性

### 智能参数验证
- **参数名称验证**: 自动检查工具参数名称是否符合 JetBrains API 要求（最大64字符，仅支持字母数字和 `_.-`）
- **复杂结构简化**: 智能处理 `anyOf`、`oneOf`、`allOf` 等复杂 JSON Schema 结构
- **嵌套对象优化**: 对于过于复杂的嵌套参数，自动转换为字符串格式以确保兼容性

### 自动转换功能
- **参数名称转换**: 自动修正不符合规范的参数名称
- **结构优化**: 对于超过10个属性的复杂工具，自动简化为单一字符串参数
- **类型适配**: 将不兼容的参数类型转换为 JetBrains AI 支持的格式

### 调试信息
在开发模式下（`GIN_MODE=debug`），系统会输出详细的工具验证和转换日志，帮助开发者理解转换过程。

## 故障排除

- **JWT 过期**: 服务会自动刷新 JWT token，检查许可证配置
- **配额不足**: 查看统计面板中的配额信息，考虑添加更多账户
- **模型不可用**: 检查 `models.json` 中的模型映射配置
- **工具调用失败**: 检查工具参数是否符合 JetBrains API 规范，启用调试模式查看详细日志

## 许可证

MIT License