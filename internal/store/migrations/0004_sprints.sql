-- 三期：看板与迭代（ADR 0012）。迭代是时间盒容器，任务通过 sprint_id 归属；
-- 工作量 points 是任务的可选整数字段。燃尽图由动态回放，不另存快照。

create table if not exists sprints (
  id         text primary key,
  org_id     text not null references organizations(id),
  team_id    text references teams(id),          -- 空表示按组织
  name       text not null,
  goal       text not null default '',
  starts_on  date not null,
  ends_on    date not null,
  status     text not null default 'planning',   -- planning|active|closed
  created_by text not null,
  started_at timestamptz,
  closed_at  timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
create index if not exists sprints_org_status_idx on sprints(org_id, status);
create index if not exists sprints_team_idx on sprints(org_id, team_id);

alter table tasks add column if not exists points int;
alter table tasks add column if not exists sprint_id text references sprints(id);
create index if not exists tasks_sprint_idx on tasks(org_id, sprint_id);

do $$
declare t text;
begin
  foreach t in array array['sprints']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
