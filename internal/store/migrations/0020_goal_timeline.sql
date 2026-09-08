-- 二十期：路线图时间线的两个字段（ADR 0022）。
--
-- date_precision  时间粒度：week | month | quarter | half | year，空字符串等价于 week。
--                 语义是「日期填到多细为止」，路线图最细就到周（再细是甘特图的事）。
--                 与信心度正交；条的吸附、雾边、对外降精度都靠它。
-- rank            排序权重：泳道内的手动次序，空表示没手工排过，按日期与创建时间排。
--
-- 两列对旧目标都是空：旧目标的日期照旧按到周吸附画硬边，次序照旧按计划开始与创建时间。
-- 计划起止本身一个字都不改：粒度只管怎么画、对外分享降到多粗。
-- 汇总（父目标没填日期时从子目标与任务推算）只在读时算，永远不写回这两列所在的表。

alter table goals add column if not exists date_precision text not null default '';
alter table goals add column if not exists rank           double precision;

create index if not exists goals_rank_idx on goals(org_id, rank);
