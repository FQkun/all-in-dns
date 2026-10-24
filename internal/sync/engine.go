package sync

import (
	"context"
	"strings"
	"encoding/json"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/loeia/all-in-dns/internal/provider"
)

// Dispatch 将一条记录的变更扇出到所有启用同步的云厂商（对齐 SyncService::dispatch）。
func (s *Service) Dispatch(ctx context.Context, recordID int, action string) ([]Result, error) {
	rec, err := s.getRecord(ctx, recordID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("记录不存在")
	}

	var accounts []struct {
		AccountID int `db:"account_id"`
	}
	err = s.db.SelectContext(ctx, &accounts, `
		SELECT dp.account_id FROM domain_providers dp
		JOIN cloud_accounts ca ON ca.id = dp.account_id AND ca.status = 1
		WHERE dp.domain_id = ? AND dp.sync_enabled = 1`, rec.DomainID)
	if err != nil {
		return nil, err
	}

	payloadJSON, err := json.Marshal(map[string]any{
		"domain": rec.Domain,
		"record": normalizeRecord(rec),
	})
	if err != nil {
		return nil, err
	}

	var queueIDs []int
	for _, acc := range accounts {
		res, err := s.db.ExecContext(ctx,
			`INSERT INTO sync_queue (record_id,account_id,action,payload) VALUES (?,?,?,?)`,
			recordID, acc.AccountID, action, string(payloadJSON))
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()
		queueIDs = append(queueIDs, int(id))
	}
	return s.executeBatch(ctx, queueIDs)
}

// DispatchMany 批量扇出（对齐 SyncService::dispatchMany）。
func (s *Service) DispatchMany(ctx context.Context, recordIDs []int, action string) ([]Result, error) {
	var queueIDs []int
	for _, rid := range recordIDs {
		rec, err := s.getRecord(ctx, rid)
		if err != nil {
			return nil, err
		}
		if rec == nil {
			continue
		}
		var accounts []struct {
			AccountID int `db:"account_id"`
		}
		err = s.db.SelectContext(ctx, &accounts, `
			SELECT dp.account_id FROM domain_providers dp
			JOIN cloud_accounts ca ON ca.id = dp.account_id AND ca.status = 1
			WHERE dp.domain_id = ? AND dp.sync_enabled = 1`, rec.DomainID)
		if err != nil {
			return nil, err
		}
		payloadJSON, _ := json.Marshal(map[string]any{
			"domain": rec.Domain,
			"record": normalizeRecord(rec),
		})
		for _, acc := range accounts {
			res, err := s.db.ExecContext(ctx,
				`INSERT INTO sync_queue (record_id,account_id,action,payload) VALUES (?,?,?,?)`,
				rid, acc.AccountID, action, string(payloadJSON))
			if err != nil {
				return nil, err
			}
			id, _ := res.LastInsertId()
			queueIDs = append(queueIDs, int(id))
		}
	}
	return s.executeBatch(ctx, queueIDs)
}

