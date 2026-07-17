# 上线手册 — go-live（`*.wedreamhub.com` 通配 + 正式化测试栈）

> **目标**：把已跑通的测试栈 `newapi_test`（app `127.0.0.1:3100`）正式化为对外服务，
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
| A | `*` | `64.90.4.114` | 🟠 Proxied | 通配：承载 `www`/`admin`/代理子域 → 新栈 |
| A | `@`(`wedreamhub.com`) | `64.90.4.114` | 🟠 Proxied | 主站 apex（通配不含 apex，需单列） |
| A | `api` | （**保持现状**） | 🟠 | **现网，勿改**；exact 匹配优先于通配，安全 |

> 已有的 `tokendream`（测试）记录可保留（exact 优先，仍指 3100），验证通配生效后可删。
> 验证：`dig +short '随便.wedreamhub.com'` 返回 CF IP；CF 代理下源站看到的是 CF 回源 IP。

---

## ② CF Origin CA 证书 + Full (strict)（用户/你侧 + 源站）

当前测试栈用**自签证书**（CF `Full`，不校验源站）。正式化升级为 **Origin CA + Full (strict)**：

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
仓库 `deploy/nginx/tokendream.wedreamhub.com.conf` 是单域版；通配版把 `server_name` 改为通配、证书指向 Origin CA：

```nginx
# *.wedreamhub.com → newapi_test 新栈 (127.0.0.1:3100)。多租户按 Host 解析。
# server_name 精确匹配优先：api.wedreamhub.com 仍由现网 vhost 命中，本块不影响它。
server {
    listen 80;
    listen 443 ssl;
    server_name wedreamhub.com *.wedreamhub.com;

    ssl_certificate     /www/server/panel/vhost/cert/wildcard.wedreamhub.com/fullchain.pem;
    ssl_certificate_key /www/server/panel/vhost/cert/wildcard.wedreamhub.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    # 内网入账端点：仅 auth-service 经 compose 内网调用，绝不暴露公网。
    location ^~ /api/internal/ { return 404; }

    # 支付网关 → auth-service (127.0.0.1:8180)：承载支付异步回调 + mock 确认页。
    location ^~ /auth/ {
        proxy_pass http://127.0.0.1:8180;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 60s;
    }

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
curl -fsS -H 'Host: www.wedreamhub.com' http://127.0.0.1:3100/api/status | grep success
```
最后 CF → SSL/TLS → Overview → 模式切 **Full (strict)**。
> 切 strict 前务必确认源站已是 Origin CA 证书且 vhost 覆盖该 Host，否则全站 526。
> 回退：临时切回 `Full`（非 strict）即可容忍自签，争取排查时间。

---

## ③ 灰度切流

测试栈已是目标栈，"正式化" = 通配域名指向同一栈（`:3100`）。两条路径：

- **默认（推荐，零灰度风险）**：通配 → 新栈；**`api.wedreamhub.com` 保持现网 `:3000` 不动**。新域全量、老接口零影响；出问题只回退新域（见 ④），现网始终在线。
- **小流量灰度（可选）**：
  - **CF 侧**：用 Load Balancer（按权重 origin pool）或 Rules，把一部分流量导向不同源站/端口。
  - **nginx 侧**：同一 Host 用 `split_clients` 按权重分到双 upstream（新栈 3100 / 备份栈）：
    ```nginx
    split_clients "$remote_addr$request_id" $canary_pool {
        10%  "127.0.0.1:3100";   # 新栈 10%
        *    "127.0.0.1:3100";   # 其余（示例同栈；如有并行旧栈改其端口）
    }
    # location / { proxy_pass http://$canary_pool; }   # 需配 resolver 或 upstream 块
    ```
    逐步把 10%→50%→100%，观察 `healthcheck.sh` 与业务日志。
> 由于新旧是**不同 Host**（新域 vs `api.`），无需在同 Host 内灰度即可平滑切换；`split_clients` 仅在你想对**同一域名**做百分比灰度时才需要。

---

## ④ 回滚

| 场景 | 操作 |
| --- | --- |
| 新栈程序异常 | `ssh newapi628 '/root/newapi-test/deploy/ops/rollback.sh'`（回 `:prev` 镜像，秒级）；或 `--git <稳定tag>` |
| 数据写坏需恢复 | `restore.sh /root/backups/db-<ts>.sql.gz`（二次确认；会覆盖，谨慎） |
| 证书/strict 导致全站 5xx | CF SSL 模式切回 `Full`（非 strict）争取时间，再修源站证书 |
| 新域整体不可用 | CF 把通配 / apex 记录**改回**或暂置 DNS-only / 指回旧资源；`api.` 现网始终可用作主入口 |

> `deploy.sh` 已在「健康轮询失败」时**自动**执行镜像回滚；以上为手动兜底。

---

## ⑤ 备份 / 巡检 cron（服务器）

`ssh newapi628` 后 `crontab -e` 加入：

```cron
# 每日 02:30 一致性备份（保留最近 7 份）
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

- [ ] CF：`*` + apex A 记录 Proxied 指 `64.90.4.114`；`api` 未动。
- [ ] 源站：通配 vhost + Origin CA 证书装好，`nginx -t` 通过、已 reload。
- [ ] CF SSL 模式 = **Full (strict)**，随机子域 `https://x.wedreamhub.com/api/status` 返回 `success`。
- [ ] `api.wedreamhub.com`（现网）仍 200，未受影响。
- [ ] `deploy.sh` 跑通一次（含自动 `:prev` 镜像 + 部署前备份）。
- [ ] `healthcheck.sh` 全绿；备份/巡检 cron 已装并产出日志。
- [ ] 演练 `rollback.sh` 与一次 `restore.sh`（演练环境）成功。
