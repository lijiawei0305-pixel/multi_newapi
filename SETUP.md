# 环境与起步说明（协作者 · Phase 2 前端）

> Wedreamhub AI 聚合平台 —— 基于 [New API](https://github.com/QuantumNous/new-api) 二次开发的**多租户代理分销平台**。
> 本文帮你把项目跑起来。**真实密钥不在仓库**（安全约定），向**项目负责人**索取。

## 1. 技术栈
- **后端**：Go 1.21+（gin + GORM，模块路径 `github.com/QuantumNous/new-api`）
- **前端**：`web/default`（React 19 + TanStack Router 文件路由 + shadcn/ui + bun + rsbuild）
- **数据/基建**：MySQL 8 + Redis + Nginx + Docker Compose

## 2. 仓库结构（多租户增量都在 `internal/`，不改 new-api 原生源以便 rebase）
| 路径 | 作用 |
| --- | --- |
| `internal/mtwire` | 多租户装配层（hook 注入、租户解析 TenantMiddleware、http、对账） |
| `internal/{agent,tokenplan,payment,moderation,risk,siteconfig,promotion}` | 各业务模块（端口接口 + gormrepo） |
| `auth-service/` | **已退役**（dormant，不构建/不部署）—— 真实支付改走主站进程内 SDK（`internal/payment/realpay`），凭据存 DB（后台「系统设置 → 支付」选项卡表单）；目录仅作历史参考 |
| `web/default/src` | 前端：`features/` 业务页、`routes/_authenticated/` 文件路由、`i18n/locales/` |
| `doc/` | 文档：`acceptance.md`=验收基准（已对齐开发合同）、`architecture/data-model/billing/...` |
| `deploy/` | `docker-compose.test.yml` + `ops/` 部署脚本 + `.env.test.example` |
| `CLAUDE.md` | 项目驾驶舱：定位 / 路由表 / 硬约束 / 工作纪律（先读这个） |

## 3. 环境变量
```bash
cp deploy/.env.test.example /root/newapi-test/.env   # 或本地路径
# 编辑填入真实值（向负责人索取）：MT_INTERNAL_SECRET / AUTH_SIGN_SECRET / SESSION_SECRET / 上游 key / 域名
```
DB/Redis 默认值内置在 `deploy/docker-compose.test.yml`（测试栈隔离：项目名 `newapi_test`、端口 `127.0.0.1:3100`）。

## 4. 跑起来
### 4a. 全栈（服务器测试栈，推荐 —— 后端构建/部署一律在服务器，见 CLAUDE.md W4）
```bash
docker compose -p newapi_test --env-file /root/newapi-test/.env \
  -f deploy/docker-compose.test.yml up -d --build
# 起来后测试栈在 127.0.0.1:3100（经 nginx 反代到 *.wedreamhub.com）
```
### 4b. 前端本地开发
```bash
cd web/default
bun install                       # 装依赖（package.json 已入库；无 bun.lock，装最新匹配版本）
bun run dev                       # 本地 dev server
bunx @tanstack/router-cli generate  # 改了 routes/ 文件路由后重生成 routeTree.gen.ts
bun run build                     # 产物构建（Docker 内由 rsbuild 构建后 go:embed 进二进制）
```

## 5. 关键约定（务必遵守）
- 🔑 **密钥不入库**：真实 `.env` / 私钥只在服务器；提交前 grep 密钥。SSH 私钥严禁入库。
- 🏗️ **构建/迁移/E2E 在服务器**：Mac/本地只做代码编辑与调试（CLAUDE.md W4）。
- 🚧 **测试栈与现网隔离**：`newapi_test`(3100) ≠ 现网 `newapi_YFNf`(3000)，**勿碰现网**。
- 📝 **前端版权头**：`.ts/.tsx` 顶部保留 GNU AGPL 头（参考既有文件）。
- 🌐 **i18n**：English key 即身份；中文译文在 `web/default/src/i18n/locales/zh.json`（`{"translation":{...}}`）。
- 🪝 **多租户 hook 模式**：`internal/platform/agenthook` 包级函数变量；mtwire 在启动时 `InstallHooks` 注入（避免 native→mtwire 循环依赖）。

## 6. 文档入口（按需读，别凭记忆改）
- `CLAUDE.md` —— 项目驾驶舱（每次先看路由表）
- `doc/acceptance.md` —— 验收基准（三期 · 已对齐开发合同的 6 项新增交付）
- `doc/tasks/progress.md` —— 开发进度看板
- `RETRO.md` —— 踩坑复盘（反复出现的坑已升级为硬约束）
