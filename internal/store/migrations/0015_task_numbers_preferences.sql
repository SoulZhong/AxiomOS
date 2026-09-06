-- 十五期：任务序号与显示偏好（docs/plans/2026-09-kaneo-informed-features.md 第 4 项）。
--
-- 任务序号：组织内从 1 递增的整数，界面上写成 #123。已有任务按创建时间回填；新任务在插入时从
-- organizations.next_task_number 原子取号（update … returning，行锁保证同一组织不重号）。
-- 与 0009 / 0010 一样，回填跨组织执行，依赖迁移以表所有者 / 超级用户身份运行。
alter table tasks add column if not exists number integer;
alter table organizations add column if not exists next_task_number integer not null default 1;

with numbered as (
  select id, row_number() over (partition by org_id order by created_at, id) as n from tasks
)
update tasks t set number = numbered.n from numbered where t.id = numbered.id and t.number is null;

update organizations o set next_task_number = coalesce((select max(number) from tasks where org_id = o.id), 0) + 1;

alter table tasks alter column number set not null;
create unique index if not exists tasks_org_number_idx on tasks(org_id, number);

-- 显示偏好：一条记录属于一个成员（个人）或一个角色（组织给这类人的默认），data 是部分对象——
-- 只存明确设过的字段，解析时个人 → 角色 → 系统默认逐层补齐（ADR 0015 的解析顺序）。
create table if not exists preferences (
  id           bigserial primary key,
  org_id       text not null references organizations(id),
  subject_kind text not null check (subject_kind in ('member', 'role')),
  subject_id   text not null,
  data         jsonb not null default '{}',
  updated_at   timestamptz not null default now(),
  unique (org_id, subject_kind, subject_id)
);

do $$
declare t text;
begin
  foreach t in array array['preferences']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
