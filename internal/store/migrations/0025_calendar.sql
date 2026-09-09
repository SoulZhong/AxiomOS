-- 二十五期：外部日历与日程（ADR 0032）。
--
-- org_calendars        组织级的外部日历连接，一家提供方一行（飞书、企业微信、Google 日历可以同时接）。
--                      credentials 明文字段（如企业日历 ID、OAuth 客户端 ID），secrets_enc 保密字段整体加密；
--                      飞书 / 企业微信没填的字段自动沿用 IM 集成里同一提供方的凭据。
--                      last_*  最近一次同步的时间、结果（ok | failed）、失败原因（完整句子，界面直接显示）。
-- calendar_identities  成员与外部日历账号的绑定：飞书 / 企业微信沿用外部目录的身份（这里只在成员手动指定时才有行）；
--                      Google 由成员自己授权，刷新令牌加密存在 secrets_enc 里，email 只用来显示"连的是谁"。
-- calendar_events      同步来的副本：只存忙闲、标题、起止、全天、链接；不存参会人与正文。
--                      按 (org, provider, member, external_id) 幂等写入；同步时把区间内不再返回的删掉。

create table if not exists org_calendars (
  org_id        text not null references organizations(id),
  provider      text not null,
  credentials   jsonb not null default '{}',
  secrets_enc   bytea,
  proxy_url     text not null default '',
  enabled       boolean not null default true,
  last_sync_at  timestamptz,
  last_status   text not null default '',
  last_error    text not null default '',
  updated_at    timestamptz not null default now(),
  primary key (org_id, provider)
);

create table if not exists calendar_identities (
  id               text primary key,
  org_id           text not null references organizations(id),
  provider         text not null,
  member_id        text not null references members(id),
  external_user_id text not null default '',
  email            text not null default '',
  secrets_enc      bytea,
  connected_at     timestamptz not null default now(),
  unique (org_id, provider, member_id)
);

create table if not exists calendar_events (
  id          text primary key,
  org_id      text not null references organizations(id),
  provider    text not null,
  member_id   text not null references members(id),
  external_id text not null,
  title       text not null default '',
  starts_at   timestamptz not null,
  ends_at     timestamptz not null,
  all_day     boolean not null default false,
  busy        boolean not null default true,
  url         text not null default '',
  updated_at  timestamptz not null default now(),
  unique (org_id, provider, member_id, external_id)
);
create index if not exists calendar_events_member_time_idx on calendar_events(org_id, member_id, starts_at);

do $$
declare t text;
begin
  foreach t in array array['org_calendars', 'calendar_identities', 'calendar_events']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
