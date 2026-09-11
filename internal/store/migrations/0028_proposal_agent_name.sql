-- 二十八期：待确认操作记下提交时 Agent 的名字。
-- 摘要句子是提交时生成并存下的文本，里面的名字会冻住；读的时候拿这个快照把它换成 Agent 现在的名字，改名后两边才对得上。
alter table proposals add column if not exists agent_name text not null default '';
