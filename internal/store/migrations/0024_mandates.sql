-- 二十四期：委托（ADR 0028）。
--
-- mandates   所有者把一个任务交给一个 Agent 做到底的凭据。同一任务、同一 Agent 同时最多一份活动中的委托。
--            plan_id      来自哪份目标方案（待确认操作的 ID），直接指派时为空。
--            goal_id / type_version  发出时任务的目标与类型版本；任一变化即失效。
--            budget_*     预算，本期只存不算（ADR 0030 再记账）。
--            side_effects 对外动作逐项许可，jsonb {external_link: true}。
--            ask_me       这份委托里仍要问人的事，text[]：reassign | cancel | create_outside_plan | accept。
--            grants       发出时 Agent 的授权快照，jsonb {grant: mode}；委托内的判定与重放都用它。
--            task_version 发出时任务的版本；assignee_id 发出时的负责人。任一变化即失效。
--            status       active | revoked | stale。
--
-- tasks.version / goals.version  每次写入加一，待确认操作与委托据此判断对象有没有变。
-- tasks.acceptance_mode          human（默认）| auto：自动验收由人在方案里逐任务选。
-- tasks.plan_id                  来自哪份目标方案。
-- proposals.bundle_id            同一任务上同一 Agent 连续提出的多条合成一捆。
-- proposals.target_version       提出时对象的版本；答的时候不一致即失效（stale）。
-- proposals.grants_snapshot      提出时 Agent 的授权快照，重放用它。
-- runs.mandate_id                在哪份委托之下开的执行记录。

create table if not exists mandates (
  id            text primary key,
  org_id        text not null references organizations(id),
  task_id       text not null references tasks(id),
  agent_id      text not null references agents(id),
  owner_id      text not null references members(id),
  plan_id       text not null default '',
  goal_id       text not null default '',
  type_version  int not null default 0,
  budget_cost   numeric(14,2),
  budget_tokens bigint,
  deadline      date,
  side_effects  jsonb not null default '{}',
  ask_me        text[] not null default '{}',
  grants        jsonb not null default '{}',
  task_version  int not null default 1,
  assignee_id   text not null default '',
  status        text not null default 'active',
  reason        text not null default '',
  created_at    timestamptz not null default now(),
  ended_at      timestamptz
);
create index if not exists mandates_task_idx on mandates(org_id, task_id);
create index if not exists mandates_agent_idx on mandates(org_id, agent_id);
create unique index if not exists mandates_active_uniq on mandates(org_id, task_id, agent_id) where status = 'active';

alter table tasks add column if not exists version int not null default 1;
alter table tasks add column if not exists acceptance_mode text not null default 'human';
alter table tasks add column if not exists plan_id text not null default '';
alter table goals add column if not exists version int not null default 1;
alter table proposals add column if not exists bundle_id text not null default '';
alter table proposals add column if not exists target_version int not null default 0;
alter table proposals add column if not exists grants_snapshot jsonb not null default '{}';
alter table runs add column if not exists mandate_id text not null default '';
create index if not exists proposals_bundle_idx on proposals(org_id, bundle_id);

do $$
declare t text;
begin
  foreach t in array array['mandates']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
