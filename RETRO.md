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
