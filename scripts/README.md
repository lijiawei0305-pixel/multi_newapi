# 自定义域名证书签发脚本（服务器侧）

代理自助绑定自定义域名后，DNS TXT 校验通过（状态 `dns_verified`）的域名由这组脚本**异步**签发
Let's Encrypt 证书、写 Nginx vhost 上线，并回写主站把状态推到 `active`（详见 `doc/domains-ssl.md`）。

> 运行环境：服务器 `64.90.4.114`（Debian 12 + 宝塔 + Docker）。唯一现网多租户栈
> `newapi_test` 的 app 监听 `127.0.0.1:3100`；原 `newapi_YFNf`（`:3000`）已删除，不得再当作隔离/回退目标。
> **红线**：脚本只反代到该回环端口；各域名写独立 vhost、不声明 `default_server`，
> 并在 Nginx 层拒绝 `/api/internal/`，与宝塔已有站点共存。

## 组成

| 文件 | 作用 |
| --- | --- |
| `custom-domain-lib.sh` | 共享配置 + 函数（vhost 模板、reload、回写、证书到期解析）。被其余脚本 source。 |
| `install-acme.sh` | 下载固定 acme.sh commit，校验内置 SHA-256 后才以 root 执行；默认 CA=Let's Encrypt，建 webroot/状态目录并装 systemd timer。可幂等重跑。 |
| `issue-cert.sh <域名>` | 为单个 `dns_verified` 域名签发并上线：HTTP-01 webroot → 装证书 → 写 443 vhost → 回写转 active。 |
| `domain-cert-loop.sh` | 轮询主站 `GET /api/internal/domain/pending-cert` → 逐个 `issue-cert.sh`，含 LE 限流指数退避。 |

## 安装（一次）

```bash
cd /root/newapi-test/scripts   # 或本仓库 scripts/ 同步到服务器的位置
chmod +x *.sh
./install-acme.sh
```

安装器当前锁定 acme.sh `v3.1.4` commit `3661fd86b6304115e42f43910e6dd452ab9866d6`，下载件 SHA-256 不匹配时会在执行任何远程代码、写 unit 或调用 `systemctl` 前中止。unit/timer 更新为事务式：任一写入或 `systemctl` 步骤失败会恢复原文件与 enable/active 状态。装好后 `newapi-cert.timer` 每 2 分钟跑一次 `domain-cert-loop.sh`，自动签发新通过校验的域名。

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

`MT_INTERNAL_SECRET` 只从权限 `600` 的 `ENV_FILE` 读取。回写内网 API 时，脚本经 stdin FD
向 curl 传递 header，不会把密钥放进可被 `ps`/`/proc/*/cmdline` 读取的进程参数，也不产生密钥临时文件。

## 续期

安装器显式使用 `--no-cron`，调度只由 `newapi-cert.timer` 接管，避免 root crontab 与 systemd 重复运行。`issue-cert.sh` 在 `--install-cert` 时登记 `--reloadcmd "nginx -s reload"`，证书更新后自动热加载 Nginx。

## 前置（代理侧）

1. 把自定义域名 **A 记录**指向 `64.90.4.114`（直连，不要套 Cloudflare 代理，否则 HTTP-01 取不到）。
2. 添加 **TXT 记录** `_newapi-verify.<域名> = <绑定时返回的 verify_token>`，再点"去验证"。
