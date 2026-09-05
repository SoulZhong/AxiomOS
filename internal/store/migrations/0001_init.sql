-- AxiomOS 初始结构。所有组织内数据都带 org_id，并由行级安全强制隔离（ADR 0001）。
-- 应用层在每个事务开头执行：SET LOCAL app.org_id = '<org id>'。

create table if not exists accounts (
  id            text primary key,
  email         text not null unique,
  password_hash text not null,
  name          text not null,
  created_at    timestamptz not null default now()
);

create table if not exists organizations (
  id              text primary key,
  slug            text not null unique,
  name            text not null,
  owner_member_id text,
  currency        text not null default 'CNY',
  created_at      timestamptz not null default now(),
  deactivated_at  timestamptz
);

create table if not exists members (
  id         text primary key,
  org_id     text not null references organizations(id),
  account_id text not null references accounts(id),
  name       text not null,
  roles      text[] not null default '{}',
  active     boolean not null default true,
  created_at timestamptz not null default now(),
  unique (org_id, account_id)
);

create table if not exists teams (
  id             text primary key,
  org_id         text not null references organizations(id),
  parent_id      text references teams(id),
  name           text not null,
  lead_member_id text references members(id)
);

create table if not exists team_members (
  org_id    text not null,
  team_id   text not null references teams(id),
  member_id text not null references members(id),
  primary key (team_id, member_id)
);

create table if not exists capabilities (
  org_id text not null references organizations(id),
  name   text not null,
  title  text not null,
  primary key (org_id, name)
);

create table if not exists agents (
  id              text primary key,
  org_id          text not null references organizations(id),
  owner_member_id text not null references members(id),
  name            text not null,
  runtime         text not null default 'custom',
  capabilities    text[] not null default '{}',
  grants          jsonb not null default '{}',   -- {grant: "direct"|"with_approval"}
  max_concurrent  int not null default 1,
  shared          boolean not null default false,
  token_hash      text unique,
  last_seen_at    timestamptz,
  revoked_at      timestamptz,
  created_at      timestamptz not null default now()
);

create table if not exists task_types (
  org_id     text not null references organizations(id),
  name       text not null,
  version    int not null,
  title      text not null,
  built_in   boolean not null default false,
  definition jsonb not null,
  created_at timestamptz not null default now(),
  primary key (org_id, name, version)
);

create table if not exists task_type_current (
  org_id  text not null,
  name    text not null,
  version int not null,
  primary key (org_id, name)
);

create table if not exists goals (
  id                text primary key,
  org_id            text not null references organizations(id),
  parent_id         text references goals(id),
  team_id           text references teams(id),
  owner_member_id   text not null references members(id),
  title             text not null,
  description       text not null default '',
  status            text not null default 'active',   -- draft|active|achieved|abandoned
  budget            numeric(14,2),
  deadline          date,
  planned_start     date,
  planned_end       date,
  progress_override int,
  visibility        text not null default 'org',
  created_at        timestamptz not null default now(),
  updated_at        timestamptz not null default now()
);

create table if not exists tasks (
  id                    text primary key,
  org_id                text not null references organizations(id),
  goal_id               text references goals(id),
  parent_id             text references tasks(id),
  type_name             text not null,
  type_version          int not null,
  title                 text not null,
  description           text not null default '',
  creator_id            text not null,
  reviewer_id           text not null,
  assignee_id           text,
  required_role         text,
  pending_slot          text,
  required_capabilities text[] not null default '{}',
  human_only            boolean not null default false,
  state                 text not null,
  previous_state        text,
  last_weight           int not null default 0,
  participants          jsonb not null default '{}',
  fields                jsonb not null default '{}',
  priority              int not null default 2,
  estimate_hours        numeric(8,2),
  planned_start         date,
  planned_end           date,
  actual_start          timestamptz,
  actual_end            timestamptz,
  created_at            timestamptz not null default now(),
  updated_at            timestamptz not null default now()
);
create index if not exists tasks_goal_idx on tasks(org_id, goal_id);
create index if not exists tasks_assignee_idx on tasks(org_id, assignee_id);
create index if not exists tasks_state_idx on tasks(org_id, state);

