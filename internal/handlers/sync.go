package handlers

import (
	"net/http"

	"github.com/loeia/all-in-dns/internal/httpapi"
)

type syncLogRow struct {
	ID            int     `db:"id" json:"id"`
	RecordID      *int    `db:"record_id" json:"record_id"`
	AccountID     int     `db:"account_id" json:"account_id"`
	Provider      string  `db:"provider" json:"provider"`
	Action        string  `db:"action" json:"action"`
	Status        string  `db:"status" json:"status"`
	Message       *string `db:"message" json:"message"`
	RequestCostMs *int    `db:"request_cost_ms" json:"request_cost_ms"`
	CreatedAt     string  `db:"created_at" json:"created_at"`
	P             string  `db:"p" json:"p"`
	AccountName   string  `db:"account_name" json:"account_name"`
}

func (h *Handler) listSyncLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.RequireUser(r.Context(), w, r); !ok {
		return
	}
	rows := make([]syncLogRow, 0)
	err := h.db.SelectContext(r.Context(), &rows, `
		SELECT l.*, ca.provider AS p, ca.name AS account_name FROM sync_logs l
		JOIN cloud_accounts ca ON ca.id = l.account_id ORDER BY l.id DESC LIMIT 200`)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	httpapi.OK(w, rows)
}

func (h *Handler) clearSyncLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(ctx, `DELETE FROM sync_logs`)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	n, _ := res.RowsAffected()
	h.logger.Op(ctx, &u.ID, u.Username, "synclog.clear", "", map[string]any{"rows": n}, httpapi.IP(r))
	httpapi.OK(w, nil)
}

type queueRow struct {
	ID          int     `db:"id" json:"id"`
	RecordID    int     `db:"record_id" json:"record_id"`
	AccountID   int     `db:"account_id" json:"account_id"`
	Action      string  `db:"action" json:"action"`
	Payload     string  `db:"payload" json:"payload"`
	Status      string  `db:"status" json:"status"`
	RetryCount  int     `db:"retry_count" json:"retry_count"`
	LastError   *string `db:"last_error" json:"last_error"`
	CreatedAt   string  `db:"created_at" json:"created_at"`
	ExecutedAt  *string `db:"executed_at" json:"executed_at"`
	NextRetryAt *string `db:"next_retry_at" json:"next_retry_at"`
	Provider    string  `db:"provider" json:"provider"`
}

func (h *Handler) listSyncQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.RequireUser(r.Context(), w, r); !ok {
		return
	}
	rows := make([]queueRow, 0)
	err := h.db.SelectContext(r.Context(), &rows, `
		SELECT q.*, ca.provider FROM sync_queue q JOIN cloud_accounts ca ON ca.id=q.account_id
		WHERE q.status IN ('pending','failed') ORDER BY q.id DESC LIMIT 100`)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	httpapi.OK(w, rows)
}

func (h *Handler) retryNow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, ok := h.auth.RequireRole(ctx, w, r, "admin", "operator"); !ok {
		return
	}
	_, _ = h.db.ExecContext(ctx,
		`UPDATE sync_queue SET status='pending', retry_count=0, next_retry_at=NULL WHERE status IN ('pending','failed')`)
	done, err := h.sync.ProcessPendingQueue(ctx, 100)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	httpapi.OK(w, map[string]any{"count": len(done), "items": done})
}

func (h *Handler) clearSyncQueue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin", "operator")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(ctx, `DELETE FROM sync_queue WHERE status IN ('pending','failed')`)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	n, _ := res.RowsAffected()
	h.logger.Op(ctx, &u.ID, u.Username, "syncqueue.clear", "", map[string]any{"rows": n}, httpapi.IP(r))
	httpapi.OK(w, map[string]any{"count": n})
}
