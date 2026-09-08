-- 二十二期：幂等键（ADR 0025 第 3 条）。
--
-- idempotency_keys  写操作的"这件事我已经做过了"记录。客户端自己生成一个键（≤64 字符），
--                   同一个组织、同一个执行者、同一个键在 24 小时内只生效一次：第二次不再写，
--                   直接把第一次的结果原样返回，并注明"这次没有重复创建"。
--
-- args_hash  归一化后的参数的 SHA-256。同一个键换了参数不能悄悄返回旧结果——那会让人以为
--            新的意图生效了，实际什么也没发生。这种情况直接拒绝，让人换一个键。
-- endpoint   工具名（MCP）或接口路径（HTTP）。换了工具也算参数不同，一起拒绝。
-- result_ref 第一次写出来的对象 ID（任务、目标、里程碑…），便于顺着它去看。
-- result     第一次的完整结果快照，重复调用时原样返回。
-- expires_at 24 小时后过期，由后台巡检清掉；过期之后同一个键可以重新使用。

create table if not exists idempotency_keys (
  id          text primary key,
  org_id      text not null references organizations(id),
  executor_id text not null,
  key         text not null,
  endpoint    text not null,
  args_hash   text not null,
  result_ref  text not null default '',
  result      jsonb not null default 'null',
  created_at  timestamptz not null default now(),
  expires_at  timestamptz not null
);
create unique index if not exists idempotency_keys_uniq on idempotency_keys(org_id, executor_id, key);
create index if not exists idempotency_keys_expiry_idx on idempotency_keys(org_id, expires_at);

do $$
declare t text;
begin
  foreach t in array array['idempotency_keys']
  loop
    execute format('alter table %I enable row level security', t);
    execute format('alter table %I force row level security', t);
    execute format('drop policy if exists org_isolation on %I', t);
    execute format('create policy org_isolation on %I using (org_id = current_setting(''app.org_id'', true)) with check (org_id = current_setting(''app.org_id'', true))', t);
  end loop;
end $$;
grant select, insert, update, delete on all tables in schema public to axiomos_app;
grant usage, select on all sequences in schema public to axiomos_app;
