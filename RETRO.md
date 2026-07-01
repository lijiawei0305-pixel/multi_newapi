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

---

## 二、构建与依赖

> Go 编译、Node 前端构建、版本不兼容、依赖拉取、缓存等构建侧的坑。

### [已解决] golangci-lint 未装于 Mac，质量门降级
- **现象**：Mac 本机无 `golangci-lint`，prompt.md 约定的 Go 静态检查门无法原样执行。
- **根因**：本机未安装，且为保持 Worker 离线、零外部依赖（纯标准库单测），不临时安装。
- **解决/规避**：质量门降级为 `go build` + `go vet` + `gofmt -l` + `go test -race -cover`（均内置、离线可用）；集成阶段在服务器/CI 安装 `golangci-lint` 补强。
- **升级**：暂不升级；待集成阶段加 CI 后再固化为门禁。

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

### [未解决·需决策] feature/finance-report 工作树落后于服务器：缺未入库的 internal/siteconfig/gormrepo
- **现象**：本地校验后端时，服务器 `go build` 报 `wire.go: could not import internal/siteconfig/gormrepo`；`a.ReportRepo undefined` 是该 import 失败使整个 mtwire 包类型失效的**级联**。
- **根因**：本 `.ccg` 工作树分支 `feature/finance-report` 的 HEAD **从未含** siteconfig 装配（`git show HEAD:wire.go` 无 siteconfig），而服务器 `/root/newapi-test` 有一份**未入库（scp 而来，git 不跟踪）**的 `internal/siteconfig/gormrepo`（OEM 装修 §5/§9）+ 对应 wire.go/mt-router.go/侧栏/语言包装配。两边 wire.go 各自是公共祖先的超集（本地多 reportrepo、服务器多 siteconfig）——**必须 3-way 合并，不能整文件覆盖**（否则丢 siteconfig）。财务报表改动为纯增量、与 siteconfig 区域不相交：`git diff HEAD -- wire.go mt-router.go` 生成的补丁在服务器真实副本上 `patch --fuzz=3` 干净套用（0 rej），合并后 `go build` EXIT=0 已验证。
- **规避/待决**：部署财务报表时，wire.go/mt-router.go/use-sidebar-data.ts/6 语言包这 4 类需与服务器版**合并**（非覆盖）；或先把服务器未入库的 siteconfig/gormrepo 收编进分支（rebase 到真实基线）再统一同步。**入库策略待用户定**。
