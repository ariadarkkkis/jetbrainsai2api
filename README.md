# JetBrains AI OpenAI API 适配器

<div align="center">

![版本](https://img.shields.io/badge/版本-3.0.0-blue.svg)
![许可证](https://img.shields.io/badge/许可证-MIT-green.svg)
![Python](https://img.shields.io/badge/Python-3.11+-brightgreen.svg)

</div>


---

> 高性能异步 AI 代理服务，将 JetBrains AI 的大语言模型转换为 OpenAI API 格式，支持真正的流式响应和高并发处理。

## 🚀 更新日志 (v3.0.0)
*   **新增 Anthropic API 兼容**：无缝对接 Anthropic SDK，现已支持 `/v1/messages` 端点。
*   **智能配额管理**：自动检测并轮换超出配额的 JetBrains 账户，最大化服务可用性。

## 🚀 更新日志 (v2.0.0)
*   **全面兼容 Function Calling**：完全实现 OpenAI 的 `tools` 和 `tool_calls` 功能，支持完整的函数调用流程。

## 🚀 更新日志 (v1.4.0)
*   **增强 OpenAI 兼容性**：优化了对消息 `role` 的处理逻辑，修复了因角色不规范导致部分客户端调用失败的问题，提升了整体适配性。

## 🚀 更新日志 (v1.3.0)
*   **新增 JWT 自动刷新机制**：告别每日手动更换 JWT！现在可以通过配置 `licenseId` 和 `authorization` 实现 JWT 自动刷新，一劳永逸。
*   **向下兼容**：旧的 `jwt` 配置格式仍然有效，可与新格式混合使用，无缝升级。


## ✨ 核心特性

- **⚡ 高并发异步架构**：基于 httpx + FastAPI，支持数千并发连接
- **🔧 OpenAI 完全兼容**：零修改集成现有 OpenAI 客户端和工具
- **🔐 动态认证**：支持 JWT 自动刷新与轮询，大幅简化认证管理
- **📦 开箱即用**：Docker 一键部署，配置简单

## ⚡ 快速开始

### 1. 克隆项目
```bash
git clone https://github.com/oDaiSuno/jetbrainsai2api.git
cd jetbrainsai2api
```

### 2. 配置密钥

#### 配置 JetBrains AI JWT
通过IDE(这里以Pycharm为例)和Reqable(小黄鸟)获取JWT
1. 打开Pycharm中的`设置`，搜索`代理`，选择`自动检测代理设置`并应用
   <img src="images/image-20250703175459818.png" alt="image-20250703175459818" style="zoom:33%;" />

2. 打开小黄鸟并启动`代理设置`，在pycharm中与AI聊下天，在小黄鸟中找到类似于`auth/jetbrains-jwt/provide-access/license/v2`的接口，然后将请求头里的`authorization`（注意只需复制`Bear`后面的内容）和请求体里的`licenseId`复制下来

   <img src="images/image-20250703175648995.png" alt="image-20250703175648995" style="zoom:33%;" />

   <img src="images/image-20250704191812645.png" alt="image-20250704191812645" style="zoom:33%;" />

   <img src="images/image-20250704191843579.png" alt="image-20250704191843579" style="zoom:33%;" />

3. 当然，你也可以直接在小黄鸟中寻找类似于`v5/llm/chat/stream/v7`的接口，把请求头中`grazie-authenticate-jwt`的内容复制下来即为你的`JWT`。

   <img src="images/image-20250703175928552.png" alt="image-20250703175928552" style="zoom: 33%;" />

创建 `jetbrainsai.json` 文件。支持以下两种格式，可混合使用：

**1. 自动刷新（推荐）**
> `licenseId` 和 `authorization` 可在 JetBrains 相关的登录验证请求中捕获。（如上述2.过程）
```json
[
    {
        "licenseId": "Oxxxx",
        "authorization": "eyJhbGcxxx"
    }
]
```

**2. 静态 JWT**
```json
[
    {
        "jwt": "your-jwt-here-1"
    }
]
```

**3. 混合使用**
```json
[
    {
        "jwt": "your-jwt-here-1",
        "licenseId": "Oxxxx",
        "authorization": "eyJhbGcxxx"
    }
]
```

#### 配置客户端密钥
创建 `client_api_keys.json`：
```json
[
  "sk-client-key-1",
  "sk-client-key-2"
]
```

#### 配置可用模型(不推荐改动)
创建 `models.json`：
```json
[
    "anthropic-claude-3.7-sonnet",
    "anthropic-claude-4-sonnet",
    "google-chat-gemini-pro-2.5",
    "openai-o4-mini",
    "openai-o3-mini",
    "openai-o3",
    "openai-o1",
    "openai-gpt-4o",
    "anthropic-claude-3.5-sonnet",
    "openai-gpt4.1"
]
```

### 3. 启动服务

#### 方式一：Docker 部署（推荐）
```bash
docker-compose up -d
```

#### 方式二：本地运行
```bash
pip install -r requirements.txt
uvicorn main:app --host 0.0.0.0 --port 8000
```

### 4. 验证服务
```bash
curl -H "Authorization: Bearer sk-client-key-1" http://localhost:8000/v1/models
```

## 🔌 API 接口

### 聊天完成
```http
POST /v1/chat/completions
Authorization: Bearer <client-api-key>
Content-Type: application/json
```

**请求示例：**
```json
{
  "model": "anthropic-claude-3.5-sonnet",
  "messages": [
    {"role": "user", "content": "你好"}
  ],
  "stream": true
}
```

### Anthropic 消息
> 此接口用于兼容 Anthropic SDK。
```http
POST /v1/messages
x-api-key: <client-api-key>
Content-Type: application/json
x-anthropic-version: 2023-06-01
```
**请求示例：**
```json
{
  "model": "anthropic-claude-3.5-sonnet",
  "messages": [
    {"role": "user", "content": "你好"}
  ],
  "max_tokens": 1024,
  "stream": true
}
```
> [!NOTE]
> 使用 Anthropic SDK 时，请务必在 `client` 初始化时传入 `base_url`。

### 模型列表
```http
GET /v1/models
Authorization: Bearer <client-api-key>
```

## 💻 使用示例

### Python + OpenAI SDK
```python
import openai

client = openai.OpenAI(
    api_key="sk-client-key-1",
    base_url="http://localhost:8000/v1"
)

# 流式对话
response = client.chat.completions.create(
    model="anthropic-claude-3.5-sonnet",
    messages=[{"role": "user", "content": "写一首关于春天的诗"}],
    stream=True
)

for chunk in response:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="")
```

### Python + Anthropic SDK
```python
import anthropic

client = anthropic.Anthropic(
    api_key="sk-client-key-1",
    base_url="http://localhost:8000/v1",
)

with client.messages.stream(
    max_tokens=1024,
    messages=[{"role": "user", "content": "写一首关于夏天的诗"}],
    model="anthropic-claude-3.5-sonnet",
) as stream:
    for text in stream.text_stream:
        print(text, end="", flush=True)
```

### cURL
```bash
# OpenAI API
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Authorization: Bearer sk-client-key-1" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "anthropic-claude-3.5-sonnet",
    "messages": [{"role": "user", "content": "你好"}],
    "stream": true
  }'

# Anthropic API
curl -X POST http://localhost:8000/v1/messages \
  -H "x-api-key: sk-client-key-1" \
  -H "Content-Type: application/json" \
  -H "x-anthropic-version: 2023-06-01" \
  -d '{
    "model": "anthropic-claude-3.5-sonnet",
    "messages": [{"role": "user", "content": "你好"}],
    "max_tokens": 1024,
    "stream": true
  }'
```

## 📁 项目结构

```
jetbrainsai2api/
├── main.py              # 主程序（异步服务器 + API 适配器）
├── requirements.txt     # Python 依赖
├── Dockerfile          # Docker 构建文件
├── docker-compose.yml  # Docker Compose 配置
├── jetbrainsai.json     # JetBrains AI JWT 配置
├── client_api_keys.json # 客户端 API 密钥配置
└── models.json         # 可用模型配置
```

---

<div align="center">

**如果这个项目对您有帮助，请考虑给个 ⭐ Star！**

[![Star History Chart](https://api.star-history.com/svg?repos=oDaiSuno/jetbrainsai2api&type=Date)](https://www.star-history.com/#oDaiSuno/jetbrainsai2api&Date)
</div> 