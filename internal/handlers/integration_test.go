package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/mysql"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"golang.org/x/crypto/bcrypt"

	"github.com/loeia/all-in-dns/internal/auth"
	"github.com/loeia/all-in-dns/internal/config"
	"github.com/loeia/all-in-dns/internal/database"
	"github.com/loeia/all-in-dns/internal/logger"
	redisclient "github.com/loeia/all-in-dns/internal/redis"
	"github.com/loeia/all-in-dns/internal/sync"
)

// 集成测试需要 Docker；无 Docker 时自动跳过。

type testEnv struct {
	cfg *config.Config
	h   *Handler
	db  *database.DB
}

func setupEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()

	mysqlC, err := mysql.Run(ctx, "mysql:8.0",
		mysql.WithDatabase("dns_manager"),
		mysql.WithUsername("test"),
		mysql.WithPassword("testpass"))
	if err != nil {
		t.Skipf("启动 MySQL 容器失败（可能无 Docker）: %v", err)
	}
	t.Cleanup(func() { _ = mysqlC.Terminate(ctx) })
	mysqlHost, _ := mysqlC.Host(ctx)
	mysqlPort, _ := mysqlC.MappedPort(ctx, "3306/tcp")
	mysqlPortInt, _ := strconv.Atoi(mysqlPort.Port())

	redisC, err := tcredis.Run(ctx, "redis:7")
	if err != nil {
		t.Skipf("启动 Redis 容器失败: %v", err)
	}
	t.Cleanup(func() { _ = redisC.Terminate(ctx) })
	redisHost, _ := redisC.Host(ctx)
	redisPort, _ := redisC.MappedPort(ctx, "6379/tcp")
	redisPortInt, _ := strconv.Atoi(redisPort.Port())

	cfg := config.Default()
	cfg.DB = config.DB{Host: mysqlHost, Port: mysqlPortInt, Name: "dns_manager", User: "test", Pass: "testpass", Charset: "utf8mb4"}
	cfg.Redis = config.Redis{Host: redisHost, Port: redisPortInt, Prefix: "dns:"}
	cfg.App.SecretKey = "0123456789abcdef0123456789abcdef"
	cfg.App.CronKey = "test-cron-key-1234567890"

	db, err := database.Open(cfg.DB)
	if err != nil {
		t.Fatalf("连接 MySQL 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := applySchema(db, cfg.DB.Name); err != nil {
		t.Fatalf("导 schema 失败: %v", err)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("admin23"), bcrypt.DefaultCost)
	if _, err := db.Exec(`INSERT INTO users (username,password_hash,role) VALUES (?,?,?)`, "admin", string(hash), "admin"); err != nil {
		t.Fatalf("建管理员失败: %v", err)
	}

	rdb, err := redisclient.Open(cfg.Redis)
	if err != nil {
		t.Fatalf("连接 Redis 失败: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })

	lg := logger.New(db)
	syncSvc := sync.New(db, lg, cfg.App.SecretKey, cfg.App.MaxRetry, cfg.App.RetryBaseDelay)
	authSvc := auth.New(db, rdb, cfg.App.SecretKey, cfg.SessionTTLDuration())
	h := New(db, rdb, authSvc, syncSvc, lg, cfg)

	return &testEnv{cfg: cfg, h: h, db: db}
}

func applySchema(db *database.DB, dbName string) error {
	raw, err := os.ReadFile("../../migrations/schema.sql")
	if err != nil {
		return err
	}
	sqlText := strings.ReplaceAll(string(raw), "dns_manager", dbName)
	for _, s := range strings.Split(sqlText, ";") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("执行 [%s...]: %w", truncate60(s), err)
		}
	}
	return nil
}

func truncate60(s string) string {
	if len(s) <= 60 {
		return s
	}
	return s[:60]
}

