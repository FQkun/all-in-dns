# DNS 统一管理平台

一站式管理腾讯云（DNSPod）、阿里云（Alidns）、火山引擎 DNS 三大平台的域名解析记录。在一处增删改解析，自动实时同步到所有已接入的云平台，失败自动重试，全程留痕。

## 界面预览

**首页概览**

![首页](docs/home.png)

**域名解析列表**

![域名列表](docs/domains.png)

**云账号管理**

![云账号](docs/accounts.png)

## 功能特性

- **三云同步**：修改一次记录，实时同步到腾讯云 / 阿里云 / 火山引擎，支持 A / AAAA / CNAME / MX / TXT / NS / SRV / CCAA 全类型记录
- **覆盖式同步引擎**：同步队列 + 指数退避自动重试 + 「远端已存在」自动登记，避免重复写入与死循环报错
- **按厂商启用**：一个域名可以只在部分云平台有 NS（`domain_providers` 按厂商独立开关）
- **写入前冲突校验**：同域名 / 主机 / 线路下 A、AAAA 与 CNAME 不共存，本地先拦截，避免三平台同步全部失败
- **记录归一化**：线路值映射、尾点剔除、MX 优先级剥离，消除各平台格式差异导致的重复记录
- **云账号安全**：AccessKey / Secret AES-256-GCM 加密入库，列表只显示掩码
- **备份与恢复**：每日自动备份（保留 30 份），支持备份映射恢复
- **导入 / 导出**：解析记录批量导入导出
- **认证与审计**：登录认证（可选阿里云 ESA 滑动验证码）、操作日志、同步日志
- **响应式 UI**：原生 JS 单页应用，无框架依赖

## 技术栈

- PHP 8.2+
- MySQL 5.7+ / 8.0
- Redis
- Nginx + PHP-FPM

## 安装

1. 将代码部署到站点目录，Web 根目录指向 `public/`
2. 确保 `src/config/` 与 `storage/` 目录可写
3. 在数据库中提前创建好库（如 `dns_manager`）
4. 浏览器访问 `/install.php`，按向导完成：
   - 环境检测（PHP 版本 / 扩展 / 目录权限）
   - 配置 MySQL / Redis 连接，可勾选启用阿里云 ESA 安全验证码
   - 自动导入 `database/schema.sql`（重复导入会先删旧表）
   - 创建管理员账号
5. 安装完成后 `install.php` 自动锁定，请保留锁定状态

### 启用 ESA 验证码（可选）

安装向导第 2 步勾选「启用阿里云 ESA 安全验证码」，填入 Scene ID 与 Prefix。
控制台配置时，「需验签的接口（URI）」填写 `你的域名/*` 即可。
配置地址：https://esa.console.aliyun.com/captcha/configuration

不启用时登录页不做验证码校验。

## 定时任务

在宝塔等面板添加「访问 URL」计划任务，每分钟访问：

```
https://你的域名/cron.php?key=安装时自动生成的cron_key
```

每次执行重试失败队列；每天首个执行触发自动备份。

## 目录结构

```
public/            Web 根目录（index.php 前端控制器、install.php 安装向导、cron.php 计划任务入口）
  assets/          前端 SPA（原生 JS / CSS）
src/
  config/          config.php（安装时生成，含数据库 / Redis / 密钥配置）
  Core/            Database、RedisClient、Crypto、Auth、Logger、Router、SyncService
  Core/Providers/  腾讯云（TC3 签名）、阿里云（RPC HMAC-SHA1）、火山引擎（V4 签名）
  api/             routes.php（/api/* 路由）
database/          schema.sql（可重复导入）
cron/              retry.php、backup.php（CLI 备用脚本）
```


## License

本项目基于修改版 MIT 协议开源，附加以下条款：

1. **禁止商业牟利**：禁止将本项目或其衍生作品用于任何形式的牟利商业用途（包括但不限于出售、付费部署、付费服务、SaaS 收费等）。
2. **保留出处**：任何二次开发、衍生作品必须保留并注明原项目 GitHub 链接：https://github.com/FQkun/all-in-dns
3. 其余权利与限制遵循 MIT 协议（MIT License）。

完整条款见 [LICENSE](LICENSE)。
