// Package sync 实现同步引擎，对齐原 PHP 版 SyncService。
package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/loeia/all-in-dns/internal/database"
	"github.com/loeia/all-in-dns/internal/logger"
	"github.com/loeia/all-in-dns/internal/provider"
)

// Service 同步引擎。
type Service struct {
	db        *database.DB
	logger    *logger.Logger
	secretKey string
	maxRetry  int
	retryBase int
}

// New 构造 Service。
func New(db *database.DB, lg *logger.Logger, secretKey string, maxRetry, retryBase int) *Service {
	if maxRetry <= 0 {
		maxRetry = 5
	}
	if retryBase <= 0 {
		retryBase = 60
	}
	return &Service{db: db, logger: lg, secretKey: secretKey, maxRetry: maxRetry, retryBase: retryBase}
}

// Result 单个队列任务的执行结果。
type Result struct {
	QueueID   int    `json:"queue_id"`
	Provider  string `json:"provider,omitempty"`
	Status    string `json:"status"` // success | failed
	Message   string `json:"message,omitempty"`
	WillRetry bool   `json:"will_retry,omitempty"`
}

// recordRow 记录 + 域名名（对齐 getRecord 查询）。
type recordRow struct {
	ID        int    `db:"id"`
	DomainID  int    `db:"domain_id"`
	Domain    string `db:"domain"`
	SubDomain string `db:"sub_domain"`
	Type      string `db:"type"`
	Line      string `db:"line"`
	Value     string `db:"value"`
	TTL       int    `db:"ttl"`
	Priority  *int   `db:"priority"`
	Status    string `db:"status"`
}

// queueJob 队列任务（对齐 executeBatch 查询）。
type queueJob struct {
	ID         int    `db:"id"`
	RecordID   int    `db:"record_id"`
	AccountID  int    `db:"account_id"`
	Action     string `db:"action"`
	Payload    string `db:"payload"`
	Status     string `db:"status"`
	RetryCount int    `db:"retry_count"`
	Provider   string `db:"provider"`
}

// payload 队列任务负载（对齐 dispatch 写入的 JSON）。
type payload struct {
	Domain string                    `json:"domain"`
	Record provider.NormalizedRecord `json:"record"`
}

func parsePayload(raw string) payload {
	var p payload
	_ = json.Unmarshal([]byte(raw), &p)
	return p
}

// normalizeRecord 归一化记录（对齐 SyncService::normalizeRecord）。
func normalizeRecord(r *recordRow) provider.NormalizedRecord {
	sub := r.SubDomain
	if sub == "" {
		sub = "@"
	}
	line := r.Line
	if line == "" {
		line = "默认"
	}
	return provider.NormalizedRecord{
		Sub:      sub,
		Type:     r.Type,
		Line:     line,
		Value:    r.Value,
		TTL:      r.TTL,
		Priority: r.Priority,
	}
}

// getRecord 查询记录（含域名），不存在返回 nil。
func (s *Service) getRecord(ctx context.Context, recordID int) (*recordRow, error) {
	var rec recordRow
	err := s.db.GetContext(ctx, &rec,
		`SELECT r.id, r.domain_id, r.sub_domain, r.type, r.line, r.value, r.ttl, r.priority, r.status, d.name AS domain
		 FROM records r JOIN domains d ON d.id = r.domain_id WHERE r.id = ?`,
		recordID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// MakeProvider 按账号 ID 解密构造 Provider（对齐 ProviderFactory::makeById）。
func (s *Service) MakeProvider(ctx context.Context, accountID int) (provider.Provider, error) {
	var acc provider.Account
	err := s.db.GetContext(ctx, &acc,
		`SELECT provider, access_key_id, access_key_secret, extra FROM cloud_accounts WHERE id = ?`,
		accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("云账号不存在: %d", accountID)
	}
	if err != nil {
		return nil, err
	}
	return provider.New(acc, s.secretKey)
}

// markMap 写入/更新记录与云厂商的映射（对齐 SyncService::markMap）。
func (s *Service) markMap(ctx context.Context, recordID, accountID int, providerName, status, msg string, providerRecordID *string) {
	var pid any
	if providerRecordID != nil {
		pid = *providerRecordID
	}
	_, _ = s.db.ExecContext(ctx, `
		INSERT INTO record_provider_map (record_id,account_id,provider,provider_record_id,sync_status,sync_message,synced_at,retry_count)
		VALUES (?,?,?,?,?,?,NOW(),0)
		ON DUPLICATE KEY UPDATE
			sync_status=VALUES(sync_status),
			sync_message=VALUES(sync_message),
			synced_at=IF(VALUES(sync_status)='success' OR VALUES(sync_status)='deleted', NOW(), synced_at),
			provider_record_id=IF(VALUES(provider_record_id) IS NOT NULL, VALUES(provider_record_id), provider_record_id)`,
		recordID, accountID, providerName, pid, status, truncateRunes(msg, 500))
}

// getRemoteId 查询远端记录 ID（对齐 SyncService::getRemoteId）。
func (s *Service) getRemoteID(ctx context.Context, recordID, accountID int) *string {
	var id *string
	err := s.db.GetContext(ctx, &id,
		`SELECT provider_record_id FROM record_provider_map WHERE record_id=? AND account_id=?`,
		recordID, accountID)
	if err != nil {
		return nil
	}
	if id != nil && *id == "" {
		return nil
	}
	return id
}

// isDuplicateError 识别「远端已存在同记录」类错误（对齐 SyncService::isDuplicateError）。
func (s *Service) isDuplicateError(msg string) bool {
	return strings.Contains(msg, "ErrDBDuplicated") ||
		strings.Contains(msg, "DomainRecordDuplicate") ||
		strings.Contains(msg, "ResourceAlreadyExists") ||
		strings.Contains(msg, "SubdomainRollLimit") ||
		strings.Contains(msg, "already have duplicate record")
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
