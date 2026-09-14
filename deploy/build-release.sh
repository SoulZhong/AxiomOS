#!/usr/bin/env bash
# 打一份可部署的发布包：构建静态前端，交叉编译 linux/amd64 的 axiomd，打成 dist/axiomos-<提交>.tar.gz。
# CI 与本机都用它，服务器上不需要 Go 与 Node。
#   PUBLIC_URL=https://axiom.tutorkin.com bash deploy/build-release.sh
# 包内：axiomd、web/（Next.js 静态导出）、deploy/（部署脚本与配置）、VERSION（git 提交号）。
set -euo pipefail
cd "$(git rev-parse --show-toplevel 2>/dev/null || echo "$(dirname "$0")/..")"

PUBLIC_URL="${PUBLIC_URL:-https://axiom.tutorkin.com}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"
VERSION="${VERSION:-$(git rev-parse HEAD 2>/dev/null || echo dev)}"
OUT="${OUT:-dist}"

echo "▶ 构建前端（NEXT_PUBLIC_API_BASE=${PUBLIC_URL}）"
( cd web && NEXT_PUBLIC_API_BASE="$PUBLIC_URL" NEXT_PUBLIC_MOCK=0 pnpm build )
[ -f web/out/index.html ] || { echo "✗ web/out 里没有 index.html" >&2; exit 1; }

echo "▶ 编译 axiomd（$GOOS/${GOARCH}，版本 ${VERSION:0:12}）"
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$stage/axiomd" ./cmd/axiomd

cp -R web/out "$stage/web"
mkdir -p "$stage/deploy"
cp deploy/*.sh deploy/*.yml deploy/*.service deploy/*.conf deploy/*.example "$stage/deploy/"
echo "$VERSION" > "$stage/VERSION"

mkdir -p "$OUT"
pkg="$OUT/axiomos-${VERSION:0:12}.tar.gz"
tar -czf "$pkg" -C "$stage" .
echo "✓ 发布包：${pkg}（$(du -h "$pkg" | cut -f1)）"
