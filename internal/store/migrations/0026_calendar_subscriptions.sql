-- 二十六期：日历订阅（ADR 0032 补记）。
--
-- calendar_identities  多四列：label 是外部日历的名字（订阅链接的 X-WR-CALNAME），
--                      last_* 是这个成员最近一次同步的时间、结果（ok | failed）、失败原因——成员自己在个人设置里看。
-- calendar_feeds       成员对外发布的订阅源：token 是链接里的密钥，谁拿到都能读这个人的任务、目标、里程碑（只读）。
--                      一人一条；重新生成就是换 token。

alter table calendar_identities add column if not exists label        text not null default '';
alter table calendar_identities add column if not exists last_sync_at timestamptz;
alter table calendar_identities add column if not exists last_status  text not null default '';
alter table calendar_identities add column if not exists last_error   text not null default '';

create table if not exists calendar_feeds (
  token      text primary key,
  org_id     text not null references organizations(id),
  member_id  text not null references members(id),
  created_at timestamptz not null default now(),
  unique (org_id, member_id)
);

alter table calendar_feeds enable row level security;
alter table calendar_feeds force row level security;
drop policy if exists org_isolation on calendar_feeds;
create policy org_isolation on calendar_feeds using (org_id = current_setting('app.org_id', true)) with check (org_id = current_setting('app.org_id', true));
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
