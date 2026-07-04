# 支付概览设计(2026-07-04)

> 现有「支付对账」页只显示对账任务的运行记录 + 卡单列表,是"一串串记录",不直观。增强为:**顶部支付概览(4 状态卡 + 可筛订单列表),现有对账运维折叠到下方**。经 4 轮互动确认。数据全查 `payment_orders`(充值+套餐订单同表,共用 4 态)。

## 一、状态分类(四态,含入账中间态)

| 数据库 `status` | 专业名 | 徽章色 |
| --- | --- | --- |
| `created` | 待支付 | 灰/黄 |
| `paid` | 已收款待入账 | 蓝 |
| `credited` | 支付成功 | 绿 |
| `failed` | 支付失败 | 红 |

## 二、页面(增强 `features/payment-reconcile`,从上到下)

1. **筛选栏**:时间区间(默认近 30 天,预设 今日/近7天/近30天/近90天)· 支付方式(全部/微信/支付宝)· 类型(全部/充值/套餐订阅)。
2. **4 状态卡**:按筛选口径统计(**不含状态筛**——4 卡恒显全部),每卡 = 中文名 + 笔数 + ¥金额。点某卡 → 列表按该状态筛(再点取消,选中高亮)。
3. **订单列表**:按筛选 + 选中状态,分页。列:订单号 · 类型(充值/套餐) · 支付方式(微信/支付宝) · 金额(¥) · 状态(彩色徽章) · 用户/租户 · 时间(东八区)。
4. **对账运维(折叠区,默认收起)**:把现有「立即对账」按钮 + 心跳、卡单列表、运行记录收进一个可展开区,不占眼。

## 三、后端

**端点** `GET /api/admin/reconcile/overview`(AdminAuth,跨租户),挂在既有 `/api/admin/reconcile` 组。入参:
- `start_timestamp`/`end_timestamp`(epoch 秒,复用 `parseTimeRange`)
- `provider`(可选:`wxpay`|`alipay`;空=全部)
- `type`(可选:`recharge`|`subscription`;空=全部)
- `status`(可选:`created`|`paid`|`credited`|`failed`;仅作用于列表;空=全部)
- `page`/`page_size`(列表分页)

**返回**(统一信封 data):
```
{
  "summary": [ {"status":"created","count":N,"amount_cny":X}, ...4 态恒全 ],
  "orders": {
    "items": [ {"order_no","type","provider","amount_cny","status","user_id","tenant_id","created_at_ts"} ],
    "total": T, "page": P, "page_size": S
  }
}
```

**查询**(`payment_orders`,金额取 `actual_paid`¥):
- summary:`WHERE created_at∈[start,end] AND (provider) AND (type) GROUP BY status`,4 态缺补 0。
- orders:同上 `+ (status)`,`ORDER BY created_at DESC` 分页;另 `COUNT(*)` 得 total。
- 直接用 `a.DB` 查 `payment_orders`(跨租户,不加 tenant 作用域;主站 admin 视角)。金额边界 round2。

## 四、落地

1. 后端:`internal/mtwire/payment_overview.go`(handler + DTO)+ `router/mt-router.go` 注册 `/overview`。
2. 前端:`features/payment-reconcile/index.tsx` 顶部加概览(筛选栏 + 4 卡 + 列表),现有运维内容包进折叠区;`api.ts` 加 `getPaymentOverview`。W5 全中文(徽章/表头/类型/方式/状态名)。
3. 门禁:`go build`/`go test`(mtwire/payment)、`tsgo -b`、`bun run build`。**本地自测,不擅自部署**(等用户验完一起推/部署)。
