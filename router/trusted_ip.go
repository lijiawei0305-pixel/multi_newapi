package router

import "github.com/gin-gonic/gin"

// ConfigureTrustedClientIP 决定 gin 如何解析 c.ClientIP()（供限流/风控/日志按真实客户端计）。
//
// 背景（audit 2026-07-17 · High）：全仓从未 SetTrustedProxies → gin 默认信任 0.0.0.0/0
// （defaultTrustedCIDRs）+ ForwardedByClientIP → validateHeader 从右往左遍历 XFF，每跳皆
// 「可信」时返回**最左**、即客户端自填值。于是 ① 按 IP 的限流可被每请求换一个伪造 XFF 绕过；
// ② 可反向武器化：拿受害者 IP 灌满其桶把其踢下 /api。
//
// 本站源站前是 Cloudflare（CF Origin CA 证书在用），真实客户端 IP 由 CF 写在权威头
// CF-Connecting-IP（客户端无法经 CF 伪造它）。设 TrustedPlatform 后 gin 直接取该头：
//   - 经 CF 的流量：取真实客户端，伪造 XFF 被彻底无视；
//   - 无该头的流量（如本机 127.0.0.1 直连、未经 CF 的路径）：gin 回退到既有解析逻辑，
//     不比现状更糟（见 gin context.go ClientIP：TrustedPlatform 头为空即继续向下）。
//
// 残余前提（需服务器/边缘侧保障，非本函数职责）：源站应锁到仅 CF 可达（防绕过 CF 直连
// 源站伪造 CF-Connecting-IP）——对应部署清单 8a「CF Full-strict + 源站 CF 白名单」。
func ConfigureTrustedClientIP(engine *gin.Engine) {
	engine.TrustedPlatform = gin.PlatformCloudflare // "CF-Connecting-IP"
}
