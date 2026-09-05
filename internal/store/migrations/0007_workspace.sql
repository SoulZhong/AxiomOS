-- 七期：工作台布局（ADR 0015）。
--
-- 首页是按角色配置的工作台。一条布局要么属于一个角色（role_name），要么是某个成员的
-- 个人微调（member_id），二者只居其一。区块键与预设键是系统定义的，这里只存组织的选择。
-- blocks 是有序的区块键数组；preset 记下套用的预设键，逐块改过后为空。

create table if not exists workspace_layouts (
  id         bigserial primary key,
  org_id     text not null references organizations(id),
  role_name  text,
  member_id  text,
  blocks     jsonb not null default '[]',
  preset     text,
  updated_at timestamptz not null default now(),
  constraint workspace_layouts_one_target check ((role_name is null) <> (member_id is null))
);
create unique index if not exists workspace_layouts_role_idx on workspace_layouts(org_id, role_name) where role_name is not null;
create unique index if not exists workspace_layouts_member_idx on workspace_layouts(org_id, member_id) where member_id is not null;

do $$
declare t text;
begin
  foreach t in array array['workspace_layouts']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
