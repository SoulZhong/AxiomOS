-- 十三期：IM 集成的冲突决定（ADR 0017 补记四）。预览里认出的候选（邮箱 / 手机号 / 姓名且部门同名 / 团队同名同层级）
-- 要人决定：合并到已有（merge，local_id 是本地对象）、作为新建（create）、跳过（skip）。决定存下来，下次预览不再问；
-- 「跳过」的对象列为已跳过，可随时改主意（删除这条记录）。external_name 只为界面上的「已跳过」列表留个名字。
create table if not exists directory_decisions (
  id            text primary key,
  org_id        text not null references organizations(id),
  provider      text not null,
  kind          text not null,            -- member | team
  external_id   text not null,
  external_name text not null default '',
  decision      text not null,            -- merge | create | skip
  local_id      text,
  decided_by    text not null default '',
  decided_at    timestamptz not null default now(),
  unique (org_id, provider, kind, external_id)
);

do $$
declare t text;
begin
  foreach t in array array['directory_decisions']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
