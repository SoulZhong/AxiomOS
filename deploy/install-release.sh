#!/usr/bin/env bash
# 在生产机上安装一个发布包并切换过去；健康检查不过就自动回滚到上一份。服务器上不需要源码、Go 或 Node。
#   bash /opt/axiomos/deploy/install-release.sh                      # 从 GitHub Release「latest」下载最新发布包并安装
#   bash /opt/axiomos/deploy/install-release.sh /tmp/axiomos-x.tar.gz # 安装指定的包（CI 用 scp 传上来后就是这么调）
#
# 发布包里有：axiomd（linux/amd64 二进制）、web/（静态前端）、deploy/（这份脚本与配置）、VERSION（提交号）。
set -euo pipefail

RELEASE_URL="${RELEASE_URL:-https://github.com/SoulZhong/AxiomOS/releases/latest/download/axiomos-linux-amd64.tar.gz}"
TARBALL="${1:-}"
if [ -z "$TARBALL" ]; then
  TARBALL="/tmp/axiomos-latest-$$.tar.gz"
  echo "▶ 下载最新发布包 $RELEASE_URL"
  curl -fsSL --retry 3 -o "$TARBALL" "$RELEASE_URL" || { echo "✗ 下载失败：GitHub Release「latest」还不存在（CI 在 master 上跑过一次后才有），或网络不通" >&2; exit 1; }
  trap 'rm -f "$TARBALL"' EXIT
fi
APP_ROOT=/opt/axiomos
RELEASES="$APP_ROOT/releases"
HEALTH_URL="http://127.0.0.1:8080/healthz"
KEEP=5   # 保留最近几份发布包

[ -f "$TARBALL" ] || { echo "✗ 找不到发布包：$TARBALL" >&2; exit 1; }
mkdir -p "$RELEASES"

version="$(tar -xzOf "$TARBALL" VERSION 2>/dev/null | tr -d '[:space:]' || true)"
[ -n "$version" ] || version="unknown"
target="$RELEASES/$(date +%Y%m%d-%H%M%S)-${version:0:12}"
echo "▶ 解包到 $target"
mkdir -p "$target"
tar -xzf "$TARBALL" -C "$target"
chmod +x "$target/axiomd"
[ -d "$target/web" ] || { echo "✗ 发布包里没有 web/ 目录" >&2; rm -rf "$target"; exit 1; }
# 发布包自带最新的 deploy/，让 /opt/axiomos/deploy 跟着更新（compose、服务文件的改动随代码一起走）
if [ -d "$target/deploy" ]; then
  cp "$target"/deploy/*.sh "$target"/deploy/*.yml "$target"/deploy/*.service "$target"/deploy/*.conf "$target"/deploy/*.example "$APP_ROOT/deploy/" 2>/dev/null || true
  chmod +x "$APP_ROOT"/deploy/*.sh
fi

previous="$(readlink -f "$APP_ROOT/current" 2>/dev/null || true)"
echo "▶ 切换 current → ${target}（上一份：${previous:-无}）"
ln -sfn "$target" "$APP_ROOT/current.tmp" && mv -Tf "$APP_ROOT/current.tmp" "$APP_ROOT/current"

# 服务文件若有变化就更新（需要 sudo；sudoers 只放行了 systemctl 的这几条，daemon-reload 交给 setup-server.sh 那次）
sudo systemctl restart axiomd

echo "▶ 健康检查 $HEALTH_URL"
ok=
for _ in $(seq 1 30); do
  if curl -fsS --max-time 3 "$HEALTH_URL" 2>/dev/null | grep -q '"ok":true'; then ok=1; break; fi
  sleep 2
done
if [ -z "$ok" ]; then
  echo "✗ 新版本 60 秒内没有通过健康检查，最近日志：" >&2
  sudo systemctl status axiomd --no-pager -n 30 >&2 || true
  if [ -n "$previous" ] && [ -x "$previous/axiomd" ]; then
    echo "▶ 回滚到 $previous" >&2
    ln -sfn "$previous" "$APP_ROOT/current.tmp" && mv -Tf "$APP_ROOT/current.tmp" "$APP_ROOT/current"
    sudo systemctl restart axiomd
    rm -rf "$target"
  fi
  exit 1
fi
running="$(curl -fsS --max-time 3 "$HEALTH_URL" | jq -r .version 2>/dev/null || true)"
echo "✓ 部署完成：版本 ${running:-$version}"

# 清理旧发布包（保留最近 KEEP 份，不删 current 指向的那份）
current_real="$(readlink -f "$APP_ROOT/current")"
ls -1dt "$RELEASES"/*/ 2>/dev/null | tail -n +$((KEEP + 1)) | while read -r old; do
  old="${old%/}"
  [ "$old" = "$current_real" ] && continue
  rm -rf "$old"
done
