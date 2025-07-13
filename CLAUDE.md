# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

这是一个JetBrains AI转OpenAI兼容API的代理服务器，支持将JetBrains AI接口转换为OpenAI格式，方便与现有OpenAI客户端集成。项目包含Go和Python两个实现版本。

## 开发命令

### Go版本
```bash
# 构建可执行文件
go build -o jetbrainsai2api main.go

# 运行Go版本（默认端口7860）
./jetbrainsai2api

# 或直接运行
go run main.go

# 查看依赖
go mod tidy
```

### Python版本
```bash
# 安装依赖
cd python
pip install -r requirements.txt

# 运行Python版本（默认端口7860）
python main.py

# 使用uvicorn运行（推荐）
uvicorn main:app --host 0.0.0.0 --port 7860

# 测试工具调用功能
python testFC.py
```

### 环境配置
```bash
# 复制并编辑环境变量文件
cp .env.example .env
# 编辑.env文件，配置必要的API密钥和账户信息
```

## 核心架构

### 双语言实现
- **Go版本** (`main.go`): 高性能Gin框架实现，包含请求统计和Web监控界面
- **Python版本** (`python/main.py`): FastAPI实现，更易于扩展和调试

### 认证与账户管理
- 支持多种认证方式：Bearer token、x-api-key头部
- JetBrains账户轮询机制，自动处理JWT刷新和配额检查
- 环境变量配置：`CLIENT_API_KEYS`、`JETBRAINS_LICENSE_IDS`、`JETBRAINS_AUTHORIZATIONS`

### 模型映射系统
- `models.json`配置文件定义API模型ID到JetBrains内部模型的映射
- 支持Anthropic风格的模型名称映射（`anthropic_model_mappings`）
- 示例配置：
  ```json
  {
    "models": {
      "claude-4-sonnet": "anthropic-claude-4-sonnet"
    },
    "anthropic_model_mappings": {
      "claude-sonnet-4-20250514": "claude-4-sonnet"
    }
  }
  ```

### API端点兼容性
- **OpenAI兼容**: `/v1/models`、`/v1/chat/completions`
- **Anthropic兼容**: `/v1/messages`
- 支持流式和非流式响应
- 完整的工具调用（Function Calling）支持

### 消息格式转换
项目的核心功能是在不同API格式之间转换：
1. **OpenAI → JetBrains**: 将OpenAI格式的消息转换为JetBrains内部格式
2. **Anthropic → OpenAI → JetBrains**: Anthropic请求先转为OpenAI格式，再转为JetBrains格式
3. **JetBrains → OpenAI/Anthropic**: 将JetBrains响应转换回客户端期望的格式

### 监控与统计（仅Go版本）
- Web界面统计面板：`/`（中文界面）
- 实时QPS监控、请求成功率统计
- Token配额监控和过期预警
- 自动刷新功能

## 重要配置文件

### models.json
定义可用模型及其映射关系，格式：
```json
{
  "models": {
    "api-model-id": "internal-jetbrains-model"
  },
  "anthropic_model_mappings": {
    "anthropic-model-name": "api-model-id"
  }
}
```

### .env文件
必须配置的环境变量：
- `CLIENT_API_KEYS`: 客户端API密钥（逗号分隔）
- `JETBRAINS_LICENSE_IDS`: JetBrains许可证ID（逗号分隔）
- `JETBRAINS_AUTHORIZATIONS`: JetBrains授权token（逗号分隔）
- `PORT`: 服务端口（默认7860）

## 开发注意事项

### 修改模型配置
编辑`models.json`文件添加新的模型映射，服务会自动重新加载配置。

### 调试JetBrains API交互
- Go版本：设置`GIN_MODE=debug`环境变量
- Python版本：查看控制台输出的详细日志

### 测试工具调用
使用`python/testFC.py`脚本测试Function Calling功能，确保工具调用的完整流程正常工作。

### 统计监控
Go版本提供完整的Web监控界面，访问`http://localhost:7860`查看服务状态和统计信息。