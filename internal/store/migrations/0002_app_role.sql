-- 行级安全对超级用户不生效。为此建立一个非超级用户角色 axiomos_app，
-- 每个组织事务都 SET LOCAL ROLE 到它，让策略对任何连接用户都生效。
do $$
begin
  if not exists (select 1 from pg_roles where rolname = 'axiomos_app') then
    create role axiomos_app nologin;
  end if;
end $$;
grant usage on schema public to axiomos_app;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
alter default privileges in schema public grant select, insert, update, delete on tables to axiomos_app;
alter default privileges in schema public grant usage, select on sequences to axiomos_app;
grant axiomos_app to current_user;
