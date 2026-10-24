// dnsctl 是 DNS 统一管理平台的运维入口。
//
//	dev      本地开发：仅 /api（mock/代理）+ CORS，前端用 Vite
//	serve    生产：/api + 前端构建产物（web/dist），首次启动自动初始化
//	cron-run 定时任务：重试队列 + 清理孤儿 + 每日备份
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"

	"github.com/loeia/all-in-dns/internal/auth"
	"github.com/loeia/all-in-dns/internal/config"
	"github.com/loeia/all-in-dns/internal/database"
	"github.com/loeia/all-in-dns/internal/handlers"
	"github.com/loeia/all-in-dns/internal/logger"
	"github.com/loeia/all-in-dns/internal/redis"
	"github.com/loeia/all-in-dns/internal/server"
	"github.com/loeia/all-in-dns/internal/sync"
)

func main() {
	// 加载 .env 到环境变量（不存在则忽略，dev 模式可无 .env）
	_ = godotenv.Load()

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "dev":
		os.Exit(runDev(os.Args[2:]))
	case "serve":
		os.Exit(runServe(os.Args[2:]))
	case "cron-run":
		os.Exit(runCron(os.Args[2:]))
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `用法:
  dnsctl dev       [--addr :8080] [--proxy http://real-backend:8080]
  dnsctl serve     [--addr :8080]
  dnsctl cron-run

dev 模式：
  仅提供 /api（默认 mock 数据，带 CORS）。前端用 ` + "`cd web && npm run dev`" + ` 启动 Vite，
  由 Vite 把 /api 代理到本进程。指定 --proxy 时把 /api 转发到真实后端。
serve / cron-run 模式：
  从 .env 读取配置（DNS_DB_* / DNS_REDIS_* / DNS_SECRET_KEY / DNS_CRON_KEY / DNS_ADMIN_*）。
`)
}

// osContext 返回后台 context（CLI 场景无请求上下文）。
func osContext() context.Context { return context.Background() }

func runDev(args []string) int {
	cfg := config.FromEnv()
	cfg.Mode = "dev"

	fsg := flag.NewFlagSet("dev", flag.ExitOnError)
	fsg.StringVar(&cfg.Addr, "addr", cfg.Addr, "监听地址")
	fsg.StringVar(&cfg.APIProxy, "proxy", "", "真实后端地址（如 http://127.0.0.1:8080），留空使用 mock")
	_ = fsg.Parse(args)

	return serve(cfg, nil, nil)
}

func runServe(args []string) int {
	addr := ":8080"
	fsg := flag.NewFlagSet("serve", flag.ExitOnError)
	fsg.StringVar(&addr, "addr", ":8080", "监听地址")
	_ = fsg.Parse(args)

	cfg := config.FromEnv()
	cfg.Mode = "serve"
	cfg.Addr = addr

	if len(cfg.App.SecretKey) < 32 {
		fmt.Fprintln(os.Stderr, "系统密钥未配置或过短（DNS_SECRET_KEY，至少 32 字符）")
		return 2
	}
	setTimezone(cfg.App.Timezone)

	// 首次启动自动初始化（幂等：库存在/管理员存在则跳过）
	if err := ensureInit(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// 数据库
	db, err := database.Open(cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, "数据库连接失败:", err)
		return 1
	}
	defer db.Close()

	// Redis
	rdb, err := redis.Open(cfg.Redis)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Redis 连接失败:", err)
		return 1
	}
	defer rdb.Close()

	// 依赖装配
	lg := logger.New(db)
	syncSvc := sync.New(db, lg, cfg.App.SecretKey, cfg.App.MaxRetry, cfg.App.RetryBaseDelay)
	authSvc := auth.New(db, rdb, cfg.App.SecretKey, cfg.SessionTTLDuration())
	h := handlers.New(db, rdb, authSvc, syncSvc, lg, cfg)

	return serve(cfg, os.DirFS("web/dist"), h.Routes())
}

func serve(cfg *config.Config, fsys fs.FS, apiHandler http.Handler) int {
	srv := server.New(cfg, fsys, apiHandler)

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	slog.Info("dnsctl 启动", "config", cfg.String(), "addr", cfg.Addr)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "服务错误:", err)
		return 1
	}
	return 0
}

func setTimezone(tz string) {
	if tz == "" {
		return
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		time.Local = loc
	}
}
