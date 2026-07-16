// Package stats 现仅承载财务/统计模块的错误码命名空间（errors.go：STATS_RANGE_INVALID 等），
// 其中 ErrRangeInvalid 被 internal/mtwire/report.go 复用。
//
// 早期这里还有一套 StatsService / MemBillingReader / MemSubscriptionReader（service.go /
// reader.go / port.go）的统计服务实现，但生产统计走 mtwire 的报表口径，该套从未装配（0 装配点
// 引用），已于 2026-07-16 作为死码移除。若日后需重建统计服务，请从活装配点（SetMtRouter →
// mtwire）接线，勿再放置「有 doc.go + 绿测试却无人调用」的休眠实现。
package stats
