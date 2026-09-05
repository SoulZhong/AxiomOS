-- 二期：多语言、平台管理员、组织角色、邀请。

alter table accounts add column if not exists locale text not null default '';
alter table accounts add column if not exists platform_admin boolean not null default false;
alter table organizations add column if not exists default_locale text not null default 'zh-CN';

-- 组织自定义角色；名称多语言
create table if not exists roles (
  org_id      text not null references organizations(id),
  name        text not null,
  titles      jsonb not null default '{}',
  permissions text[] not null default '{}',
  builtin     boolean not null default false,
  created_at  timestamptz not null default now(),
  primary key (org_id, name)
);

-- 能力标签名称改为多语言（保留旧 title 列作为 zh-CN 回退）
alter table capabilities add column if not exists titles jsonb not null default '{}';
update capabilities set titles = jsonb_build_object('zh-CN', title) where titles = '{}'::jsonb;

create table if not exists invitations (
  id          text primary key,
  org_id      text not null references organizations(id),
  email       text not null,
  name        text not null default '',
  roles       text[] not null default '{}',
  token_hash  text not null unique,
  invited_by  text,
  expires_at  timestamptz not null,
  accepted_at timestamptz,
  created_at  timestamptz not null default now()
);

-- 平台管理员会话：org_id 为空字符串
alter table sessions alter column org_id set default '';

do $$
declare t text;
begin
  foreach t in array array['roles','invitations']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
