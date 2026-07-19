# 上线手册 — go-live（`*.wedreamhub.com` 通配 + 单生产栈）

> **目标**：维护已正式化、对外服务的生产栈 `newapi_test`（app `127.0.0.1:3100`），
> 用 `*.wedreamhub.com` 通配域名承载主站 + 全部代理子域，CF Origin CA 升到 **Full (strict)**。
> **注**（2026-07-03 更新）：原 stock 栈 `newapi_YFNf`（`:3000`）**已删除**，`newapi_test`（`:3100`）已是**唯一现网**；本手册原述"保持 YFNf 作即时回退保险"已不再成立——回退改走 `rollback.sh`（:prev 镜像秒级 / 归档源码 `--to <ts>` 重建）。
> 运维脚本见 [`README.md`](README.md)；迁移见 [`migrate-note.md`](migrate-note.md)。
> 服务器 `64.90.4.114`（Debian 12 + 宝塔 nginx + Docker compose v2），登录 `ssh newapi628`。

多租户识别靠 **Host 头**：一个通配 vhost 反代到 `:3100`，app 按 Host 解析租户 —— 故无需为每个子域单建 vhost。

---

## ① 通配 DNS（Cloudflare 面板，用户/你侧操作）

在 CF `wedreamhub.com` 区添加 / 确认以下记录（**橙云代理 Proxied**）：

| 类型 | 名称 | 内容 | 代理 | 说明 |
| --- | --- | --- | --- | --- |
| A | `*` | `64.90.4.114` | 🟠 Proxied | 通配：承载 `www`/`admin`/代理子域 → 当前生产栈 |
| A | `@`(`wedreamhub.com`) | `64.90.4.114` | 🟠 Proxied | 主站 apex（通配不含 apex，需单列） |
| A | `api` | `64.90.4.114` | 🟠 Proxied | exact vhost 优先于通配，但与其它域名统一反代唯一 app `:3100` |

> 已有的 `tokendream` 历史验收记录可保留（exact 优先，仍指 3100），验证通配生效后可删。
> 验证：`dig +short '随便.wedreamhub.com'` 返回 CF IP；CF 代理下源站看到的是 CF 回源 IP。

---

## ② CF Origin CA 证书 + Full (strict)（用户/你侧 + 源站）

当前仓库契约是 **Origin CA + Full (strict)**。新机或证书轮换按下列步骤签发并安装；已有环境先核对证书 SAN/有效期，不要退回自签：

**A. CF 面板签发 Origin 证书**
1. CF → SSL/TLS → **Origin Server** → **Create Certificate**。
2. Hostnames 填 `*.wedreamhub.com` **和** `wedreamhub.com`（两个都要，否则 apex 不被覆盖）。
3. 选 RSA/ECC、有效期（最长 15 年）→ 生成 → 复制 **Origin Certificate** 与 **Private Key**。

**B. 装到源站宿主 nginx（服务器）**
```bash
ssh newapi628
mkdir -p /www/server/panel/vhost/cert/wildcard.wedreamhub.com
# 把 CF 给的证书/私钥分别写入（nano/vim 粘贴；私钥 600）
#   /www/server/panel/vhost/cert/wildcard.wedreamhub.com/fullchain.pem   ← Origin Certificate
#   /www/server/panel/vhost/cert/wildcard.wedreamhub.com/privkey.pem     ← Private Key
chmod 600 /www/server/panel/vhost/cert/wildcard.wedreamhub.com/privkey.pem
```

**C. 通配 vhost**（新建 `/www/server/panel/vhost/nginx/wildcard.wedreamhub.com.conf`）
权威配置是仓库 `deploy/nginx/wildcard.wedreamhub.com.conf`；下列片段仅解释其关键约束：

```nginx
# *.wedreamhub.com → 唯一生产栈 newapi_test (127.0.0.1:3100)。多租户按 Host 解析。
# server_name 精确匹配优先：api.wedreamhub.com 由仓库 api vhost 命中，但同样指向唯一 app :3100。
server {
    listen 80;
    server_name wedreamhub.com *.wedreamhub.com;
    return 308 https://$host$request_uri;
}

server {
    listen 443 ssl;
    server_name wedreamhub.com *.wedreamhub.com;

    ssl_certificate     /www/server/panel/vhost/cert/wildcard.wedreamhub.com/fullchain.pem;
    ssl_certificate_key /www/server/panel/vhost/cert/wildcard.wedreamhub.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    # 内网管理端点绝不暴露公网。
    location ^~ /api/internal/ { return 404; }

    location / {
        proxy_pass http://127.0.0.1:3100;
        proxy_http_version 1.1;
        proxy_set_header Host $host;                 # ★ 多租户识别依赖原始 Host
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 300s;
    }
}
```

