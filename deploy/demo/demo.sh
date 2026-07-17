#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# demo.sh — 一条命令跑通主流程的自动化演示（**服务器上运行**，bash + curl）。
#
#   逐步打印「步骤 → 结果」，每步打印关键数值；任一步失败 set -e 退出。
#   走真实 API（多租户 Host + 会话 cookie + New-Api-User 头），仅「读取演示账号的
#   API Key」与「校验账目数值」时直读 DB（演示在服务器上，DB 可达，注释标注）。
#
#   主流程（对标 Phase 1 已验证闭环）：
#     ① chanuser1 登录
#     ② 买 mini 套餐（返 order_no/pay_url）
#     ③ auth-service /auth/mock/confirm 确认支付
#     ④ 校验：原生订阅 active + 代理 demoagent 得 tokenplan_spread
#     ⑤ chanuser1 用 API Key 调 /v1（gpt-5.4-mini），证明走「订阅桶」(logs.billing_source)
#     ⑥ demoagent 查收益 / 申请提现
#     ⑦ admin 审核通过（校验金额守恒）
#     ⑧ 充值 $1（mock）→ quota +500000
#
# 用法（服务器）：
#   cd /root/newapi-test/deploy/demo && ./demo.sh
#   PLAN_ID=3 WITHDRAW_CNY=20 ./demo.sh        # 任意参数可用环境变量覆盖
#
# 幂等友好：每次跑都是「新订单 / 新充值」（金额累加，无害）；API Key 按名复用、不重复建。
# 安全：guard_target（旧名 guard_not_prod）确认目标确为期望栈 newapi_test（挡误配）。
# ⚠ 注意：newapi_test 现为唯一现网/生产——demo.sh 会在生产上造演示订单/充值，仅限受控演示时运行。
# 演示口令仅供演示，部署后务必改（见 README）。
# ─────────────────────────────────────────────────────────────────────────────
source "$(dirname "$0")/../ops/lib.sh"
guard_not_prod
require curl; require docker; require python3

APP="http://127.0.0.1:${APP_PORT}"     # 主站 app（经 Host 头识别租户）
AUTH="http://127.0.0.1:${AUTH_PORT}"   # auth-service（支付网关，mock）

# ── 演示账号 / 参数（部署后务必改密；可用环境变量覆盖）─────────────────────────
ADMIN_USER="${ADMIN_USER:-admin}";       ADMIN_PASS="${ADMIN_PASS:?请先 export ADMIN_PASS=<演示栈管理员密码>（不入库）}"
AGENT_USER="${AGENT_USER:-demoagent}";   AGENT_PASS="${AGENT_PASS:-demoagent123}"
BUYER_USER="${BUYER_USER:-chanuser1}";   BUYER_PASS="${BUYER_PASS:-chanuser123}"
TENANT_ID="${TENANT_ID:-1}"              # tokendream（demoagent 为 owner）
PLAN_ID="${PLAN_ID:-2}"                  # mini（¥119，月限额 $220，代理成本 ¥95.20→差价 ¥23.80）
PLAN_MODEL="${PLAN_MODEL:-gpt-5.4-mini}" # 上游已配渠道的模型
TOKEN_NAME="${TOKEN_NAME:-demo-cli}"     # 演示用 API Key 名（按名复用）
WITHDRAW_CNY="${WITHDRAW_CNY:-10}"       # 演示提现额（¥）
RECHARGE_USD="${RECHARGE_USD:-1}"        # 演示充值额（$）

# ── mysql 容器（仅用于「读 API Key」与「校验数值」；主链路全走 API）──────────────
MYSQL_CID="$(docker ps \
  --filter "label=com.docker.compose.project=$STACK" \
  --filter "label=com.docker.compose.service=$MYSQL_SVC" -q | head -1)"
[ -n "$MYSQL_CID" ] || die "找不到栈 $STACK 的 $MYSQL_SVC 容器（栈未启动？）"
db() { docker exec -i "$MYSQL_CID" mysql -u"$DB_USER" -p"$DB_PASS" -N -e "USE \`$DB_NAME\`; $1" 2>/dev/null; }

