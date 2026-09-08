-- 二十三期：幂等键先占位再干活（ADR 0025 第 3 条）。
--
-- 原来的顺序是"先做，做完再记键"：两个带同一个键的请求同时进来，都查不到键、都真做了一遍，
-- 唯一索引只挡住第二条记录，副作用已经各自提交完了——正是幂等键要防的事。
--
-- 改成"先占位再干活"：
--   state='pending'    已经占住这个键，活还在干。
--   state='done'       干完了，result 是那一次的结果，重复提交原样返回它。
-- started_at 是这次占位的时刻，配上 60 秒的租约：还在租约里说明真的有人在做，
--            后来的同键调用直接回一句「上一次的调用还在进行中」；超过租约说明上一次的进程死了，
--            后来者接手（把 started_at 推到现在）继续做。
--            干砸了就把占位删掉——失败本来就该允许重试。
--
-- 已有的行都是"干完了"的记录，所以 state 默认 done、started_at 默认建表时刻。

alter table idempotency_keys add column if not exists state      text        not null default 'done';
alter table idempotency_keys add column if not exists started_at timestamptz not null default now();
create index if not exists idempotency_keys_state_idx on idempotency_keys(org_id, state, started_at);
