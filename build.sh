#!/usr/bin/env bash
# 一键构建脚本:前端 → 嵌入 → Go 单二进制
# 用法:./build.sh [输出目录],默认 dist/
set -e
cd "$(dirname "$0")"

echo "[1/3] 构建前端…"
(cd web && npm install && npm run build)

echo "[2/3] 嵌入前端产物…"
rm -rf backend/internal/webfs/dist
mkdir -p backend/internal/webfs/dist
cp -r web/dist backend/internal/webfs/dist

echo "[3/3] 编译 Go 单二进制…"
OUT="${1:-dist}"
mkdir -p "$OUT"
(cd backend && CGO_ENABLED=0 go build -ldflags="-s -w" -o "../$OUT/liteapidoc" .)

echo "完成: $OUT/liteapidoc"
echo "运行: $OUT/liteapidoc -web 9900 -mock 9901 -db ./data/liteapidoc.db"
