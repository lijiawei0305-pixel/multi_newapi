package payment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ensure strings used
var _ = strings.TrimSpace

// ReconcileResult 汇总一次对账兜底的结果（供 admin 端点 / 日志展示）。
type ReconcileResult struct {
	Scanned    int               // 扫到的卡单数
	Reconciled []string          // 成功补入账的 order_no
	Expired    []string          // 未付超时/过期 → 自动置 failed 的 order_no（仅 created 路径）
	Failed     map[string]string // order_no → 错误（本次仍失败、留待下次再扫）
}

// reconcileScanLimit 对账单轮从存储层拉取的最大订单数（PAY-REC-02 SQL LIMIT）。
const reconcileScanLimit = 200

// QueryFunc 主动查单函数：返回结构化 QueryResult（P0-1）。
// 不得再使用 bool + "query"/"reconcile" 伪交易号。
type QueryFunc func(ctx context.Context, orderNo, provider string) (*QueryResult, error)

// ReconcileStuckPaid 扫描卡在 paid 的订单，重跑幂等 sink 后再推进 credited（P0-2）。
//
// 卡单成因：入账进程在 OnPaid 完成与 CAS(paid→credited) 之间崩溃。
// 非 CAS winner 不得仅凭 status=paid 推进 credited——本方法是唯一的 stuck-paid 恢复路径：
// 必须先 OnPaid 成功，再 MarkCredited。
//
// before 通常取 now-5min：过滤在途订单。
func (g *Gateway) ReconcileStuckPaid(ctx context.Context, before time.Time) (ReconcileResult, error) {
	stuck, err := g.repo.ListByStatus(ctx, OrderPaid, before, reconcileScanLimit)
	if err != nil {
		return ReconcileResult{}, err
	}
	res := ReconcileResult{Scanned: len(stuck), Failed: map[string]string{}}
	for _, ord := range stuck {
		if err := g.recoverStuckPaid(ctx, ord); err != nil {
			res.Failed[ord.OrderNo] = err.Error()
			continue
		}
		res.Reconciled = append(res.Reconciled, ord.OrderNo)
	}
	return res, nil
}

// recoverStuckPaid 对单笔 paid 订单重跑 sink → credited。
// 使用订单已持久化的真实 ProviderTransactionID，禁止 "reconcile" 占位符。
func (g *Gateway) recoverStuckPaid(ctx context.Context, ord *PayOrder) error {
	if ord == nil {
		return ErrOrderNotFound
	}
	sink, ok := g.sinks[ord.Type]
	if !ok {
		return fmt.Errorf("no sink for type %s", ord.Type)
	}
	txnID := strings.TrimSpace(ord.ProviderTransactionID)
	if IsPlaceholderTxnID(txnID) {
		// 无真实交易号：不能编造；留 paid 告警
		return ErrPaymentFactInvalid
	}
	info := &CallbackInfo{
		Provider:   ord.Provider,
		OrderNo:    ord.OrderNo,
		Success:    true,
		TxnID:      txnID,
		PaidAmount: ord.ActualPaid,
	}
	paid := ord.toPaidOrder(info, g.now())
	if err := sink.OnPaid(ctx, paid); err != nil {
		return err
	}
	if ok, csErr := g.repo.MarkCredited(ctx, ord.OrderNo, g.now()); csErr != nil {
		return csErr
	} else if !ok {
		// 已被其它路径 credited：幂等成功
		if current, err := g.repo.GetByOrderNo(ctx, ord.OrderNo); err == nil && current.Status == OrderCredited {
			return nil
		}
		return fmt.Errorf("paid→credited CAS failed")
	}
	return nil
}

// ListStuckPaid 只读列出卡在 paid 的订单。
func (g *Gateway) ListStuckPaid(ctx context.Context, before time.Time) ([]*PayOrder, error) {
	return g.repo.ListByStatus(ctx, OrderPaid, before, reconcileScanLimit)
}

