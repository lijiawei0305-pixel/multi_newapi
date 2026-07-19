# 真实微信/支付宝支付上线指南（主站进程内模式）

> 当前生产拓扑只有 `app + mysql + redis`。微信 Native 与支付宝电脑网站支付 SDK 运行在主站进程内，
> 商户凭据通过管理后台写入现有 options 数据库配置，不需要独立支付进程、额外回环端口或单独的 Nginx upstream。请用数据库访问控制和受限备份保护这些凭据。

## 1. 上线前置

- 按 [`deploy/.env.test.example`](../../../deploy/.env.test.example) 创建服务器 `.env`，权限设为 `600`。
- `MT_PAY_NOTIFY_BASE` 必须是外部可访问的 HTTPS 规范域名，例如 `https://tokendream.wedreamhub.com`；不要带路径或末尾 `/`。
- 按 [`deploy/docker-compose.test.yml`](../../../deploy/docker-compose.test.yml) 启动唯一生产栈。
- Nginx 使用仓库 `deploy/nginx/` 下对应 vhost：HTTP 只做 308 HTTPS 跳转，HTTPS 的普通流量统一反代主站回环端口，`/api/internal/` 保持公网 404。
- Cloudflare 使用 Full strict，并确认源站证书覆盖实际回调域名。

主站会按渠道生成以下固定异步通知地址：

- 微信：`$MT_PAY_NOTIFY_BASE/api/pay/wechat/notify`
- 支付宝：`$MT_PAY_NOTIFY_BASE/api/pay/alipay/notify`

回调 handler 内执行平台验签、订单金额核对与幂等入账。不要用手工 HTTP 请求伪造成功回调。

## 2. 配置商户凭据

1. 以管理员身份进入「系统设置 → 集成 → 支付」。
2. 分别在微信、支付宝选项卡填写商户号、应用 ID、平台证书/公钥及商户私钥等字段。
3. 使用管理页连接验证；验证不通过时不要启用对应渠道。
4. 在「新增支付方式」中按需上架 `wxpay_official` 或 `alipay_official`。

私钥、API v3 key 与平台证书不得写进仓库、Compose 文件、命令历史或本指南。轮换凭据时先在受控窗口验证新配置，再删除旧密钥。

## 3. 部署与依赖验收

从仓库根执行受支持的发布入口：

```bash
./deploy/ops/deploy.sh
```

在服务器确认三服务拓扑、依赖 readiness 与运行制品版本：

```bash
cd /root/newapi-test/deploy/ops
./healthcheck.sh
docker compose -p newapi_test --env-file /root/newapi-test/.env \
  -f /root/newapi-test/deploy/docker-compose.test.yml ps
```

`healthcheck.sh` 必须同时通过 `/health/live`、依赖感知的 `/health/ready` 以及 app/mysql/redis 容器检查。`/api/status` 仅用于版本身份，不可替代 readiness。

## 4. 受控小额验收

真实资金演示默认拒绝运行；必须显式确认后执行：

```bash
ssh newapi628
cd /root/newapi-test/deploy/demo
export ADMIN_PASS='当前管理员口令'
ALLOW_REAL_PAYMENT_DEMO=1 PAY_PROVIDER=wxpay ./demo.sh
```

验收至少覆盖：

1. 充值下单返回支付凭据；真实付款后订单只入账一次。
2. 套餐付款后 SUB 订单激活一条原生订阅，重复通知不重复发货或分润。
3. 暂时中断回调后，支付对账能通过平台主动查单补入账。
4. 平台返回金额与本地订单不一致时拒绝入账并保留可审计错误。
5. 演示后执行只读账目对账：

```bash
cd /root/newapi-test/deploy/ops
./reconcile.sh
```

## 5. 故障处置与回退

- 单渠道异常：先在管理后台停用该渠道，保留订单与回调记录供对账，不切换到伪支付或手工确认。
- 回调超时：先查商户后台真实交易状态，再看支付对账页；不得直接修改订单状态。
- 发布回退：使用 [`deploy/ops/rollback.sh`](../../../deploy/ops/rollback.sh) 的成对 release 回滚，并要求版本/readiness 验收通过。
- 数据恢复：只能使用配对 MySQL + Redis manifest，严格遵循 [`deploy/ops/README.md`](../../../deploy/ops/README.md) 的维护停流流程。
