-- 十六期：Agent 设备码授权（ADR 0018）。
--
-- Agent 端先向系统要一个设备码与一个 8 位验证码，人在网页里输入验证码批准并选授权，系统创建 Agent，
-- Agent 端轮询拿到令牌（只给一次）。这张表在组织之外（申请时还不知道属于哪个组织），不做行级安全；
-- 令牌用服务端密钥加密暂存，交付后即清空。
create table if not exists agent_device_codes (
  id               text primary key,
  device_code_hash text not null unique,
  user_code        text not null unique,
  client           text not null,
  name             text not null default '',
  status           text not null default 'pending',   -- pending | approved | denied | expired
  org_id           text,
  agent_id         text,
  approved_by      text,
  token_enc        bytea,
  token_taken_at   timestamptz,
  last_polled_at   timestamptz,
  decided_at       timestamptz,
  expires_at       timestamptz not null,
  created_at       timestamptz not null default now()
);
create index if not exists agent_device_codes_status_idx on agent_device_codes(status, expires_at);

-- 连接检查用：Agent 最近一次调用了哪个 MCP 工具，接入向导据此显示「已经在线并调用过 whoami」。
alter table agents add column if not exists last_tool text;
alter table agents add column if not exists last_tool_at timestamptz;

grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
