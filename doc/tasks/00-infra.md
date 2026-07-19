# 🏗️ Infra & Foundation — 最小可执行任务（MET）

> ⚠️ **历史基建存档（不是当前运维手册）。** 下文 `newapi_YFNf:3000` 与「`newapi_test`
> 是可销毁测试栈」是 2026-07-03 以前的当时事实；前者已删除，后者已是唯一生产栈。
> 当前事实见 [`STATUS.md`](STATUS.md)，发布/备份/恢复只按 [`../../deploy/ops/README.md`](../../deploy/ops/README.md)。

> **职责**：代码基线（fork new-api）、镜像构建、数据库迁移基建、宝塔网关/域名、支付回调服务、`platform` 横切包。
> **设计参考**：[detailed-design.md](../detailed-design.md) §1/§5/§6 ｜ [proposal.md](../proposal.md) §10/§12/§15
> **当时现网事实（历史）**：宝塔 Docker 应用 `newapi_YFNf`（`calciumion/new-api:latest` + `redis` + `mysql:8.2`，DB=`new-api`，跑在 `127.0.0.1:3000`）；域名 `wedreamhub.com`，`api.wedreamhub.com` 已反代到 3000；宿主无 Go/Node（构建在 Docker 内）。
> **约定**：每个子任务的「✅」是完成/测试标准，达成即勾选。

## A. 代码基线
- [ ] Fork `calciumion/new-api` 源码进本项目仓库（Go 后端 + React 前端）｜ ✅ `docker build` 可产出自定义镜像，`/api/status` 返回 success
- [x] 建 `/internal` 领域分包骨架（对齐 detailed-design §5：tenant/identity/agent/pricing/billing/wallet/tokenplan/payment/promotion/siteconfig/relay/stats/risk/platform）｜ ✅ `go build ./...` 通过
- [ ] 每个领域包放占位 `port.go`（接口）+ `*_test.go`（骨架）｜ ✅ `go vet ./...`、`go test ./...` 全绿（空实现允许 skip）

## B. platform 横切包
- [x] `platform/errors`：统一 `AppError{Code,Msg,HTTP}` + Gin 错误中间件 ｜ ✅ 单测：错误码→HTTP 映射正确
- [x] `platform/ctx`：`Principal{UserID,TenantID,Role}` 注入/读取 ｜ ✅ 单测：上下文存取一致
- [ ] `platform/txn`：`WithTx(ctx, fn)` 事务包装（GORM）｜ ✅ 单测：提交/回滚行为正确
- [ ] `platform/cache`：Redis 封装（get/set/del/incr）｜ ✅ 集成测：连 `redis://redis` 读写通过
- [ ] `platform/db`：GORM 初始化 + `scopeByTenant(db, tid)` ｜ ✅ 单测：scope 自动注入 `tenant_id` 条件

## C. 数据库迁移基建
- [ ] 引入版本化迁移（golang-migrate 或受控 AutoMigrate），建 `migrations/` ｜ ✅ 可 `up`/`down`，幂等
- [ ] 基线迁移：在 new-api 既有表上新增多租户/代理/tokenplan 全表（对齐 proposal §6）｜ ✅ 在 MySQL 8.2 容器 DB `new-api` 跑通，`SHOW TABLES` 含新表
- [ ] 既有表改造迁移：`tenant_tokens.tenant_id`、日志表 `tenant_id`、`tenants.tokenplan_enabled`、`agent_earning_logs.source_type` 扩枚举 ｜ ✅ 迁移可回滚，现网数据不丢

## D. 网关与域名（宝塔）
- [ ] 宝塔新增 `*.wedreamhub.com` 泛解析 vhost + `www./admin.`，反代 `127.0.0.1:3000`，透传 `Host` ｜ ✅ `aaa.wedreamhub.com` 能进入后端并被租户解析
- [ ] 申请并配置 `*.wedreamhub.com` 通配符 SSL ｜ ✅ `https://aaa.wedreamhub.com` 绿锁
- [ ] 保留 `api.wedreamhub.com` 现有反代不受影响 ｜ ✅ 现网调用不中断

