// Package auth 提供基于 JWT 的认证、注册与角色鉴权。
// 密码使用 bcrypt 哈希存储；JWT 用 secret_key 做 HS256 签名，仅承载 user_id，
// 用户角色与状态每次请求实时查库，保证权限变更即时生效。
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/loeia/all-in-dns/internal/database"
	"github.com/loeia/all-in-dns/internal/httpapi"
	"github.com/loeia/all-in-dns/internal/redis"
)

const (
	loginFailKey = "loginfail:"
	tokenIssuer  = "all-in-dns"
)

// User 用户（不含密码哈希）。
type User struct {
	ID          int     `db:"id" json:"id"`
	Username    string  `db:"username" json:"username"`
	Role        string  `db:"role" json:"role"`
	LastLoginAt *string `db:"last_login_at" json:"last_login_at"`
}

type userWithHash struct {
	User
	PasswordHash string `db:"password_hash"`
}

// Claims JWT 载荷：只存身份标识，角色实时查库。
type Claims struct {
	UserID int `json:"uid"`
	jwt.RegisteredClaims
}

// Service 认证服务。
type Service struct {
	db         *database.DB
	redis      *redis.Client
	secretKey  string
	sessionTTL time.Duration
}

// New 构造认证服务。
func New(db *database.DB, r *redis.Client, secretKey string, sessionTTL time.Duration) *Service {
	if sessionTTL <= 0 {
		sessionTTL = 24 * time.Hour
	}
	return &Service{db: db, redis: r, secretKey: secretKey, sessionTTL: sessionTTL}
}

// Attempt 校验用户名密码（bcrypt）。失败返回 (nil, nil)。
func (s *Service) Attempt(ctx context.Context, username, password string) (*User, error) {
	var u userWithHash
	err := s.db.GetContext(ctx, &u,
		`SELECT id, username, password_hash, role, last_login_at FROM users WHERE username=?`, username)
	if errors.Is(err, sql.ErrNoRows) || u.PasswordHash == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, nil
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE users SET last_login_at=NOW() WHERE id=?`, u.ID)
	return &u.User, nil
}

// IssueToken 签发 JWT。
func (s *Service) IssueToken(u *User) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: u.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.sessionTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.secretKey))
}

// ParseToken 验证 JWT 并返回当前用户（角色实时查库）。无效或用户不存在返回 nil。
func (s *Service) ParseToken(ctx context.Context, tokenStr string) (*User, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("非法的签名算法")
		}
		return []byte(s.secretKey), nil
	})
	if err != nil || !token.Valid {
		return nil, nil
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || claims.UserID == 0 {
		return nil, nil
	}
	var u User
	err = s.db.GetContext(ctx, &u,
		`SELECT id, username, role, last_login_at FROM users WHERE id=?`, claims.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// TokenFromRequest 从 Authorization: Bearer <token> 提取 token。
func TokenFromRequest(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return ""
}

// RequireUser 要求已登录，未登录时写 401 并返回 false。
func (s *Service) RequireUser(ctx context.Context, w http.ResponseWriter, r *http.Request) (*User, bool) {
	u, err := s.ParseToken(ctx, TokenFromRequest(r))
	if err != nil {
		httpapi.Error(w, err)
		return nil, false
	}
	if u == nil {
		httpapi.Fail(w, http.StatusUnauthorized, "未登录")
		return nil, false
	}
	return u, true
}

// RequireRole 要求指定角色之一，无权限时写 403 并返回 false。
func (s *Service) RequireRole(ctx context.Context, w http.ResponseWriter, r *http.Request, roles ...string) (*User, bool) {
	u, ok := s.RequireUser(ctx, w, r)
	if !ok {
		return nil, false
	}
	for _, role := range roles {
		if u.Role == role {
			return u, true
		}
	}
	httpapi.Fail(w, http.StatusForbidden, "无权限")
	return nil, false
}

// LoginFailKey 登录失败限流 key。
func LoginFailKey(ip, username string) string {
	return loginFailKey + ip + ":" + strings.ToLower(username)
}

// randomHex 生成十六进制随机串（install 等场景复用）。
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
