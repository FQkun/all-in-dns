package provider

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// 阿里云 Alidns，RPC 风格 HMAC-SHA1 签名。
const (
	aliyunHost    = "alidns.aliyuncs.com"
	aliyunVersion = "2015-01-09"
)

type aliyunProvider struct {
	*baseProvider
}

func newAliyun(ak, sk string, extra map[string]string) *aliyunProvider {
	return &aliyunProvider{baseProvider: newBase(ak, sk, extra)}
}

func (a *aliyunProvider) Name() string { return "aliyun" }

// BuildRequest 构造 RPC HMAC-SHA1 签名 GET 请求（对齐 PHP AliyunProvider::buildRequest）。
func (a *aliyunProvider) BuildRequest(action string, params map[string]any) (*HTTPRequest, error) {
	all := map[string]any{
		"Action":           action,
		"Version":          aliyunVersion,
		"Format":           "JSON",
		"AccessKeyId":      a.ak,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":   nonceHex(),
		"Timestamp":        a.now().UTC().Format("2006-01-02T15:04:05Z"),
	}
	for k, v := range params {
		all[k] = v
	}

	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys) // 对齐 PHP ksort

	var canonical strings.Builder
	for i, k := range keys {
		if i > 0 {
			canonical.WriteByte('&')
		}
		canonical.WriteString(rawURLEncode(k))
		canonical.WriteByte('=')
		canonical.WriteString(rawURLEncode(asString(all[k])))
	}

	strToSign := "GET&" + rawURLEncode("/") + "&" + rawURLEncode(canonical.String())
	sig := base64.StdEncoding.EncodeToString(hmacSHA1([]byte(strToSign), []byte(a.sk+"&")))
	_, baseURL := a.endpoint(aliyunHost)
	url := baseURL + "/?" + canonical.String() + "&Signature=" + rawURLEncode(sig)

	return &HTTPRequest{Method: "GET", URL: url}, nil
}

// ParseResponse 解析阿里云响应（对齐 PHP AliyunProvider::parseResponse）。
func (a *aliyunProvider) ParseResponse(action string, code int, resp []byte) (map[string]any, error) {
	var j map[string]any
	if err := json.Unmarshal(resp, &j); err != nil {
		j = map[string]any{}
	}
	if _, hasCode := j["Code"]; hasCode {
		c := asString(j["Code"])
		if c == "" {
			c = "Error"
		}
		if action == "DeleteDomainRecord" &&
			(c == "DomainRecordNotBelongToUser" || c == "DomainRecord.NotExist") {
			return map[string]any{}, nil
		}
		if c == "InvalidDomainName.NoExist" || c == "DomainRecord.NotExist" {
			return nil, providerErrorf("阿里云: 该账号下不存在此域名（云解析 DNS 未添加）")
		}
		return nil, providerErrorf("阿里云: %s %s", c, asString(j["Message"]))
	}
	return expect(j, code, resp)
}

func (a *aliyunProvider) call(ctx context.Context, action string, params map[string]any) (map[string]any, error) {
	req, err := a.BuildRequest(action, params)
	if err != nil {
		return nil, err
	}
	code, body, _, err := a.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	return a.ParseResponse(action, code, body)
}

func (a *aliyunProvider) Ping(ctx context.Context) (PingResult, error) {
	r, err := a.call(ctx, "DescribeDomains", map[string]any{"PageSize": 1})
	if err != nil {
		return PingResult{}, err
	}
	return PingResult{OK: true, Domains: asInt(r["TotalCount"])}, nil
}

func (a *aliyunProvider) CheckDomain(ctx context.Context, domain string) (CheckDomainResult, error) {
	r, err := a.call(ctx, "DescribeDomainInfo", map[string]any{"DomainName": domain})
	if err != nil {
		return CheckDomainResult{}, err
	}
	if asString(r["DomainId"]) == "" {
		return CheckDomainResult{OK: false, Message: "域名不存在"}, nil
	}
	return CheckDomainResult{OK: true, DomainID: asString(r["DomainId"])}, nil
}

func (a *aliyunProvider) ListRecords(ctx context.Context, domain string) ([]RemoteRecord, error) {
	var out []RemoteRecord
	pageNum := 1
	for {
		r, err := a.call(ctx, "DescribeDomainRecords", map[string]any{
			"DomainName": domain, "PageSize": 100, "PageNumber": pageNum,
		})
		if err != nil {
			return nil, err
		}
		records := asSlice(asMap(r["DomainRecords"])["Record"])
		for _, item := range records {
			rec, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, RemoteRecord{
				ID:       asString(rec["RecordId"]),
				Sub:      asString(rec["RR"]),
				Type:     asString(rec["Type"]),
				Line:     LineFrom("aliyun", asString(rec["Line"])),
				Value:    asString(rec["Value"]),
				TTL:      asInt(rec["TTL"]),
				Priority: intPtrOrNil(rec["Priority"]),
			})
		}
		total := asInt(r["TotalCount"])
		if total == 0 {
			total = len(out)
		}
		pageNum++
		if len(out) >= total || len(records) == 0 {
			break
		}
	}
	return out, nil
}

func (a *aliyunProvider) OpParams(op, domain string, rec NormalizedRecord, remoteID *string) (OpCall, error) {
	switch op {
	case "create":
		return OpCall{Action: "AddDomainRecord", Params: a.writeParams(rec, nil, domain)}, nil
	case "update":
		if remoteID != nil {
			return OpCall{Action: "UpdateDomainRecord", Params: a.writeParams(rec, remoteID, "")}, nil
		}
		return OpCall{Action: "AddDomainRecord", Params: a.writeParams(rec, nil, domain)}, nil
	case "delete":
		id := ""
		if remoteID != nil {
			id = *remoteID
		}
		return OpCall{Action: "DeleteDomainRecord", Params: map[string]any{"RecordId": id}}, nil
	default:
		return OpCall{}, fmt.Errorf("未知操作: %s", op)
	}
}

func (a *aliyunProvider) writeParams(rec NormalizedRecord, recordID *string, domain string) map[string]any {
	p := map[string]any{}
	if domain != "" {
		p["DomainName"] = domain
	}
	if recordID != nil {
		p["RecordId"] = *recordID
	}
	p["RR"] = rec.Sub
	p["Type"] = rec.Type
	p["Value"] = writeValue(rec.Type, rec.Value)
	p["TTL"] = writeTtl(rec.TTL)
	p["Line"] = LineTo("aliyun", rec.Line)
	if rec.Priority != nil && *rec.Priority != 0 {
		p["Priority"] = *rec.Priority
	}
	return p
}

// rawURLEncode 精确复现 PHP rawurlencode（RFC 3986，空格 %20，保留 -_.~）。
func rawURLEncode(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var buf strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			buf.WriteByte(c)
		} else {
			buf.WriteByte('%')
			buf.WriteByte(hexDigits[c>>4])
			buf.WriteByte(hexDigits[c&0xf])
		}
	}
	return buf.String()
}

// nonceHex 生成 32 位十六进制随机串（对齐 PHP bin2hex(random_bytes(16))）。
func nonceHex() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
