-- 增量迁移：子账号与域名级权限（已有数据库执行这个，不要跑 schema.sql，那会清空数据）
--
-- 背景：
--   面向中小企业的多员工协作场景。管理员创建子账号后，只能操作被授权的域名；
--   其他域名的记录、绑定、校验、生效检测一律不可见也不可改。
--
-- 执行方式：
--   mysql -h HOST -P PORT -u USER -p dns_manager < migrations/002_user_scope.sql
--
-- 说明：
--   scope_all=1 表示不受域名限制（现有账号默认 1，保证升级后不受影响；
--   新建子账号时由接口写入 0 并授权具体域名）。

ALTER TABLE users ADD COLUMN scope_all TINYINT NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS user_domains (
  user_id INT UNSIGNED NOT NULL,
  domain_id INT UNSIGNED NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (user_id, domain_id),
  KEY idx_domain (domain_id)
) ENGINE=InnoDB;
