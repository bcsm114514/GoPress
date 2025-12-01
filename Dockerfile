# === 第一阶段：构建 (Builder) ===
FROM golang:1.25.3-alpine AS builder

# 设置工作目录
WORKDIR /app

# 复制依赖文件并下载
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 编译 (去除调试符号，减小体积)
# CGO_ENABLED=0 确保静态链接
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o gopress .

# === 第二阶段：运行 (Runtime) ===
# 使用 Alpine 作为基础镜像 (非常小，且包含 shell 方便调试)
FROM alpine:latest

# 安装基础证书 (用于 HTTPS 请求) 和 时区数据
RUN apk --no-cache add ca-certificates tzdata

# 设置工作目录
WORKDIR /app

# 从构建阶段复制编译好的二进制文件
COPY --from=builder /app/gopress .

# 暴露端口
EXPOSE 3000

# 声明数据卷 (告诉用户这些目录需要持久化)
# config.json 和 gopress.db 也会生成在 /app 下
VOLUME ["/app/themes", "/app/plugins", "/app/storage"]

# 启动命令
CMD ["./gopress"]