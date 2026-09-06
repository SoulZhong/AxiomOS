-- 十四期：邀请记下它要加入的团队和它预建的待激活成员（ADR 0017）。
-- 手工邀请与 CSV 导入、IM 同步一致：新邮箱先建一个待激活成员，邀请指向它；作废邀请时可以把这个从未激活的成员一并删掉。
alter table invitations add column if not exists team_id   text;
alter table invitations add column if not exists member_id text;
