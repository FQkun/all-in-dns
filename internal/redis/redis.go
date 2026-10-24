// Package redis 封装 go-redis 客户端，key 前缀处理对齐原 PHP 版 RedisClient。
package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/loeia/all-in-dns/internal/config"
)

// Client 包装 *goredis.Client，所有 key 自动加前缀。
type Client struct {
	c      *goredis.Client
	prefix string
}

// Open 建立连接并 Ping 验证。
func Open(cfg config.Redis) (*Client, error) {
	c := goredis.NewClient(&goredis.Options{
		Addr:     joinAddr(cfg.Host, cfg.Port),
		Password: cfg.Auth,
		DB:       cfg.DB,
	})
	if err := c.Ping(context.Background()).Err(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return &Client{c: c, prefix: cfg.Prefix}, nil
}

// New 用已有配置构造（不 Ping，供测试注入）。
func New(c *goredis.Client, prefix string) *Client {
	return &Client{c: c, prefix: prefix}
}

func (r *Client) key(k string) string { return r.prefix + k }

// Get 读取字符串，key 不存在返回空串。
func (r *Client) Get(ctx context.Context, k string) (string, error) {
	v, err := r.c.Get(ctx, r.key(k)).Result()
	if err == goredis.Nil {
		return "", nil
	}
	return v, err
}

// Set 写入字符串；ttl <= 0 表示永不过期。
func (r *Client) Set(ctx context.Context, k, v string, ttl time.Duration) error {
	return r.c.Set(ctx, r.key(k), v, ttl).Err()
}

// Incr 自增；首次创建（结果==1）时若 ttl>0 则设置过期。
func (r *Client) Incr(ctx context.Context, k string, ttl time.Duration) (int64, error) {
	n, err := r.c.Incr(ctx, r.key(k)).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 && ttl > 0 {
		if err := r.c.Expire(ctx, r.key(k), ttl).Err(); err != nil {
			return n, err
		}
	}
	return n, nil
}

// Del 删除一个或多个 key。
func (r *Client) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	ks := make([]string, len(keys))
	for i, k := range keys {
		ks[i] = r.key(k)
	}
	return r.c.Del(ctx, ks...).Err()
}

// Close 关闭连接。
func (r *Client) Close() error { return r.c.Close() }

func joinAddr(host string, port int) string {
	if port <= 0 {
		return host
	}
	return host + ":" + itoa(port)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
