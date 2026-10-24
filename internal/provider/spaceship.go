package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Spaceship DNS（Namecheap 旗下），使用 X-API-Key + X-API-Secret 认证（无签名）。
// API 为批量接口：GET 列表 / PUT 保存 / DELETE 删除，记录无独立 ID（用 type|name|value 合成）。
const (
	spaceshipHost = "spaceship.dev"
	spaceshipBase = "https://spaceship.dev/api/v1"
)

type spaceshipProvider struct {
	*baseProvider
}

func newSpaceship(ak, sk string, extra map[string]string) *spaceshipProvider {
	return &spaceshipProvider{baseProvider: newBase(ak, sk, extra)}
}

func (s *spaceshipProvider) Name() string { return "spaceship" }

// base API 根地址（测试时可用 baseURL 覆盖）。
func (s *spaceshipProvider) base() string {
	if s.baseURL != "" {
		return strings.TrimRight(s.baseURL, "/")
	}
	return spaceshipBase
}

// BuildRequest 构造请求。
func (s *spaceshipProvider) BuildRequest(action string, params map[string]any) (*HTTPRequest, error) {
	headers := []string{
		"X-API-Key: " + s.ak,
		"X-API-Secret: " + s.sk,
		"Content-Type: application/json",
	}
	domain := asString(params["domain"])

	switch action {
	case "listRecords":
		take := asInt(params["take"])
		if take <= 0 {
			take = 500
		}
		url := fmt.Sprintf("%s/dns/records/%s?take=%d&skip=%d", s.base(), domain, take, asInt(params["skip"]))
		return &HTTPRequest{Method: "GET", URL: url, Headers: headers}, nil

	case "listDomains":
		take := asInt(params["take"])
		if take <= 0 {
			take = 500
		}
		url := fmt.Sprintf("%s/domains?take=%d&skip=%d", s.base(), take, asInt(params["skip"]))
		return &HTTPRequest{Method: "GET", URL: url, Headers: headers}, nil

	case "saveRecords":
		body, err := jsonEncode(map[string]any{"force": true, "items": params["items"]})
		if err != nil {
			return nil, err
		}
		return &HTTPRequest{Method: "PUT", URL: s.base() + "/dns/records/" + domain, Headers: headers, Body: []byte(body)}, nil

	case "deleteRecords":
		body, err := jsonEncode(params["items"])
		if err != nil {
			return nil, err
		}
		return &HTTPRequest{Method: "DELETE", URL: s.base() + "/dns/records/" + domain, Headers: headers, Body: []byte(body)}, nil
	}
	return nil, fmt.Errorf("Spaceship 未知操作: %s", action)
}

// ParseResponse 解析响应（错误为 application/problem+json，字段 detail）。
func (s *spaceshipProvider) ParseResponse(action string, code int, resp []byte) (map[string]any, error) {
	if code == 204 {
		return map[string]any{}, nil
	}
	var j map[string]any
	if err := json.Unmarshal(resp, &j); err != nil {
		j = map[string]any{}
	}
	if code < 200 || code >= 300 {
		detail := asString(j["detail"])
		if detail == "" {
			detail = truncateRunes(string(resp), 300)
		}
		return nil, providerErrorf("Spaceship: %s", detail)
	}
	return j, nil
}

func (s *spaceshipProvider) call(ctx context.Context, action string, params map[string]any) (map[string]any, error) {
	req, err := s.BuildRequest(action, params)
	if err != nil {
		return nil, err
	}
	code, body, _, err := s.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.ParseResponse(action, code, body)
}

func (s *spaceshipProvider) Ping(ctx context.Context) (PingResult, error) {
	r, err := s.call(ctx, "listDomains", map[string]any{"take": 1, "skip": 0})
	if err != nil {
		return PingResult{}, err
	}
	return PingResult{OK: true, Domains: asInt(r["total"])}, nil
}

// CheckDomain 通过读取该域名的记录列表判断域名是否在此账号下。
func (s *spaceshipProvider) CheckDomain(ctx context.Context, domain string) (CheckDomainResult, error) {
	_, err := s.call(ctx, "listRecords", map[string]any{"domain": domain, "take": 1, "skip": 0})
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "404") {
			return CheckDomainResult{OK: false, Message: "域名不存在"}, nil
		}
		return CheckDomainResult{}, err
	}
	return CheckDomainResult{OK: true, DomainID: domain}, nil
}

func (s *spaceshipProvider) ListRecords(ctx context.Context, domain string) ([]RemoteRecord, error) {
	var out []RemoteRecord
	skip := 0
	for {
		r, err := s.call(ctx, "listRecords", map[string]any{"domain": domain, "take": 500, "skip": skip})
		if err != nil {
			return nil, err
		}
		items := asSlice(r["items"])
		for _, it := range items {
			rec, ok := it.(map[string]any)
			if !ok {
				continue
			}
			rr := spaceshipToRemote(rec)
			if strings.ToUpper(rr.Type) == "NS" && strings.ToLower(rr.Sub) == "@" {
				continue
			}
			out = append(out, rr)
		}
		total := asInt(r["total"])
		skip += len(items)
		if len(items) == 0 || skip >= total {
			break
		}
	}
	return out, nil
}

