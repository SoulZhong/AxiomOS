-- 八期：里程碑（ADR 0016）。里程碑属于目标，是目标在时间轴上的刻度，不是任务：
-- 没有执行者、流程、执行记录与成本。「已达到」由人确认（reached_at），状态由日期算出，不落库。
-- 目标被删时里程碑随之删除（on delete cascade）：里程碑是目标的一部分，不单独存活。

create table if not exists milestones (
  id          text primary key,
  org_id      text not null references organizations(id),
  goal_id     text not null references goals(id) on delete cascade,
  title       text not null,
  description text not null default '',
  due_on      date not null,
  reached_at  timestamptz,
  created_by  text not null,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
);
create index if not exists milestones_goal_idx on milestones(org_id, goal_id);

do $$
declare t text;
begin
  foreach t in array array['milestones']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
