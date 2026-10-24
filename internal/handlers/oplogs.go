package handlers

import (
	"net/http"

	"github.com/loeia/all-in-dns/internal/httpapi"
)

type opLogRow struct {
	ID        int     `db:"id" json:"id"`
	UserID    *int    `db:"user_id" json:"user_id"`
	Username  *string `db:"username" json:"username"`
	Action    string  `db:"action" json:"action"`
	Target    *string `db:"target" json:"target"`
	Detail    *string `db:"detail" json:"detail"`
	IP        *string `db:"ip" json:"ip"`
	CreatedAt string  `db:"created_at" json:"created_at"`
}

func (h *Handler) listOpLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.RequireRole(r.Context(), w, r, "admin"); !ok {
		return
	}
	rows := make([]opLogRow, 0)
	if err := h.db.SelectContext(r.Context(), &rows,
		`SELECT * FROM operation_logs ORDER BY id DESC LIMIT 200`); err != nil {
		httpapi.Error(w, err)
		return
	}
	httpapi.OK(w, rows)
}

func (h *Handler) clearOpLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin")
	if !ok {
		return
	}
	res, err := h.db.ExecContext(ctx, `DELETE FROM operation_logs`)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	n, _ := res.RowsAffected()
	h.logger.Op(ctx, &u.ID, u.Username, "oplog.clear", "", map[string]any{"rows": n}, httpapi.IP(r))
	httpapi.OK(w, nil)
}
