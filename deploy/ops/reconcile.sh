#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# reconcile.sh — 唯一现网 / 生产栈 newapi_test 账目对账（**服务器上运行**）。
#
#   只读 SQL 校验（绝不写库），逐项 PASS/FAIL + 末尾汇总。校验五类账目一致性：
#     1) 充值对账   payment_orders 无卡在 paid（瞬态）；order_no 唯一（无重复入账）。
#     2) 套餐对账   每个 activated 的 SUB 单 ↔ 一条 user_subscriptions + 一条
#                   tokenplan_subscriptions；无 SUB 单激活两次（无原生订阅复用/重复台账）。
#     3) 分润对账   agent_earning_logs 按 tenant 求和 == agent_wallets.total_earned；
#                   idem_key 唯一（无重复分润）。
#     4) 提现对账   Σ(pending + approved)==frozen_withdraw_amount；
#                   total_earned == withdrawable + frozen + Σ(paid)（金额守恒）。
#     5) 越权/孤儿  无 agent_earning_logs / wallet / withdrawal 属于无 agent_profile 的 tenant。
#
#   退出码 = FAIL 项数（0=全绿，可接 cron/告警）。每个 check 的 SQL 返回「违规行」，
#   空集即 PASS，非空即 FAIL 并打印差异明细。
#
# 用法（服务器）：
#   cd /root/newapi-test/deploy/ops && ./reconcile.sh
#   DB_NAME=other ./reconcile.sh           # 覆盖默认（lib.sh 提供全部参数默认值）
#
# 安全：只读；guard_target（旧名 guard_not_prod）确认目标确为期望栈 newapi_test（挡误配）。
# ─────────────────────────────────────────────────────────────────────────────
source "$(dirname "$0")/lib.sh"
guard_not_prod
require docker
require_db_pass          # DB 口令缺失即中止（fail-closed）——只读对账 SQL 必需

# ── 解析 mysql 容器（compose 标签定位，cwd/compose 文件无关）────────────────────
MYSQL_CID="$(docker ps \
  --filter "label=com.docker.compose.project=$STACK" \
  --filter "label=com.docker.compose.service=$MYSQL_SVC" -q | head -1)"
[ -n "$MYSQL_CID" ] || die "找不到栈 $STACK 的 $MYSQL_SVC 容器（栈未启动？）"

# db：执行 SQL，-N 无表头（供 check 比对违规行）。库名含连字符需反引号。
# 密码经 stdin 送入容器内短命 0600 option file，不出现在 docker/mysql argv。
db()  { mysql_with_secret mysql -u"$DB_USER" -N -e "USE \`$DB_NAME\`; $1" 2>/dev/null; }
# dbt：带表格边框输出（供概览/台账展示）。
dbt() { mysql_with_secret mysql -u"$DB_USER" -t -e "USE \`$DB_NAME\`; $1" 2>/dev/null; }

PASS=0; FAIL=0
section() { printf '\n\033[1;36m── %s ─────────────────────────────\033[0m\n' "$*"; }
info()    { printf '\033[1;34m[i]\033[0m %s\n' "$*"; }

# check <名称> <SQL>：SQL 须返回「违规行」；空=PASS，非空=FAIL+打印明细。
check() {
  local name="$1" sql="$2" out
  out="$(db "$sql")"
  if [ -z "$out" ]; then
    printf '\033[1;32m[PASS]\033[0m %s\n' "$name"; PASS=$((PASS + 1))
  else
    printf '\033[1;31m[FAIL]\033[0m %s\n' "$name"; FAIL=$((FAIL + 1))
    printf '%s\n' "$out" | sed 's/^/         ⤷ /'
  fi
}

echo "────────── reconcile @ $(date '+%F %T') 栈=$STACK 库=$DB_NAME ──────────"

