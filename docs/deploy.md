# 部署：axiom.tutorkin.com（HTTPS 证书、自动续期、自动化部署）

参考 nook 的做法（`nook/docs/HTTPS证书部署指南-Certbot-Nginx.md`）：Nginx 终止 TLS，证书由 certbot 向 Let's Encrypt 申请、systemd timer 自动续期；代码走 GitHub Actions 在推送 master 时自动部署。与 nook 不同的是 AxiomOS 不在服务器上编译，**服务器上没有源码、不装 Go 与 Node**：CI 打好发布包（Go 二进制 + 静态前端）传上去，服务器只负责解包、切换、健康检查、回滚。发布包同时挂在 GitHub Release「latest」上，服务器随时能自己下载最新的装。

## 一、组成

| 环节 | 在哪 | 做什么 |
|---|---|---|
| `deploy/setup-server.sh` | 服务器，跑一次 | 装 Nginx / certbot / Docker，起数据库容器，生成密钥与环境文件，装 systemd 服务，申请证书，确认自动续期 |
| `deploy/build-release.sh` | CI 或本机 | 构建前端、交叉编译 axiomd，打成 `dist/axiomos-<提交>.tar.gz` |
| `deploy/install-release.sh` | 服务器，每次部署 | 解包到 `/opt/axiomos/releases/<时间-提交>`，切 `current` 符号链接，重启服务，健康检查不过就回滚 |
| `.github/workflows/ci.yml` | GitHub Actions | 测试 → 打包 → 更新 GitHub Release「latest」→ scp 到服务器 → 调 install-release.sh；部署串行排队 |
| `deploy/nginx-axiomos.conf` | 服务器 `/etc/nginx/sites-available/axiomos` | 80 端口起始配置；certbot 原地加 443 与跳转 |
| `deploy/axiomd.service` | 服务器 `/etc/systemd/system/axiomd.service` | 以 `axiomos` 用户跑 `/opt/axiomos/current/axiomd`，崩了自动拉起 |
| `deploy/axiomd.env.example` | 服务器 `/etc/axiomos/axiomd.env` | 环境变量模板；首次由 setup 脚本生成随机密钥 |
| `deploy/docker-compose.yml` | 服务器 `/opt/axiomos/deploy/` | PostgreSQL 17，只绑 127.0.0.1:5439，密码在 `/etc/axiomos/db.env` |

服务器目录：

```
/opt/axiomos/
  releases/20260914-120000-abcdef123456/   axiomd  web/  deploy/  VERSION
  current -> releases/...                  正在跑的那份
  deploy/                                  compose、安装脚本（随发布包更新）
/etc/axiomos/axiomd.env                    环境变量（0640，root:axiomos）
/etc/axiomos/db.env                        数据库密码（0600）
```

## 二、服务器初始化（一次）

前置：域名 `axiom.tutorkin.com` 的 A 记录指向服务器公网 IP（81.70.8.203），安全组放开 80 / 443。

```bash
# 在服务器上（ubuntu 用户，有 sudo）。只下载 deploy/ 里的六个文件，不 clone 仓库
mkdir -p ~/axiomos-deploy && cd ~/axiomos-deploy
for f in setup-server.sh install-release.sh docker-compose.yml axiomd.service axiomd.env.example nginx-axiomos.conf; do
  curl -fsSLO "https://raw.githubusercontent.com/SoulZhong/AxiomOS/master/deploy/$f"
done
sudo DOMAIN=axiom.tutorkin.com CERT_EMAIL=admin@tutorkin.com bash setup-server.sh
```

脚本可反复运行。它会：