// ReconcileStuckCreated 扫卡在 created 的订单，经权威查单补账或过期。
//
// 已付 → CreditFromQueryResult（真实 txn + 金额分）；
// 查无此单且超时 → failed；
// 查单失败 → 不动。
func (g *Gateway) ReconcileStuckCreated(
	ctx context.Context,
	before time.Time,
	expireAge time.Duration,
	limit int,
	query QueryFunc,
) (ReconcileResult, error) {
	if query == nil {
		return ReconcileResult{}, nil
	}
	fetchLimit := limit
	if fetchLimit <= 0 {
		fetchLimit = reconcileScanLimit
	}
	created, err := g.repo.ListByStatus(ctx, OrderCreated, before, fetchLimit)
	if err != nil {
		return ReconcileResult{}, err
	}
	res := ReconcileResult{Failed: map[string]string{}}
	expireCutoff := g.now().Add(-expireAge)
	for _, ord := range created {
		res.Scanned++
		expired := expireAge > 0 && ord.CreatedAt.Before(expireCutoff)
		if err := g.applyQueryResult(ctx, ord, query, expired, &res); err != nil {
			// applyQueryResult 已写入 res.Failed
			_ = err
		}
	}
	return res, nil
}

// applyQueryResult 对单笔订单执行权威查单并按结果推进（与 due-query 共用 ClaimForQuery）。
func (g *Gateway) applyQueryResult(
	ctx context.Context,
	ord *PayOrder,
	query QueryFunc,
	expired bool,
	res *ReconcileResult,
) error {
	now := g.now()
	// 全量对账也走同一 claim 入口，禁止绕过 fencing
	token, claimed, cErr := g.repo.ClaimForQuery(ctx, ord.OrderNo, now, now.Add(queryClaimLease))
	if cErr != nil {
		res.Failed[ord.OrderNo] = "claim: " + cErr.Error()
		return cErr
	}
	if !claimed {
		return nil
	}
	defer func() {
		if rec := recover(); rec != nil {
			g.logf("payment: stuck-created panic order=%s: %v", ord.OrderNo, rec)
			res.Failed[ord.OrderNo] = fmt.Sprintf("panic: %v", rec)
			_ = g.repo.ReleaseQueryClaim(ctx, ord.OrderNo, token)
		}
	}()

	// 复用 processDueOrder 语义：构造合成 due 路径
	ir := g.processDueOrderWithToken(ctx, ord, query, now, token)
	if ir.reconciled {
		res.Reconciled = append(res.Reconciled, ir.orderNo)
	}
	if ir.expired {
		res.Expired = append(res.Expired, ir.orderNo)
	}
	if ir.failMsg != "" {
		res.Failed[ir.orderNo] = ir.failMsg
	}
	_ = expired // 本地 2h 过期不再单独 failed；由 close/recovery 处理
	return nil
}

// markCreatedFailed 把仍 created 的单原子置 failed。
func (g *Gateway) markCreatedFailed(ctx context.Context, orderNo string) bool {
	ok, err := g.repo.CompareAndSetStatus(ctx, orderNo, OrderCreated, OrderFailed)
	if err != nil || !ok {
		g.logf("payment: reconcile expire %s: created→failed failed (ok=%v err=%v)", orderNo, ok, err)
		return false
	}
	return true
}

// queryWorkerCount 主动查单并发上限（有界 pool）。
const queryWorkerCount = 4

// queryPerOrderTimeout 每笔 QueryOrder 独立短 deadline。
const queryPerOrderTimeout = 4 * time.Second

// queryClaimLease 查单租约时长：认领后把 next_query_at 推到 now+lease，防多实例重复查单。
// 崩溃后租约到期即可再认领。
const queryClaimLease = 30 * time.Second

