package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOK(t *testing.T) {
	w := httptest.NewRecorder()
	OK(w, map[string]any{"a": 1})
	if w.Code != 200 {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"code":0`) {
		t.Fatalf("body = %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"data":{"a":1}`) {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestFail(t *testing.T) {
	w := httptest.NewRecorder()
	Fail(w, http.StatusBadRequest, "参数错误")
	if w.Code != 400 {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"code":400`) || !strings.Contains(w.Body.String(), "参数错误") {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestIP(t *testing.T) {
	cases := []struct {
		name    string
		header  map[string]string
		remote  string
		want    string
	}{
		{"X-Real-IP 优先", map[string]string{"X-Real-Ip": "1.2.3.4"}, "5.6.7.8:80", "1.2.3.4"},
		{"X-Forwarded-For 取首个", map[string]string{"X-Forwarded-For": "9.9.9.9, 10.0.0.1"}, "5.6.7.8:80", "9.9.9.9"},
		{"回退 RemoteAddr", map[string]string{}, "8.8.8.8:123", "8.8.8.8"},
		{"非法 IP 忽略", map[string]string{"X-Real-Ip": "not-an-ip"}, "8.8.8.8:123", "8.8.8.8"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			for k, v := range c.header {
				r.Header.Set(k, v)
			}
			r.RemoteAddr = c.remote
			if got := IP(r); got != c.want {
				t.Fatalf("IP = %q, want %q", got, c.want)
			}
		})
	}
}

func TestInput(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"test","n":1}`))
	r.Header.Set("Content-Type", "application/json")
	in := Input(r)
	if in["name"] != "test" {
		t.Fatalf("input = %+v", in)
	}
	if int(in["n"].(float64)) != 1 {
		t.Fatalf("n = %v", in["n"])
	}
}

func TestNoCache(t *testing.T) {
	var called bool
	h := NoCache(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if !called {
		t.Fatal("handler not called")
	}
	if w.Header().Get("Cache-Control") != "no-store, no-cache, must-revalidate, max-age=0" {
		t.Fatalf("Cache-Control = %q", w.Header().Get("Cache-Control"))
	}
}

func TestRecover(t *testing.T) {
	h := Recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "服务器内部错误") {
		t.Fatalf("body = %s", w.Body.String())
	}
}
