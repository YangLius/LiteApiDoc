# LiteApiDoc — 轻量化接口文档 + Mock 一体化平台
# 多阶段构建:前端 Vite 构建 → Go 编译纯静态单二进制 → 极简运行镜像
FROM node:22-alpine AS web
WORKDIR /app/web
COPY web/package.json web/package-lock.json* ./
RUN npm install
COPY web/ ./
RUN npm run build

FROM golang:1.24-alpine AS backend
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download
COPY backend/ ./
# 嵌入前端构建产物
RUN rm -rf internal/webfs/dist && mkdir -p internal/webfs/dist
COPY --from=web /app/web/dist ./internal/webfs/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /liteapidoc .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates=20240712-r0 tzdata=2024a-r1
COPY --from=backend /liteapidoc /usr/local/bin/liteapidoc
ENV LITEAPIC_WEB_PORT=9900 LITEAPIC_MOCK_PORT=9901 LITEAPIC_DB=/data/liteapidoc.db
VOLUME /data
EXPOSE 9900 9901
ENTRYPOINT ["liteapidoc"]
CMD ["-web", "9900", "-mock", "9901", "-db", "/data/liteapidoc.db"]
