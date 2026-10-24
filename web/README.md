# web/ · DNS 控制台前端

Go 后端（`internal/handlers`）的配套前端。**界面已二次开发**：由原来的 Vue 3 组件式界面，换成零依赖的原生 JS 单页控制台（保留 Vite 作为构建工具）。

- 技术：原生 ESM + CSS + 自研 SVG 图表，仅 `vite` 一个 devDependency
- 产物：`dist/index.html` + `dist/assets/*`，由 Go 的 `os.DirFS("web/dist")` 直接托管
- 认证：JWT（`localStorage.dns_token`），请求头 `Authorization: Bearer`
- 路由：hash 路由（`#/dashboard`、`#/domains/1` …），与 Go 端"只服务 `/` 与 `/assets/`"的静态策略一致

## 目录

```
web/
├── index.html             入口（引入 /src/main.js）
├── vite.config.js         /api 代理到 8080；构建产物 dist/assets
├── package.json           仅 vite
├── src/
│   ├── main.js            入口
│   ├── app.js             外壳：侧栏 / 顶栏 / 路由 / 登录页 / 启动
│   ├── core.js            DOM、转义、厂商元数据、Toast、Modal、主题
│   ├── api.js             API 客户端（JWT + {code,msg,data} 信封 + CSV 下载）
│   ├── mock.js            演示模式引擎（对齐 Go 接口契约，localStorage 持久化）
│   ├── charts.js          零依赖 SVG 图表
│   ├── ai.js              AI 诊断助手（悬浮球 + 抽屉，调 /api/ai/chat）
│   ├── router-state.js    当前路由快照（避免循环依赖）
│   ├── styles/console.css 设计系统（亮/暗双主题，响应式）
│   └── views/
│       ├── dashboard.js   概览：KPI / 24h 趋势 / 厂商健康度 / 失败任务
│       ├── domains.js     域名：同步开关、绑定管理、一致性校验、生效检测
│       ├── records.js     解析记录：筛选 / 增删改 / 批量 / CSV / 云端拉取
│       ├── accounts.js    云账号：卡片、连通测试
│       ├── sync.js        同步中心：重试队列 + 日志
│       ├── oplogs.js      操作日志
│       ├── backups.js     备份恢复
│       └── notify.js      消息通知渠道
└── legacy-vue/            原 Vue 3 界面（原样保留，仅供回滚，不参与构建）
```

## 开发 / 构建

```bash
# 前端单独开发（配合后端 mock，不需要 MySQL/Redis）
make dev-api        # 终端 1：Go mock API  :8080
make dev-web        # 终端 2：Vite          :5173（/api 代理到 8080）

# 生产构建
cd web && npm install && npm run build     # 或直接在仓库根 make build
```

`make build` 会先构建前端再编译单二进制；`./bin/dnsctl serve --addr :8080` 在**仓库根目录**运行时才能读到 `web/dist`。

## 两种数据模式

| 模式 | 触发条件 | 数据来源 |
|---|---|---|
| `api` | `GET /api/auth/me` 返回 JSON | 真实 Go 后端 |
| `demo` | 上面探测失败（纯静态托管 / 无后端） | `src/mock.js` 内置演示数据 |

强制指定：`?mode=api` / `?mode=demo`。其它调试参数：`?login=1` 先看登录页，`?ai=1` 直接展开 AI 助手，`?theme=dark` 暗色。

## 与后端接口的对应

| 页面 | 调用的接口 |
|---|---|
| 概览 | `GET /api/monitor/summary`、`/api/sync/logs`、`/api/sync/queue`、`/api/domains`、`/api/accounts`（趋势与厂商健康度由同步日志在浏览器端聚合） |
| 域名 | `GET/POST /api/domains`、`/api/domains/{id}/accounts`、`/toggle`、`/verify`、`/repair`、`/probe`、`/bind`、`/unbind`、`DELETE /api/domains/{id}` |
| 解析记录 | `GET/POST /api/domains/{id}/records`、`POST /api/records/{id}`、`DELETE /api/records/{id}`、`/batch-delete`、`/import-remote`、`/import`、`/export` |
| 云账号 | `GET/POST /api/accounts`、`DELETE /api/accounts/{id}`、`/test` |
| 同步中心 | `GET/DELETE /api/sync/queue`、`/sync/logs`、`POST /api/sync/retry-now` |
| 备份 | `GET/POST /api/backups`、`/backups/delete`、`/backups/{id}/restore` |
| 通知 | `GET /api/notify/events`、`GET/POST/PUT/DELETE /api/notify/channels`、`/test` |
| AI 助手 | `POST /api/ai/chat`（`{message, page, domain}` → `{answer}`） |

后端**未做任何改动**：没有新增/修改路由、没有动数据库迁移脚本，纯前端替换。

## AI 助手交互

- 右下悬浮球打开 / 关闭抽屉
- 头部 **⤢** 按钮放大 / 还原窗口（400px ↔ 880px），偏好记在 localStorage
- 输入框左侧麦克风 = 语音输入（见下）

## AI 助手的语音输入

输入框左侧的麦克风按钮 = 语音转文字，**纯浏览器端实现**（Web Speech API），不经过本项目后端，也不需要任何 API Key。

- 点一下开始聆听（按钮变红、顶部出现「正在聆听」条），再点一下停止
- 边说边出字：中间结果实时写入输入框，说完可先编辑再点发送
- 语言固定 `zh-CN`；识别结果会追加到输入框已有内容后面，不会覆盖
- 出错有明确提示：麦克风权限被拒 / 没有麦克风 / 没听到声音 / 语音服务网络不通

**使用前提**

| 条件 | 说明 |
|---|---|
| 浏览器 | Chrome、Edge（Firefox 不支持；Safari 15+ 支持但中文效果一般） |
| 安全上下文 | 必须 `https://` 或 `localhost` / `127.0.0.1`，纯 http 域名下浏览器禁止麦克风 |
| 网络 | Chrome 的识别走 Google 在线服务，国内常失败并报 `network`；**优先用 Edge**（走微软服务，可正常识别） |

不满足条件时按钮不会消失，点击会给出对应提示，不影响打字提问。

> 如果以后想支持"上传录音转文字"（离线/更稳），需要后端加一个 ASR 接口（如 OpenAI Whisper 兼容）；目前刻意只做前端方案，保持后端零改动。

## 说明

- 解析记录列表接口只支持 `page/size`，所以"关键字 / 类型 / 状态"筛选在前端按当前页做，页脚会提示本页筛出多少条。
- `GET /api/domains` 只返回 `provider:on/off` 汇总，而同步开关需要 `account_id`，因此域名列表会并行拉取每个域名的绑定明细（域名 ≤ 40 个时才拉，超过则退化为静态标签，开关改在「绑定管理」里操作）。
- 截图见 `../docs/screenshots/`。
