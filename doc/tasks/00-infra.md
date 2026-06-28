# 🏗️ Infra & Foundation — 最小可执行任务（MET）

> **职责**：代码基线（fork new-api）、镜像构建、数据库迁移基建、宝塔网关/域名、支付回调服务、`platform` 横切包。
> **设计参考**：[detailed-design.md](../detailed-design.md) §1/§5/§6 ｜ [proposal.md](../proposal.md) §10/§12/§15
> **现网事实（已 SSH 核实）**：宝塔 Docker 应用 `newapi_YFNf`（`calciumion/new-api:latest` + `redis` + `mysql:8.2`，DB=`new-api`，跑在 `127.0.0.1:3000`）；域名 `wedreamhub.com`，`api.wedreamhub.com` 已反代到 3000；宿主无 Go/Node（构建在 Docker 内）。
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
- [ ] compose 栈新增 `auth-service` 容器（支付回调），加入 `baota_net` ｜ ✅ `docker compose up -d` 后容器 healthy
- [ ] 宝塔 Nginx 增 `^~ /pay/`、`^~ /auth/` 转发到 auth-service ｜ ✅ 公网可达 `/pay/wxpay/notify`、`/auth/alipay/notify`

## F. 部署联调
- [ ] 用自定义镜像替换 `newapi_YFNf/docker-compose.yml` 的 `calciumion/new-api:${VERSION}` ｜ ✅ 替换后栈 healthy、站点可登录
- [ ] 输出一键启动/回滚说明（含镜像 tag、迁移、备份）｜ ✅ 按文档可重新拉起