1. `apt` 装 Nginx、snapd；没有 certbot 就 `snap install --classic certbot`（服务器上已有 apt 装的 certbot 2.9.0，会直接用它）；没有 Docker 就装 Docker。
2. 建系统用户 `axiomos`、目录 `/opt/axiomos`，给部署账号（默认 `ubuntu`）一条只允许 `systemctl restart axiomd` 与 `reload nginx` 的免密 sudo。
3. 生成 `/etc/axiomos/db.env`（数据库密码）与 `/etc/axiomos/axiomd.env`（`AXIOMOS_SECRET_KEY`、平台管理员密码随机生成）。**`AXIOMOS_SECRET_KEY` 丢了就解不开组织已存的凭据，请备份。**
4. `docker compose up -d` 起 PostgreSQL，等它就绪。
5. 装并启用 `axiomd.service`（发布包到位后才会真正跑起来）。
6. 写 Nginx 站点（只 80）、重载。
7. 域名解析到本机时执行 `certbot --nginx -d axiom.tutorkin.com --key-type ecdsa --redirect`，非交互；否则提示 DNS 生效后重跑。
8. 装续期钩子 `/etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh`，确认 certbot 的 systemd timer（snap 装的叫 `snap.certbot.renew.timer`，apt 装的叫 `certbot.timer`）在跑，`certbot renew --dry-run` 走一遍。
9. 还没有发布包时从 GitHub Release「latest」下载最新的装上（第一版部署）。Release 由 CI 在 master 上跑过一次后生成；还没有就跳过，之后推送 master 由 CI 部署。

结束时打印平台管理员的邮箱与初始密码（后台 `/admin/`），登录后立刻改掉。

## 三、GitHub Actions 自动部署

仓库 Settings → Environments 建 `prod-tencent-cloud`，加三个 secret：

| Secret | 值 |
|---|---|
| `DEPLOY_HOST` | `81.70.8.203` |
| `DEPLOY_USER` | `ubuntu` |
| `SSH_PRIVATE_KEY` | 一把专用部署密钥的私钥；公钥追加到服务器 `~ubuntu/.ssh/authorized_keys` |

生成专用密钥（本机）：

```bash
ssh-keygen -t ed25519 -N '' -C axiomos-deploy -f ~/.ssh/axiomos-deploy
ssh ubuntu@81.70.8.203 'cat >> ~/.ssh/authorized_keys' < ~/.ssh/axiomos-deploy.pub
gh api -X PUT repos/SoulZhong/AxiomOS/environments/prod-tencent-cloud >/dev/null
gh secret set DEPLOY_HOST --env prod-tencent-cloud --body 81.70.8.203
gh secret set DEPLOY_USER --env prod-tencent-cloud --body ubuntu
gh secret set SSH_PRIVATE_KEY --env prod-tencent-cloud < ~/.ssh/axiomos-deploy
```

之后每次推送 master：`build` 任务在 CI 里跑后端测试（带 PostgreSQL）、前端类型检查、打发布包；`deploy` 任务把包 scp 到 `/tmp/axiomos-release/`，再 ssh 运行 `install-release.sh`。两次推送挨得近时排队而不是取消。Pull request 只跑 `build`。

## 四、手动部署与回滚

```bash
# 在服务器上装 GitHub Release「latest」（CI 每次在 master 上跑完都会更新它）
ssh ubuntu@81.70.8.203 'bash /opt/axiomos/deploy/install-release.sh'

# 本机打包并上传（不经 CI 也不经 Release）
PUBLIC_URL=https://axiom.tutorkin.com bash deploy/build-release.sh
scp dist/axiomos-*.tar.gz ubuntu@81.70.8.203:/tmp/
ssh ubuntu@81.70.8.203 'bash /opt/axiomos/deploy/install-release.sh /tmp/axiomos-*.tar.gz'

# 回滚：把 current 指回上一份
ssh ubuntu@81.70.8.203 'ls -1t /opt/axiomos/releases/ | head -3'
ssh ubuntu@81.70.8.203 'ln -sfn /opt/axiomos/releases/<上一份> /opt/axiomos/current && sudo systemctl restart axiomd'
```

安装脚本自己也会回滚：新版本 60 秒内没通过 `http://127.0.0.1:8080/healthz`（进程活着且连得上数据库）就切回上一份并重启。发布包保留最近 5 份。

## 五、验证

```bash
curl -I https://axiom.tutorkin.com                     # 200，HTTP 会 301 到 HTTPS
curl -s https://axiom.tutorkin.com/healthz              # {"ok":true,"version":"<提交>"}
openssl s_client -connect axiom.tutorkin.com:443 -servername axiom.tutorkin.com </dev/null 2>/dev/null | openssl x509 -noout -dates -subject
ssh ubuntu@81.70.8.203 'systemctl list-timers --all --no-pager | grep certbot; sudo certbot certificates'
ssh ubuntu@81.70.8.203 'sudo journalctl -u axiomd -n 50 --no-pager'
```

