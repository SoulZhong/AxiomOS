#!/usr/bin/env bash
# AxiomOS 生产机一次性初始化（Ubuntu 24.04）：装 Nginx、certbot、Docker，起数据库，装 systemd 服务，
# 申请 Let's Encrypt 证书并确认自动续期。可以反复运行，已做过的步骤会跳过。
#
# 服务器上只放部署文件与发布包，不放源码、不装 Go 与 Node。用法（在服务器上，以有 sudo 权限的用户执行）：
#   mkdir -p ~/axiomos-deploy && cd ~/axiomos-deploy
#   for f in setup-server.sh install-release.sh docker-compose.yml axiomd.service axiomd.env.example nginx-axiomos.conf; do
#     curl -fsSLO "https://raw.githubusercontent.com/SoulZhong/AxiomOS/master/deploy/$f"; done
#   sudo DOMAIN=axiom.tutorkin.com CERT_EMAIL=admin@tutorkin.com bash setup-server.sh
#
# 结尾会从 GitHub Release「latest」下载最新发布包装上（第一版部署）；之后的每次部署由 GitHub Actions 完成
# （.github/workflows/ci.yml → deploy/install-release.sh），这个脚本不需要再跑。步骤说明见 docs/deploy.md。
set -euo pipefail

DOMAIN="${DOMAIN:-axiom.tutorkin.com}"
CERT_EMAIL="${CERT_EMAIL:-admin@tutorkin.com}"
DEPLOY_USER="${DEPLOY_USER:-${SUDO_USER:-ubuntu}}"   # CI 用来 ssh 上来的账号，要能写 /opt/axiomos 与重启服务
APP_ROOT=/opt/axiomos
ETC=/etc/axiomos
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ "$(id -u)" != "0" ]; then
  echo "✗ 请用 sudo 运行。" >&2
  exit 1
fi

say() { echo "▶ $*"; }
ok() { echo "✓ $*"; }

# ---------- 1. 系统包 ----------
say "安装 Nginx、snapd、基础工具"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq nginx snapd curl openssl ca-certificates jq >/dev/null
systemctl enable --now nginx >/dev/null
ok "Nginx $(nginx -v 2>&1 | sed 's/.*nginx\///')"

if ! command -v certbot >/dev/null 2>&1; then
  say "用 snap 安装 certbot"
  snap install core >/dev/null 2>&1 || true
  snap refresh core >/dev/null 2>&1 || true
  snap install --classic certbot >/dev/null
  ln -sf /snap/bin/certbot /usr/bin/certbot
fi
ok "certbot $(certbot --version 2>&1 | awk '{print $2}')"

if ! command -v docker >/dev/null 2>&1; then
  say "安装 Docker（数据库跑在容器里）"
  curl -fsSL https://get.docker.com | sh >/dev/null
fi
systemctl enable --now docker >/dev/null
ok "Docker $(docker --version | awk '{print $3}' | tr -d ,)"

# ---------- 2. 用户与目录 ----------
if ! id axiomos >/dev/null 2>&1; then
  useradd --system --home "$APP_ROOT" --shell /usr/sbin/nologin axiomos
fi
mkdir -p "$APP_ROOT/releases" "$ETC"
# 部署账号要能写 releases 与切换 current；服务以 axiomos 用户运行，只读发布包
chown -R "$DEPLOY_USER":axiomos "$APP_ROOT"
chmod 2775 "$APP_ROOT" "$APP_ROOT/releases"
# 让部署账号不输密码就能重启服务与重载 Nginx（只限这几条命令）
cat > /etc/sudoers.d/axiomos-deploy <<EOF
$DEPLOY_USER ALL=(root) NOPASSWD: /usr/bin/systemctl restart axiomd, /usr/bin/systemctl start axiomd, /usr/bin/systemctl stop axiomd, /usr/bin/systemctl status axiomd, /usr/bin/systemctl reload nginx
EOF
chmod 0440 /etc/sudoers.d/axiomos-deploy
# 把仓库里的 deploy/ 复制一份到 /opt/axiomos/deploy，compose 与安装脚本从这里用（发布包每次也会带最新的一份）
mkdir -p "$APP_ROOT/deploy"
cp "$HERE"/docker-compose.yml "$HERE"/install-release.sh "$HERE"/axiomd.service "$HERE"/axiomd.env.example "$HERE"/nginx-axiomos.conf "$APP_ROOT/deploy/"
chmod +x "$APP_ROOT/deploy/install-release.sh"
ok "目录就绪：${APP_ROOT}（部署账号 ${DEPLOY_USER}，运行账号 axiomos）"

