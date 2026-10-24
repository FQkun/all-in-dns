package provider

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// 火山引擎 DNS，AWS V4 风格签名。
const (
	volcHost    = "dns.volcengineapi.com"
	volcService = "dns"
	volcVersion = "2018-08-01"
	volcRegion  = "cn-beijing"
)

var volcWriteActions = map[string]bool{
	"CreateRecord": true, "UpdateRecord": true, "DeleteRecord": true,
}

var mxValuePrefix = regexp.MustCompile(`^\d+\s+`)

type volcProvider struct {
	*baseProvider

	zidMu    sync.Mutex
	zidCache map[string]int
}

func newVolc(ak, sk string, extra map[string]string) *volcProvider {
	return &volcProvider{
		baseProvider: newBase(ak, sk, extra),
		zidCache:     map[string]int{},
	}
}

func (v *volcProvider) Name() string { return "volc" }

// BuildRequest 构造 V4 签名请求（对齐 PHP VolcProvider::buildRequest）。
func (v *volcProvider) BuildRequest(action string, query map[string]any) (*HTTPRequest, error) {
	now := v.now()
	date := now.UTC().Format("20060102")
	dt := now.UTC().Format("20060102T150405Z")

	isWrite := volcWriteActions[action]
	var body string
	var qp map[string]any
	if isWrite {
		b, err := jsonEncode(query)
		if err != nil {
			return nil, err
		}
		body = b
		qp = map[string]any{"Action": action, "Version": volcVersion}
	} else {
		body = ""
		qp = map[string]any{"Action": action, "Version": volcVersion}
		for k, val := range query {
			qp[k] = val
		}
	}

	keys := make([]string, 0, len(qp))
	for k := range qp {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var canonicalQuery strings.Builder
	for i, k := range keys {
		if i > 0 {
			canonicalQuery.WriteByte('&')
		}
		canonicalQuery.WriteString(rawURLEncode(k))
		canonicalQuery.WriteByte('=')
		canonicalQuery.WriteString(rawURLEncode(asString(qp[k])))
	}

	host, baseURL := v.endpoint(volcHost)
	payloadHash := hexSHA256(body)
	canonicalHeaders := "content-type:application/json\n" +
		"host:" + host + "\n" +
		"x-content-sha256:" + payloadHash + "\n" +
		"x-date:" + dt + "\n"
	signedHeaders := "content-type;host;x-content-sha256;x-date"

	method := "GET"
	if isWrite {
		method = "POST"
	}
	canonicalRequest := method + "\n/\n" + canonicalQuery.String() + "\n" +
		canonicalHeaders + "\n" + signedHeaders + "\n" + payloadHash

	credScope := date + "/" + volcRegion + "/" + volcService + "/request"
	strToSign := "HMAC-SHA256\n" + dt + "\n" + credScope + "\n" + hexSHA256(canonicalRequest)

	kDate := hmacSHA256([]byte(date), []byte(v.sk))
	kRegion := hmacSHA256([]byte(volcRegion), kDate)
	kService := hmacSHA256([]byte(volcService), kRegion)
	kSigning := hmacSHA256([]byte("request"), kService)
	signature := hex.EncodeToString(hmacSHA256([]byte(strToSign), kSigning))

	sessionToken := v.extra["session_token"]
	authHeader := fmt.Sprintf("HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		v.ak, credScope, signedHeaders, signature)
	url := baseURL + "/?" + canonicalQuery.String()

	headers := []string{
		"Content-Type: application/json",
		"X-Date: " + dt,
		"X-Content-Sha256: " + payloadHash,
		"Authorization: " + authHeader,
	}
	if sessionToken != "" {
		headers = append(headers, "X-Security-Session-Token: "+sessionToken)
	}

	var bodyBytes []byte
	if isWrite {
		bodyBytes = []byte(body)
	}
	return &HTTPRequest{Method: method, URL: url, Headers: headers, Body: bodyBytes}, nil
}

// ParseResponse 解析火山引擎响应（对齐 PHP VolcProvider::parseResponse）。
func (v *volcProvider) ParseResponse(action string, code int, resp []byte) (map[string]any, error) {
	var j map[string]any
	if err := json.Unmarshal(resp, &j); err != nil {
		j = map[string]any{}
	}
	if meta, ok := j["ResponseMetadata"].(map[string]any); ok {
		if e, ok := meta["Error"].(map[string]any); ok {
			errorCode := asString(e["Code"])
			if action == "DeleteRecord" && errorCode == "ErrDBNotFound" {
				return map[string]any{}, nil
			}
			return nil, providerErrorf("火山引擎: %s %s", errorCode, asString(e["Message"]))
		}
	}
	result, _ := j["Result"].(map[string]any)
	if result == nil {
		result = map[string]any{}
	}
	return expect(result, code, resp)
}

func (v *volcProvider) call(ctx context.Context, action string, params map[string]any) (map[string]any, error) {
	req, err := v.BuildRequest(action, params)
	if err != nil {
		return nil, err
	}
	code, body, _, err := v.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	return v.ParseResponse(action, code, body)
}

func (v *volcProvider) Ping(ctx context.Context) (PingResult, error) {
	r, err := v.call(ctx, "ListZones", map[string]any{"PageSize": 1})
	if err != nil {
		return PingResult{}, err
	}
	count := asInt(r["TotalCount"])
	if count == 0 {
		count = len(asSlice(r["Zones"]))
	}
	return PingResult{OK: true, Domains: count}, nil
}

func (v *volcProvider) CheckDomain(ctx context.Context, domain string) (CheckDomainResult, error) {
	r, err := v.call(ctx, "CheckDomain", map[string]any{"Key": "domain", "Value": domain})
	if err != nil {
		return CheckDomainResult{}, err
	}
	isLB := true
	if val, ok := r["IsLB"]; ok {
		isLB = asBool(val)
	}
	if !isLB {
		return CheckDomainResult{OK: false, Message: "域名不存在"}, nil
	}
	return CheckDomainResult{OK: true, DomainID: domain}, nil
}

func (v *volcProvider) ListRecords(ctx context.Context, domain string) ([]RemoteRecord, error) {
	zid, err := v.zid(ctx, domain)
	if err != nil {
		return nil, err
	}
	var out []RemoteRecord
	page := 1
	for {
		r, err := v.call(ctx, "ListRecords", map[string]any{"ZID": zid, "PageNumber": page, "PageSize": 100})
		if err != nil {
			return nil, err
		}
		records := asSlice(r["Records"])
		for _, item := range records {
			rec, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, RemoteRecord{
				ID:       asString(rec["RecordID"]),
				Sub:      asString(rec["Host"]),
				Type:     asString(rec["Type"]),
				Line:     LineFrom("volc", asString(rec["Line"])),
				Value:    asString(rec["Value"]),
				TTL:      asInt(rec["TTL"]),
				Priority: intPtrOrNil(rec["Weight"]),
			})
		}
		total := asInt(r["TotalCount"])
		if total == 0 {
			total = len(out)
		}
		page++
		if len(out) >= total || len(records) == 0 {
			break
		}
	}
	return out, nil
}

// zid 查询域名对应的 Zone ID（带缓存，对齐 PHP VolcProvider::zid）。
func (v *volcProvider) zid(ctx context.Context, domain string) (int, error) {
	target := strings.TrimRight(strings.ToLower(domain), ".")

	v.zidMu.Lock()
	defer v.zidMu.Unlock()
	if id, ok := v.zidCache[target]; ok {
		return id, nil
	}

	page := 1
	for {
		r, err := v.call(ctx, "ListZones", map[string]any{"PageNumber": page, "PageSize": 100})
		if err != nil {
			return 0, err
		}
		zones := asSlice(r["Zones"])
		for _, item := range zones {
			z, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name := strings.TrimRight(strings.ToLower(asString(z["ZoneName"])), ".")
			if name == target {
				id := asInt(z["ZID"])
				v.zidCache[target] = id
				return id, nil
			}
		}
		total := asInt(r["TotalCount"])
		if total == 0 {
			total = len(zones)
		}
		page++
		if len(zones) == 0 || (page-1)*100 >= total {
			break
		}
	}
	return 0, providerErrorf("火山引擎: 该账号下不存在域名 %s", domain)
}

func (v *volcProvider) OpParams(op, domain string, rec NormalizedRecord, remoteID *string) (OpCall, error) {
	switch op {
	case "create":
		return OpCall{Action: "CreateRecord", Params: v.writeParams(domain, rec, nil)}, nil
	case "update":
		if remoteID != nil {
			return OpCall{Action: "UpdateRecord", Params: v.writeParams(domain, rec, remoteID)}, nil
		}
		return OpCall{Action: "CreateRecord", Params: v.writeParams(domain, rec, nil)}, nil
	case "delete":
		if remoteID != nil && *remoteID != "" {
			return OpCall{Action: "DeleteRecord", Params: map[string]any{"RecordID": *remoteID}}, nil
		}
		return OpCall{}, providerErrorf("火山引擎: 缺少远端记录 ID")
	default:
		return OpCall{}, fmt.Errorf("未知操作: %s", op)
	}
}

func (v *volcProvider) writeParams(domain string, rec NormalizedRecord, recordID *string) map[string]any {
	p := map[string]any{}
	if recordID != nil {
		p["RecordID"] = *recordID
	} else {
		// 注意：真实场景 zid 由同步引擎在执行时解析；此处延迟到需要时再查，
		// 但 opParams 返回的 params 会被 buildRequest 直接编码，故这里必须立即解析。
		// 为保持签名顺序稳定，此处在无缓存时同步查询。
		zid, _ := v.zid(context.Background(), domain)
		p["ZID"] = zid
	}
	p["Host"] = rec.Sub
	p["Type"] = rec.Type
	val := writeValue(rec.Type, rec.Value)
	if strings.ToUpper(rec.Type) == "MX" && rec.Priority != nil && *rec.Priority != 0 && !mxValuePrefix.MatchString(val) {
		val = strconv.Itoa(*rec.Priority) + " " + val
	}
	p["Value"] = val
	p["TTL"] = writeTtl(rec.TTL)
	p["Line"] = LineTo("volc", rec.Line)
	return p
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		b, _ := strconv.ParseBool(t)
		return b
	}
	return false
}