// do 执行一次 HTTP 请求（可选携带 Bearer token）。
func do(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// login 登录并返回 JWT token。
func login(t *testing.T, h http.Handler) string {
	t.Helper()
	w := do(h, "POST", "/api/auth/login", `{"username":"admin","password":"admin23"}`, "")
	if w.Code != 200 {
		t.Fatalf("登录失败: code=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
			User  struct {
				Username string `json:"username"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析登录响应失败: %v", err)
	}
	if resp.Data.Token == "" {
		t.Fatalf("登录未返回 token: %s", w.Body.String())
	}
	return resp.Data.Token
}

// ---- 测试 ----

func TestLoginFlow(t *testing.T) {
	env := setupEnv(t)
	h := env.h.Routes()

	token := login(t, h)

	w := do(h, "POST", "/api/auth/login", `{"username":"admin","password":"wrong"}`, "")
	if w.Code != 401 {
		t.Fatalf("错误密码应 401，got %d", w.Code)
	}

	w = do(h, "GET", "/api/auth/me", "", token)
	if !strings.Contains(w.Body.String(), `"username":"admin"`) {
		t.Fatalf("me 响应异常: %s", w.Body.String())
	}

	w = do(h, "GET", "/api/monitor/summary", "", "")
	if w.Code != 401 {
		t.Fatalf("未登录应 401，got %d", w.Code)
	}
}

func TestDomainAndRecordFlow(t *testing.T) {
	env := setupEnv(t)
	h := env.h.Routes()
	token := login(t, h)

	w := do(h, "POST", "/api/domains", `{"name":"example-test.com","remark":"集成测试"}`, token)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "domain_id") {
		t.Fatalf("建域名失败: %d %s", w.Code, w.Body.String())
	}

	w = do(h, "GET", "/api/domains", "", token)
	if !strings.Contains(w.Body.String(), "example-test.com") {
		t.Fatalf("域名列表异常: %s", w.Body.String())
	}

	w = do(h, "POST", "/api/domains/1/records", `{"sub":"@","type":"A","line":"默认","value":"1.2.3.4","ttl":600}`, token)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "record_id") {
		t.Fatalf("建记录失败: %d %s", w.Code, w.Body.String())
	}

	w = do(h, "GET", "/api/domains/1/records?page=1&size=100", "", token)
	if !strings.Contains(w.Body.String(), "1.2.3.4") {
		t.Fatalf("记录列表异常: %s", w.Body.String())
	}

	w = do(h, "POST", "/api/domains/1/records", `{"sub":"@","type":"A","line":"默认","value":"1.2.3.4","ttl":600}`, token)
	if w.Code != 409 {
		t.Fatalf("重复记录应 409，got %d", w.Code)
	}

	w = do(h, "POST", "/api/domains/1/records", `{"sub":"@","type":"CNAME","line":"默认","value":"example.com","ttl":600}`, token)
	if w.Code != 409 {
		t.Fatalf("A/CNAME 冲突应 409，got %d", w.Code)
	}
}

func TestBackupFlow(t *testing.T) {
	env := setupEnv(t)
	h := env.h.Routes()
	token := login(t, h)

	do(h, "POST", "/api/domains", `{"name":"bk.example.com"}`, token)
	do(h, "POST", "/api/domains/1/records", `{"sub":"@","type":"A","line":"默认","value":"1.2.3.4","ttl":600}`, token)

	w := do(h, "POST", "/api/backups", "", token)
	if w.Code != 200 {
		t.Fatalf("创建备份失败: %d %s", w.Code, w.Body.String())
	}

	w = do(h, "GET", "/api/backups", "", token)
	if !strings.Contains(w.Body.String(), "手动备份") {
		t.Fatalf("备份列表异常: %s", w.Body.String())
	}

	w = do(h, "POST", "/api/backups/1/restore", "", token)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "need_verify") {
		t.Fatalf("恢复备份失败: %d %s", w.Code, w.Body.String())
	}
}

func TestMonitorSummary(t *testing.T) {
	env := setupEnv(t)
	h := env.h.Routes()
	token := login(t, h)

	do(h, "POST", "/api/domains", `{"name":"mon.example.com"}`, token)
	do(h, "POST", "/api/domains/1/records", `{"sub":"@","type":"A","line":"默认","value":"1.2.3.4","ttl":600}`, token)

	w := do(h, "GET", "/api/monitor/summary", "", token)
	body := w.Body.String()
	if !strings.Contains(body, `"domains":1`) || !strings.Contains(body, `"records":1`) {
		t.Fatalf("监控统计异常: %s", body)
	}
}

func TestOperationLogs(t *testing.T) {
	env := setupEnv(t)
	h := env.h.Routes()
	token := login(t, h)

	w := do(h, "GET", "/api/operation-logs", "", token)
	if !strings.Contains(w.Body.String(), "login") {
		t.Fatalf("操作日志异常: %s", w.Body.String())
	}
}

func TestExportCSV(t *testing.T) {
	env := setupEnv(t)
	h := env.h.Routes()
	token := login(t, h)

	do(h, "POST", "/api/domains", `{"name":"exp.example.com"}`, token)
	do(h, "POST", "/api/domains/1/records", `{"sub":"www","type":"A","line":"默认","value":"1.2.3.4","ttl":600}`, token)

	w := do(h, "GET", "/api/domains/1/export", "", token)
	if w.Code != 200 {
		t.Fatalf("导出失败: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "sub_domain,type,line,value,ttl,priority") {
		t.Fatalf("CSV 表头异常: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"www","A","默认","1.2.3.4"`) {
		t.Fatalf("CSV 内容异常: %s", w.Body.String())
	}
}
