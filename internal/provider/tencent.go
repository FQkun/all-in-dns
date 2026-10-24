package provider

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// 腾讯云 DNSPod，TC3-HMAC-SHA256 签名。
const (
	tencentHost    = "dnspod.tencentcloudapi.com"
	tencentService = "dnspod"
	tencentVersion = "2021-03-23"
)

type tencentProvider struct {
	*baseProvider
}

func newTencent(ak, sk string, extra map[string]string) *tencentProvider {
	return &tencentProvider{baseProvider: newBase(ak, sk, extra)}
}

func (t *tencentProvider) Name() string { return "tencent" }

// BuildRequest 构造 TC3 签名请求（对齐 PHP TencentProvider::buildRequest）。
func (t *tencentProvider) BuildRequest(action string, params map[string]any) (*HTTPRequest, error) {
	now := t.now()
	ts := now.Unix()
	date := now.UTC().Format("2006-01-02")
	payload, err := jsonEncode(params)
	if err != nil {
		return nil, err
	}

	host, urlStr := t.endpoint(tencentHost)
	hashedBody := hexSHA256(payload)
	canonical := "POST\n/\n\n" +
		"content-type:application/json; charset=utf-8\n" +
		"host:" + host + "\n" +
		"x-tc-action:" + strings.ToLower(action) + "\n\n" +
		"content-type;host;x-tc-action\n" +
		hashedBody
	credScope := date + "/" + tencentService + "/tc3_request"
	strToSign := "TC3-HMAC-SHA256\n" + strconv.FormatInt(ts, 10) + "\n" + credScope + "\n" + hexSHA256(canonical)

	kDate := hmacSHA256([]byte(date), []byte("TC3"+t.sk))
	kService := hmacSHA256([]byte(tencentService), kDate)
	kSigning := hmacSHA256([]byte("tc3_request"), kService)
	signature := hex.EncodeToString(hmacSHA256([]byte(strToSign), kSigning))

	auth := fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=content-type;host;x-tc-action, Signature=%s",
		t.ak, credScope, signature)

	return &HTTPRequest{
		Method: "POST",
		URL:    urlStr,
		Headers: []string{
			"Content-Type: application/json; charset=utf-8",
			"X-TC-Action: " + action,
			"X-TC-Version: " + tencentVersion,
			"X-TC-Timestamp: " + strconv.FormatInt(ts, 10),
			"Authorization: " + auth,
		},
		Body: []byte(payload),
	}, nil
}

// ParseResponse 解析腾讯云响应（对齐 PHP TencentProvider::parseResponse）。
func (t *tencentProvider) ParseResponse(action string, code int, resp []byte) (map[string]any, error) {
	var j map[string]any
	if err := json.Unmarshal(resp, &j); err != nil {
		j = map[string]any{}
	}
	respMap, ok := j["Response"].(map[string]any)
	if !ok {
		return expect(j, code, resp)
	}
	if errObj, ok := respMap["Error"].(map[string]any); ok {
		errCode := asString(errObj["Code"])
		errMsg := asString(errObj["Message"])
		if action == "DeleteRecord" &&
			(errCode == "InvalidParameter.RecordIdInvalid" || errCode == "ResourceNotFound.Record") {
			return map[string]any{}, nil
		}
		if strings.HasPrefix(errCode, "ResourceNotFound") {
			return nil, providerErrorf("腾讯云: 该账号下不存在此域名（DNSPod 未添加）")
		}
		return nil, providerErrorf("腾讯云: %s %s", errCode, errMsg)
	}
	return expect(respMap, code, resp)
}

func (t *tencentProvider) call(ctx context.Context, action string, params map[string]any) (map[string]any, error) {
	req, err := t.BuildRequest(action, params)
	if err != nil {
		return nil, err
	}
	code, body, _, err := t.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	return t.ParseResponse(action, code, body)
}

func (t *tencentProvider) Ping(ctx context.Context) (PingResult, error) {
	r, err := t.call(ctx, "DescribeDomainList", map[string]any{"Limit": 30})
	if err != nil {
		return PingResult{}, err
	}
	count := asInt(r["TotalCount"])
	if count == 0 {
		count = len(asSlice(r["DomainList"]))
	}
	return PingResult{OK: true, Domains: count}, nil
}

func (t *tencentProvider) CheckDomain(ctx context.Context, domain string) (CheckDomainResult, error) {
	r, err := t.call(ctx, "DescribeDomain", map[string]any{"Domain": domain})
	if err != nil {
		return CheckDomainResult{}, err
	}
	var domainID string
	if info, ok := r["DomainInfo"].(map[string]any); ok {
		domainID = asString(info["Id"])
	}
	return CheckDomainResult{OK: true, DomainID: domainID}, nil
}

func (t *tencentProvider) ListRecords(ctx context.Context, domain string) ([]RemoteRecord, error) {
	var out []RemoteRecord
	offset := 0
	for {
		r, err := t.call(ctx, "DescribeRecordList", map[string]any{"Domain": domain, "Offset": offset, "Limit": 100})
		if err != nil {
			return nil, err
		}
		recordList := asSlice(r["RecordList"])
		for _, item := range recordList {
			rec, ok := item.(map[string]any)
			if !ok {
				continue
			}
			typ := asString(rec["Type"])
			name := asString(rec["Name"])
			if strings.ToUpper(typ) == "NS" && name == "@" {
				continue
			}
			out = append(out, RemoteRecord{
				ID:       asString(rec["RecordId"]),
				Sub:      name,
				Type:     typ,
				Line:     LineFrom("tencent", asString(rec["Line"])),
				Value:    asString(rec["Value"]),
				TTL:      asInt(rec["TTL"]),
				Priority: intPtrOrNil(rec["MX"]),
			})
		}
		total := asInt(asMap(r["RecordCountInfo"])["TotalCount"])
		if total == 0 {
			total = len(out)
		}
		offset += 100
		if offset >= total || len(recordList) == 0 {
			break
		}
	}
	return out, nil
}

func (t *tencentProvider) OpParams(op, domain string, rec NormalizedRecord, remoteID *string) (OpCall, error) {
	switch op {
	case "create":
		return OpCall{Action: "CreateRecord", Params: t.writeParams(domain, rec, nil)}, nil
	case "update":
		if remoteID != nil {
			return OpCall{Action: "ModifyRecord", Params: t.writeParams(domain, rec, remoteID)}, nil
		}
		return OpCall{Action: "CreateRecord", Params: t.writeParams(domain, rec, nil)}, nil
	case "delete":
		id := 0
		if remoteID != nil {
			id, _ = strconv.Atoi(*remoteID)
		}
		return OpCall{Action: "DeleteRecord", Params: map[string]any{"Domain": domain, "RecordId": id}}, nil
	default:
		return OpCall{}, fmt.Errorf("未知操作: %s", op)
	}
}

func (t *tencentProvider) writeParams(domain string, rec NormalizedRecord, recordID *string) map[string]any {
	p := map[string]any{"Domain": domain}
	if recordID != nil {
		id, _ := strconv.Atoi(*recordID)
		p["RecordId"] = id
	}
	p["SubDomain"] = rec.Sub
	p["RecordType"] = rec.Type
	p["RecordLine"] = LineTo("tencent", rec.Line)
	p["Value"] = writeValue(rec.Type, rec.Value)
	p["TTL"] = writeTtl(rec.TTL)
	if rec.Priority != nil && *rec.Priority != 0 {
		p["MX"] = *rec.Priority
	}
	return p
}

