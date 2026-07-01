# 自定义域名证书签发脚本（服务器侧）

代理自助绑定自定义域名后，DNS TXT 校验通过（状态 `dns_verified`）的域名由这组脚本**异步**签发
Let's Encrypt 证书、写 Nginx vhost 上线，并回写主站把状态推到 `active`（详见 `doc/domains-ssl.md`）。

> 运行环境：服务器 `64.90.4.114`（Debian 12 + 宝塔 + Docker）。多租户测试栈 app 监听 `127.0.0.1:3100`。
> **红线**：脚本只反代到 `127.0.0.1:3100`，绝不碰现网 `newapi_YFNf`（:3000）；各域名写独立 vhost、不声明
> `default_server`，与宝塔已有 Nginx 零冲突。

## 组成

| 文件 | 作用 |
| --- | --- |
| `custom-domain-lib.sh` | 共享配置 + 函数（vhost 模板、reload、回写、证书到期解析）。被其余脚本 source。 |
| `install-acme.sh` | 安装 acme.sh（默认 CA=Let's Encrypt）+ 建 webroot/状态目录 + 装 systemd timer。**一次性**。 |
| `issue-cert.sh <域名>` | 为单个 `dns_verified` 域名签发并上线：HTTP-01 webroot → 装证书 → 写 443 vhost → 回写转 active。 |
| `domain-cert-loop.sh` | 轮询主站 `GET /api/internal/domain/pending-cert` → 逐个 `issue-cert.sh`，含 LE 限流指数退避。 |

## 安装（一次）

```bash
cd /root/newapi-test/scripts   # 或本仓库 scripts/ 同步到服务器的位置
chmod +x *.sh
./install-acme.sh
```

装好后 `newapi-cert.timer` 每 2 分钟跑一次 `domain-cert-loop.sh`，自动签发新通过校验的域名。

## 手动签发 / 排错

```bash
./issue-cert.sh proxy.example.com      # 立即为某域名签发
journalctl -u newapi-cert.service -n 50 --no-pager
tail -f /var/log/newapi-cert.log
systemctl list-timers newapi-cert.timer
```

## 配置（环境变量覆盖，见 custom-domain-lib.sh）

`ACME_EMAIL` `WEBROOT` `VHOST_DIR` `CERT_ROOT` `UPSTREAM`(默认 127.0.0.1:3100) `ENV_FILE`(取
`MT_INTERNAL_SECRET`) `API_BASE` `STATE_DIR` `LOG_FILE`。

## 续期

acme.sh 自带 cron 续期；`issue-cert.sh` 在 `--install-cert` 时登记 `--reloadcmd "nginx -s reload"`，
续期后自动热加载 Nginx。

## 前置（代理侧）

1. 把自定义域名 **A 记录**指向 `64.90.4.114`（直连，不要套 Cloudflare 代理，否则 HTTP-01 取不到）。
2. 添加 **TXT 记录** `_newapi-verify.<域名> = <绑定时返回的 verify_token>`，再点"去验证"。
