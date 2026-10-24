package handlers

import (
	"net/http"

	"github.com/loeia/all-in-dns/internal/httpapi"
)

func (h *Handler) monitorSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, ok := h.auth.RequireUser(ctx, w, r); !ok {
		return
	}
	count := func(sql string) int {
		var c int
		_ = h.db.GetContext(ctx, &c, sql)
		return c
	}
	httpapi.OK(w, map[string]any{
		"domains":          count(`SELECT COUNT(*) c FROM domains`),
		"records":          count(`SELECT COUNT(*) c FROM records`),
		"pending_queue":    count(`SELECT COUNT(*) c FROM sync_queue WHERE status='pending'`),
		"failed_queue":     count(`SELECT COUNT(*) c FROM sync_queue WHERE status='failed'`),
		"failed_sync_24h":  count(`SELECT COUNT(*) c FROM sync_logs WHERE status='failed' AND created_at > DATE_SUB(NOW(), INTERVAL 24 HOUR)`),
		"success_sync_24h": count(`SELECT COUNT(*) c FROM sync_logs WHERE status='success' AND created_at > DATE_SUB(NOW(), INTERVAL 24 HOUR)`),
	})
}
