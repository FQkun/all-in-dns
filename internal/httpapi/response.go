// Package httpapi 提供统一的 JSON 响应、请求解析与通用中间件。
// 响应格式对齐原 PHP 版 Http::ok / Http::fail：{code, msg, data}。
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// Body 统一响应体。
type Body struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

// OK 成功响应（code=0）。
func OK(w http.ResponseWriter, data any) {
	JSON(w, http.StatusOK, Body{Code: 0, Msg: "ok", Data: data})
}

// Fail 业务失败，code 即 HTTP 状态码。
func Fail(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, Body{Code: status, Msg: msg})
}

// JSON 写出 JSON，并附带 X-Time 计时头。
func JSON(w http.ResponseWriter, status int, body Body) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Time", elapsedMS())
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// Error 统一错误处理（等价 PHP Router 的全局 catch，带 request_id）。
// 同时把错误详情写入日志，便于排查。
func Error(w http.ResponseWriter, err error) {
	rid := requestID()
	slog.Error("请求处理失败", "error", err, "request_id", rid)
	JSON(w, http.StatusInternalServerError, Body{
		Code: 500,
		Msg:  "服务器内部错误",
		Data: map[string]string{"request_id": rid},
	})
}

// Input 解析 JSON 请求体到 map（对齐 Http::input：JSON 优先）。
func Input(r *http.Request) map[string]any {
	var m map[string]any
	if r.Body != nil {
		dec := json.NewDecoder(r.Body)
		_ = dec.Decode(&m)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m
}

// IP 获取客户端 IP（对齐 Http::ip：优先 X-Real-IP / X-Forwarded-For）。
func IP(r *http.Request) string {
	for _, h := range []string{"X-Real-Ip", "X-Forwarded-For"} {
		v := r.Header.Get(h)
		if v == "" {
			continue
		}
		first := strings.TrimSpace(strings.Split(v, ",")[0])
		if net.ParseIP(first) != nil {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// Middleware 包装 handler 的通用中间件类型。
type Middleware func(http.Handler) http.Handler

// Chain 按顺序组合中间件。
func Chain(h http.Handler, mw ...Middleware) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// Recover 捕获 panic，返回统一 500（等价 Router 的 catch Throwable）。
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic 恢复", "panic", rec, "path", r.URL.Path, "method", r.Method)
				err, ok := rec.(error)
				if !ok {
					err = errors.New("panic")
				}
				Error(w, err)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// NoCache 禁止缓存（用于动态 API）。
func NoCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		next.ServeHTTP(w, r)
	})
}

var start = time.Now()

func elapsedMS() string {
	return itoa(int(time.Since(start).Milliseconds()))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func requestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
