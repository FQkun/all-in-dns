package provider

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

// ---- JSON 解析辅助（云厂商响应多为 map[string]any） ----

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return fmt.Sprintf("%v", v)
}

func asInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, _ := strconv.Atoi(t)
		return n
	}
	return 0
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

// intPtrOrNil 把 JSON 数字/字符串转为 *int，空值返回 nil（对应 PHP `$v ?? null`）。
func intPtrOrNil(v any) *int {
	if v == nil {
		return nil
	}
	n := asInt(v)
	if n == 0 {
		return nil
	}
	return &n
}

// ---- 签名辅助 ----

func hexSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(data, key []byte) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write(data)
	return h.Sum(nil)
}

func hmacSHA1(data, key []byte) []byte {
	h := hmac.New(sha1.New, key)
	_, _ = h.Write(data)
	return h.Sum(nil)
}