// ReconcileDueQueries 处理 next_query_at 到期的 created 订单。
// 有界 worker pool；每笔独立 timeout；入账使用真实 QueryResult。
func (g *Gateway) ReconcileDueQueries(
	ctx context.Context,
	limit int,
	query QueryFunc,
) (ReconcileResult, error) {
	if query == nil {
		return ReconcileResult{}, nil
	}
	if limit <= 0 {
		limit = reconcileScanLimit
	}
	now := g.now()
	due, err := g.repo.ListDueForQuery(ctx, now, limit)
	if err != nil {
		return ReconcileResult{}, err
	}
	res := ReconcileResult{Scanned: len(due), Failed: map[string]string{}}
	if len(due) == 0 {
		return res, nil
	}

	type itemResult struct {
		orderNo    string
		reconciled bool
		expired    bool
		failMsg    string
	}
	jobs := make(chan *PayOrder)
	out := make(chan itemResult, len(due))
	workers := queryWorkerCount
	if workers > len(due) {
		workers = len(due)
	}
	var wg sync.WaitGroup
	workerFn := func() {
		defer func() {
			if rec := recover(); rec != nil {
				g.logf("payment: query worker panic: %v", rec)
			}
			wg.Done()
		}()
		for ord := range jobs {
			if ctx.Err() != nil {
				out <- itemResult{orderNo: ord.OrderNo, failMsg: "ctx: " + ctx.Err().Error()}
				continue
			}
			ir := g.processDueOrder(ctx, ord, query, now)
			out <- ir
		}
	}
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go workerFn()
	}
	go func() {
		for _, ord := range due {
			select {
			case jobs <- ord:
			case <-ctx.Done():
			}
		}
		close(jobs)
		wg.Wait()
		close(out)
	}()

	for ir := range out {
		if ir.reconciled {
			res.Reconciled = append(res.Reconciled, ir.orderNo)
		}
		if ir.expired {
			res.Expired = append(res.Expired, ir.orderNo)
		}
		if ir.failMsg != "" {
			res.Failed[ir.orderNo] = ir.failMsg
		}
	}
	return res, nil
}

func (g *Gateway) processDueOrder(ctx context.Context, ord *PayOrder, query QueryFunc, now time.Time) struct {
	orderNo    string
	reconciled bool
	expired    bool
	failMsg    string
} {
	type itemResult = struct {
		orderNo    string
		reconciled bool
		expired    bool
		failMsg    string
	}
	ir := itemResult{orderNo: ord.OrderNo}
	// Phase E：fenced claim；token 持有者才能 Finish
	token, claimed, cErr := g.repo.ClaimForQuery(ctx, ord.OrderNo, now, now.Add(queryClaimLease))
	if cErr != nil {
		ir.failMsg = "claim: " + cErr.Error()
		return ir
	}
	if !claimed {
		return ir
	}
	return g.processDueOrderWithToken(ctx, ord, query, now, token)
}

