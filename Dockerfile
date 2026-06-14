FROM golang:1.22-alpine AS builder

WORKDIR /build
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o wx-admin .

FROM alpine:3.19
RUN apk add --no-cache tzdata ca-certificates
ENV TZ=Asia/Shanghai

WORKDIR /app
COPY --from=builder /build/wx-admin .
COPY static ./static

EXPOSE 8022

# 环境变量配置
# PORT       - 监听端口 (默认 8022)
# WX_API     - 后端服务地址 (默认 http://127.0.0.1:8061)
# ADMIN_TOKEN - 管理密码 (默认 admin123)
# REDIS_ADDR - Redis 地址 (默认 127.0.0.1:6379)

ENTRYPOINT ["./wx-admin"]
