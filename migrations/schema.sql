-- DNS 统一管理平台（重复导入会先删旧表）
CREATE DATABASE IF NOT EXISTS dns_manager DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE dns_manager;

DROP TABLE IF EXISTS notify_channels;
DROP TABLE IF EXISTS backups;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS operation_logs;
DROP TABLE IF EXISTS sync_logs;
DROP TABLE IF EXISTS sync_queue;
DROP TABLE IF EXISTS record_provider_map;
DROP TABLE IF EXISTS records;
DROP TABLE IF EXISTS domain_providers;
DROP TABLE IF EXISTS domains;
DROP TABLE IF EXISTS cloud_accounts;
DROP TABLE IF EXISTS users;

CREATE TABLE users (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  username VARCHAR(64) NOT NULL UNIQUE,
  password_hash VARCHAR(255) NOT NULL,
  role ENUM('admin','operator','viewer') NOT NULL DEFAULT 'operator',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_login_at DATETIME NULL
) ENGINE=InnoDB;

CREATE TABLE cloud_accounts (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  provider ENUM('tencent','aliyun','volc','spaceship') NOT NULL,
  name VARCHAR(64) NOT NULL,
  access_key_id VARBINARY(512) NOT NULL,
  access_key_secret VARBINARY(1024) NOT NULL,
  extra TEXT NULL,
  status TINYINT NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_provider_name (provider, name)
) ENGINE=InnoDB;

CREATE TABLE domains (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(255) NOT NULL UNIQUE,
  status ENUM('active','pending','error','disabled') NOT NULL DEFAULT 'active',
  remark VARCHAR(255) NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB;

CREATE TABLE domain_providers (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  domain_id INT UNSIGNED NOT NULL,
  account_id INT UNSIGNED NOT NULL,
  provider ENUM('tencent','aliyun','volc','spaceship') NOT NULL,
  provider_domain_id VARCHAR(128) NULL,
  sync_enabled TINYINT NOT NULL DEFAULT 1,
  last_verify_at DATETIME NULL,
  verify_status ENUM('ok','fail','unknown') NOT NULL DEFAULT 'unknown',
  verify_message VARCHAR(255) NULL,
  UNIQUE KEY uk_domain_account (domain_id, account_id),
  KEY idx_domain (domain_id)
) ENGINE=InnoDB;

CREATE TABLE records (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  domain_id INT UNSIGNED NOT NULL,
  sub_domain VARCHAR(255) NOT NULL DEFAULT '@',
  type VARCHAR(10) NOT NULL,
  line VARCHAR(64) NOT NULL DEFAULT '默认',
  value VARCHAR(2048) NOT NULL,
  ttl INT UNSIGNED NOT NULL DEFAULT 600,
  priority INT UNSIGNED NULL,
  remark VARCHAR(255) NULL,
  status ENUM('active','disabled','pending') NOT NULL DEFAULT 'active',
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_records_identity (domain_id, line, sub_domain, type, value(191)),
  KEY idx_domain (domain_id),
  KEY idx_domain_host (domain_id, sub_domain, type)
) ENGINE=InnoDB;

CREATE TABLE record_provider_map (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  record_id INT UNSIGNED NOT NULL,
  account_id INT UNSIGNED NOT NULL,
  provider ENUM('tencent','aliyun','volc','spaceship') NOT NULL,
  provider_record_id VARCHAR(128) NULL,
  sync_status ENUM('pending','success','failed','deleted') NOT NULL DEFAULT 'pending',
  sync_message VARCHAR(512) NULL,
  synced_at DATETIME NULL,
  retry_count INT UNSIGNED NOT NULL DEFAULT 0,
  UNIQUE KEY uk_record_account (record_id, account_id),
  KEY idx_record (record_id),
  KEY idx_account (account_id),
  KEY idx_retry (sync_status, retry_count)
) ENGINE=InnoDB;

CREATE TABLE sync_queue (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  record_id INT UNSIGNED NOT NULL,
  account_id INT UNSIGNED NOT NULL,
  action ENUM('create','update','delete') NOT NULL,
  payload TEXT NOT NULL,
  status ENUM('pending','processing','success','failed') NOT NULL DEFAULT 'pending',
  retry_count INT UNSIGNED NOT NULL DEFAULT 0,
  last_error VARCHAR(512) NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  executed_at DATETIME NULL,
  next_retry_at DATETIME NULL,
  KEY idx_status (status, next_retry_at),
  KEY idx_record (record_id),
  KEY idx_account (account_id)
) ENGINE=InnoDB;

CREATE TABLE sync_logs (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  record_id INT UNSIGNED NULL,
  account_id INT UNSIGNED NOT NULL,
  provider VARCHAR(16) NOT NULL,
  action VARCHAR(16) NOT NULL,
  status ENUM('success','failed') NOT NULL,
  message VARCHAR(512) NULL,
  request_cost_ms INT UNSIGNED NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_record (record_id),
  KEY idx_created (created_at),
  KEY idx_account_created (account_id, created_at)
) ENGINE=InnoDB;

CREATE TABLE operation_logs (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  user_id INT UNSIGNED NULL,
  username VARCHAR(64) NULL,
  action VARCHAR(64) NOT NULL,
  target VARCHAR(255) NULL,
  detail TEXT NULL,
  ip VARCHAR(45) NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_created (created_at)
) ENGINE=InnoDB;

CREATE TABLE backups (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  data LONGBLOB NOT NULL,
  is_gzip TINYINT NOT NULL DEFAULT 0,
  record_count INT UNSIGNED NOT NULL DEFAULT 0,
  created_by VARCHAR(64) NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB;

CREATE TABLE notify_channels (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(64) NOT NULL,
  type ENUM('wecom','feishu','dingtalk','webhook') NOT NULL,
  config TEXT NOT NULL,
  events TEXT NOT NULL,
  enabled TINYINT NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB;

CREATE TABLE settings (
  k VARCHAR(64) PRIMARY KEY,
  v TEXT NULL
) ENGINE=InnoDB;
