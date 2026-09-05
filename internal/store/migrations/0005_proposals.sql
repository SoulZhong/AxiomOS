-- 四期：待确认操作（ADR 0003）。Agent 的某项授权是「需要人确认」时，动作不立即执行，
-- 而是记成一条待确认操作，等人点确认后由系统以该 Agent 的身份重新执行。
--
-- 0001 里有一张同名的占位表（从未写入过数据），这里换成完整结构。

drop table if exists proposals;

create table proposals (
  id           text primary key,
  org_id       text not null references organizations(id),
  agent_id     text not null,                     -- 发起的 Agent
  owner_id     text not null,                     -- Agent 的所有者，默认的确认人
  action       text not null,                     -- 动作代码名，如 task.transition
  grant_name   text not null default '',          -- 触发这个动作所需的授权
  target_kind  text not null default '',          -- task | goal | sprint | task_type | agent
  target_id    text not null default '',
  target_title text not null default '',
  payload      jsonb not null default '{}',       -- 再执行这个动作所需的全部输入
  summary      jsonb not null default '{}',       -- {语言: 完整句子}，说明确认后会发生什么
  status       text not null default 'pending',   -- pending|approved|rejected|expired
  decided_by   text,
  decided_at   timestamptz,
  reason       text not null default '',          -- 拒绝理由（完整句子）
  created_at   timestamptz not null default now(),
  expires_at   timestamptz not null
);
create index if not exists proposals_org_status_idx on proposals(org_id, status);
create index if not exists proposals_org_agent_idx on proposals(org_id, agent_id);

do $$
declare t text;
begin
  foreach t in array array['proposals']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
