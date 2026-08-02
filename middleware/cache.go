package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func Cache() func(c *gin.Context) {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		switch {
		case path == "/" || path == "/index.html":
			// HTML 入口始终协商缓存，避免发版后用户卡在旧壳
			c.Header("Cache-Control", "no-cache")
		case strings.HasPrefix(path, "/lp-assets/"):
			// 落地页静态素材（粒子 bin / logo）：内容随发版整体替换；长缓存 + 可压缩传输
			//（gin gzip 中间件已对响应做压缩）。零观感：不改资源字节。
			c.Header("Cache-Control", "public, max-age=2592000") // 30 days
		default:
			c.Header("Cache-Control", "max-age=604800") // one week
		}
		c.Header("Cache-Version", "b688f2fb5be447c25e5aa3bd063087a83db32a288bf6a4f35f2d8db310e40b14")
		c.Next()
	}
}
