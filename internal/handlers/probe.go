package handlers

import (
	"net/http"
	"strings"

	"github.com/loeia/all-in-dns/internal/dnsprobe"
	"github.com/loeia/all-in-dns/internal/httpapi"
)

// probeDomain 检测域名解析在各公共 DNS 的实际生效情况（多地域探测）。
func (h *Handler) probeDomain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, ok := h.auth.RequireUser(ctx, w, r); !ok {
		return
	}
	id := intFromPath(r, "id")

	var domainName string
	if err := h.db.GetContext(ctx, &domainName, `SELECT name FROM domains WHERE id=?`, id); err != nil || domainName == "" {
		httpapi.Fail(w, http.StatusNotFound, "域名不存在")
		return
	}

	// 只探测默认线路（公共 DNS 无法指定线路）
	type localRec struct {
		SubDomain string `db:"sub_domain"`
		Type      string `db:"type"`
		Value     string `db:"value"`
	}
	var recs []localRec
	err := h.db.SelectContext(ctx, &recs, `
		SELECT sub_domain, type, value FROM records
		WHERE domain_id=? AND status='active' AND line='默认'
		ORDER BY type, sub_domain`, id)
	if err != nil {
		httpapi.Error(w, err)
		return
	}

	type key struct{ sub, typ string }
	groups := map[key][]string{}
	order := make([]key, 0)
	for _, rec := range recs {
		k := key{strings.ToLower(rec.SubDomain), strings.ToUpper(rec.Type)}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], rec.Value)
	}

	// 限制最多探测 20 条，避免过慢
	if len(order) > 20 {
		order = order[:20]
	}

	items := make([]map[string]any, 0, len(order))
	for _, k := range order {
		fqdn := dnsprobe.FQDN(k.sub, domainName)
		local := groups[k]
		results := dnsprobe.Probe(ctx, fqdn, k.typ, dnsprobe.DefaultEndpoints)
		items = append(items, map[string]any{
			"sub":     k.sub,
			"type":    k.typ,
			"fqdn":    fqdn,
			"local":   local,
			"results": results,
			"verdict": probeVerdict(local, results, k.typ),
		})
	}

	httpapi.OK(w, map[string]any{
		"domain": domainName,
		"items":  items,
	})
}

// probeVerdict 对比本地值与各探测点结果，给出结论。
func probeVerdict(local []string, results []dnsprobe.Result, typ string) string {
	want := make([]string, 0, len(local))
	for _, v := range local {
		want = append(want, normalizeValue(typ, v))
	}

	anyOK := false
	allMatch := true
	for _, res := range results {
		switch res.Status {
		case "ok":
			anyOK = true
			got := make([]string, 0, len(res.Records))
			for _, v := range res.Records {
				got = append(got, normalizeValue(typ, v))
			}
			for _, wv := range want {
				if !contains(got, wv) {
					allMatch = false
					break
				}
			}
		case "nxdomain":
			allMatch = false
		}
	}

	if !anyOK {
		for _, res := range results {
			if res.Status == "nxdomain" {
				return "nxdomain"
			}
		}
		return "error"
	}
	if allMatch {
		return "ok"
	}
	return "mismatch"
}

// normalizeValue 归一化记录值，便于跨来源对比。
func normalizeValue(typ, v string) string {
	v = strings.TrimSpace(v)
	switch strings.ToUpper(typ) {
	case "MX":
		// 探测值形如 "10 mail.example.com."，本地存的是去掉优先级的域名
		if i := strings.Index(v, " "); i > 0 {
			v = v[i+1:]
		}
		v = strings.TrimSuffix(v, ".")
		return strings.ToLower(v)
	case "CNAME", "NS", "SRV":
		return strings.ToLower(strings.TrimSuffix(v, "."))
	case "TXT":
		return strings.Trim(v, "\"")
	case "A", "AAAA":
		return strings.ToLower(v)
	}
	return v
}