func (s *spaceshipProvider) OpParams(op, domain string, rec NormalizedRecord, remoteID *string) (OpCall, error) {
	switch op {
	case "create", "update":
		// Spaceship 的 PUT 是「新增或更新 TTL」，用 force 强制覆盖冲突
		item, err := spaceshipItem(rec)
		if err != nil {
			return OpCall{}, err
		}
		return OpCall{Action: "saveRecords", Params: map[string]any{
			"domain": domain,
			"items":  []any{item},
		}}, nil
	case "delete":
		item, err := spaceshipDeleteItem(rec)
		if err != nil {
			return OpCall{}, err
		}
		return OpCall{Action: "deleteRecords", Params: map[string]any{
			"domain": domain,
			"items":  []any{item},
		}}, nil
	}
	return OpCall{}, fmt.Errorf("Spaceship 未知操作: %s", op)
}

// ---- 记录转换 ----

// spaceshipToRemote Spaceship 记录 → 统一 RemoteRecord。
func spaceshipToRemote(rec map[string]any) RemoteRecord {
	typ := strings.ToUpper(asString(rec["type"]))
	rr := RemoteRecord{
		Sub:  asString(rec["name"]),
		Type: typ,
		Line: "默认", // Spaceship 无线路概念
		TTL:  asInt(rec["ttl"]),
	}
	switch typ {
	case "A", "AAAA":
		rr.Value = asString(rec["address"])
	case "CNAME":
		rr.Value = asString(rec["cname"])
	case "MX":
		rr.Value = asString(rec["exchange"])
		if p := asInt(rec["preference"]); p >= 0 {
			rr.Priority = &p
		}
	case "TXT":
		rr.Value = asString(rec["value"])
	case "NS":
		rr.Value = asString(rec["nameserver"])
	case "CAA":
		rr.Value = fmt.Sprintf("%d %s %s", asInt(rec["flag"]), asString(rec["tag"]), asString(rec["value"]))
	case "SRV":
		rr.Value = fmt.Sprintf("%d %d %d %s", asInt(rec["priority"]), asInt(rec["weight"]), asInt(rec["port"]), asString(rec["target"]))
		rr.Sub = strings.TrimSpace(asString(rec["service"]) + "." + asString(rec["protocol"]))
	default:
		rr.Value = asString(rec["value"])
	}
	// 无独立 ID，用 type|name|value 合成
	rr.ID = strings.ToLower(rr.Sub + "|" + typ + "|" + rr.Value)
	return rr
}

// spaceshipItem 归一化记录 → Spaceship 保存项。
func spaceshipItem(rec NormalizedRecord) (map[string]any, error) {
	typ := strings.ToUpper(rec.Type)
	item := map[string]any{
		"type": typ,
		"name": rec.Sub,
		"ttl":  spaceshipTTL(rec.TTL),
	}
	switch typ {
	case "A", "AAAA":
		item["address"] = writeValue(typ, rec.Value)
	case "CNAME":
		item["cname"] = writeValue(typ, rec.Value)
	case "MX":
		item["exchange"] = writeValue(typ, rec.Value)
		p := 10
		if rec.Priority != nil {
			p = *rec.Priority
		}
		item["preference"] = p
	case "TXT":
		item["value"] = rec.Value
	case "NS":
		item["nameserver"] = writeValue(typ, rec.Value)
	case "CAA":
		flag, tag, val := parseCAA(rec.Value)
		item["flag"] = flag
		item["tag"] = tag
		item["value"] = val
	case "SRV":
		pri, weight, port, target := parseSRV(rec.Value)
		item["priority"] = pri
		item["weight"] = weight
		item["port"] = port
		item["target"] = writeValue("SRV", target)
		service, protocol := splitSRVName(rec.Sub)
		item["service"] = service
		item["protocol"] = protocol
		item["name"] = "@"
	default:
		return nil, fmt.Errorf("Spaceship 不支持的记录类型: %s", typ)
	}
	return item, nil
}

// spaceshipDeleteItem 删除项（按 type + name + 值精确匹配）。
func spaceshipDeleteItem(rec NormalizedRecord) (map[string]any, error) {
	full, err := spaceshipItem(rec)
	if err != nil {
		return nil, err
	}
	// 删除项不需要 ttl
	delete(full, "ttl")
	return full, nil
}

// spaceshipTTL Spaceship TTL 限制 60-3600。
func spaceshipTTL(ttl int) int {
	if ttl == 0 {
		ttl = 600
	}
	if ttl < 60 {
		return 60
	}
	if ttl > 3600 {
		return 3600
	}
	return ttl
}

// parseCAA 解析 "0 issue letsencrypt.org" / `0 issue "letsencrypt.org"`。
func parseCAA(v string) (int, string, string) {
	v = strings.TrimSpace(v)
	parts := strings.Fields(v)
	if len(parts) < 3 {
		return 0, "issue", strings.Trim(v, `"`)
	}
	flag, _ := strconv.Atoi(parts[0])
	tag := parts[1]
	val := strings.Trim(strings.Join(parts[2:], " "), `"`)
	return flag, tag, val
}

// parseSRV 解析 "priority weight port target"。
func parseSRV(v string) (int, int, int, string) {
	parts := strings.Fields(strings.TrimSpace(v))
	if len(parts) < 4 {
		return 0, 0, 0, strings.TrimSpace(v)
	}
	pri, _ := strconv.Atoi(parts[0])
	weight, _ := strconv.Atoi(parts[1])
	port, _ := strconv.Atoi(parts[2])
	return pri, weight, port, parts[3]
}

// splitSRVName 把 "_sip._tcp" 拆成 service/protocol。
func splitSRVName(sub string) (string, string) {
	parts := strings.SplitN(strings.TrimSpace(sub), ".", 2)
	service := "_sip"
	protocol := "_tcp"
	if len(parts) > 0 && parts[0] != "" {
		service = parts[0]
	}
	if len(parts) > 1 && parts[1] != "" {
		protocol = parts[1]
	}
	return service, protocol
}