## E. 支付回调服务
- [x] 真实支付内置于主站进程内（`internal/payment/realpay` + `internal/mtwire/payment_inprocess.go`），无需独立 `auth-service` 容器 ｜ ✅ 随主站镜像构建（auth-service 已退役）
- [ ] 回调经主站 nginx `location /` 反代到 app（`/api/pay/wechat/notify`、`/api/pay/alipay/notify`），无需独立 `^~ /pay/ /auth/` 块；凭据存 DB（后台「系统设置 → 支付」选项卡表单）｜ ✅ 公网可达回调地址

## F. 部署联调
- [ ] 用自定义镜像替换 `newapi_YFNf/docker-compose.yml` 的 `calciumion/new-api:${VERSION}` ｜ ✅ 替换后栈 healthy、站点可登录
- [ ] 输出一键启动/回滚说明（含镜像 tag、迁移、备份）｜ ✅ 按文档可重新拉起

---

## G. 集成阶段（纵切打通）执行计划 ★

> **new-api 基座事实（已浅克隆核实）**：module `github.com/QuantumNous/new-api`，Go 1.25.1，gin + GORM + gin-sessions，`//go:embed web/default/dist`（前端构建产物内嵌）。结构：`router/`(api/relay/dashboard 路由) · `controller/`(handler) · `model/`(GORM 模型 + DB) · `middleware/`(auth/distributor/cache) · `relay/`(上游转发) · `common/database.go`(DB 初始化) · `service/` · `dto/` · `setting/`。
>
> **合并方式（增量为主）**：本仓库演进为 new-api fork —— new-api 源码作基座，`internal/*` 14 模块作增量层，经 `cmd/main`/适配器 wire 进 new-api 的 router/middleware；租户识别接 `middleware/distributor.go` 一侧；扣费/中继复用 `relay/`。统一 `go.mod`。
>
> **硬约束**：本地无 Docker/DB（已核实）→ **迁移/GORM/运行/E2E 一律在服务器独立测试栈**（compose project `newapi_test`、端口 `127.0.0.1:3100`、DB `new-api-test`、独立 redis；**不碰现网 `newapi_YFNf`/3000**）。Mac 仅 `go build` 编译校验。

**纵切顺序**（每切 = GORM repo + 迁移 + handler + 装配 + 测试栈 E2E）：
- [x] **Slice 1 · 租户管道**：GORM `TenantRepo` + `tenants/tenant_domains` 迁移 + 按 Host 解析的 `GET /api/tenant/current` → 测试栈跑通 → curl/playwright 冒烟（证明 Mac 代码→服务器构建→DB→端点→浏览器 全链路）
- [x] **Slice 2 · 身份与钱包**：Identity 鉴权中间件 + Wallet GORM + 充值/余额端点
- [x] **Slice 3 · tokenplan**：套餐 CRUD/购买/计量 + 购买页对接 `api-contract.md`
- [x] **Slice 4 · 中继计费**：`/v1/*` 接 relay + 双桶扣费 + 日志
- [x] 组装层适配器（见 progress.md「组装层 TODO」与 `api-contract.md` §4）

**测试栈部署步骤**（服务器，经 `ssh newapi628`）：
- [x] `rsync -az -e 'ssh -p 5522' --exclude .git --exclude scratchpad ./ newapi628:/root/newapi-test/`
- [x] `ssh newapi628 'cd /root/newapi-test && docker build -t newapi-mt:test .'`
- [x] `docker compose -p newapi_test up -d`（端口 3100、DB `new-api-test`、独立 redis）→ 迁移 → `curl 127.0.0.1:3100/api/status`
- [x] playwright-cli 冒烟（对照 `api-contract.md`）
