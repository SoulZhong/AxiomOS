-- 二十七期：用量自动采集（ADR 0030）。
--
-- agent_usage_sessions  客户端（Claude Code 等）按会话上报的累计用量，按 (Agent, 会话, 模型) 一行；
--                       客户端每轮结束时把整个会话的累计数报上来，服务端与上一次比较得出增量，重报不会重复计。
-- agent_usage_entries   每次得出的增量一条：那一刻 Agent 有开着的执行记录就归口到它（run_id / task_id），
--                       没有就是「未归口用量」（两者为空），在 Agent 名下按时间累着，组织概览与 Agent 页看得到。

create table if not exists agent_usage_sessions (
  org_id             text not null references organizations(id),
  agent_id           text not null references agents(id),
  session_key        text not null,
  client             text not null default '',
  model_id           text not null,
  input_tokens       bigint not null default 0,
  output_tokens      bigint not null default 0,
  cache_read_tokens  bigint not null default 0,
  cache_write_tokens bigint not null default 0,
  updated_at         timestamptz not null default now(),
  primary key (org_id, agent_id, session_key, model_id)
);

create table if not exists agent_usage_entries (
  id                 text primary key,
  org_id             text not null references organizations(id),
  agent_id           text not null references agents(id),
  session_key        text not null,
  client             text not null default '',
  model_id           text not null,
  run_id             text references runs(id),
  task_id            text references tasks(id),
  input_tokens       bigint not null default 0,
  output_tokens      bigint not null default 0,
  cache_read_tokens  bigint not null default 0,
  cache_write_tokens bigint not null default 0,
  at                 timestamptz not null default now()
);
create index if not exists agent_usage_entries_agent_idx on agent_usage_entries(org_id, agent_id, at);

do $$
declare t text;
begin
  foreach t in array array['agent_usage_sessions', 'agent_usage_entries']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
