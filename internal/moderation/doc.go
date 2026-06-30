// Package moderation — 内容审核（违禁词屏蔽）。详见 doc/detailed-design.md §2.14。
//
// 在 /v1 转发前扫描用户输入消息，命中违禁词则按词动作 remind（放行+提醒，默认）
// 或 block（拦截），并记录违规事件供管理员审阅。
//
// 多租户：全站基础库（tenant_id=0）∪ 各租户自有词。内置 Aho-Corasick 匹配，
// 不 import 原生 service 包（§1.4：模块只声明自己的依赖接口，不依赖兄弟/原生包）。
package moderation