# ════════════════════════════════════════════════════════════════════════════
# 概览（INFO，非判定项）—— created/pending 为「已下单未支付」的废弃单，属正常态。
# ════════════════════════════════════════════════════════════════════════════
section "概览（INFO）"
info "充值订单 payment_orders（按状态；created=用户下单未付，正常）"
dbt "SELECT status, COUNT(*) n, ROUND(SUM(actual_paid),2) sum_cny FROM payment_orders GROUP BY status;"
info "套餐订单 mt_subscription_orders（按状态；pending=下单未付，正常）"
dbt "SELECT status, COUNT(*) n, ROUND(SUM(amount_cny),2) sum_cny FROM mt_subscription_orders GROUP BY status;"
info "代理分润 agent_earning_logs（按来源）"
dbt "SELECT source_type, COUNT(*) n,
            CAST(CAST(SUM(amount_units) AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS sum_cny
       FROM agent_earning_logs GROUP BY source_type;"
info "提现 agent_withdrawals（按状态）"
dbt "SELECT status, COUNT(*) n,
            CAST(CAST(SUM(amount_units) AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS sum_cny
       FROM agent_withdrawals GROUP BY status;"

# ════════════════════════════════════════════════════════════════════════════
# 1) 充值对账（payment_orders）
# ════════════════════════════════════════════════════════════════════════════
section "1) 充值对账"
# paid 是「验签后入账中」的瞬态：OnPaid 成功→credited、失败→回滚 created。长期卡 paid =
# 入账中断未兜底，须人工核查（违规行=卡单）。created/credited/failed 均为合法静态。
check "1.1 payment_orders 无卡在 paid（瞬态未落地）" \
  "SELECT order_no, status, updated_at FROM payment_orders WHERE status='paid';"
# order_no 是入账幂等键，重复即可能重复加额度。
check "1.2 payment_orders order_no 唯一（无重复入账）" \
  "SELECT order_no, COUNT(*) c FROM payment_orders GROUP BY order_no HAVING c>1;"

# ════════════════════════════════════════════════════════════════════════════
# 2) 套餐对账（mt_subscription_orders ↔ user_subscriptions / tokenplan_subscriptions）
# ════════════════════════════════════════════════════════════════════════════
section "2) 套餐对账"
# 激活=建一条原生 user_subscriptions（native_sub_id 回填）。activated 却无原生订阅 = 激活半成品。
check "2.1 每个 activated SUB 单 ↔ 一条 user_subscriptions（原生订阅桶）" \
  "SELECT order_no, native_sub_id FROM mt_subscription_orders o WHERE status='activated'
     AND (native_sub_id=0 OR NOT EXISTS (SELECT 1 FROM user_subscriptions u WHERE u.id=o.native_sub_id));"
# 激活同时落一条我们的 tokenplan_subscriptions 台账（source_order_id=order_no）。
check "2.2 每个 activated SUB 单 ↔ 一条 tokenplan_subscriptions（我方台账）" \
  "SELECT order_no FROM mt_subscription_orders o WHERE status='activated'
     AND NOT EXISTS (SELECT 1 FROM tokenplan_subscriptions t WHERE t.source_order_id=o.order_no);"
# 同一 SUB 单激活两次 → 同 source_order_id 出现多条台账（重复发货）。
check "2.3 无 SUB 单重复台账（tokenplan_subscriptions.source_order_id 唯一）" \
  "SELECT source_order_id, COUNT(*) c FROM tokenplan_subscriptions
     WHERE source_order_id<>'' GROUP BY source_order_id HAVING c>1;"
# 一条原生订阅被多个 activated 单引用 → 激活幂等被击穿（重复建原生桶）。
check "2.4 无原生订阅被多个 activated SUB 单复用（native_sub_id 唯一）" \
  "SELECT native_sub_id, COUNT(*) c FROM mt_subscription_orders
     WHERE status='activated' AND native_sub_id>0 GROUP BY native_sub_id HAVING c>1;"
check "2.5 mt_subscription_orders order_no 唯一" \
  "SELECT order_no, COUNT(*) c FROM mt_subscription_orders GROUP BY order_no HAVING c>1;"

# ════════════════════════════════════════════════════════════════════════════
# 3) 分润对账（agent_earning_logs ↔ agent_wallets）
# ════════════════════════════════════════════════════════════════════════════
section "3) 分润对账"
# 钱包累计收益必须等于其全部收益明细之和（每条收益增 total_earned）。
# *_units 是 1e-8 元定点整数权威列；直接按整数对账，避免 SQLite/客户端浮点容差掩盖差异。
check "3.1 Σ(agent_earning_logs.amount) == agent_wallets.total_earned（按租户）" \
  "SELECT w.tenant_id,
          CAST(CAST(w.total_earned_units AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS total_earned,
          CAST(CAST(COALESCE(e.s,0) AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS log_sum
     FROM agent_wallets w
     LEFT JOIN (SELECT tenant_id, SUM(amount_units) s FROM agent_earning_logs GROUP BY tenant_id) e
            ON e.tenant_id=w.tenant_id
    WHERE w.total_earned_units <> COALESCE(e.s,0);"
# 有收益明细却无钱包 = 收益落到不存在的钱包（孤儿收益）。
check "3.2 无收益明细落在不存在的钱包（每个有收益的租户都有钱包）" \
  "SELECT e.tenant_id,
          CAST(CAST(SUM(e.amount_units) AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS sum_cny
     FROM agent_earning_logs e
    WHERE e.tenant_id NOT IN (SELECT tenant_id FROM agent_wallets) GROUP BY e.tenant_id;"
# idem_key_hash 是分润强幂等键（SHA256(tenant\0source_type\0source_id)）；
# 使用纯小写 hex 规避 MySQL 默认排序规则与 PostgreSQL/SQLite 的大小写语义差异。
check "3.3 agent_earning_logs 精确幂等哈希唯一（无重复分润）" \
  "SELECT idem_key_hash, COUNT(*) c FROM agent_earning_logs GROUP BY idem_key_hash HAVING COUNT(*)>1;"
check "3.4 agent_earning_logs 精确幂等哈希完整且正确" \
  "SELECT id, tenant_id, source_type, source_id FROM agent_earning_logs
    WHERE idem_key_hash IS NULL
       OR BINARY idem_key_hash<>BINARY LOWER(SHA2(CONCAT(CAST(tenant_id AS CHAR),CHAR(0),source_type,CHAR(0),source_id),256));"
# 兼容 decimal 列供旧报表读取，但不可偏离权威整数列；偏离说明存在绕过仓储的写入或迁移异常。
check "3.5 agent_wallets 金额镜像与权威 units 一致" \
  "SELECT tenant_id FROM agent_wallets
    WHERE api_balance IS NULL OR api_balance_units IS NULL
       OR withdrawable_balance IS NULL OR withdrawable_balance_units IS NULL
       OR frozen_withdraw_amount IS NULL OR frozen_withdraw_amount_units IS NULL
       OR total_earned IS NULL OR total_earned_units IS NULL
       OR api_balance*100000000<>api_balance_units
       OR withdrawable_balance*100000000<>withdrawable_balance_units
       OR frozen_withdraw_amount*100000000<>frozen_withdraw_amount_units
       OR total_earned*100000000<>total_earned_units;"
check "3.6 agent_earning_logs 金额镜像与权威 units 一致" \
  "SELECT id, tenant_id FROM agent_earning_logs
    WHERE amount IS NULL OR amount_units IS NULL OR amount*100000000<>amount_units;"

# ════════════════════════════════════════════════════════════════════════════
# 4) 提现对账（agent_withdrawals ↔ agent_wallets，金额守恒）
# ════════════════════════════════════════════════════════════════════════════
section "4) 提现对账"
# 申请提现冻结可提现余额；pending 待审核与 approved 待打款都仍占用冻结额。
check "4.1 Σ(pending + approved 提现额) == agent_wallets.frozen_withdraw_amount（按租户）" \
  "SELECT w.tenant_id,
          CAST(CAST(w.frozen_withdraw_amount_units AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS frozen,
          CAST(CAST(COALESCE(p.s,0) AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS frozen_order_sum
     FROM agent_wallets w
     LEFT JOIN (SELECT tenant_id, SUM(amount_units) s FROM agent_withdrawals WHERE status IN ('pending','approved') GROUP BY tenant_id) p
            ON p.tenant_id=w.tenant_id
    WHERE w.frozen_withdraw_amount_units <> COALESCE(p.s,0);"
# 金额守恒：累计收益 = 可提现 + 冻结中 + 已打款。rejected 已解冻退回，approved 仍在 frozen。
check "4.2 金额守恒 total_earned == withdrawable + frozen + Σ(paid 已打款)（按租户）" \
  "SELECT w.tenant_id,
          CAST(CAST(w.total_earned_units AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS total_earned,
          CAST(CAST(w.withdrawable_balance_units AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS withdrawable,
          CAST(CAST(w.frozen_withdraw_amount_units AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS frozen,
          CAST(CAST(COALESCE(a.s,0) AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS paid_out
     FROM agent_wallets w
     LEFT JOIN (SELECT tenant_id, SUM(amount_units) s FROM agent_withdrawals WHERE status='paid' GROUP BY tenant_id) a
            ON a.tenant_id=w.tenant_id
    WHERE w.total_earned_units <> (w.withdrawable_balance_units + w.frozen_withdraw_amount_units + COALESCE(a.s,0));"
check "4.3 agent_withdrawals 金额镜像与权威 units 一致" \
  "SELECT id, tenant_id FROM agent_withdrawals
    WHERE amount IS NULL OR amount_units IS NULL OR amount*100000000<>amount_units;"
check "4.4 提现状态与金额合法" \
  "SELECT id, tenant_id, status, amount_units FROM agent_withdrawals
    WHERE status NOT IN ('pending','approved','paid','rejected') OR amount_units<=0
       OR (status='paid' AND (payout_ref='' OR paid_at IS NULL))
       OR (status<>'paid' AND (payout_ref<>'' OR paid_at IS NOT NULL));"
check "4.5 钱包冻结提现余额不为负" \
  "SELECT tenant_id, frozen_withdraw_amount_units FROM agent_wallets
    WHERE frozen_withdraw_amount_units<0;"
check "4.6 每笔提现都归属存在的钱包" \
  "SELECT wd.id, wd.tenant_id FROM agent_withdrawals wd
    LEFT JOIN agent_wallets w ON w.tenant_id=wd.tenant_id
   WHERE w.tenant_id IS NULL;"
check "4.7 非空打款凭证精确哈希不重复" \
  "SELECT payout_ref_hash, COUNT(*) AS duplicate_count FROM agent_withdrawals
    WHERE payout_ref<>'' GROUP BY payout_ref_hash HAVING COUNT(*)>1;"
check "4.8 提现幂等键/打款凭证哈希完整且逐字节一致" \
  "SELECT id, tenant_id FROM agent_withdrawals
    WHERE (request_key IS NULL AND request_key_hash IS NOT NULL)
       OR (request_key IS NOT NULL AND (BINARY request_key<>BINARY TRIM(request_key) OR request_key_hash IS NULL OR BINARY request_key_hash<>BINARY LOWER(SHA2(request_key,256))))
       OR (payout_ref='' AND payout_ref_hash IS NOT NULL)
       OR (payout_ref<>'' AND (BINARY payout_ref<>BINARY TRIM(payout_ref) OR payout_ref_hash IS NULL OR BINARY payout_ref_hash<>BINARY LOWER(SHA2(payout_ref,256))));"
check "4.9 打款凭证 claim 与提现单双向一致" \
  "SELECT wd.id, wd.payout_ref FROM agent_withdrawals wd
     LEFT JOIN agent_payout_ref_claims_v3 c ON c.payout_ref_hash=wd.payout_ref_hash
    WHERE wd.payout_ref<>'' AND (c.withdrawal_id IS NULL OR c.withdrawal_id<>wd.id OR BINARY c.payout_ref<>BINARY wd.payout_ref)
    UNION ALL
   SELECT c.withdrawal_id, c.payout_ref FROM agent_payout_ref_claims_v3 c
     LEFT JOIN agent_withdrawals wd ON wd.id=c.withdrawal_id
    WHERE wd.id IS NULL OR wd.status<>'paid' OR wd.paid_at IS NULL OR wd.payout_ref_hash IS NULL
       OR BINARY wd.payout_ref_hash<>BINARY c.payout_ref_hash OR BINARY c.payout_ref<>BINARY wd.payout_ref
       OR BINARY c.payout_ref_hash<>BINARY LOWER(SHA2(c.payout_ref,256));"

# ════════════════════════════════════════════════════════════════════════════
# 5) 越权 / 孤儿（agent_* 行必须归属一个有 agent_profile 的租户）
# ════════════════════════════════════════════════════════════════════════════
section "5) 越权 / 孤儿"
check "5.1 无收益明细属于无 agent_profile 的租户" \
  "SELECT DISTINCT tenant_id FROM agent_earning_logs WHERE tenant_id NOT IN (SELECT tenant_id FROM agent_profiles);"
check "5.2 无钱包属于无 agent_profile 的租户" \
  "SELECT tenant_id FROM agent_wallets WHERE tenant_id NOT IN (SELECT tenant_id FROM agent_profiles);"
check "5.3 无提现单属于无 agent_profile 的租户" \
  "SELECT DISTINCT tenant_id FROM agent_withdrawals WHERE tenant_id NOT IN (SELECT tenant_id FROM agent_profiles);"

# ════════════════════════════════════════════════════════════════════════════
# 代理钱包台账（守恒视图）—— residual 应恒为 0。
# ════════════════════════════════════════════════════════════════════════════
section "代理钱包台账（守恒视图，residual 应为 0）"
dbt "SELECT w.tenant_id,
            CAST(CAST(w.total_earned_units AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS total_earned,
            CAST(CAST(w.withdrawable_balance_units AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS withdrawable,
            CAST(CAST(w.frozen_withdraw_amount_units AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS frozen,
            CAST(CAST(COALESCE(a.s,0) AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS paid_out,
            CAST(CAST(w.total_earned_units-(w.withdrawable_balance_units+w.frozen_withdraw_amount_units+COALESCE(a.s,0)) AS DECIMAL(28,8))/100000000 AS DECIMAL(28,8)) AS residual
       FROM agent_wallets w
       LEFT JOIN (SELECT tenant_id,SUM(amount_units) s FROM agent_withdrawals WHERE status='paid' GROUP BY tenant_id) a
              ON a.tenant_id=w.tenant_id;"

# ── 汇总 ─────────────────────────────────────────────────────────────────────
echo
if [ "$FAIL" -eq 0 ]; then
  printf '\033[1;32m对账完成：%d 项全部 PASS（栈=%s 库=%s）\033[0m\n' "$PASS" "$STACK" "$DB_NAME"
else
  printf '\033[1;31m对账完成：%d PASS / %d FAIL（栈=%s 库=%s）—— 见上方 ⤷ 差异明细\033[0m\n' "$PASS" "$FAIL" "$STACK" "$DB_NAME"
fi
exit "$FAIL"
