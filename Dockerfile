# HuggingFace Spaces部署用Dockerfile
# 使用官方Go镜像作为构建基础
FROM golang:1.23-alpine AS builder

# 安装构建依赖
RUN apk add --no-cache git ca-certificates tzdata

# 设置工作目录
WORKDIR /app

# 复制go.mod和go.sum文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o jetbrainsai2api .

# 使用轻量级Alpine镜像作为运行时基础
FROM alpine:latest

# 安装CA证书和时区数据
RUN apk --no-cache add ca-certificates tzdata

# 创建非root用户
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# 设置工作目录
WORKDIR /app

# 从构建镜像复制二进制文件
COPY --from=builder /app/jetbrainsai2api .

# 复制静态文件和配置文件
COPY --from=builder /app/static ./static
COPY --from=builder /app/models.json .

# 创建数据目录并设置权限
RUN mkdir -p /app/data && chown -R appuser:appgroup /app

# 切换到非root用户
USER appuser

# 暴露端口（HuggingFace Spaces通常使用7860）
EXPOSE 7860

# 设置环境变量
ENV ADDR=0.0.0.0
ENV PORT=7860
ENV GIN_MODE=release
ENV TZ=Asia/Shanghai

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:7860/health || exit 1

# 启动应用
CMD ["./jetbrainsai2api"]