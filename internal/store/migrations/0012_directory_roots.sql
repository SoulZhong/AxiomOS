-- 十二期：外部目录的多个同步根部门（ADR 0017 补记二）。飞书应用的通讯录权限范围可以只含部分部门，
-- 接入向导让人从被授权的部门里勾选几个作为同步根，各自成为顶层团队。
-- root_department_id 保留做兼容：等于列表的第一个，列表为空时是提供方的根（整个企业）。
alter table org_directory
  add column if not exists root_department_ids text[] not null default '{}';
