// Command authservice 启动独立支付网关服务（微信/支付宝下单 + 回调主站内网入账）。
//
// 用法：authservice -config /path/config.yaml （或 AUTH_CONFIG 环境变量）。
// 部署：经 nginx `^~ /auth/` 反代；仅暴露 /auth/*，端口仅内部网络可达。
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	authservice "github.com/QuantumNous/new-api/auth-service"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", os.Getenv("AUTH_CONFIG"), "path to config.yaml")
	flag.Parse()
	if configPath == "" {
		configPath = "config.yaml"
	}

	cfg, err := authservice.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("auth-service: load config: %v", err)
	}

	srv := authservice.NewServer(cfg)
	httpSrv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("auth-service listening on %s (mock=%v) → callback %s", cfg.Server.Addr, cfg.Mock, cfg.Internal.CallbackURL)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("auth-service: serve: %v", err)
	}
}
