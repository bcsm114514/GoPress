# === 第一阶段：构建 ===
FROM golang:1.25.3-alpine AS builder

# 安装 git (为了下载依赖)
RUN apk --no-cache add git

WORKDIR /app

# 复制依赖并下载
COPY go.mod go.sum ./
RUN go mod download

# 复制源码
COPY . .

# 编译 (CGO_ENABLED=0 确保静态链接，-s -w 减小体积)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o gopress .

# === 第二阶段：运行 ===
FROM alpine:latest

# 安装基础库
RUN apk --no-cache add ca-certificates tzdata

# 1. 设置工作目录为 /data (这是你将来挂载的地方)
WORKDIR /data

# 2. 把二进制文件复制到暂存区 /opt (防止被挂载覆盖)
COPY --from=builder /app/gopress /opt/gopress-bin

# 3. 创建启动脚本
# 这个脚本会在容器启动时执行：
# a. 把 /opt 里的新版程序复制到 /data
# b. 赋予执行权限
# c. 启动程序
RUN echo '#!/bin/sh' > /entrypoint.sh && \
    echo 'echo "-> Syncing GoPress binary to /data..."' >> /entrypoint.sh && \
    echo 'cp /opt/gopress-bin /data/gopress' >> /entrypoint.sh && \
    echo 'chmod +x /data/gopress' >> /entrypoint.sh && \
    echo 'echo "-> Starting GoPress..."' >> /entrypoint.sh && \
    echo 'exec /data/gopress' >> /entrypoint.sh && \
    chmod +x /entrypoint.sh

# 4. 暴露端口
EXPOSE 3000

# 5. 声明数据卷
VOLUME ["/data"]

# 6. 设置入口点
ENTRYPOINT ["/entrypoint.sh"]