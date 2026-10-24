// Package dnsprobe 通过公共 DoH（DNS over HTTPS）探测域名在各地区的实际解析结果。
package dnsprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

// Endpoint 一个公共 DNS 探测点。
type Endpoint struct {
	Name string
	URL  string
}

// DefaultEndpoints 默认探测点（均为 JSON 格式 DoH）。
var DefaultEndpoints = []Endpoint{
	{Name: "阿里 DNS", URL: "https://dns.alidns.com/resolve"},
	{Name: "腾讯 DNS", URL: "https://doh.pub/dns-query"},
	{Name: "Google", URL: "https://dns.google/resolve"},
}

// Result 单个探测点的结果。
type Result struct {
	Name      string   `json:"name"`
	Records   []string `json:"records"`
	Status    string   `json:"status"` // ok | nxdomain | error
	Error     string   `json:"error,omitempty"`
	LatencyMs int      `json:"latency_ms"`
}

var typeCode = map[string]int{
	"A": 1, "NS": 2, "CNAME": 5, "MX": 15, "TXT": 16, "AAAA": 28, "SRV": 33, "CAA": 257,
}

type dohResponse struct {
	Status int `json:"Status"` // 0=NOERROR, 3=NXDOMAIN
	Answer []struct {
		Name string `json:"name"`
		Type int    `json:"type"`
		TTL  int    `json:"TTL"`
		Data string `json:"data"`
	} `json:"Answer"`
}

var client = &http.Client{Timeout: 6 * time.Second}

// Probe 对一个 FQDN + 记录类型，并发查询所有探测点。
func Probe(ctx context.Context, fqdn, recordType string, endpoints []Endpoint) []Result {
	results := make([]Result, len(endpoints))
	done := make(chan int, len(endpoints))

	for i, ep := range endpoints {
		go func(i int, ep Endpoint) {
			results[i] = probeOne(ctx, ep, fqdn, recordType)
			done <- i
		}(i, ep)
	}
	for range endpoints {
		<-done
	}
	return results
}

func probeOne(ctx context.Context, ep Endpoint, fqdn, recordType string) Result {
	start := time.Now()
	res := Result{Name: ep.Name}

	code, ok := typeCode[strings.ToUpper(recordType)]
	if !ok {
		res.Status = "error"
		res.Error = "不支持的记录类型"
		return res
	}

	u := fmt.Sprintf("%s?name=%s&type=%d", ep.URL, neturl.QueryEscape(fqdn), code)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		res.Status = "error"
		res.Error = err.Error()
		return res
	}
	req.Header.Set("Accept", "application/dns-json")

	httpResp, err := client.Do(req)
	res.LatencyMs = int(time.Since(start).Milliseconds())
	if err != nil {
		res.Status = "error"
		res.Error = "查询失败"
		return res
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		res.Status = "error"
		res.Error = fmt.Sprintf("HTTP %d", httpResp.StatusCode)
		return res
	}

	var dr dohResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&dr); err != nil {
		res.Status = "error"
		res.Error = "解析响应失败"
		return res
	}

	if dr.Status == 3 {
		res.Status = "nxdomain"
		res.Records = []string{}
		return res
	}

	res.Records = make([]string, 0, len(dr.Answer))
	for _, a := range dr.Answer {
		// 只保留与查询类型名匹配的答案（CNAME 链会带出额外记录）
		if a.Type != code && a.Type != 5 {
			continue
		}
		res.Records = append(res.Records, strings.TrimSpace(a.Data))
	}
	res.Status = "ok"
	return res
}

// FQDN 把子域名拼成全域名。
func FQDN(sub, domain string) string {
	sub = strings.TrimSpace(sub)
	if sub == "" || sub == "@" {
		return domain
	}
	return sub + "." + domain
}
