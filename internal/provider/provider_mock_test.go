package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTencentCheckDomainParam 锁定 CheckDomain 必须传 Domain 参数（曾误用 Name）。
func TestTencentCheckDomainParam(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Response":{"DomainInfo":{"Id":123}}}`))
	}))
	defer server.Close()

	p := newTencent("ak", "sk", nil)
	p.baseURL = server.URL

	res, err := p.CheckDomain(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, `"Domain":"example.com"`) {
		t.Fatalf("CheckDomain 请求体应包含 Domain 参数，got: %s", gotBody)
	}
	if strings.Contains(gotBody, `"Name"`) {
		t.Fatalf("CheckDomain 不应再传 Name 参数，got: %s", gotBody)
	}
	if res.OK != true || res.DomainID != "123" {
		t.Fatalf("CheckDomain 结果异常: %+v", res)
	}
}

// TestAliyunCheckDomainParam 锁定阿里云 CheckDomain 用 DomainName 参数。
func TestAliyunCheckDomainParam(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"DomainId":"456"}`))
	}))
	defer server.Close()

	p := newAliyun("ak", "sk", nil)
	p.baseURL = server.URL

	res, err := p.CheckDomain(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "DomainName=example.com") {
		t.Fatalf("CheckDomain 应包含 DomainName=example.com，got: %s", gotQuery)
	}
	if res.DomainID != "456" {
		t.Fatalf("DomainID = %s", res.DomainID)
	}
}

// TestVolcCheckDomainParam 锁定火山引擎 CheckDomain 用 Key=domain 参数。
func TestVolcCheckDomainParam(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Result":{"IsLB":true}}`))
	}))
	defer server.Close()

	p := newVolc("ak", "sk", nil)
	p.baseURL = server.URL

	res, err := p.CheckDomain(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "Key=domain") || !strings.Contains(gotQuery, "Value=example.com") {
		t.Fatalf("CheckDomain 应包含 Key=domain & Value=example.com，got: %s", gotQuery)
	}
	if res.OK != true || res.DomainID != "example.com" {
		t.Fatalf("CheckDomain 结果异常: %+v", res)
	}
}
