package config

import (
	"os"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.DB.Port != 3306 || c.DB.Charset != "utf8mb4" {
		t.Fatalf("db defaults wrong: %+v", c.DB)
	}
	if c.Redis.Port != 6379 || c.Redis.Prefix != "dns:" {
		t.Fatalf("redis defaults wrong: %+v", c.Redis)
	}
	if c.App.MaxRetry != 5 || c.App.RetryBaseDelay != 60 || c.App.SessionTTL != 86400 {
		t.Fatalf("app defaults wrong: %+v", c.App)
	}
	if c.Addr != ":8080" {
		t.Fatalf("addr default = %q", c.Addr)
	}
}

func TestFromEnv(t *testing.T) {
	os.Setenv("DNS_DB_HOST", "db.example.com")
	os.Setenv("DNS_DB_PORT", "3308")
	os.Setenv("DNS_SECRET_KEY", "abcdef0123456789abcdef0123456789")
	defer func() {
		os.Unsetenv("DNS_DB_HOST")
		os.Unsetenv("DNS_DB_PORT")
		os.Unsetenv("DNS_SECRET_KEY")
	}()

	c := FromEnv()
	if c.DB.Host != "db.example.com" || c.DB.Port != 3308 {
		t.Fatalf("env not applied: %+v", c.DB)
	}
	if c.App.SecretKey != "abcdef0123456789abcdef0123456789" {
		t.Fatalf("secret_key env not applied")
	}
}

func TestSessionTTLDuration(t *testing.T) {
	c := Default()
	if c.SessionTTLDuration().Seconds() != 86400 {
		t.Fatalf("ttl = %v", c.SessionTTLDuration())
	}
	c.App.SessionTTL = 0
	if c.SessionTTLDuration().Seconds() != 86400 {
		t.Fatalf("zero ttl should fall back to 24h")
	}
}

func TestAIEnabled(t *testing.T) {
	c := Default()
	if c.AIEnabled() {
		t.Fatal("默认未配置 APIKey 应禁用 AI")
	}
	c.AI.APIKey = "sk-test"
	if !c.AIEnabled() {
		t.Fatal("填入 APIKey 后应启用")
	}
	c.AI.BaseURL = ""
	if c.AIEnabled() {
		t.Fatal("缺少 BaseURL 应禁用")
	}
}