// ProcessPendingQueue 重试待处理任务（对齐 SyncService::processPendingQueue）。
func (s *Service) ProcessPendingQueue(ctx context.Context, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 50
	}
	_, _ = s.db.ExecContext(ctx, `
		UPDATE sync_queue SET status='pending'
		WHERE status='processing' AND executed_at IS NULL AND created_at < DATE_SUB(NOW(), INTERVAL 10 MINUTE)`)

	var jobs []struct {
		ID int `db:"id"`
	}
	err := s.db.SelectContext(ctx, &jobs, `
		SELECT q.id FROM sync_queue q
		JOIN cloud_accounts ca ON ca.id = q.account_id
		WHERE q.status = 'pending' AND (q.next_retry_at IS NULL OR q.next_retry_at <= NOW())
		ORDER BY q.id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(jobs))
	for _, j := range jobs {
		ids = append(ids, j.ID)
	}
	return s.executeBatch(ctx, ids)
}

// executeBatch 批量并行执行队列任务（对齐 SyncService::executeBatch）。
func (s *Service) executeBatch(ctx context.Context, jobIDs []int) ([]Result, error) {
	jobIDs = compactInts(jobIDs)
	if len(jobIDs) == 0 {
		return []Result{}, nil
	}

	// 查询任务（含 provider）
	query, args, err := sqlx.In(`
		SELECT q.id, q.record_id, q.account_id, q.action, q.payload, q.status, q.retry_count, ca.provider
		FROM sync_queue q JOIN cloud_accounts ca ON ca.id = q.account_id WHERE q.id IN (?)`, jobIDs)
	if err != nil {
		return nil, err
	}
	query = s.db.Rebind(query)
	var rows []queueJob
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	jobMap := map[int]*queueJob{}
	for i := range rows {
		jobMap[rows[i].ID] = &rows[i]
	}

	// 认领任务（pending -> processing）
	for id := range jobMap {
		res, err := s.db.ExecContext(ctx,
			`UPDATE sync_queue SET status='processing' WHERE id=? AND status='pending'`, id)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 0 {
				delete(jobMap, id)
			}
		}
	}

	results := map[int]Result{}
	tasks := []provider.ParallelTask{}
	taskQueueIDs := []int{}
	taskRemoteIDs := []*string{}

	// 跳过记录已删除的任务
	for id, job := range jobMap {
		if job.Action == "delete" {
			continue
		}
		var cnt int
		_ = s.db.GetContext(ctx, &cnt,
			`SELECT COUNT(*) FROM records r JOIN domains d ON d.id=r.domain_id WHERE r.id=?`, job.RecordID)
		if cnt == 0 {
			_, _ = s.db.ExecContext(ctx,
				`UPDATE sync_queue SET status='success', executed_at=NOW(), last_error='记录已删除，跳过同步' WHERE id=?`, id)
			results[id] = Result{QueueID: id, Provider: job.Provider, Status: "success", Message: "记录已删除，跳过"}
			delete(jobMap, id)
		}
	}

	// 组装任务
	for _, id := range jobIDs {
		job, ok := jobMap[id]
		if !ok {
			if _, has := results[id]; !has {
				results[id] = Result{QueueID: id, Status: "failed", Message: "任务不存在"}
			}
			continue
		}
		payload := parsePayload(job.Payload)
		recordID, accountID := job.RecordID, job.AccountID

		prov, err := s.MakeProvider(ctx, accountID)
		if err != nil {
			results[id] = s.failJob(ctx, job, err.Error(), nil)
			delete(jobMap, id)
			continue
		}
		remoteID := s.getRemoteID(ctx, recordID, accountID)
		if job.Action == "update" && remoteID == nil {
			remoteID = s.alignExisting(ctx, accountID, payload)
		}
		if job.Action == "delete" && remoteID == nil {
			found := s.alignExisting(ctx, accountID, payload)
			if found == nil {
				s.markMap(ctx, recordID, accountID, job.Provider, "deleted", "", nil)
				_, _ = s.db.ExecContext(ctx,
					`UPDATE record_provider_map SET provider_record_id=NULL WHERE record_id=? AND account_id=?`,
					recordID, accountID)
				_, _ = s.db.ExecContext(ctx,
					`UPDATE sync_queue SET status='success', executed_at=NOW(), last_error=NULL WHERE id=?`, id)
				s.logger.Sync(ctx, &recordID, accountID, job.Provider, "delete", true, "远端不存在，视为已删除", nil)
				results[id] = Result{QueueID: id, Provider: job.Provider, Status: "success"}
				delete(jobMap, id)
				continue
			}
			remoteID = found
		}

		op, err := prov.OpParams(job.Action, payload.Domain, payload.Record, remoteID)
		if err != nil {
			results[id] = s.failJob(ctx, job, err.Error(), nil)
			delete(jobMap, id)
			continue
		}
		tasks = append(tasks, provider.ParallelTask{Provider: prov, Action: op.Action, Params: op.Params})
		taskQueueIDs = append(taskQueueIDs, id)
		taskRemoteIDs = append(taskRemoteIDs, remoteID)
	}

	// 并发执行
	parallel := provider.CallParallel(ctx, tasks)

	for i, out := range parallel {
		queueID := taskQueueIDs[i]
		job := jobMap[queueID]
		recordID, accountID := job.RecordID, job.AccountID
		costMs := out.CostMs

		if out.Error != "" {
			errMsg := out.Error
			if s.isDuplicateError(errMsg) && job.Action != "delete" {
				remoteID := s.alignExisting(ctx, accountID, parsePayload(job.Payload))
				if remoteID != nil {
					s.markMap(ctx, recordID, accountID, job.Provider, "success", "", remoteID)
					_, _ = s.db.ExecContext(ctx,
						`UPDATE sync_queue SET status='success', executed_at=NOW(), last_error=NULL WHERE id=?`, queueID)
					s.logger.Sync(ctx, &recordID, accountID, job.Provider, job.Action, true, "远端已存在，已自动对齐", &costMs)
					results[queueID] = Result{QueueID: queueID, Provider: job.Provider, Status: "success"}
					continue
				}
			}
			results[queueID] = s.failJob(ctx, job, errMsg, &costMs)
			continue
		}

		// 成功回写
		var newID *string
		switch {
		case job.Action == "delete":
			newID = nil
		case taskRemoteIDs[i] != nil:
			newID = taskRemoteIDs[i]
		default:
			if v, ok := out.Result["RecordId"]; ok {
				s := asString(v)
				newID = &s
			} else if v, ok := out.Result["RecordID"]; ok {
				s := asString(v)
				newID = &s
			}
		}
		status := "success"
		if job.Action == "delete" {
			status = "deleted"
		}
		s.markMap(ctx, recordID, accountID, job.Provider, status, "", newID)
		_, _ = s.db.ExecContext(ctx,
			`UPDATE sync_queue SET status='success', executed_at=NOW(), last_error=NULL WHERE id=?`, queueID)
		s.logger.Sync(ctx, &recordID, accountID, job.Provider, job.Action, true, "", &costMs)
		results[queueID] = Result{QueueID: queueID, Provider: job.Provider, Status: "success"}
	}

	ordered := make([]Result, 0, len(jobIDs))
	for _, id := range jobIDs {
		if r, ok := results[id]; ok {
			ordered = append(ordered, r)
		} else {
			ordered = append(ordered, Result{QueueID: id, Status: "failed", Message: "任务不存在"})
		}
	}
	return ordered, nil
}

// failJob 任务失败处理：指数退避重试（对齐 SyncService::failJob）。
func (s *Service) failJob(ctx context.Context, job *queueJob, message string, costMs *int) Result {
	queueID := job.ID
	recordID := job.RecordID
	accountID := job.AccountID
	retry := job.RetryCount + 1
	failed := retry >= s.maxRetry
	delay := s.retryBase * (1 << (retry - 1))

	status := "pending"
	if failed {
		status = "failed"
	}
	_, _ = s.db.ExecContext(ctx, `
		UPDATE sync_queue SET status=?, retry_count=?, last_error=?, next_retry_at=DATE_ADD(NOW(), INTERVAL ? SECOND) WHERE id=?`,
		status, retry, truncateRunes(message, 500), delay, queueID)
	s.markMap(ctx, recordID, accountID, job.Provider, "failed", message, nil)
	s.logger.Sync(ctx, &recordID, accountID, job.Provider, job.Action, false, message, costMs)

	// 最终失败（不再重试）时才通知，避免重试期间刷屏
	if failed && s.notifyFn != nil {
		p := parsePayload(job.Payload)
		record := strings.TrimSpace(p.Record.Sub + " " + p.Record.Type + " " + p.Record.Value)
		s.notifyFn(ctx, "sync_failed", p.Domain, job.Provider, record, message)
	}

	return Result{QueueID: queueID, Provider: job.Provider, Status: "failed", Message: message, WillRetry: !failed}
}

// alignExisting 在远端按归一化键查找已存在记录（对齐 SyncService::alignExisting）。
func (s *Service) alignExisting(ctx context.Context, accountID int, p payload) *string {
	rec := p.Record
	want := RecKey(rec.Sub, rec.Type, rec.Value, rec.Line)

	prov, err := s.MakeProvider(ctx, accountID)
	if err != nil {
		return nil
	}
	records, err := prov.ListRecords(ctx, p.Domain)
	if err != nil {
		return nil
	}
	for _, rr := range records {
		if RecKey(rr.Sub, rr.Type, rr.Value, rr.Line) == want {
			id := rr.ID
			return &id
		}
	}
	return nil
}

func compactInts(in []int) []int {
	out := make([]int, 0, len(in))
	for _, v := range in {
		if v > 0 {
			out = append(out, v)
		}
	}
	return out
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%g", t)
	case int:
		return fmt.Sprintf("%d", t)
	default:
		return fmt.Sprintf("%v", v)
	}
}