# ── 小工具 ───────────────────────────────────────────────────────────────────
# jget <a.b.0.c>：从 stdin 的 JSON 取嵌套字段（list 用数字下标）；缺 jq 故用 python3。
jget() { python3 -c '
import sys, json
d = json.load(sys.stdin)
for k in sys.argv[1].split("."):
    if k == "":
        continue
    d = d[int(k)] if isinstance(d, list) else d[k]
if isinstance(d, bool):
    print("true" if d else "false")
elif d is None:
    print("")
else:
    print(d)' "$1"; }

step() { printf '\n\033[1;36m▶ 步骤 %s\033[0m  %s\n' "$1" "$2"; }
res()  { printf '   \033[1;32m✓\033[0m %s\n' "$*"; }
kv()   { printf '     %-24s %s\n' "$1" "$2"; }

# assert_ok <resp> <label>：断言 {success:true}，否则打印 message 退出。
assert_ok() {
  local ok msg
  ok="$(printf '%s' "$1" | jget success 2>/dev/null || echo false)"
  if [ "$ok" != "true" ]; then
    msg="$(printf '%s' "$1" | jget message 2>/dev/null || true)"
    die "$2 失败：${msg:-$1}"
  fi
}

# login <user> <pass> <jar> → echo user_id（并写 cookie 到 jar）。
login() {
  local resp
  resp="$(curl -sS -c "$3" -H "Host: $HOST_HEADER" -H 'Content-Type: application/json' \
        -X POST "$APP/api/user/login" -d "{\"username\":\"$1\",\"password\":\"$2\"}")"
  assert_ok "$resp" "登录 $1"
  printf '%s' "$resp" | jget data.id
}

# capi <method> <path> <jar> <uid> [json-body]：带 Host + 会话 + New-Api-User 的控制台 API。
capi() {
  local m="$1" p="$2" jar="$3" uid="$4" body="${5:-}"
  if [ -n "$body" ]; then
    curl -sS -b "$jar" -H "Host: $HOST_HEADER" -H "New-Api-User: $uid" \
         -H 'Content-Type: application/json' -X "$m" "$APP$p" -d "$body"
  else
    curl -sS -b "$jar" -H "Host: $HOST_HEADER" -H "New-Api-User: $uid" -X "$m" "$APP$p"
  fi
}

# confirm_pay <order_no> → echo HTTP code（auth-service mock 确认页 = 合成合法回调）。
confirm_pay() {
  curl -sS -o /dev/null -w '%{http_code}' -X POST "$AUTH/auth/mock/confirm" --data-urlencode "order=$1"
}

WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT
JAR_BUYER="$WORK/buyer.cookie"; JAR_AGENT="$WORK/agent.cookie"; JAR_ADMIN="$WORK/admin.cookie"

echo "════════ newapi628 主流程自动化演示  栈=$STACK  Host=$HOST_HEADER ════════"
echo "  app=$APP   auth-service=$AUTH   $(date '+%F %T')"

# ════════════════════════════════════════════════════════════════════════════
# ① 终端用户登录
# ════════════════════════════════════════════════════════════════════════════
step 1 "终端用户「$BUYER_USER」登录"
BUYER_ID="$(login "$BUYER_USER" "$BUYER_PASS" "$JAR_BUYER")"
CUR="$(curl -sS -H "Host: $HOST_HEADER" "$APP/api/tenant/current")"
res "登录成功（user_id=$BUYER_ID）"
kv "归属租户" "$(printf '%s' "$CUR" | jget data.site_name)（slug=$(printf '%s' "$CUR" | jget data.slug)）"

# ════════════════════════════════════════════════════════════════════════════
# ② 购买套餐 → 生成待支付订单
# ════════════════════════════════════════════════════════════════════════════
step 2 "购买套餐（plan_id=$PLAN_ID）→ 下单"
EARNED_BEFORE="$(db "SELECT COALESCE(total_earned,0) FROM agent_wallets WHERE tenant_id=$TENANT_ID;")"
BUY="$(capi POST "/api/tenant/token-plans/$PLAN_ID/purchase" "$JAR_BUYER" "$BUYER_ID" '{"provider":"wxpay"}')"
assert_ok "$BUY" "购买下单"
ORDER="$(printf '%s'  "$BUY" | jget data.order_no)"
AMT_CNY="$(printf '%s' "$BUY" | jget data.amount_cny)"
PAY_URL="$(printf '%s' "$BUY" | jget data.pay_url)"
res "下单成功"
kv "order_no"   "$ORDER"
kv "应付金额"   "¥$AMT_CNY"
kv "支付页 URL" "$PAY_URL"

# ════════════════════════════════════════════════════════════════════════════
# ③ auth-service 模拟支付确认（合成合法回调 → 主站内网入账）
# ════════════════════════════════════════════════════════════════════════════
step 3 "auth-service 模拟支付确认（POST /auth/mock/confirm）"
CODE="$(confirm_pay "$ORDER")"
[ "$CODE" = "200" ] || die "支付确认 HTTP $CODE（期望 200）"
res "支付回调成功（HTTP 200）→ 验签 + 幂等 + 主站内网入账 + 激活"

# ════════════════════════════════════════════════════════════════════════════
# ④ 校验：原生订阅激活 + 代理获得套餐差价
# ════════════════════════════════════════════════════════════════════════════
step 4 "校验：原生订阅 active + 代理「$AGENT_USER」得 tokenplan_spread"
SUB_STATUS="$(db "SELECT status FROM mt_subscription_orders WHERE order_no='$ORDER';")"
[ "$SUB_STATUS" = "activated" ] || die "SUB 订单未激活（status=$SUB_STATUS）"
NATIVE_SUB_ID="$(db "SELECT native_sub_id FROM mt_subscription_orders WHERE order_no='$ORDER';")"
read -r NS_AMT NS_STATUS <<<"$(db "SELECT amount_total, status FROM user_subscriptions WHERE id=$NATIVE_SUB_ID;")"
[ "$NS_STATUS" = "active" ] || die "原生订阅非 active（status=$NS_STATUS）"
res "SUB 订单 activated → 原生订阅 id=$NATIVE_SUB_ID status=$NS_STATUS"
kv "amount_total" "$NS_AMT quota（= \$$(python3 -c "print($NS_AMT/500000)") 额度上限）"
SPREAD="$(db "SELECT amount FROM agent_earning_logs WHERE source_id='$ORDER' AND source_type='tokenplan_spread';")"
EARNED_AFTER="$(db "SELECT COALESCE(total_earned,0) FROM agent_wallets WHERE tenant_id=$TENANT_ID;")"
[ -n "$SPREAD" ] || die "未见 tokenplan_spread 分润"
res "代理获得 tokenplan_spread = ¥$SPREAD（零售价 − 代理成本价）"
kv "代理 total_earned" "¥$EARNED_BEFORE → ¥$EARNED_AFTER"

# ════════════════════════════════════════════════════════════════════════════
# ⑤ 终端用户用 API Key 调 /v1，证明走「订阅桶」计费
# ════════════════════════════════════════════════════════════════════════════
step 5 "终端用户用 API Key 调 /v1（$PLAN_MODEL）→ 证明走订阅桶"
# API Key 明文仅创建时可得，演示在服务器上故直读该用户 demo-cli 令牌的 key（按名复用，不重复建）。
KEY="$(db "SELECT \`key\` FROM tokens WHERE user_id=$BUYER_ID AND name='$TOKEN_NAME' ORDER BY id LIMIT 1;")"
if [ -z "$KEY" ]; then
  # 注意：new-api token 路由是 POST /api/token/（带尾斜杠，否则 307 重定向丢 body）。
  TK="$(capi POST "/api/token/" "$JAR_BUYER" "$BUYER_ID" "{\"name\":\"$TOKEN_NAME\",\"unlimited_quota\":true,\"remain_quota\":0}")"
  assert_ok "$TK" "创建 API Key"
  KEY="$(db "SELECT \`key\` FROM tokens WHERE user_id=$BUYER_ID AND name='$TOKEN_NAME' ORDER BY id LIMIT 1;")"
  res "已创建演示 API Key（name=$TOKEN_NAME）"
else
  res "复用已存在演示 API Key（name=$TOKEN_NAME）"
fi
[ -n "$KEY" ] || die "无法取得 API Key"
V1="$(curl -sS --max-time 60 -H "Host: $HOST_HEADER" -H "Authorization: Bearer sk-$KEY" \
      -H 'Content-Type: application/json' -X POST "$APP/v1/chat/completions" \
      -d "{\"model\":\"$PLAN_MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"reply with DEMO-OK only\"}],\"max_tokens\":16}")"
CONTENT="$(printf '%s' "$V1" | jget choices.0.message.content 2>/dev/null || true)"
[ -n "$CONTENT" ] || die "/v1 调用失败：$V1"
res "/v1 调用成功，模型回复：「$CONTENT」"
sleep 1   # 消费日志异步落库，稍候再读
read -r LOG_ID LOG_QUOTA LOG_SRC <<<"$(db "SELECT id, quota, JSON_UNQUOTE(JSON_EXTRACT(other,'\$.billing_source')) FROM logs WHERE user_id=$BUYER_ID AND type=2 ORDER BY id DESC LIMIT 1;")"
kv "logs.id"        "$LOG_ID"
kv "本次扣费 quota" "$LOG_QUOTA"
kv "billing_source" "$LOG_SRC"
[ "$LOG_SRC" = "subscription" ] || die "计费来源应为 subscription，实为「$LOG_SRC」"
res "计费来源 = subscription（走订阅桶扣减，未动钱包余额）"

# ════════════════════════════════════════════════════════════════════════════
# ⑥ 代理查收益 + 申请提现
# ════════════════════════════════════════════════════════════════════════════
step 6 "代理「$AGENT_USER」查看收益并申请提现 ¥$WITHDRAW_CNY"
AGENT_ID="$(login "$AGENT_USER" "$AGENT_PASS" "$JAR_AGENT")"
EARN="$(capi GET "/api/tenant/earnings" "$JAR_AGENT" "$AGENT_ID")"
assert_ok "$EARN" "查询收益"
W_BEFORE="$(printf '%s' "$EARN" | jget data.withdrawable_cny)"
F_BEFORE="$(printf '%s' "$EARN" | jget data.frozen_cny)"
T_TOTAL="$(printf '%s'  "$EARN" | jget data.total_earned_cny)"
res "收益台账"
kv "可提现 withdrawable" "¥$W_BEFORE"
kv "冻结中 frozen"       "¥$F_BEFORE"
kv "累计 total_earned"   "¥$T_TOTAL"
WD="$(capi POST "/api/tenant/withdrawals" "$JAR_AGENT" "$AGENT_ID" "{\"amount_cny\":$WITHDRAW_CNY}")"
assert_ok "$WD" "申请提现"
WID="$(printf '%s' "$WD" | jget data.id)"
res "提现申请已提交：id=$WID 金额 ¥$WITHDRAW_CNY status=$(printf '%s' "$WD" | jget data.status)"

# ════════════════════════════════════════════════════════════════════════════
# ⑦ 管理员审核通过（校验金额守恒）
# ════════════════════════════════════════════════════════════════════════════
step 7 "管理员「$ADMIN_USER」审核通过提现 id=$WID（校验金额守恒）"
ADMIN_ID="$(login "$ADMIN_USER" "$ADMIN_PASS" "$JAR_ADMIN")"
APPR="$(capi POST "/api/admin/withdrawals/$WID/approve" "$JAR_ADMIN" "$ADMIN_ID" '{"remark":"demo approve"}')"
assert_ok "$APPR" "审核通过"
read -r W_AFTER F_AFTER T_AFTER <<<"$(db "SELECT withdrawable_balance, frozen_withdraw_amount, total_earned FROM agent_wallets WHERE tenant_id=$TENANT_ID;")"
APPROVED_SUM="$(db "SELECT COALESCE(SUM(amount),0) FROM agent_withdrawals WHERE tenant_id=$TENANT_ID AND status='approved';")"
res "提现 id=$WID 已通过（线下打款）"
kv "可提现 withdrawable" "¥$W_BEFORE → ¥$W_AFTER"
kv "冻结中 frozen"       "¥$F_BEFORE → ¥$F_AFTER"
kv "累计 total_earned"   "¥$T_AFTER（不变）"
kv "Σ已审批提现"        "¥$APPROVED_SUM"
python3 -c "
w,f,t,a = $W_AFTER, $F_AFTER, $T_AFTER, $APPROVED_SUM
assert abs(t-(w+f+a)) < 1e-6, f'守恒失败：total_earned={t} != withdrawable+frozen+approved={w+f+a}'
" || die "金额守恒校验失败"
res "金额守恒成立：total_earned = withdrawable + frozen + Σapproved"

# ════════════════════════════════════════════════════════════════════════════
# ⑧ 充值 $1（mock）→ quota +500000
# ════════════════════════════════════════════════════════════════════════════
step 8 "终端用户充值 \$$RECHARGE_USD（mock）→ quota 入账"
Q0="$(db "SELECT quota FROM users WHERE id=$BUYER_ID;")"
RC="$(capi POST "/api/tenant/wallet/recharge" "$JAR_BUYER" "$BUYER_ID" "{\"amount_usd\":$RECHARGE_USD,\"provider\":\"wxpay\"}")"
assert_ok "$RC" "充值下单"
RORDER="$(printf '%s' "$RC" | jget data.order_no)"
RC_CNY="$(printf '%s' "$RC" | jget data.amount_cny)"
kv "order_no" "$RORDER"
kv "应付"     "¥$RC_CNY（= \$$RECHARGE_USD × 汇率）"
CODE="$(confirm_pay "$RORDER")"
[ "$CODE" = "200" ] || die "充值确认 HTTP $CODE（期望 200）"
Q1="$(db "SELECT quota FROM users WHERE id=$BUYER_ID;")"
RST="$(db "SELECT status FROM payment_orders WHERE order_no='$RORDER';")"
DELTA=$((Q1 - Q0))
EXPECT="$(python3 -c "print(int($RECHARGE_USD*500000))")"
res "充值确认成功，RCG 订单 status=$RST"
kv "quota" "$Q0 → $Q1（Δ=$DELTA）"
[ "$DELTA" = "$EXPECT" ] || die "quota 增量应为 $EXPECT，实为 $DELTA"
res "quota 增量 = $DELTA（= \$$RECHARGE_USD × 500000，符合 \$1=500k quota 口径）"

# ── 收尾 ─────────────────────────────────────────────────────────────────────
printf '\n\033[1;32m══════ 演示全部 8 步通过 ══════\033[0m\n'
kv "买家"     "$BUYER_USER (id=$BUYER_ID) @ 租户 $(printf '%s' "$CUR" | jget data.slug)"
kv "套餐订单" "$ORDER（¥$AMT_CNY）→ 原生订阅 #$NATIVE_SUB_ID active"
kv "代理分润" "tokenplan_spread ¥$SPREAD → 提现 ¥$WITHDRAW_CNY 已审批（守恒）"
kv "/v1 计费" "billing_source=subscription（log #$LOG_ID, 扣 $LOG_QUOTA quota）"
kv "充值"     "\$$RECHARGE_USD → quota +$DELTA"
printf '\033[1;34m提示：跑对账请执行 deploy/ops/reconcile.sh\033[0m\n'
