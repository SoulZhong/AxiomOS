-- 十期：「待我处理」与「组织概况」成为区块（ADR 0015 补记四）。
--
-- 页面上不再有布局之外的固定区域，所以已存的个人与角色布局要在最上方补上 inbox（0,0,12×2）与
-- readouts（0,2,12×1），其余区块整体下移 3 行；之后是否保留由使用者决定。
--
-- blocks 先后存过三种形状：["my_tasks", ...]、[{key, w, h}]、[{key, x, y, w, h}]，同一数组里也可能混用。
-- 这里不在 SQL 里重做排布，而是统一改写成读取层（domain.ParseLayout）能直接消化的形状：
--   * 字符串元素改成 {key} 对象（不给坐标，读取时按顺序致密排布，落在带坐标的区块之下）；
--   * 带 x/y 的元素 y 加 3（原地下移）；
--   * 只有 key/w/h 的元素原样保留（同样在读取时排布）。
-- 再把 inbox 与 readouts 带坐标放在数组最前。ParseLayout 先固定带坐标的区块、再按顺序给其余区块找第一个
-- 放得下的格子，所以无论旧行是哪种形状，inbox 都在左上角、readouts 紧随其后、原有区块保持相对顺序。
--
-- 幂等：已含 inbox 的数组跳过。空数组是「明确清除」的记录，保持为空。套用执行视角（preset = 'doer'）的角色行只补
-- inbox 不补 readouts，与新的执行视角预设一致（一线成员不需要组织读数）。
-- 与 0009 一样，这条更新跨组织执行，依赖迁移以表所有者 / 超级用户身份运行（行级安全对其不生效）。

update workspace_layouts
set blocks =
  (case when preset = 'doer'
        then '[{"key":"inbox","x":0,"y":0,"w":12,"h":2}]'::jsonb
        else '[{"key":"inbox","x":0,"y":0,"w":12,"h":2},{"key":"readouts","x":0,"y":2,"w":12,"h":1}]'::jsonb
   end)
  || coalesce((
       select jsonb_agg(
                case
                  when jsonb_typeof(e) = 'string' then jsonb_build_object('key', e #>> '{}')
                  when jsonb_typeof(e) = 'object' and jsonb_typeof(e -> 'x') = 'number' and jsonb_typeof(e -> 'y') = 'number'
                    then e || jsonb_build_object('y', (e ->> 'y')::int + 3)
                  else e
                end
                order by ord)
       from jsonb_array_elements(blocks) with ordinality as t(e, ord)
     ), '[]'::jsonb),
    updated_at = now()
where jsonb_typeof(blocks) = 'array'
  and blocks <> '[]'::jsonb
  and not exists (
    select 1 from jsonb_array_elements(blocks) e
    where e = '"inbox"'::jsonb or (jsonb_typeof(e) = 'object' and e ->> 'key' = 'inbox')
  );
