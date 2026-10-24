package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/loeia/all-in-dns/internal/auth"
	"github.com/loeia/all-in-dns/internal/httpapi"
)

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	in := httpapi.Input(r)

	captchaOn := strings.TrimSpace(h.cfg.App.CaptchaPrefix) != ""
	captchaParam := strings.TrimSpace(str(in["captcha"]))
	if captchaParam == "" {
		captchaParam = strings.TrimSpace(r.Header.Get("captcha-verify-param"))
	}
	if captchaOn && captchaParam == "" {
		httpapi.Fail(w, http.StatusForbidden, "请先完成安全验证")
		return
	}

	username := str(in["username"])
	ip := httpapi.IP(r)
	limitKey := auth.LoginFailKey(ip, username)

	fails := 0
	if v, err := h.redis.Get(ctx, limitKey); err == nil {
		fails = intOr(v, 0)
	}
	if fails >= 10 {
		httpapi.Fail(w, http.StatusTooManyRequests, "尝试次数过多，请 10 分钟后再试")
		return
	}

	u, err := h.auth.Attempt(ctx, username, str(in["password"]))
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	if u == nil {
		_, _ = h.redis.Incr(ctx, limitKey, 10*time.Minute)
		h.logger.Op(ctx, nil, username, "login.failed", "", map[string]any{"ip": ip}, ip)
		httpapi.Fail(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	_ = h.redis.Del(ctx, limitKey)

	token, err := h.auth.IssueToken(u)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	h.logger.Op(ctx, &u.ID, u.Username, "login", "", nil, ip)
	httpapi.OK(w, map[string]any{"token": token, "user": u})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	// 无状态 JWT：登出由前端删除 token 完成，这里仅记录日志（尽力而为）。
	if u, err := h.auth.ParseToken(r.Context(), auth.TokenFromRequest(r)); err == nil && u != nil {
		h.logger.Op(r.Context(), &u.ID, u.Username, "logout", "", nil, httpapi.IP(r))
	}
	httpapi.OK(w, nil)
}

func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := h.auth.RequireUser(r.Context(), w, r)
	if !ok {
		return
	}
	httpapi.OK(w, map[string]any{"user": u})
}
