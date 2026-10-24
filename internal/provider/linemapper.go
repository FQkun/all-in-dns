package provider

import "strings"

// 跨平台解析线路映射，逐项对齐原 PHP 版 LineMapper。
// 统一标准键，再按平台输出各自取值；未登记线路原样透传。

var lineOutput = map[string]map[string]string{
	"default": {"tencent": "默认", "aliyun": "default", "volc": "default"},
	"telecom": {"tencent": "电信", "aliyun": "telecom", "volc": "telecom"},
	"unicom":  {"tencent": "联通", "aliyun": "unicom", "volc": "unicom"},
	"mobile":  {"tencent": "移动", "aliyun": "mobile", "volc": "mobile"},
	"tietong": {"tencent": "铁通", "aliyun": "tietong", "volc": "tietong"},
	"edu":     {"tencent": "教育网", "aliyun": "edu", "volc": "edu"},
	"oversea": {"tencent": "境外", "aliyun": "oversea", "volc": "oversea"},
}

var lineLabel = map[string]string{
	"default": "默认", "telecom": "电信", "unicom": "联通", "mobile": "移动",
	"tietong": "铁通", "edu": "教育网", "oversea": "境外",
}

var lineAlias = map[string]string{
	"默认": "default", "default": "default",
	"电信": "telecom", "telecom": "telecom", "cn_telecom": "telecom",
	"联通": "unicom", "unicom": "unicom", "cn_unicom": "unicom", "cucc": "unicom",
	"移动": "mobile", "mobile": "mobile", "cn_mobile": "mobile", "cmcc": "mobile",
	"铁通": "tietong", "tietong": "tietong",
	"教育网": "edu", "edu": "edu", "cernet": "edu",
	"境外": "oversea", "oversea": "oversea", "overseas": "oversea",
}

// lineKey 返回标准键；未登记返回 ok=false。
func lineKey(line string) (string, bool) {
	l := strings.TrimSpace(line)
	if l == "" {
		return "default", true
	}
	k, ok := lineAlias[strings.ToLower(l)]
	return k, ok
}

// LineTo 转为平台写入值；未登记线路原样透传。
func LineTo(provider, line string) string {
	raw := strings.TrimSpace(line)
	k, ok := lineKey(raw)
	if !ok {
		return raw
	}
	if byProvider, ok := lineOutput[k]; ok {
		if v, ok := byProvider[provider]; ok {
			return v
		}
	}
	return raw
}

// LineFrom 平台值还原为标准中文线路名；未登记线路原样返回。
func LineFrom(provider, line string) string {
	raw := strings.TrimSpace(line)
	if raw == "" {
		return "默认"
	}
	for k, byProvider := range lineOutput {
		if v, ok := byProvider[provider]; ok && v == raw {
			return lineLabel[k]
		}
	}
	if k, ok := lineKey(raw); ok {
		return lineLabel[k]
	}
	return raw
}
