-- 十九期：路线图三字段（ADR 0021）。
--
-- horizon     时间桶：now | next | later，空字符串表示还没排期。
-- confidence  信心度：high | medium | low，空字符串表示没填。
-- outcome     一句话成果指标，纯文本，最多 200 字。
--
-- 三列都默认空：旧目标一律显示为「还没排期」，既有行为不受影响，不做任何回填。
-- 时间桶与计划起止互不覆盖：有精确日期时时间桶只用来分组。

alter table goals add column if not exists horizon    text not null default '';
alter table goals add column if not exists confidence text not null default '';
alter table goals add column if not exists outcome    text not null default '';

create index if not exists goals_horizon_idx on goals(org_id, horizon);
