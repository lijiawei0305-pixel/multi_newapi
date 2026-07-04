# 提现闭环补强设计(2026-07-04)

> 审计结论:提现核心流程(申请/审核/钱安全、原子 CAS)已通,但缺"真打款"闭环。本次补 **#1 收款账户 / #2 已打款状态+凭证 / #3 驳回理由**;#4 起提门槛+手续费、#5 通知暂不做。

## #1 收款账户
- `agent_profiles` 加字段:`payout_method`(`alipay` | `bank`)、`payout_account`(账号)、`payout_name`(实名)、`payout_bank`(开户行,仅 bank 时)。
- 代理在"提现/收款"设置里填写(可改)。**申请提现前若未设收款账户 → 拦下,提示先设**。
- 提现申请时把当前收款账户**快照**进 `agent_withdrawals`(加同名字段)—— 记录不可变、管理员审核时看得到打款目标。

## #2 已打款状态 + 凭证
- 状态机:`pending →(通过)approved →(标记已打款)paid`,或 `pending →(驳回)rejected`。**`approved` 不再是终态**。
- `agent_withdrawals` 加:`payout_ref`(打款单号/凭证)、`paid_at`(打款时间)。
- **钱流调整(重要)**:
  - 申请:`withdrawable → frozen`(不变)。
  - **通过(approve):不动钱**(仅记决策,钱仍在 frozen)—— 改现有"approve 即扣 frozen"的行为。
  - **标记已打款(mark-paid):`frozen −= amt`(钱真正出账)** + 记 `payout_ref`/`paid_at`。
  - 驳回(仅 pending):`frozen → withdrawable`(退回,不变)。
- 新端点 `POST /api/admin/withdrawals/:id/mark-paid`(入参 `payout_ref`),仅 `approved → paid`,CAS(`WHERE status='approved'`)。
- 管理员 UI:`approved` 的单显示「标记已打款」(填打款单号);代理历史显示 `paid` + 单号 + 打款时间。

## #3 驳回理由
- 前端 `reject` 发的字段与后端绑定的字段**对齐**(现前端发 `reason`、后端收 `remark`,不匹配 → 理由丢失),驳回理由存 `agent_withdrawals.remark`。
- 代理提现历史表加「备注/驳回原因」列(展示 remark)。

## 落地改动
1. **后端**:`internal/agent/` model+repo(收款账户字段、`paid` 状态、`payout_ref`/`paid_at`、快照)+ `mark-paid` 端点 + 钱流调整(approve 不扣、mark-paid 扣)+ reason/remark 修正 + 申请前校验收款账户。
2. **前端**:代理收款账户设置表单 + 提现申请(带账户/校验)+ 管理员审核(显示收款账户 + 「标记已打款」)+ 历史表(`paid`/单号/驳回原因)。前端文案中文(W5)。
