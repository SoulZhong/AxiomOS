# AxiomOS 前端

Next.js（App Router，静态导出）+ TypeScript + Tailwind。构建产物是纯静态文件，由 Go 进程 `cmd/axiomd` 托管。用语以仓库根目录 `CONTEXT.md` 为准：界面上只出现日常中文（或对应的英文），代码里的英文只是标识符。

## 多语言

界面支持 `zh-CN` 与 `en-US`，字典在 `src/lib/i18n.ts`（无第三方库）：`t(key, params)` 取文案，`useLocale()` 读当前语言，`<LocaleProvider>` 在客户端解析语言。

- 登录前：`localStorage["axiomos.locale"]` → `navigator.language`（`zh*` → 中文，否则英文）；登录后以 `GET /auth/me` 里的 `member.locale` 为准。
- 切换语言：左侧导航底部、登录页、邀请页、平台后台都有切换；已登录时会 `PATCH /auth/me {locale}`，并总是写 localStorage。切换后整棵页面树重挂。
- 每个请求带 `Accept-Language: <当前语言>`。数据自带的名字（状态、步骤、参与角色、授权、角色、交付物类型、能力标签、拒绝理由、动态摘要）由后端按语言返回，前端不翻；`src/lib/terms.ts` 里只有缺失时的兜底。
- 日期、金额、token 的格式跟随语言（`src/lib/format.ts`）：中文 `9月3日`、`1.2 万`，英文 `Sep 3`、`12.0K`；金额人民币用 `¥`，其他货币用符号或代码前缀。

## 运行

```bash
pnpm install
pnpm dev          # 开发服务器 http://localhost:3000
pnpm build        # 静态导出到 out/
pnpm lint
```

Node 20+，pnpm 8。仓库根目录的 `make web` 等价于 `pnpm dev`。

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `NEXT_PUBLIC_API_BASE` | `http://localhost:8080` | 后端地址；所有请求走 `<base>/api/v1/...`，带 Cookie（`credentials: include`），后端需允许该来源并返回 `Access-Control-Allow-Credentials` |
| `NEXT_PUBLIC_MOCK` | `0` | 设为 `1` 时不访问后端，全部页面用 `src/lib/mock.ts` 里的示例数据渲染 |

复制 `.env.example` 为 `.env.local` 修改。变量在构建时内联，改了要重新 `pnpm build`。

## 示例数据模式

```bash
NEXT_PUBLIC_MOCK=1 pnpm dev
```

内存里有一个小组织：四个成员（小王 = 当前登录者，产品/设计/管理员）、四个 Agent、四个目标、十几个任务。其中：

- `T1` 需求「登录页改版」走到开发中，有两段执行记录（提问前后各一段，执行者是小李的编码 Agent），可以看用量与成本；
- `T2` Bug「金额显示错位」通过「发现于」挂在 `T1` 上，所以 `T1` 的「测试通过」会被挡；
- `T6` 需求「导出报表」测试角色空缺，出现在待领取任务里，要求角色「测试」；
- `T5` 发布「v2.0 发布」以 `T1` 为前置，甘特图上有箭头。

示例模式里动作会真的改内存数据（状态变化、领取、开始执行、附交付物、评论、建关联、新建任务/目标/Agent、组织设置、平台后台），刷新页面即复位。登录页任意密码可登录，密码填 `wrong` 看失败提示。小王是组织负责人，能打开「组织设置」。平台后台 `/admin/login/` 同样任意密码；邀请页可用 `/invite/demo/` 试。示例数据里服务端返回的名字按当前语言输出（`mock.ts` 里一张 zh → en 小表）。

## 目录

```
src/lib/api.ts        接口客户端与全部数据类型（后端按这里的形状实现；含 api.org / api.invitations / api.admin）
src/lib/i18n.ts       中英文字典、t()、useLocale()、LocaleProvider
src/lib/mock.ts       示例数据与精简的流程内核（含组织设置、邀请、平台后台）
src/lib/terms.ts      代码标识符 -> 当前语言文案（状态类型、授权、优先级……）与兜底
src/lib/format.ts     日期、金额、token 的显示（跟随语言）
src/lib/hooks.ts      数据加载、动态路由 id、执行者列表、能力标签名
src/components/       左侧导航、平台后台外壳、语言切换、基础控件、状态标签、任务表、条形图、甘特图、价格编辑
src/app/              页面：登录 /login、首页 /、目标 /goals、任务 /tasks、待领取任务 /backlog、
                      甘特图 /gantt、Agent /agents、运营看板 /dashboard、流程 /task-types、
                      组织设置 /settings（仅组织负责人或持有「组织设置」权限的角色可见）、
                      邀请接受 /invite/[token]（公开）、
                      平台后台 /admin（独立外壳与登录页 /admin/login，组织 /admin/organizations、
                      全局价格表 /admin/pricing、平台管理员 /admin/admins）
```

## 静态托管注意事项

`next.config.ts` 设了 `output: "export"` 和 `trailingSlash: true`，每个页面导出为 `out/<路径>/index.html`，普通静态文件服务器直接可用。

动态路由 `/tasks/[id]`、`/goals/[id]`、`/invite/[token]` 无法在构建时枚举参数，只导出了占位页 `out/tasks/_/index.html`、`out/goals/_/index.html`、`out/invite/_/index.html`，真实 id / token 由页面在浏览器里从地址栏读取。Go 服务需要三条回落规则：

- `/tasks/<任意>/` → `out/tasks/_/index.html`
- `/goals/<任意>/` → `out/goals/_/index.html`
- `/invite/<任意>/` → `out/invite/_/index.html`

其余页面都是固定路径（含 `/settings/`、`/admin/`、`/admin/login/`、`/admin/organizations/`、`/admin/pricing/`、`/admin/admins/`），直接对应 `out/<路径>/index.html`；未命中的路径回落到 `out/404.html` 即可。平台后台用独立 Cookie `axiomos_admin`，页面本身不需要特殊托管规则。
