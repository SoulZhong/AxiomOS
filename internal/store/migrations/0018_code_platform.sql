-- 十八期：代码平台与外部事件（ADR 0020，docs/plans/2026-09-kaneo-informed-features.md 第 8 项）。
--
-- org_code_platform  组织的代码平台配置：形状与 org_directory 一致（非保密凭据明文放 credentials，
--                    保密字段合成一个 JSON 用服务端密钥加密放 secrets_enc）；webhook_secret_enc 是本系统
--                    生成、要填到代码平台回调里的签名密钥。
-- code_repos         选中要接的仓库，以及最后一次建回调的结果。
-- external_links     任务上挂的外部链接：PR 由 webhook 自动建与更新，其余手工添加。
-- webhook_events     去重：同一投递编号只处理一次（代码平台会重发）。
--
-- 外部身份复用 external_identities，kind = 'git_user'，external_id 是外部登录名。

create table if not exists org_code_platform (
  org_id             text primary key references organizations(id),
  provider           text not null default '',
  credentials        jsonb not null default '{}',
  secrets_enc        bytea,
  webhook_secret_enc bytea,
  proxy_url          text not null default '',
  updated_at         timestamptz not null default now()
);

create table if not exists code_repos (
  org_id     text not null references organizations(id),
  provider   text not null,
  repo_id    text not null,
  full_name  text not null,
  enabled    boolean not null default true,
  hook_ok    boolean not null default false,
  hook_error text not null default '',
  updated_at timestamptz not null default now(),
  primary key (org_id, provider, repo_id)
);

create table if not exists external_links (
  id          text primary key,
  org_id      text not null references organizations(id),
  task_id     text not null references tasks(id),
  provider    text not null default '',              -- github | gitlab | gitee | ''（手工链接）
  kind        text not null default 'other',         -- pr | issue | doc | design | other
  url         text not null,
  title       text not null default '',
  external_id text not null default '',              -- 平台上的唯一编号，如 github:owner/name#12
  status      text not null default '',              -- open | draft | merged | closed | passed | failed
  actor_name  text not null default '',              -- 外部操作者的登录名
  created_by  text not null default '',              -- 手工添加时的执行者
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
);
create unique index if not exists external_links_task_url_idx on external_links(org_id, task_id, url);
create index if not exists external_links_task_idx on external_links(org_id, task_id);
create index if not exists external_links_external_idx on external_links(org_id, external_id);

create table if not exists webhook_events (
  org_id      text not null references organizations(id),
  provider    text not null,
  delivery_id text not null,
  received_at timestamptz not null default now(),
  primary key (org_id, provider, delivery_id)
);
create index if not exists webhook_events_age_idx on webhook_events(received_at);

do $$
declare t text;
begin
  foreach t in array array['org_code_platform', 'code_repos', 'external_links', 'webhook_events']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
