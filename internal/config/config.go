// Package config 定义运行时配置。配置来源为环境变量（含 .env 文件加载）。
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// App 应用级配置。
type App struct {
	SecretKey      string `json:"secret_key"`       // AES-256-GCM 密钥
	CronKey        string `json:"cron_key"`         // cron 触发密钥
	AdminUser      string `json:"admin_user"`       // 初始管理员用户名
	AdminPass      string `json:"admin_pass"`       // 初始管理员密码
	CaptchaScene   string `json:"captcha_scene"`    // 阿里云 ESA Scene ID
	CaptchaPrefix  string `json:"captcha_prefix"`   // 阿里云 ESA Prefix
	SessionTTL     int    `json:"session_ttl"`      // 秒
	MaxRetry       int    `json:"max_retry"`        // 同步最大重试次数
	RetryBaseDelay int    `json:"retry_base_delay"` // 指数退避基数（秒）
	Timezone       string `json:"timezone"`
}

// DB MySQL 配置。
type DB struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Name    string `json:"name"`
	User    string `json:"user"`
	Pass    string `json:"pass"`
	Charset string `json:"charset"`
}

// Redis 配置。
type Redis struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Auth   string `json:"auth"`
	DB     int    `json:"db"`
	Prefix string `json:"prefix"`
}

// Config 总配置。
type Config struct {
	App   App   `json:"app"`
	DB    DB    `json:"db"`
	Redis Redis `json:"redis"`

	// 运行时注入（非配置文件项）
	Addr     string `json:"-"` // HTTP 监听地址
	Mode     string `json:"-"` // dev | serve
	WebDir   string `json:"-"` // 前端静态资源目录（serve 模式）
	APIProxy string `json:"-"` // dev 模式：把 /api/* 转发到真实后端（留空则走内置 mock）
}

// Default 返回带默认值的配置。
func Default() *Config {
	return &Config{
		App: App{
			SessionTTL:     86400,
			MaxRetry:       5,
			RetryBaseDelay: 60,
			Timezone:       "Asia/Shanghai",
			AdminUser:      "admin",
			AdminPass:      "admin123",
		},
		DB:    DB{Port: 3306, Charset: "utf8mb4"},
		Redis: Redis{Port: 6379, Prefix: "dns:"},
		Addr:  ":8080",
		Mode:  "dev",
	}
}

// FromEnv 用默认值 + 环境变量构造配置（环境变量由 godotenv 从 .env 加载）。
func FromEnv() *Config {
	c := Default()
	applyEnv(c)
	return c
}

func applyEnv(c *Config) {
	if v := os.Getenv("DNS_DB_HOST"); v != "" {
		c.DB.Host = v
	}
	if v := os.Getenv("DNS_DB_PORT"); v != "" {
		c.DB.Port, _ = strconv.Atoi(v)
	}
	if v := os.Getenv("DNS_DB_NAME"); v != "" {
		c.DB.Name = v
	}
	if v := os.Getenv("DNS_DB_USER"); v != "" {
		c.DB.User = v
	}
	if v := os.Getenv("DNS_DB_PASS"); v != "" {
		c.DB.Pass = v
	}
	if v := os.Getenv("DNS_REDIS_HOST"); v != "" {
		c.Redis.Host = v
	}
	if v := os.Getenv("DNS_REDIS_PORT"); v != "" {
		c.Redis.Port, _ = strconv.Atoi(v)
	}
	if v := os.Getenv("DNS_REDIS_AUTH"); v != "" {
		c.Redis.Auth = v
	}
	if v := os.Getenv("DNS_SECRET_KEY"); v != "" {
		c.App.SecretKey = v
	}
	if v := os.Getenv("DNS_CRON_KEY"); v != "" {
		c.App.CronKey = v
	}
	if v := os.Getenv("DNS_CAPTCHA_SCENE"); v != "" {
		c.App.CaptchaScene = v
	}
	if v := os.Getenv("DNS_CAPTCHA_PREFIX"); v != "" {
		c.App.CaptchaPrefix = v
	}
	if v := os.Getenv("DNS_ADMIN_USER"); v != "" {
		c.App.AdminUser = v
	}
	if v := os.Getenv("DNS_ADMIN_PASS"); v != "" {
		c.App.AdminPass = v
	}
}

// String 摘要（隐藏密码）。
func (c *Config) String() string {
	return fmt.Sprintf("mode=%s addr=%s apiProxy=%s captcha=%t",
		c.Mode, c.Addr, c.APIProxy, c.App.CaptchaPrefix != "")
}

// SessionTTLDuration 返回会话 TTL。
func (c *Config) SessionTTLDuration() time.Duration {
	if c.App.SessionTTL <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(c.App.SessionTTL) * time.Second
}