create table if not exists task_relations (
  org_id     text not null,
  task_id    text not null references tasks(id),
  type       text not null,                 -- blocks | found_in | relates_to
  other_id   text not null references tasks(id),
  created_at timestamptz not null default now(),
  primary key (task_id, type, other_id)
);
create index if not exists task_relations_other_idx on task_relations(org_id, other_id, type);

create table if not exists artifacts (
  id         text primary key,
  org_id     text not null,
  task_id    text not null references tasks(id),
  type       text not null,
  title      text not null default '',
  ref        text not null default '',
  by_id      text not null,
  created_at timestamptz not null default now()
);

create table if not exists comments (
  id         text primary key,
  org_id     text not null,
  task_id    text not null references tasks(id),
  by_id      text not null,
  text       text not null,
  is_note    boolean not null default false,
  created_at timestamptz not null default now()
);

create table if not exists runs (
  id             text primary key,
  org_id         text not null,
  task_id        text not null references tasks(id),
  state          text not null,
  executor_id    text not null,
  started_at     timestamptz not null,
  ended_at       timestamptz,
  outcome        text,
  last_heartbeat timestamptz not null,
  usage          jsonb not null default '[]',
  cost           numeric(14,4) not null default 0
);
create index if not exists runs_task_idx on runs(org_id, task_id);
create index if not exists runs_active_idx on runs(org_id, executor_id) where ended_at is null;

create table if not exists events (
  id       bigserial primary key,
  org_id   text not null,
  type     text not null,
  task_id  text,
  actor_id text,
  at       timestamptz not null,
  data     jsonb not null default '{}'
);
create index if not exists events_task_idx on events(org_id, task_id, id);

create table if not exists sessions (
  token_hash text primary key,
  account_id text not null references accounts(id),
  org_id     text not null,
  expires_at timestamptz not null
);

-- 价格表：org_id 为空表示全局默认；每百万 token 的价格
create table if not exists price_book (
  id                      bigserial primary key,
  org_id                  text,
  model_id                text not null,
  input_per_million       numeric(12,4) not null default 0,
  output_per_million      numeric(12,4) not null default 0,
  cache_read_per_million  numeric(12,4) not null default 0,
  cache_write_per_million numeric(12,4) not null default 0,
  currency                text not null default 'USD',
  effective_from          timestamptz not null default now()
);

create table if not exists exchange_rates (
  org_id text not null,
  from_currency text not null,
  to_currency   text not null,
  rate          numeric(12,6) not null,
  primary key (org_id, from_currency, to_currency)
);

create table if not exists proposals (
  id          text primary key,
  org_id      text not null,
  agent_id    text not null,
  grant_name  text not null,
  action      jsonb not null,
  status      text not null default 'pending',   -- pending|approved|rejected
  decided_by  text,
  decided_at  timestamptz,
  created_at  timestamptz not null default now()
);

create table if not exists schedules (
  id                text primary key,
  org_id            text not null,
  cron              text not null,
  template          jsonb not null,
  next_run_at       timestamptz,
  created_by        text not null,
  created_at        timestamptz not null default now()
);

create table if not exists notifications (
  id         bigserial primary key,
  org_id     text not null,
  member_id  text not null,
  title      text not null,
  body       text not null default '',
  task_id    text,
  read_at    timestamptz,
  created_at timestamptz not null default now()
);

-- 行级安全：组织内表按 app.org_id 隔离；表所有者也受限（FORCE）。
do $$
declare t text;
begin
  foreach t in array array['members','teams','team_members','capabilities','agents','task_types','task_type_current',
    'goals','tasks','task_relations','artifacts','comments','runs','events','proposals','schedules','notifications','exchange_rates']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