**D. 校验 + 重载 + 切 strict**
```bash
nginx -t && nginx -s reload      # 宝塔环境也可在面板「重载配置」
# 源站自检（绕 CF）：
curl -fsS -H 'Host: www.wedreamhub.com' http://127.0.0.1:3100/health/live
curl -fsS -H 'Host: www.wedreamhub.com' http://127.0.0.1:3100/health/ready
```
最后 CF → SSL/TLS → Overview → 模式切 **Full (strict)**。
> 切 strict 前务必确认源站已是 Origin CA 证书且 vhost 覆盖该 Host，否则全站 526。
> 若出现 526，保持 `Full (strict)` 与维护停流，修复源站证书/SAN/链后再开流；不要用降级到非 strict 掩盖无效证书。

---

## ③ 灰度切流

`newapi_test` 已是唯一现网栈，三个域名都指向同一个 `:3100` app；不存在可作为回退目标的旧端口。发布灰度有两条路径：

- **默认**：通过 `deploy.sh` 发布；失败时成对回滚 `:prev` 镜像与 release 树，并验证版本/readiness。
- **双 release 小流量灰度（可选）**：只有先建立了独立数据/端口/版本均可验证的第二 release，才可：
  - **CF 侧**：用 Load Balancer（按权重 origin pool）或 Rules，把一部分流量导向不同源站/端口。
  - **nginx 侧**：同一 Host 用 `split_clients` 按权重分到双 upstream（新栈 3100 / 备份栈）：
    ```nginx
    split_clients "$remote_addr$request_id" $canary_pool {
        10%  "127.0.0.1:3100";   # 新栈 10%
        *    "127.0.0.1:3100";   # 其余（示例同栈；如有并行旧栈改其端口）
    }
    # location / { proxy_pass http://$canary_pool; }   # 需配 resolver 或 upstream 块
    ```
    逐步把 10%→50%→100%，观察 `healthcheck.sh`、支付对账与业务日志。
> 未建立完整第二 release 时不要把不存在的旧栈/旧端口写进灰度配置；直接依赖已验证的 release 回滚。

---

## ④ 回滚

| 场景 | 操作 |
| --- | --- |
| 新栈程序异常 | `ssh newapi628 '/root/newapi-test/deploy/ops/rollback.sh'`（回 `:prev` 镜像）；更早版本用 `rollback.sh --list` / `rollback.sh --to <ts>`（服务器无 `.git`） |
| 数据写坏需恢复 | 先外部维护停流，再执行 `MAINTENANCE_CONFIRMED=1 restore.sh /root/backups/backup-<ts>.manifest`（强确认；成对覆盖 MySQL + Redis） |
| 证书/strict 导致全站 5xx | 保持维护停流与 `Full (strict)`，核对源站证书 SAN、有效期和链，修复并验证后再开流 |
| 单栈整体不可用 | 保持 CF 代理与维护停流，执行成对 release 回滚；只有事先验证过独立灾备源站时才切 DNS |

> `deploy.sh` 已在「健康轮询失败」时**自动**执行镜像回滚；以上为手动兜底。

---

## ⑤ 备份 / 巡检 cron（服务器）

`ssh newapi628` 后 `crontab -e` 加入：

```cron
# 每日 02:30 停写配对备份（MySQL + Redis + config + SHA-256 manifest，保留最近 7 组）
30 2 * * *   /root/newapi-test/deploy/ops/backup.sh      >> /var/log/newapi-backup.log 2>&1
# 每 5 分钟健康巡检（失败退出码=失败数；配 ALERT_WEBHOOK 可推送告警）
*/5 * * * *  ALERT_WEBHOOK= /root/newapi-test/deploy/ops/healthcheck.sh >> /var/log/newapi-health.log 2>&1
```

建议：
- 备份目录 `/root/backups` 定期外迁（异地/对象存储），防单机磁盘故障。
- `healthcheck.sh` 的 `ALERT_WEBHOOK` 接飞书/钉钉/企业微信机器人后，失败即推送。
- 上线后首日人工抽查一次 `restore.sh`（在另备库/演练环境）验证备份**可恢复**，而非只"有备份"。

---

## 上线检查清单（DoD）

- [ ] CF：`*` + apex + `api` A 记录均 Proxied 指受控源站；三个域名最终进入唯一 app `:3100`。
- [ ] 源站：通配 vhost + Origin CA 证书装好，`nginx -t` 通过、已 reload。
- [ ] CF SSL 模式 = **Full (strict)**，HTTP 请求 308 到 HTTPS，随机子域 `/health/live` 与 `/health/ready` 均返回 `status=ok`。
- [ ] `api` / `www` / `tokendream` 均经 HTTPS 返回正常，且源站/运行版本一致。
- [ ] `deploy.sh` 跑通一次（含自动成对 `:prev` 镜像/release + 部署前配对备份）。
- [ ] `healthcheck.sh` 全绿；备份/巡检 cron 已装并产出日志。
- [ ] 演练 `rollback.sh` 与一次 `restore.sh`（演练环境）成功。
