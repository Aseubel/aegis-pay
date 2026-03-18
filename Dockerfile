# 构建阶段
FROM golang:1.21-alpine AS builder

WORKDIR /app

# 安装构建依赖
RUN apk add --no-cache git

# 复制依赖文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build -o aegis-pay ./cmd/server

# 运行阶段
FROM alpine:3.19

WORKDIR /app

# 安装运行时依赖
RUN apk add --no-cache ca-certificates tzdata

# 从构建阶段复制二进制文件
COPY --from=builder /app/aegis-pay .
COPY --from=builder /app/docker-compose.yml .

# 创建非 root 用户
RUN adduser -D -u 1000 appuser
USER appuser

# 暴露端口
EXPOSE 8080

# 启动命令
ENTRYPOINT ["./aegis-pay"]
