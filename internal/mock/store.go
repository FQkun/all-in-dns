// Package mock 提供内存版 API 实现，供前端独立开发与联调用。
// 数据字段与真实后端响应结构保持一致，前端可据此开发；真实后端完成后按
// 相同契约替换（见 internal/handlers）。
package mock

import (
	"sync"
	"time"
)

// Store 内存数据仓库。所有字段导出以便 handler 直接读写。
type Store struct {
	mu sync.RWMutex

	Users    []User
	Accounts []Account
	Domains  []Domain
	Records  []Record
	SyncLogs []SyncLog
	OpLogs   []OpLog
	Backups  []Backup
	Queue    []QueueJob

	nextID int
}

// User 用户。
type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	LastLoginAt string `json:"last_login_at"`
}

// Account 云账号（列表展示字段，AK/SK 掩码）。
type Account struct {
	ID                 int    `json:"id"`
	Provider           string `json:"provider"`
	Name               string `json:"name"`
	Status             int    `json:"status"`
	CreatedAt          string `json:"created_at"`
	AccessKeyIDMasked  string `json:"access_key_id_masked"`
}

// Domain 域名。
type Domain struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Remark    string `json:"remark"`
	CreatedAt string `json:"created_at"`
	Providers string `json:"providers"` // "tencent:on,aliyun:off,volc:on"
}

// Record 解析记录。
type Record struct {
	ID         int    `json:"id"`
	DomainID   int    `json:"domain_id"`
	SubDomain  string `json:"sub_domain"`
	Type       string `json:"type"`
	Line       string `json:"line"`
	Value      string `json:"value"`
	TTL        int    `json:"ttl"`
	Priority   *int   `json:"priority"`
	Remark     string `json:"remark"`
	Status     string `json:"status"`
	UpdatedAt  string `json:"updated_at"`
	SyncStates string `json:"sync_states"` // "1|tencent|success,2|aliyun|failed"
}

// SyncLog 同步日志。
type SyncLog struct {
	ID            int    `json:"id"`
	RecordID      *int   `json:"record_id"`
	AccountID     int    `json:"account_id"`
	Provider      string `json:"provider"`
	Action        string `json:"action"`
	Status        string `json:"status"`
	Message       string `json:"message"`
	RequestCostMs int    `json:"request_cost_ms"`
	CreatedAt     string `json:"created_at"`
	P             string `json:"p"`
	AccountName   string `json:"account_name"`
}

// OpLog 操作日志。
type OpLog struct {
	ID        int    `json:"id"`
	UserID    *int   `json:"user_id"`
	Username  string `json:"username"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Detail    string `json:"detail"`
	IP        string `json:"ip"`
	CreatedAt string `json:"created_at"`
}

// Backup 备份。
type Backup struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	RecordCount int    `json:"record_count"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
}

// QueueJob 同步队列任务。
type QueueJob struct {
	ID          int    `json:"id"`
	RecordID    int    `json:"record_id"`
	AccountID   int    `json:"account_id"`
	Action      string `json:"action"`
	Payload     string `json:"payload"`
	Status      string `json:"status"`
	RetryCount  int    `json:"retry_count"`
	LastError   string `json:"last_error"`
	CreatedAt   string `json:"created_at"`
	ExecutedAt  *string `json:"executed_at"`
	NextRetryAt *string `json:"next_retry_at"`
	Provider    string `json:"provider"`
}

