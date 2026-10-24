package handlers

import "testing"

func TestCheckRecordInput(t *testing.T) {
	cases := []struct {
		name    string
		in      map[string]any
		wantSub string
		wantTyp string
		wantErr string
	}{
		{
			name:    "有效 A 记录",
			in:      map[string]any{"sub": "@", "type": "A", "line": "默认", "value": "1.2.3.4", "ttl": 600},
			wantSub: "@", wantTyp: "A",
		},
		{
			name:    "有效 AAAA",
			in:      map[string]any{"sub": "www", "type": "AAAA", "value": "2001:db8::1", "ttl": 3600},
			wantSub: "www", wantTyp: "AAAA",
		},
		{
			name:    "有效 CNAME（带尾点）",
			in:      map[string]any{"sub": "cdn", "type": "CNAME", "value": "example.com.", "ttl": 600},
			wantSub: "cdn", wantTyp: "CNAME",
		},
		{
			name:    "有效 MX（带优先级）",
			in:      map[string]any{"sub": "@", "type": "MX", "value": "mail.example.com", "ttl": 600, "priority": "10"},
			wantSub: "@", wantTyp: "MX",
		},
		{
			name:    "不支持的记录类型",
			in:      map[string]any{"type": "SOA", "value": "x", "ttl": 600},
			wantErr: "不支持的记录类型",
		},
		{
			name:    "记录值为空",
			in:      map[string]any{"type": "A", "value": "", "ttl": 600},
			wantErr: "记录值不能为空",
		},
		{
			name:    "TTL 过小",
			in:      map[string]any{"type": "A", "value": "1.2.3.4", "ttl": 30},
			wantErr: "TTL 需在 60~86400 秒之间",
		},
		{
			name:    "TTL 过大",
			in:      map[string]any{"type": "A", "value": "1.2.3.4", "ttl": 99999},
			wantErr: "TTL 需在 60~86400 秒之间",
		},
		{
			name:    "A 格式错误",
			in:      map[string]any{"type": "A", "value": "1.2.3.4.5", "ttl": 600},
			wantErr: "记录值格式不正确",
		},
		{
			name:    "A 非数字",
			in:      map[string]any{"type": "A", "value": "abc", "ttl": 600},
			wantErr: "记录值格式不正确",
		},
		{
			name:    "A 缺少段",
			in:      map[string]any{"type": "A", "value": "1.2.3", "ttl": 600},
			wantErr: "记录值格式不正确",
		},
		{
			name:    "CNAME 格式错误",
			in:      map[string]any{"type": "CNAME", "value": "not a cname", "ttl": 600},
			wantErr: "记录值格式不正确",
		},
		{
			name:    "MX 缺优先级",
			in:      map[string]any{"type": "MX", "value": "mail.example.com", "ttl": 600},
			wantErr: "MX 优先级不合法",
		},
		{
			name:    "MX 优先级越界",
			in:      map[string]any{"type": "MX", "value": "mail.example.com", "ttl": 600, "priority": 99999},
			wantErr: "MX 优先级不合法",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ri, err := checkRecordInput(c.in)
			if c.wantErr != "" {
				if err != c.wantErr {
					t.Fatalf("want err %q, got %q", c.wantErr, err)
				}
				return
			}
			if err != "" {
				t.Fatalf("unexpected err: %q", err)
			}
			if ri.Sub != c.wantSub {
				t.Errorf("sub = %q, want %q", ri.Sub, c.wantSub)
			}
			if ri.Type != c.wantTyp {
				t.Errorf("type = %q, want %q", ri.Type, c.wantTyp)
			}
		})
	}
}

func TestCheckRecordInputDefaults(t *testing.T) {
	// sub/line 缺省值与 priority 清理
	ri, err := checkRecordInput(map[string]any{"type": "A", "value": "1.2.3.4", "ttl": 600})
	if err != "" {
		t.Fatal(err)
	}
	if ri.Sub != "@" || ri.Line != "默认" {
		t.Fatalf("defaults wrong: %+v", ri)
	}
	// 非 MX 类型 priority 应为 nil
	ri2, _ := checkRecordInput(map[string]any{"type": "A", "value": "1.2.3.4", "ttl": 600, "priority": "5"})
	if ri2.Priority != nil {
		t.Fatalf("non-MX priority should be nil, got %v", *ri2.Priority)
	}
}

func TestCheckRecordInputMXPriorityParsing(t *testing.T) {
	ri, err := checkRecordInput(map[string]any{"type": "MX", "value": "mail.example.com", "ttl": 600, "priority": "20"})
	if err != "" {
		t.Fatal(err)
	}
	if ri.Priority == nil || *ri.Priority != 20 {
		t.Fatalf("priority = %v, want 20", ri.Priority)
	}
}
