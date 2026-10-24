// Package provider 定义云厂商适配层，对齐原 PHP 版 BaseProvider。
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

// ProviderError 云厂商调用错误（消息内包含厂商名与错误码，供同步引擎识别重复错误）。
type ProviderError struct{ msg string }

func (e *ProviderError) Error() string { return e.msg }

func providerErrorf(format string, args ...any) error {
	return &ProviderError{msg: fmt.Sprintf(format, args...)}
}

// RemoteRecord 各厂商 listRecords 返回的统一记录格式。
type RemoteRecord struct {
	ID       string
	Sub      string
	Type     string
	Line     string
	Value    string
	TTL      int
	Priority *int
}

// NormalizedRecord 归一化后的本地记录（对应 SyncService::normalizeRecord）。
type NormalizedRecord struct {
	Sub      string
	Type     string
	Line     string
	Value    string
	TTL      int
	Priority *int
}

// OpCall 一次写操作的调用参数（对应 opParams 返回的 [action, params]）。
type OpCall struct {
	Action string
	Params map[string]any
}

// HTTPRequest buildRequest 的产物。
type HTTPRequest struct {
	Method  string
	URL     string
	Headers []string // "Name: value"
	Body    []byte   // nil 表示无 body
}

// PingResult 连通性测试结果。
type PingResult struct {
	OK      bool `json:"ok"`
	Domains int  `json:"domains"`
}

// CheckDomainResult 域名校验结果。
type CheckDomainResult struct {
	OK       bool   `json:"ok"`
	DomainID string `json:"domain_id,omitempty"`
	Message  string `json:"message,omitempty"`
}

// Provider 云厂商能力接口。
type Provider interface {
	// Do 执行 HTTP 请求（由 baseProvider 实现）。
	Do(ctx context.Context, req *HTTPRequest) (code int, body []byte, costMs int, err error)

	Name() string
	Ping(ctx context.Context) (PingResult, error)
	CheckDomain(ctx context.Context, domain string) (CheckDomainResult, error)
	ListRecords(ctx context.Context, domain string) ([]RemoteRecord, error)
	OpParams(op, domain string, rec NormalizedRecord, remoteID *string) (OpCall, error)
	BuildRequest(action string, params map[string]any) (*HTTPRequest, error)
	ParseResponse(action string, code int, resp []byte) (map[string]any, error)
}

// baseProvider 提供通用 HTTP 执行与写值工具。
type baseProvider struct {
	ak      string
	sk      string
	extra   map[string]string
	http    *http.Client
	now     func() time.Time // 可注入时钟，便于签名测试
	baseURL string           // 测试用：覆盖默认 API 地址
}

func newBase(ak, sk string, extra map[string]string) *baseProvider {
	return &baseProvider{
		ak:    ak,
		sk:    sk,
		extra: extra,
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			},
		},
		now: time.Now,
	}
}

// endpoint 返回签名用的 host 与请求 URL；测试时可注入 baseURL 覆盖默认地址。
func (b *baseProvider) endpoint(defaultHost string) (host, urlStr string) {
	if b.baseURL != "" {
		if u, err := neturl.Parse(b.baseURL); err == nil {
			return u.Host, b.baseURL
		}
	}
	return defaultHost, "https://" + defaultHost
}

// Do 执行 HTTP 请求，返回状态码、响应体与耗时毫秒。
func (b *baseProvider) Do(ctx context.Context, req *HTTPRequest) (int, []byte, int, error) {
	var bodyReader io.Reader
	if req.Body != nil {
		bodyReader = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bodyReader)
	if err != nil {
		return 0, nil, 0, err
	}
	for _, h := range req.Headers {
		if i := strings.Index(h, ":"); i > 0 {
			httpReq.Header.Set(strings.TrimSpace(h[:i]), strings.TrimSpace(h[i+1:]))
		}
	}
	if req.Body != nil && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	resp, err := b.http.Do(httpReq)
	costMs := int(time.Since(start).Milliseconds())
	if err != nil {
		return 0, nil, costMs, fmt.Errorf("HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, costMs, fmt.Errorf("读取响应失败: %w", err)
	}
	return resp.StatusCode, data, costMs, nil
}

// expect 校验 HTTP 状态码（对齐 BaseProvider::expect）。
func expect(result map[string]any, code int, resp []byte) (map[string]any, error) {
	if code < 200 || code >= 300 {
		return nil, providerErrorf("HTTP %d: %s", code, truncateRunes(string(resp), 300))
	}
	return result, nil
}

// writeTtl 收敛 TTL 到 600-86400（对齐 BaseProvider::writeTtl）。
func writeTtl(ttl int) int {
	if ttl == 0 {
		ttl = 600
	}
	if ttl < 600 {
		return 600
	}
	if ttl > 86400 {
		return 86400
	}
	return ttl
}

// writeValue 主机名类记录值去尾点（对齐 BaseProvider::writeValue）。
func writeValue(typ, value string) string {
	v := strings.TrimSpace(value)
	switch strings.ToUpper(typ) {
	case "CNAME", "MX", "NS":
		v = strings.TrimRight(v, ".")
	}
	return v
}

// jsonEncode 编码 JSON，关闭 HTML 转义且不带末尾换行（对齐 PHP json_encode(JSON_UNESCAPED_SLASHES)）。
// 签名时 hash 的字符串与 HTTP body 必须是同一份本函数输出。
func jsonEncode(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// ParallelTask 并发执行单元。
type ParallelTask struct {
	Provider Provider
	Action   string
	Params   map[string]any
}

// ParallelResult 并发执行结果。
type ParallelResult struct {
	Result map[string]any
	Error  string
	CostMs int
}

// CallParallel 并发执行多个写操作，结果按入参顺序返回（对齐 BaseProvider::callParallel）。
func CallParallel(ctx context.Context, tasks []ParallelTask) []ParallelResult {
	results := make([]ParallelResult, len(tasks))
	if len(tasks) == 0 {
		return results
	}

	g, ctx := errgroup.WithContext(ctx)
	for i, t := range tasks {
		i, t := i, t
		g.Go(func() error {
			req, err := t.Provider.BuildRequest(t.Action, t.Params)
			if err != nil {
				results[i] = ParallelResult{Error: err.Error()}
				return nil
			}
			code, body, cost, err := t.Provider.Do(ctx, req)
			if err != nil {
				results[i] = ParallelResult{Error: err.Error(), CostMs: cost}
				return nil
			}
			out, err := t.Provider.ParseResponse(t.Action, code, body)
			if err != nil {
				results[i] = ParallelResult{Error: err.Error(), CostMs: cost}
				return nil
			}
			out["_cost_ms"] = cost
			results[i] = ParallelResult{Result: out, CostMs: cost}
			return nil
		})
	}
	_ = g.Wait()
	return results
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