// NewStore 构造并填充一份演示数据，覆盖前端所有视图的典型渲染场景。
func NewStore() *Store {
	s := &Store{nextID: 1000}
	now := time.Now().Format("2006-01-02 15:04:05")

	s.Users = []User{
		{ID: 1, Username: "admin", Role: "admin", LastLoginAt: now},
		{ID: 2, Username: "operator", Role: "operator", LastLoginAt: now},
	}

	s.Accounts = []Account{
		{ID: 1, Provider: "tencent", Name: "腾讯云主账号", Status: 1, CreatedAt: now, AccessKeyIDMasked: "****"},
		{ID: 2, Provider: "aliyun", Name: "阿里云主账号", Status: 1, CreatedAt: now, AccessKeyIDMasked: "****"},
		{ID: 3, Provider: "volc", Name: "火山引擎主账号", Status: 1, CreatedAt: now, AccessKeyIDMasked: "****"},
	}

	s.Domains = []Domain{
		{ID: 1, Name: "example.com", Status: "active", Remark: "演示域名", CreatedAt: now, Providers: "tencent:on,aliyun:on,volc:on"},
		{ID: 2, Name: "demo.org", Status: "active", Remark: "", CreatedAt: now, Providers: "tencent:on,aliyun:off"},
	}

	p1, p2, p3 := 10, 20, 0
	s.Records = []Record{
		{ID: 1, DomainID: 1, SubDomain: "@", Type: "A", Line: "默认", Value: "1.2.3.4", TTL: 600, Priority: nil, Status: "active", UpdatedAt: now, SyncStates: "1|tencent|success,2|aliyun|success,3|volc|success"},
		{ID: 2, DomainID: 1, SubDomain: "www", Type: "CNAME", Line: "默认", Value: "example.com", TTL: 600, Priority: nil, Status: "active", UpdatedAt: now, SyncStates: "1|tencent|success,2|aliyun|failed"},
		{ID: 3, DomainID: 1, SubDomain: "@", Type: "MX", Line: "默认", Value: "mail.example.com", TTL: 600, Priority: &p1, Status: "active", UpdatedAt: now, SyncStates: "1|tencent|success,2|aliyun|success,3|volc|success"},
		{ID: 4, DomainID: 1, SubDomain: "@", Type: "TXT", Line: "默认", Value: "v=spf1 include:spf.example.com -all", TTL: 600, Priority: nil, Status: "active", UpdatedAt: now, SyncStates: "1|tencent|success"},
		{ID: 5, DomainID: 2, SubDomain: "@", Type: "A", Line: "默认", Value: "5.6.7.8", TTL: 600, Priority: nil, Status: "active", UpdatedAt: now, SyncStates: "1|tencent|success"},
		{ID: 6, DomainID: 2, SubDomain: "api", Type: "A", Line: "电信", Value: "9.9.9.9", TTL: 600, Priority: &p2, Status: "disabled", UpdatedAt: now, SyncStates: "1|tencent|failed"},
		{ID: 7, DomainID: 2, SubDomain: "mail", Type: "CNAME", Line: "默认", Value: "mail.demo.org", TTL: 600, Priority: &p3, Status: "active", UpdatedAt: now, SyncStates: ""},
	}

	s.SyncLogs = []SyncLog{
		{ID: 1, RecordID: intPtr(1), AccountID: 1, Provider: "tencent", Action: "create", Status: "success", Message: "", RequestCostMs: 182, CreatedAt: now, P: "tencent", AccountName: "腾讯云主账号"},
		{ID: 2, RecordID: intPtr(2), AccountID: 2, Provider: "aliyun", Action: "create", Status: "failed", Message: "阿里云: DomainRecordDuplicate", RequestCostMs: 240, CreatedAt: now, P: "aliyun", AccountName: "阿里云主账号"},
		{ID: 3, RecordID: intPtr(3), AccountID: 3, Provider: "volc", Action: "update", Status: "success", Message: "", RequestCostMs: 155, CreatedAt: now, P: "volc", AccountName: "火山引擎主账号"},
	}

	s.OpLogs = []OpLog{
		{ID: 1, UserID: intPtr(1), Username: "admin", Action: "login", Target: "", Detail: "", IP: "127.0.0.1", CreatedAt: now},
		{ID: 2, UserID: intPtr(1), Username: "admin", Action: "domain.create", Target: "example.com", Detail: "", IP: "127.0.0.1", CreatedAt: now},
		{ID: 3, UserID: intPtr(1), Username: "admin", Action: "record.create", Target: "3", Detail: "{\"sub\":\"@\",\"type\":\"MX\"}", IP: "127.0.0.1", CreatedAt: now},
	}

	s.Backups = []Backup{
		{ID: 1, Name: "自动备份 " + now[:10], RecordCount: 7, CreatedBy: "system", CreatedAt: now},
		{ID: 2, Name: "手动备份 " + now[:10], RecordCount: 7, CreatedBy: "admin", CreatedAt: now},
	}

	next := now
	s.Queue = []QueueJob{
		{ID: 1, RecordID: 2, AccountID: 2, Action: "create", Payload: `{"domain":"example.com","record":{"sub":"www","type":"CNAME","value":"example.com"}}`, Status: "failed", RetryCount: 3, LastError: "阿里云: DomainRecordDuplicate", CreatedAt: now, NextRetryAt: &next, Provider: "aliyun"},
		{ID: 2, RecordID: 6, AccountID: 1, Action: "delete", Payload: `{"domain":"demo.org","record":{"sub":"api","type":"A","value":"9.9.9.9"}}`, Status: "pending", RetryCount: 0, CreatedAt: now, Provider: "tencent"},
	}

	return s
}

func (s *Store) next() int {
	s.nextID++
	return s.nextID
}

func intPtr(i int) *int { return &i }

func nowStr() string { return time.Now().Format("2006-01-02 15:04:05") }
