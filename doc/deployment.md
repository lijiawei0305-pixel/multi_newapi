# 部署 · Nginx · 运维

new-api fork 当前已从早期「隔离测试栈」收敛为单生产栈。本页只做权威入口，
不重复可能漂移的命令。

## 当前部署事实

- 唯一现网 / 生产 compose project：`newapi_test`。
- 服务器 release 根：`/root/newapi-test`；对外 app 仅在 `127.0.0.1:3100` 回环监听。
- 原 `newapi_YFNf`（`:3000`）已删除，不是测试、灰度或回退目标。
- 历史文件名 `deploy/docker-compose.test.yml` 仍被保留，但它是当前生产编排，
  禁止对其执行任何「测试栈可销毁」操作。

## 权威手册

- 发布、配对备份、恢复、回滚、巡检：[`deploy/ops/README.md`](../deploy/ops/README.md)
- 上线与 Nginx/Cloudflare 切流：[`deploy/ops/go-live.md`](../deploy/ops/go-live.md)
- wildcard / 自定义域名 / 证书：[`domains-ssl.md`](domains-ssl.md)
- 实际支付配置与回调：[`docs/vendor/payments/deploy-real-payments.md`](../docs/vendor/payments/deploy-real-payments.md)
- 上线验收状态：[`tasks/STATUS.md`](tasks/STATUS.md)

## 不可绕过的安全约束

- 只能用 `deploy/ops/deploy.sh` 的预检、发布前配对备份、干净 staging 换树、
  版本/readiness 验收和失败回滚链路；禁止定向覆盖文件冒充可重建发布。
- `.env` 必须为 `600` 且不入库。生产必须显式提供互不相同的
  `SESSION_SECRET` / `CRYPTO_SECRET`，并设置 `DEPLOYMENT_ENV=production` 和
  `SESSION_COOKIE_SECURE=true`。
- `/api/internal/` 必须在每个公网 Nginx vhost（包括动态自定义域名）直接返回 `404`。
- 整库恢复前必须外部停流、键入生产栈名、生成强制 pre-backup；任一闸门失败
  都保持 app stopped 与 ops lock，不得自动开流。
