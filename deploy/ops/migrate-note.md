# 迁移版本化说明（migrate-note）

> 适用：唯一现网 / 生产栈 `newapi_test`。本文说明 Phase 2 fork 当前的 schema 迁移机制、
> 升级/回滚注意点、以及"表撞名"检查规程。改表结构（加表/加字段/写迁移）前必读，
> 详细数据模型见 [`doc/data-model.md`](../../doc/data-model.md)。

## 1. 当前机制：AutoMigrate + 幂等 raw ALTER（前向、幂等、无 down）

迁移入口在 `internal/mtwire/wire.go` 的 `App.Migrate()`，**在 new-api `InitDB()` 之后**对"多租户增量表"统一迁移（复用 new-api 的共享 `model.DB`，不自开连接）。三类动作：

1. **GORM `AutoMigrate`（各模块 `*/gormrepo.AutoMigrate`）** —— 仅"增量"语义：建表、加列、加索引；**从不删列/删表/改类型缩小**。重复执行幂等。
2. **原生 `users` 表加列 = 幂等 raw ALTER**（`internal/mtwire/agent.go`）—— 先查 `information_schema.columns` 确认无列才 `ALTER TABLE users ADD COLUMN …`。**不改 new-api 的 `model.User` struct**（改它会在 upstream rebase 时冲突）。当前两列：`tenant_id`、`promotion_channel_id`（各带索引）。
3. **桥接表 `migrateSubscriptionBridge`**（`internal/mtwire/subscription_bridge.go`）—— `mt_` 前缀表，与原生订阅表物理隔离。

### 增量表清单（物理表名，权威来自各 `gormrepo` 的 `TableName()`）
| 模块 | 物理表 |
| --- | --- |
| tenant | `tenants`, `tenant_domains`, `tenant_groups`, `tenant_billing_logs` |
| tokenplan | `token_plans`, `tenant_token_plans`, `tokenplan_subscriptions`(★改名), `subscription_usage_logs`, `pending_subscription_orders` |
| agent | `agent_profiles`, `agent_wallets`, `agent_earning_logs`, `agent_withdrawals` |
| promotion | `agent_promotion_channels`, `agent_promotion_attributions` |
| wallet/兑换码 | `user_balances`, `agent_redemption_codes` |
| payment | `payment_orders` |
| 桥接(mt_) | `mt_subscription_orders`, `mt_native_subscription_plans` |
| 原生 `users` 加列 | `tenant_id`, `promotion_channel_id`（raw ALTER） |

★ `tokenplan_subscriptions` 原名 `user_subscriptions`，因与 new-api 原生 `model.UserSubscription` 撞物理表而改名（见 §3 / RETRO）。

## 2. 升级（加表/加字段）注意点

- **新表** → 在对应模块 `gormrepo` 定义 struct + `TableName()`，在其 `AutoMigrate` 注册，并确保 `wire.go App.Migrate()` 调到该模块的 `AutoMigrate`。**动手前先做 §3 撞名检查。**
- **给我们自己的表加列** → 直接改 struct，`AutoMigrate` 会补列（加列是幂等增量，安全）。
- **给 new-api 原生表加列** → **不要改原生 struct**；照 `migrateUsersTenantID` 套路写一个新的"information_schema 探测 + raw ALTER"幂等函数，挂进 `Migrate()`。原因：改原生 struct 会污染 new-api 自身 `AutoMigrate` 并在 upstream rebase 时冲突。
- **索引/唯一约束** → 谨慎对存量大表加 `UNIQUE`（历史脏数据会令 ALTER 失败）；新表随建随加无碍。
- **`quota_data.bucket_key` 升级（停机切换，不支持滚动升级）** → 部署包含该迁移的版本前，必须先 drain/停止全部旧版本应用节点并备份数据库。只在一个新版 master 上临时设置 `QUOTA_DATA_BUCKET_MIGRATION_ACK_DRAINED=1`，由它完成回填、重复 bucket 归并和唯一索引 `idx_quota_data_bucket_key` 创建；确认成功后删除该变量，再扩容新版节点。旧版本不会写 `bucket_key`，不得与此 schema 迁移并行运行，也不要把确认变量写进常驻 Compose/systemd/集群配置。既有表缺少索引且未确认时，应用会拒绝启动；索引建成后不再需要确认变量。
- 迁移**只在 app 启动时**跑一次（master 节点）；多副本部署须保证迁移幂等（已满足）。

## 3. 表撞名检查（硬规程，RETRO 已固化）

> 教训：我们的 `user_subscriptions` 与原生同名 → 我们 `AutoMigrate` 给它加 `source_order_id NOT NULL UNIQUE` → MySQL `Cannot add a UNIQUE column` / 污染原生表。**新增任何 GORM 表前必做：**

```bash
# 在仓库根执行：确认新表名不与 new-api 原生表撞车
grep -rin "TableName() string { return \"<新表名>\"" model/
# 高频撞名词：user/token/subscription/channel/log/option/group/order …
```

- 与原生共存的功能**优先复用原生表**，不另建同名表；必须新建时用模块前缀（`agent_`/`tenant_`/`tokenplan_`/`mt_`）。
- 早期「仅限测试栈、0 原生数据」的 `DROP TABLE <表>` 清理流程已退役；当前栈是生产，禁止使用该做法。如需处理存量表，必须单独设计可回滚迁移、先做配对备份并在一次性隔离环境演练。

## 4. 回滚注意点（关键：AutoMigrate 无 down-migration）

- **代码回滚 ≠ schema 回滚**：`AutoMigrate`/raw ALTER 都是**前向、不可逆**的；回退代码**不会**自动删除已加的列/表。镜像或服务器归档 `--to <ts>` 回滚（见 `rollback.sh`）只回退**程序/release 树**，新加的列/表仍在库里。
- **真正回退 schema** 需人工 `DROP/ALTER` + 必要时用 `restore.sh <backup-*.manifest>` 配对恢复 MySQL + Redis → 因此**部署前必备份**（`deploy.sh` 已将备份失败设为阻断闸门）。
- **破坏性变更**（删列/改名/缩类型）风险最高：务必先 `backup.sh`，并准备手写逆向 SQL；在一次性隔离数据库演练通过后，才能进入标准生产发布链路。
- 回滚顺序建议：① 先 `rollback.sh`（镜像 `:prev`）或 `rollback.sh --to <ts>`（无 git 归档）→ ② 如新版已写入不兼容数据，进入外部维护停流后再评估配对恢复（会丢失恢复点之后的 DB/Redis 状态）。

## 5. 版本化建议（演进方向）

- 现状契约 = **幂等 AutoMigrate + information_schema 守卫的 raw ALTER**；"版本"以**部署 git tag**（`deploy.sh` 自动打 `deploy-<ts>`）为锚。
- 若将来需要**有序、可逆、破坏性**迁移：引入 `schema_migrations` 账本表 + 编号 SQL（up/down），由独立迁移步骤执行（先于 app 启动），不要塞进 AutoMigrate。当前阶段不引入，保持增量幂等最省心。
