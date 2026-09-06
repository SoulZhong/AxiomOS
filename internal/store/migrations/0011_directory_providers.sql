-- 十一期：外部目录的提供方抽象（ADR 0017 补记）。凭据不再是飞书专属的 app_id / app_secret_enc 两列，
-- 而是按提供方声明的字段存成一个 JSON 对象：非保密字段明文放 credentials，保密字段合成一个 JSON 再整体加密放 secrets_enc。
--
-- 旧列 app_id / app_secret_enc 先保留：密文只有服务端密钥能解，SQL 做不了转换，由 Go 在第一次读到
-- "旧列有值、新列为空"的行时就地转换并写回（app.loadDirectoryConfig）。后续清理再删旧列。
alter table org_directory
  add column if not exists credentials jsonb not null default '{}',
  add column if not exists secrets_enc bytea;
alter table org_directory alter column provider drop default;
alter table org_directory alter column root_department_id drop default;
