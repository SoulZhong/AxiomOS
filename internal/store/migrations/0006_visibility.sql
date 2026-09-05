-- 六期：范围、共享边界与可见性策略（ADR 0013）。
--
-- "谁能看到多少" 是管理策略，不是系统正确性：不同客户的文化差别很大，私有化部署
-- 的客户尤其如此。所以把它做成组织级设置 + 团队上的共享边界标记，代码里只留默认值。
-- 成本归口（一条执行记录算在执行者所属团队头上）不在此列，那是 "算得对" 的问题，
-- 全系统只有一个口径。
--
-- collaboration_visibility: org（默认）| boundary（按共享边界）| team_tree（只看自己团队及下级）
-- finance_visibility:       team_tree（默认）| org | boundary
-- teams.is_boundary:        标记为共享边界的团队，其整棵子树内部互相可见

alter table organizations
  add column if not exists collaboration_visibility text not null default 'org',
  add column if not exists finance_visibility text not null default 'team_tree';

alter table teams
  add column if not exists is_boundary boolean not null default false;

update organizations set collaboration_visibility='org' where collaboration_visibility not in ('org','boundary','team_tree');
update organizations set finance_visibility='team_tree' where finance_visibility not in ('org','boundary','team_tree');