# ---------- 3. 密钥与环境文件 ----------
if [ ! -f "$ETC/db.env" ]; then
  echo "AXIOMOS_DB_PASSWORD=$(openssl rand -hex 24)" > "$ETC/db.env"
  chmod 0600 "$ETC/db.env"
  ok "生成数据库密码 → $ETC/db.env"
fi
DB_PASSWORD="$(grep '^AXIOMOS_DB_PASSWORD=' "$ETC/db.env" | cut -d= -f2-)"

ADMIN_PASSWORD_NEW=""
if [ ! -f "$ETC/axiomd.env" ]; then
  SECRET_KEY="$(openssl rand -base64 32)"
  ADMIN_PASSWORD_NEW="$(openssl rand -base64 18 | tr -d '/+=' | cut -c1-20)"
  sed -e "s|^DATABASE_URL=.*|DATABASE_URL=postgres://axiomos:${DB_PASSWORD}@127.0.0.1:5439/axiomos?sslmode=disable|" \
      -e "s|^AXIOMOS_SECRET_KEY=.*|AXIOMOS_SECRET_KEY=${SECRET_KEY}|" \
      -e "s|^PLATFORM_ADMIN_PASSWORD=.*|PLATFORM_ADMIN_PASSWORD=${ADMIN_PASSWORD_NEW}|" \
      -e "s|^PLATFORM_ADMIN_EMAIL=.*|PLATFORM_ADMIN_EMAIL=${CERT_EMAIL}|" \
      -e "s|https://axiom.tutorkin.com|https://${DOMAIN}|g" \
      "$HERE/axiomd.env.example" > "$ETC/axiomd.env"
  chown root:axiomos "$ETC/axiomd.env"
  chmod 0640 "$ETC/axiomd.env"
  ok "生成 $ETC/axiomd.env（密钥与管理员密码已随机生成）"
else
  ok "$ETC/axiomd.env 已存在，不动"
fi

# ---------- 4. 数据库 ----------
say "启动 PostgreSQL 容器"
docker compose --env-file "$ETC/db.env" -f "$APP_ROOT/deploy/docker-compose.yml" up -d >/dev/null
for _ in $(seq 1 30); do
  if docker exec axiomos-db pg_isready -U axiomos -d axiomos >/dev/null 2>&1; then break; fi
  sleep 2
done
docker exec axiomos-db pg_isready -U axiomos -d axiomos >/dev/null 2>&1 || { echo "✗ 数据库 60 秒内未就绪" >&2; exit 1; }
ok "数据库就绪（127.0.0.1:5439）"

# ---------- 5. systemd 服务 ----------
cp "$HERE/axiomd.service" /etc/systemd/system/axiomd.service
systemctl daemon-reload
systemctl enable axiomd >/dev/null 2>&1 || true
if [ -x "$APP_ROOT/current/axiomd" ]; then
  systemctl restart axiomd
  ok "axiomd 已重启"
else
  ok "axiomd 服务已登记；第一次 CI 部署放好发布包后会启动"
fi

# ---------- 6. Nginx 站点（先只开 80） ----------
if [ ! -f /etc/nginx/sites-available/axiomos ]; then
  sed "s/axiom.tutorkin.com/${DOMAIN}/g" "$HERE/nginx-axiomos.conf" > /etc/nginx/sites-available/axiomos
  ok "写入 /etc/nginx/sites-available/axiomos"