Agent 接入的 MCP 端点是 `https://axiom.tutorkin.com/mcp`；Nginx 对这个路径关了响应缓冲、读超时放到一小时，长调用不会被切断。

## 六、证书续期

- certbot 自带 systemd timer（apt 装的是 `certbot.timer`，snap 装的是 `snap.certbot.renew.timer`），每天两次检查，到期前 30 天续。这台服务器上的 aptabase 证书已经靠它续了。
- 续期后由 certbot 的 nginx 插件与 `renewal-hooks/deploy/reload-nginx.sh` 重载 Nginx，服务不中断。
- 出问题看 `sudo journalctl -u certbot.service -n 50`（snap 装的是 `snap.certbot.renew.service`）；手动续 `sudo certbot renew --force-renewal -v`。
- Let's Encrypt 对同一域名每周最多签 5 张；setup 脚本在证书已存在时跳过申请，重跑不会浪费额度。

## 七、环境变量要点

生产与本地开发不同的几项（都在 `/etc/axiomos/axiomd.env`）：

- `ADDR=127.0.0.1:8080`：只听本机。
- `SECURE_COOKIE=1`：会话 Cookie 带 Secure；Nginx 要传 `X-Forwarded-Proto`。
- `PUBLIC_URL` / `ALLOW_ORIGINS`：都填 `https://axiom.tutorkin.com`。
- `SEED_DEMO=0`：公网实例不种演示组织（演示密码是公开的）。组织由平台管理员在 `/admin/` 创建。
- `AXIOMOS_ALLOW_PRIVATE_EGRESS` 不设：出网守卫保持只许公网。

## 八、交给服务器上的 AI 助手执行的指令

把下面这段整个贴给服务器上的助手（腾讯云 OrcaTeam AI 之类）。服务器上只会有部署文件与发布包，没有源码。

```
你在 Ubuntu 24.04 的服务器 81.70.8.203 上，当前用户 ubuntu 有免密 sudo。请按顺序执行，每步失败就停下来把输出给我：

1. 取部署文件（不 clone 仓库，服务器上不放源码）：
   mkdir -p ~/axiomos-deploy && cd ~/axiomos-deploy
   for f in setup-server.sh install-release.sh docker-compose.yml axiomd.service axiomd.env.example nginx-axiomos.conf; do curl -fsSLO "https://raw.githubusercontent.com/SoulZhong/AxiomOS/master/deploy/$f"; done

2. 初始化服务器（装 Nginx / certbot / Docker，起 PostgreSQL，生成密钥，装 systemd 服务，申请 axiom.tutorkin.com 的证书并确认自动续期，最后从 GitHub Release 下载最新发布包装上；可重复运行）：
   sudo DOMAIN=axiom.tutorkin.com CERT_EMAIL=admin@tutorkin.com bash setup-server.sh
   记下它最后打印的平台管理员密码。

3. 允许 GitHub Actions 用专用密钥登录来部署：把这一行追加到 ~/.ssh/authorized_keys（已有就跳过）：
   ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILixpkvPAQnottospFfMhUTBsyuKJ61GxRx/ar+JKNoQ axiomos-deploy

4. 验证：
   sudo certbot certificates
   systemctl list-timers --all --no-pager | grep certbot
   sudo nginx -t && curl -I https://axiom.tutorkin.com
   curl -s https://axiom.tutorkin.com/healthz
   （healthz 返回 {"ok":true,...} 即部署成功；返回 502 说明发布包还没装上，等 CI 生成 Release 后运行 bash /opt/axiomos/deploy/install-release.sh）
```

服务器初始化完成后，在本机（已登录 `gh`）把三个 secret 写进 GitHub 的 `prod-tencent-cloud` 环境（第三节的命令），然后推送一次 master，GitHub Actions 会打包并部署；或者不等 CI，按第四节手动打包上传。
