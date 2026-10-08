package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"main/internal/api"
	"main/internal/api/handler"
	"main/internal/cache"
	"main/internal/captcha"
	"main/internal/config"
	"main/internal/database"
	"main/internal/httpx"
	"main/internal/logger"
	"main/internal/logstore"
	"main/internal/monitor"
	"main/internal/service"

	// 证书签发提供商子包（init 中注册工厂；providers.go 已定义表单的，子包仅补全实现）
	_ "main/internal/cert/acme"
	_ "main/internal/cert/aliyun"
	_ "main/internal/cert/huoshan"
	_ "main/internal/cert/tencent"
	_ "main/internal/dns/providers/aliyun"
	_ "main/internal/dns/providers/aliyunesa"
	_ "main/internal/dns/providers/aws"
	_ "main/internal/dns/providers/baidu"
	_ "main/internal/dns/providers/bt"
	_ "main/internal/dns/providers/cloudflare"
	_ "main/internal/dns/providers/dnsla"
	_ "main/internal/dns/providers/dnsmgr"
	_ "main/internal/dns/providers/dnspod"
	_ "main/internal/dns/providers/dynv6"
	_ "main/internal/dns/providers/goedge"
	_ "main/internal/dns/providers/henet"
	_ "main/internal/dns/providers/huawei"
	_ "main/internal/dns/providers/huoshan"
	_ "main/internal/dns/providers/jdcloud"
	_ "main/internal/dns/providers/namesilo"
	_ "main/internal/dns/providers/powerdns"
	_ "main/internal/dns/providers/qingcloud"
	_ "main/internal/dns/providers/spaceship"
	_ "main/internal/dns/providers/technitium"
	_ "main/internal/dns/providers/tencenteo"
	_ "main/internal/dns/providers/west"

	// 部署器子包
	_ "main/internal/cert/deploy/others"
	_ "main/internal/cert/deploy/panels"
	_ "main/internal/cert/deploy/providers"
	_ "main/internal/cert/deploy/servers"

	"github.com/gin-gonic/gin"
)

//go:embed all:web
var staticFS embed.FS

func main() {
	configPath := flag.String("config", "config.json", "config file path")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("加载配置失败: %v", err)
		os.Exit(1)
	}

	if err := database.Init(&cfg.Database); err != nil {
		logger.Error("初始化数据库失败: %v", err)
		os.Exit(1)
	}
	defer database.Close()

	// 初始化缓存（Redis 可选，未配置则用内存）
	cache.Init(&cache.Config{
		Enable:       cfg.Redis.Enable,
		Addr:         cfg.Redis.Addr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		MinIdleConns: cfg.Redis.MinIdleConns,
		KeyPrefix:    cfg.Redis.KeyPrefix,
	})
	defer cache.Close()
	httpx.Install()

	if err := captcha.Init(); err != nil {
		logger.Warn("行为验证码资源初始化失败: %v", err)
	}

	logstore.Init()
	database.RegisterDBCallbacks()

	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	mon := monitor.New()
	mon.Start()
	defer mon.Stop()
	service.SetCertRenewProcessStarter(handler.TriggerCertOrderProcessing)
	/* 证书签发结果通知：handler 在签发成功/失败分支触发，实现由 service 提供（避免 handler 反向依赖通知细节） */
	service.SetCertIssueNotifyHooks(service.SendCertIssueSuccessNotification, service.SendCertIssueFailNotification)
	taskCtx, taskCancel := context.WithCancel(context.Background())
	taskRunner := service.NewTaskRunner()
	scheduleRunner := service.NewScheduleRunner()
	go taskRunner.Start(taskCtx)
	go scheduleRunner.Start(taskCtx)
	defer func() {
		taskCancel()
		taskRunner.Stop()
		scheduleRunner.Stop()
	}()
	database.StartMaintenance(database.LoadMaintenanceConfig())
	defer database.StopMaintenance()
	service.StartOptimizeRunner(taskCtx)
	defer service.StopOptimizeRunner()

	router := api.SetupRouter(staticFS)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	logger.Info("服务器启动于 %s", addr)

	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 30 * time.Second, 
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP服务启动失败: %v", err)
		}
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("正在关闭HTTP服务...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP服务关闭失败: %v", err)
	}
	logger.Info("HTTP服务已关闭")
}