else
  ok "Nginx 站点配置已存在，不动（certbot 可能已在里面加了 SSL 段）"
fi
ln -sf /etc/nginx/sites-available/axiomos /etc/nginx/sites-enabled/axiomos
nginx -t
systemctl reload nginx
ok "Nginx 已重载"

# ---------- 7. 证书：申请 + 自动续期 ----------
resolved="$(dig +short "$DOMAIN" 2>/dev/null | tail -n1 || true)"
[ -z "$resolved" ] && resolved="$(getent ahostsv4 "$DOMAIN" | awk '{print $1; exit}' || true)"
myip="$(curl -fsS --max-time 5 https://api.ipify.org 2>/dev/null || curl -fsS --max-time 5 https://ifconfig.me 2>/dev/null || true)"
if [ -f "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" ]; then
  ok "证书已存在：$(openssl x509 -in "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" -noout -enddate | cut -d= -f2) 到期"
elif [ -n "$resolved" ] && { [ -z "$myip" ] || [ "$resolved" = "$myip" ]; }; then
  say "向 Let's Encrypt 申请 $DOMAIN 的证书（ECDSA）"
  certbot --nginx -d "$DOMAIN" --key-type ecdsa --non-interactive --agree-tos -m "$CERT_EMAIL" --redirect --no-eff-email
  ok "证书已签发并写入 Nginx 配置（含 HTTP → HTTPS 跳转）"
else
  echo "! 跳过证书申请：$DOMAIN 解析到「${resolved:-（未解析）}」，本机公网 IP 是「${myip:-?}」。DNS 生效后重跑本脚本即可。" >&2
fi

# 续期后重载 Nginx 的钩子（certbot --nginx 本来会做；多一道保险，也覆盖以后改用其他插件的情况）
mkdir -p /etc/letsencrypt/renewal-hooks/deploy
cat > /etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh <<'EOF'
#!/bin/sh
nginx -t && systemctl reload nginx
EOF
chmod +x /etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh
# snap 装的是 snap.certbot.renew.timer，apt 装的是 certbot.timer；两种都每天两次检查、到期前 30 天续
timer="$(systemctl list-timers --all --no-pager 2>/dev/null | grep -oE 'snap\.certbot\.renew\.timer|certbot\.timer' | head -n1 || true)"
if [ -n "$timer" ]; then
  ok "自动续期：$timer 每天两次检查，到期前 30 天续，续完自动重载 Nginx"
else
  echo "! 没找到 certbot 的 systemd timer，请检查 certbot 安装" >&2
fi
if [ -f "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" ]; then
  say "模拟一次续期（不真的续）"
  certbot renew --dry-run -q && ok "续期流程通过"
fi

# ---------- 8. 第一版部署：还没有发布包时从 GitHub Release 下载最新的装上 ----------
if [ ! -x "$APP_ROOT/current/axiomd" ]; then
  say "安装最新发布包"
  if bash "$APP_ROOT/deploy/install-release.sh"; then
    chown -R "$DEPLOY_USER":axiomos "$APP_ROOT/releases"
  else
    echo "! 第一版部署没成功（多半是 GitHub Release 还没生成）；之后推送 master 由 CI 部署，或手动运行 install-release.sh" >&2
  fi
fi

# ---------- 9. 收尾 ----------
echo
echo "========================================"
echo "初始化完成。"
echo "  站点：https://${DOMAIN}"
echo "  环境：$ETC/axiomd.env    数据库密码：$ETC/db.env"
if [ -n "$ADMIN_PASSWORD_NEW" ]; then
  echo "  平台管理员：$CERT_EMAIL / ${ADMIN_PASSWORD_NEW}（后台 /admin/，请立刻改密码）"
fi
echo "  下一步：在 GitHub 仓库的 prod-tencent-cloud 环境里配好 DEPLOY_HOST / DEPLOY_USER / SSH_PRIVATE_KEY，"
echo "         推送 master 即自动部署；也可随时在服务器上运行 bash /opt/axiomos/deploy/install-release.sh 装最新发布包"
echo "========================================"
