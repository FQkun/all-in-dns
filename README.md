# DNS 统一管理平台

一站式管理**腾讯云（DNSPod）、阿里云（Alidns）、火山引擎（DNS）、Spaceship** 四大平台的域名解析记录。在一处增删改，自动同步到所有已接入的云平台；支持 AI 诊断、解析生效检测与消息通知。

## 界面预览

**首页概览**

![首页](docs/home.png)

**域名解析列表**

![域名列表](docs/domains.png)

**云账号管理**

![云账号](docs/accounts.png)

## 核心功能

### 一、多平台同步

| 功能 | 作用 |
|---|---|
| **一处修改，多平台同步** | 改一次记录，自动推到腾讯云 / 阿里云 / 火山引擎 / Spaceship，不用挨个登录控制台 |
| **覆盖式同步引擎** | 同步队列 + 指数退避重试 + 「远端已存在」自动登记，避免重复写入和死循环报错 |
| **按厂商独立启用** | 一个域名可以只在部分平台生效（如三家用作热备份） |
| **写入前冲突校验** | A/AAAA 与 CNAME 不能共存等冲突在本地先拦截，避免各平台全部同步失败 |
| **记录归一化** | 线路映射、尾点剔除、MX 优先级剥离，消除各平台格式差异导致的重复记录 |
| **域名绑定 / 解绑账号** | 新增云账号后可随时绑定到已有域名，也可单独解绑某个平台 |

### 二、问题诊断

| 功能 | 作用 |
|---|---|
| **解析生效检测** | 通过阿里 / 腾讯 / Google 公共 DNS 实际查询，直接回答「改了为什么不生效」——已生效 / TTL 缓存中 / NS 未指向，一目了然 |
| **AI 诊断助手** | 右下角悬浮球，用大白话解释错误（如 `DomainRecordDuplicate`）并给出修复步骤。**只读查询，不会修改任何配置** |
| **一致性校验** | 对比本地与各平台远端记录，列出缺失 / 多余，支持一键补齐 |
| **从云平台拉取** | 把云端已有记录按归一化键合并导入本地（同一记录只存一份，记录出现在哪些平台），只增不删 |

### 三、运维与安全

| 功能 | 作用 |
|---|---|
| **消息通知** | 同步最终失败、域名验证失败、自动备份完成时，推送到企业微信 / 飞书 / 钉钉 / 通用 Webhook；同事件 5 分钟内去重，不刷屏 |
| **云账号安全** | AccessKey / Secret 使用 AES-256-GCM 加密入库，列表只显示掩码 |
| **备份与恢复** | 每日自动备份（保留 30 份），支持一键恢复（恢复前自动快照） |
| **导入 / 导出** | 解析记录批量导入导出 CSV |
| **认证与审计** | JWT 登录、角色权限（admin/operator/viewer）、操作日志与同步日志 |

## 技术栈

- 后端：Go 1.22+（标准库 `net/http` + sqlx + go-redis）
- 前端：Vue 3 + Vite + Vue Router（无重型 UI 库，自研样式）
- 数据库：MySQL 5.7+ / 8.0，缓存：Redis

## 目录结构

```
cmd/dnsctl/          Go 入口（dev / serve / cron-run）
internal/
  config/            配置（从 .env 读取）
  database/          MySQL（sqlx）
  redis/             Redis 封装
  crypto/            AES-256-GCM
  auth/              JWT 认证与角色
  logger/            操作 / 同步日志
  httpapi/           响应封装与中间件
  provider/          云厂商适配（腾讯 / 阿里 / 火山 / Spaceship）
  sync/              同步引擎（扇出、重试、校验、去重）
  notify/            消息通知（企业微信 / 飞书 / 钉钉 / Webhook）
  ai/                AI 诊断（大模型客户端 + 只读工具）
  dnsprobe/          解析生效检测（公共 DoH 探测）
  handlers/          /api/* 路由
  server/            HTTP 组装
web/                 Vue 3 前端
migrations/schema.sql 数据库结构（首次启动自动导入）
```

## 快速开始

### 一键运行（Docker）

```bash
make up
```

自动完成：生成随机密钥 → 启动 MySQL/Redis → 建表与管理员 → 启动服务。

浏览器访问 <http://localhost:8080>，账号 `admin` / 密码 `admin123`（可在 `.env` 修改）。

### 前端独立开发

```bash
make dev-api    # 终端 1：后端 mock API（8080，不需要数据库）
make dev-web    # 终端 2：前端 Vite（5173，/api 代理到 8080）
```

### 生产部署

```bash
make build      # 构建前端产物 + 单二进制 bin/dnsctl
./bin/dnsctl serve --addr :8080
```

定时任务（重试失败队列 + 清理 + 每日备份）：

```bash
# crontab，每分钟
* * * * * cd /path/to/project && ./bin/dnsctl cron-run
```

## 环境变量

配置统一从 `.env` 读取（`make env` 自动生成随机值）。

| 变量 | 说明 | 默认 |
|---|---|---|
| `DNS_DB_HOST` / `DNS_DB_PORT` / `DNS_DB_NAME` / `DNS_DB_USER` / `DNS_DB_PASS` | MySQL 连接 | 127.0.0.1:3307/dns_manager |
| `DNS_REDIS_HOST` / `DNS_REDIS_PORT` / `DNS_REDIS_AUTH` | Redis 连接 | 127.0.0.1:6379 |
| `DNS_SECRET_KEY` | AK/SK 加密 + JWT 签名密钥（≥32 字符） | 自动生成 |
| `DNS_CRON_KEY` | 定时任务触发密钥 | 自动生成 |
| `DNS_ADMIN_USER` / `DNS_ADMIN_PASS` | 初始管理员账号 | admin / admin123 |
| `DNS_AI_BASE_URL` / `DNS_AI_MODEL` / `DNS_AI_API_KEY` | AI 诊断助手（OpenAI 兼容接口，填入 Key 后启用） | 空（禁用） |

> AI 助手为可选功能：不配置 Key 时悬浮球仍显示，但提问会提示未配置，不影响其他功能。

## License

本项目基于修改版 MIT 协议开源，附加以下条款：

1. **禁止商业牟利**：禁止将本项目或其衍生作品用于任何形式的牟利商业用途（包括但不限于出售、付费部署、付费服务、SaaS 收费等）。
2. **保留出处**：任何二次开发、衍生作品必须保留并注明原项目 GitHub 链接：https://github.com/FQkun/all-in-dns
3. 其余权利与限制遵循 MIT 协议（MIT License）。

完整条款见 [LICENSE](LICENSE)。