func (g *Gateway) processDueOrderWithToken(ctx context.Context, ord *PayOrder, query QueryFunc, now time.Time, token string) (ir struct {
	orderNo    string
	reconciled bool
	expired    bool
	failMsg    string
}) {
	ir.orderNo = ord.OrderNo
	defer func() {
		if rec := recover(); rec != nil {
			g.logf("payment: due-order panic order=%s: %v", ord.OrderNo, rec)
			ir.failMsg = fmt.Sprintf("panic: %v", rec)
			_ = g.repo.ReleaseQueryClaim(ctx, ord.OrderNo, token)
		}
	}()

	qCtx, cancel := context.WithTimeout(ctx, queryPerOrderTimeout)
	t0 := g.now()
	g.logStage(StageQueryStart, ord.OrderNo, ord.Provider, 0, "")
	qr, qErr := query(qCtx, ord.OrderNo, string(ord.Provider))
	g.logStage(StageQueryEnd, ord.OrderNo, ord.Provider, elapsedMs(t0), "")
	cancel()

	attempts := ord.QueryAttempts + 1
	next := NextQueryAtForAttempt(ord.CreatedAt, attempts, g.now())
	expiredLocal := !ord.ExpiresAt.IsZero() && !ord.ExpiresAt.After(now)
	// 完成一轮后至少推迟 5s，避免双扫描器立即再 claim。
	// expires_at 已过去时禁止把 next 压到过去导致每 5s 热查（无 auto-close 时尤甚）。
	if expiredLocal {
		next = g.now().Add(10 * time.Minute)
	} else {
		if minNext := g.now().Add(5 * time.Second); next.Before(minNext) {
			next = minNext
		}
		if !ord.ExpiresAt.IsZero() && next.After(ord.ExpiresAt) {
			next = ord.ExpiresAt
		}
	}

	// protocol error：qr==nil && err==nil
	if qErr == nil && qr == nil {
		_, _ = g.repo.FinishQueryFenced(ctx, ord.OrderNo, token, next, attempts, "", "protocol_error")
		ir.failMsg = "query: empty result (protocol error)"
		return ir
	}

	if qErr != nil {
		if errors.Is(qErr, ErrOrderNotExist) || (qr != nil && qr.NotExist) {
			// ORDER_NOT_EXIST：重置为 local_created，允许同 out_trade_no 再 Prepay（仅此路径）。
			_, _ = g.repo.FinishQueryFenced(ctx, ord.OrderNo, token, next, attempts, CreateStateLocalCreated, string(TradeStateOrderNotExist))
			ir.failMsg = "query: order not exist; re-prepay allowed"
			return ir
		}
		// 非 NOT_EXIST 的查询错误：保持 prepay_unknown 语义（不得盲重放 Prepay）
		csKeep := CreateStatePrepayUnknown
		if ord.CreateState == CreateStateCredentialReady {
			csKeep = CreateStateCredentialReady
		}
		_, _ = g.repo.FinishQueryFenced(ctx, ord.OrderNo, token, next, attempts, csKeep, "")
		ir.failMsg = "query: " + qErr.Error()
		return ir
	}

	if !qr.BindingsOK(ord.OrderNo, ord.Provider) && !qr.NotExist {
		_, _ = g.repo.FinishQueryFenced(ctx, ord.OrderNo, token, next, attempts, "", "binding_mismatch")
		ir.failMsg = "query: binding mismatch"
		return ir
	}

	if qr.NotExist {
		// 仅 ORDER_NOT_EXIST 才重置为 local_created 允许再 Prepay
		_, _ = g.repo.FinishQueryFenced(ctx, ord.OrderNo, token, next, attempts, CreateStateLocalCreated, string(TradeStateOrderNotExist))
		return ir
	}

	if qr.Paid {
		if !qr.QueryPaidOK() || qr.OrderNo != ord.OrderNo {
			_, _ = g.repo.FinishQueryFenced(ctx, ord.OrderNo, token, next, attempts, "", "fact_invalid")
			ir.failMsg = "credit: incomplete payment facts"
			return ir
		}
		// 入账前释放 claim（credit 会改 status）；Finish 在 created 下清 token
		_, _ = g.repo.FinishQueryFenced(ctx, ord.OrderNo, token, time.Time{}, attempts, CreateStateCredentialReady, string(TradeStateSuccess))
		if cErr := g.CreditFromQueryResult(ctx, ord.OrderNo, qr); cErr != nil {
			ir.failMsg = "credit: " + cErr.Error()
		} else {
			ir.reconciled = true
		}
		return ir
	}

	if qr.Closed || qr.NormalizedState == TradeStateClosed {
		_, _ = g.repo.FinishQueryFenced(ctx, ord.OrderNo, token, next, attempts, CreateStateProviderClosed, string(TradeStateClosed))
		return ir
	}

	// USERPAYING：只退避查单，禁止 Close
	if qr.NormalizedState == TradeStateUserPaying {
		_, _ = g.repo.FinishQueryFenced(ctx, ord.OrderNo, token, next, attempts, "", string(TradeStateUserPaying))
		return ir
	}

	// REFUND / REVOKED / PAYERROR / UNKNOWN：隔离告警，禁止自动替换/关单
	switch qr.NormalizedState {
	case TradeStateRefund, TradeStateRevoked, TradeStatePayError, TradeStateUnknown:
		_, _ = g.repo.FinishQueryFenced(ctx, ord.OrderNo, token, next, attempts, "", string(qr.NormalizedState))
		ir.failMsg = "query: non-actionable trade_state=" + string(qr.NormalizedState)
		g.logf("payment: order=%s trade_state=%s isolated (no close/replace)", ord.OrderNo, qr.NormalizedState)
		return ir
	}

	// NOTPAY：凭据有效 → 继续等待；凭据丢失/过期 → close_pending 标记（auto-close 本阶段不可达）
	cs := CreateStateCredentialReady
	if strings.TrimSpace(ord.PayURL) == "" || expiredLocal {
		cs = CreateStateClosePending
	}
	_, _ = g.repo.FinishQueryFenced(ctx, ord.OrderNo, token, next, attempts, cs, qr.TradeState)
	return ir
}
