-- 九期：外部目录同步（ADR 0017）。飞书是组织结构（团队树）与人员名单的来源：
-- 部门 ↔ 团队一一映射，人员 ↔ 成员；离开的人只停用不删除，消失的部门标记为已停用。
--
-- org_directory        每个组织一份配置；App Secret 用服务端密钥加密后入库，界面只显示"是否已配置"。
-- external_identities  一个成员或团队在外部目录里的编号，多次同步之间靠它认出同一个人 / 同一个部门；
--                      同一对象可以有多个提供方的身份，为将来接企微、钉钉留位置。
-- directory_runs       每次同步一条：起止时间、新增 / 更新 / 停用数量、错误列表。
-- members.source/status 来源（manual | feishu）与激活状态：status 列只存 active | pending_activation，
--                      "已停用"仍以 active 布尔为准，读出来时派生成 inactive。
-- teams.source/external_name/active  团队来源、外部目录里的原名、是否仍存在于外部目录。

create table if not exists org_directory (
  org_id             text primary key references organizations(id),
  provider           text not null default 'feishu',
  app_id             text not null default '',
  app_secret_enc     bytea,
  root_department_id text not null default '0',
  default_role       text not null default '',
  schedule           text not null default 'manual',
  proxy_url          text not null default '',
  updated_at         timestamptz not null default now()
);

create table if not exists external_identities (
  id          text primary key,
  org_id      text not null references organizations(id),
  provider    text not null,
  kind        text not null,            -- member | team
  external_id text not null,
  local_id    text not null,
  created_at  timestamptz not null default now(),
  unique (org_id, provider, kind, external_id)
);
create index if not exists external_identities_local_idx on external_identities(org_id, kind, local_id);

create table if not exists directory_runs (
  id          text primary key,
  org_id      text not null references organizations(id),
  provider    text not null,
  started_at  timestamptz not null default now(),
  finished_at timestamptz,
  status      text not null default 'running',   -- running | ok | partial | failed
  counts      jsonb not null default '{}',
  errors      jsonb not null default '[]'
);
create index if not exists directory_runs_org_idx on directory_runs(org_id, started_at desc);

alter table members
  add column if not exists source text not null default 'manual',
  add column if not exists status text not null default 'active';
update members set status='active' where status not in ('active','pending_activation');

alter table teams
  add column if not exists source        text not null default 'manual',
  add column if not exists external_name text not null default '',
  add column if not exists active        boolean not null default true;

do $$
declare t text;
begin
  foreach t in array array['org_directory','external_identities','directory_runs']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
