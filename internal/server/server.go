// Package server 组装 HTTP 服务：API 路由 + 生产静态资源（Vue 构建产物）+ 中间件。
//
// 前后端分离：
//   - dev 模式：仅提供 /api（mock 或代理）+ CORS，前端用 `npm run dev`（Vite 代理 /api）。
//   - serve 模式：提供 /api + 静态资源（web/dist，单二进制或读文件系统）。
package server

import (
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/loeia/all-in-dns/internal/config"
	"github.com/loeia/all-in-dns/internal/httpapi"
	"github.com/loeia/all-in-dns/internal/mock"
)

// Server HTTP 服务。
type Server struct {
	cfg *config.Config
	fs  fs.FS   // 静态资源根（Vue dist），nil 表示不服务静态
	api http.Handler
}

// New 构造 Server。fs 为 nil 时仅提供 API（dev 模式）；否则同时服务静态资源。
func New(cfg *config.Config, fs fs.FS, apiHandler http.Handler) *Server {
	return &Server{cfg: cfg, fs: fs, api: apiHandler}
}

// Handler 返回完整的 http.Handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// API 路由
	mux.Handle("/api/", s.apiHandler())

	// 定时任务 HTTP 入口（真实 handler 内含 /cron）
	if s.api != nil {
		mux.Handle("/cron", s.api)
	}

	// 静态资源（serve 模式）
	if s.fs != nil {
		s.registerStatic(mux)
	}

	var h http.Handler = mux
	h = securityHeaders(h)
	if s.cfg.Mode == "dev" {
		h = cors(h)
	}
	return httpapi.Chain(h, httpapi.Recover)
}

// registerStatic 注册 SPA 静态资源：/ 返回 index.html，/assets/* 返回构建产物。
func (s *Server) registerStatic(mux *http.ServeMux) {
	assetFS, err := fs.Sub(s.fs, "assets")
	if err != nil {
		slog.Error("assets sub fs", "err", err)
		return
	}
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assetFS)))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		s.serveIndex(w)
	})
	// 其余路径 404（hash 路由只访问根路径与 assets）
}

func (s *Server) serveIndex(w http.ResponseWriter) {
	data, err := fs.ReadFile(s.fs, "index.html")
	if err != nil {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

// apiHandler 决定 /api/* 的处理器：真实 handler > 代理 > mock。
func (s *Server) apiHandler() http.Handler {
	if s.api != nil {
		return s.api
	}
	if s.cfg.Mode == "dev" && s.cfg.APIProxy != "" {
		return s.reverseProxy()
	}
	if s.cfg.Mode == "dev" {
		return mock.Handler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpapi.Fail(w, http.StatusNotImplemented, "真实后端尚未接入")
	})
}

// reverseProxy 把 /api/* 转发到真实后端（联调场景）。
func (s *Server) reverseProxy() http.Handler {
	target, err := url.Parse(s.cfg.APIProxy)
	if err != nil {
		slog.Error("invalid api proxy", "url", s.cfg.APIProxy, "err", err)
		return mock.Handler()
	}
	return httputil.NewSingleHostReverseProxy(target)
}

// securityHeaders 安全响应头。
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' https://*.alicdn.com; style-src 'self' 'unsafe-inline' https://*.alicdn.com; img-src 'self' data: https://*.alicdn.com; font-src 'self' data:; connect-src 'self' https://*.aliyuncs.com https://*.alicdn.com; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// cors 开发模式跨域支持，便于前端独立 dev server 联调。
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, captcha-verify-param")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
