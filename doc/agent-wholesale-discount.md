# 主站给代理"折扣系数"(全线批发折扣)设计(2026-07-04)

> 主站(管理员)给**每个代理**一个**折扣系数**(如 0.8),代理名下所有消费的**成本**都 = 主站价 × 系数。经用户明确纠正:**不是 per-group**——是每代理一个系数,**相对**乘在主站每个分组的基准倍率 / 每个套餐的主站价上。**不做任何成本保护**(用户自行把关,明确要求)。向后兼容:未设系数 = 现状不变。

## 一、语义 + 例子

一个系数 `r`(如 0.8)对该代理全线生效:

**消耗(按分组,相对缩放)**:代理成本 = 主站该分组基准倍率 × r
- gpt-pro 主站 1.0 → 代理成本 0.8
- gemini 主站 0.8 → 代理成本 0.64

**套餐**:代理进货价 = 该套餐主站官方售价(`Plan.BasePrice`)× r
- 主站套餐 ¥60 → 代理进货 ¥48(代理卖 ¥60 则赚 ¥12,tokenplan_spread=零售−进货)

**关键点**:现状消耗侧 `bottom_price_ratio` 是**绝对倍率**(对所有分组同一个绝对值,gemini 便宜分组会算出比主站还贵的怪成本);本设计改成**相对系数**,天然按分组缩放,正确。套餐侧现状是每套餐一个固定 `agent_cost_price`(对所有代理一样);本设计让代理按系数得 per-agent 进货价。

## 二、数据

`agent_profiles` 加一列 `discount_ratio decimal(20,8) NOT NULL DEFAULT 0`(AutoMigrate 自动加)。
- 领域字段 `AgentParams.DiscountRatio`(`internal/agent/model.go`)。
- 语义:`> 0` = 折扣系数(全线生效);`<= 0`(默认/未设)= 无折扣,回退现状行为。
- **无 guard/保护线**(不接 `pricing.Guard`,不设上下限;仅拒绝 < 0 的非法值以免算术出错)。
- gormrepo 读写映射(`internal/agent/gormrepo/gormrepo.go` profileRow + toParams/fromParams 两向,镜像 `bottom_price_ratio`)。

## 三、消耗侧(choke point 单点改)

`internal/mtwire/distribution.go` `consumeFloorRatio` 改签名 + 取值顺序:
```
consumeFloorRatio(discountRatio, bottomPriceRatio float64, group string) float64:
  if discountRatio > 0 { return modelGroupBaseline(group) * discountRatio }  // 新:相对系数
  if bottomPriceRatio > 0 { return bottomPriceRatio }                        // 旧:绝对(向后兼容)
  return modelGroupBaseline(group)                                          // 未配置:主站基准
```
三个调用处都传上 `params.DiscountRatio`(全有 `AgentParams`):
- 差价入账减数 `internal/mtwire/consume_markup.go:70`(`creditRatioMarkup`)
- 代理设卖价下限校验 `internal/mtwire/distribution.go:447`(`HandleAgentSetGroupRatio`)
- 卖价下限展示 `internal/mtwire/distribution.go:399`(`HandleAgentListGroups`)

> 联动(预期行为,非 bug):代理某分组的成本同时是他在该分组卖给下级的**最低价**(不能亏本卖)。系数一改,两者一起变。

## 四、套餐侧

1. `internal/tokenplan/model.go` `PurchaseInput`(:260)加 `DiscountRatio float64`。
2. `internal/tokenplan/subscription.go` `Purchase`(:45)算进货价并快照:
   ```
   agentCost := plan.AgentCostPrice
   if in.DiscountRatio > 0 { agentCost = plan.BasePrice * in.DiscountRatio }
   // SavePendingPurchase 的 AgentCostPrice: agentCost（原 :90 是 plan.AgentCostPrice）
   ```
   `spread = tokenplanSpread(RetailPrice, agentCost)` 于 `ActivateFromPayment`(:134)自然生效,不动。
3. 调用处 `internal/mtwire/http.go:235` 解析该租户代理的系数并传入:
   `DiscountRatio: <该租户 AgentParams.DiscountRatio>`(经 `a.AgentService` 现有按租户取 AgentType/params 的方法;主站平台租户无代理→系数 0→回退 plan.AgentCostPrice,现状不变)。

## 五、后端 DTO(镜像 PackageDiscount 的装配,但不接 guard)

`internal/mtwire/agent.go`:`agentCreateIn`/`agentPatchIn`(:611/:622)加 `discount_ratio`(patch 用 `*float64`),`SetAgentType`/`PatchAgent` 写入 `params.DiscountRatio`(:646/:777 附近),代理详情出参(:735/:1087)带 `discount_ratio`。**不加** `ValidateGroupRatio`/`DiscountFloor` 校验。

## 六、前端(一个输入框)

`web/default/src/features/agents/`:
- `types.ts` `Agent` 加 `discount_ratio: number`;`lib/agent-form.ts` 表单 schema 加该字段(默认 0/空)。
- `components/agent-mutate-drawer.tsx`:加一个数字输入「折扣系数」,helper 文案「= 主站价 × 系数(如 0.8 即八折);留空/0 = 不打折」。PATCH 提交带上 `discount_ratio`。
- W5 全中文。

## 七、兼容 + 测试

- **零回归**:`discount_ratio<=0` 时消耗/套餐都走原路径。现有 `bottom_price_ratio`(若某代理设过)仍作为消耗侧次选回退。
- 测试:`consumeFloorRatio` 新分支(系数×基准、回退链)、`Purchase` 按系数算进货价、DTO round-trip。跑通现有 `internal/mtwire`/`internal/tokenplan`/`internal/agent` 单测。

## 八、落地/门禁/纪律

1. 后端:agent model/gormrepo(字段)→ consumeFloorRatio + 3 callers → tokenplan Purchase + PurchaseInput → mtwire agent DTO + http.go 解析。
2. 前端:agents types/form/drawer 一个输入框。
3. 门禁:`go build ./...`(排除预存 web/classic embed 报错)+ `go test ./internal/mtwire/... ./internal/tokenplan/... ./internal/agent/...`;`cd web/default && bun run typecheck` 干净、`bun run build` 成功;无 `consumptionCaveat`。
4. **禁改锁定文件**:`system-settings/*`、`i18n/locales/*`、`payment_inprocess.go`、`realpay/*`、`option.go`、`payment_wxpay_alipay.go`。提交只 `git add` 显式路径(严禁 `-A`/`.`/`-a`)。
