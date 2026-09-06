-- 十七期：通知外发（ADR 0019，docs/plans/2026-09-kaneo-informed-features.md 第 7 项）。
--
-- notification_channels    组织级通道配置：一行一个通道（feishu / wecom / email / webhook）。非保密字段明文放 config，
--                          保密字段合成一个 JSON 用服务端密钥加密放 secrets_enc（与 org_directory 同一套做法）。
-- notification_policies    组织允许外发的事件类型；没有这一行时全部允许。
-- preferences              个人通知偏好复用显示偏好表：subject_kind = 'notify'，subject_id = 成员 ID，data 只存明确设过的规则与安静时段。
-- notification_deliveries  每次外发一条投递：事项（站内通知 / 待确认操作 / 逾期任务 / 到期里程碑）、通道、状态、错误原话、次数；
--                          同一事项、同一收件人、同一通道只发一次（唯一键去重）。

create table if not exists notification_channels (
  org_id      text not null references organizations(id),
  channel     text not null,
  enabled     boolean not null default true,
  config      jsonb not null default '{}',
  secrets_enc bytea,
  updated_at  timestamptz not null default now(),
  primary key (org_id, channel)
);

create table if not exists notification_policies (
  org_id        text primary key references organizations(id),
  allowed_kinds text[] not null default '{}',
  updated_at    timestamptz not null default now()
);

alter table preferences drop constraint if exists preferences_subject_kind_check;
alter table preferences add constraint preferences_subject_kind_check check (subject_kind in ('member', 'role', 'notify'));

create table if not exists notification_deliveries (
  id              bigserial primary key,
  org_id          text not null references organizations(id),
  item_id         text not null,                     -- notification:<id> | proposal:<id> | task:<id>:overdue | milestone:<id>:due | test:<id>
  notification_id bigint,
  kind            text not null,
  member_id       text not null,
  channel         text not null,
  status          text not null default 'queued',    -- queued | sent | failed | skipped
  title           text not null default '',
  body            text not null default '',
  url             text not null default '',
  error           text not null default '',
  attempts        integer not null default 0,
  next_at         timestamptz not null default now(),
  created_at      timestamptz not null default now(),
  sent_at         timestamptz,
  unique (org_id, item_id, member_id, channel)
);
create index if not exists notification_deliveries_queue_idx on notification_deliveries(next_at) where status = 'queued';
create index if not exists notification_deliveries_member_idx on notification_deliveries(org_id, member_id, created_at desc);
create index if not exists notification_deliveries_org_idx on notification_deliveries(org_id, created_at desc);

do $$
declare t text;
begin
  foreach t in array array['notification_channels', 'notification_policies', 'notification_deliveries']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
