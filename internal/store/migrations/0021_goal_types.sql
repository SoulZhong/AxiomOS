-- 二十一期：目标类型（ADR 0023）。
--
-- goal_types  组织自己维护的一张词表：名称、颜色、图标、排序、是否停用，外加一个
--             default_precision（新建这个类型的目标时预填的时间粒度，仍可逐个目标改）。
--             它只做分类与显示：**没有流程**、不决定权限、不决定成本归口、不决定可见范围。
--             这一条是写死的边界——目标本来就没有流程，给类型配流程会让目标退化成大任务。
-- goals.type_id  目标的类型，可空（空表示「未分类」）。旧目标全部为空，不做猜测性回填。
--
-- 删除用 on delete restrict：还有目标在用的类型不许删，只能停用（与团队、能力标签同一套规则）。
-- 改名只改显示，不动历史：动态里记的是当时的名字。

create table if not exists goal_types (
  id                text primary key,
  org_id            text not null references organizations(id),
  name              text not null,
  color             text not null default '',
  icon              text not null default '',
  sort              int not null default 0,
  active            boolean not null default true,
  default_precision text not null default '',
  created_at        timestamptz not null default now(),
  updated_at        timestamptz not null default now()
);
create index if not exists goal_types_sort_idx on goal_types(org_id, sort, created_at);

alter table goals add column if not exists type_id text references goal_types(id) on delete restrict;
create index if not exists goals_type_idx on goals(org_id, type_id);

do $$
declare t text;
begin
  foreach t in array array['goal_types']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
