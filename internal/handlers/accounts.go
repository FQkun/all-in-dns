package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jmoiron/sqlx"

	"github.com/loeia/all-in-dns/internal/crypto"
	"github.com/loeia/all-in-dns/internal/httpapi"
)

type accountRow struct {
	ID                int    `db:"id" json:"id"`
	Provider          string `db:"provider" json:"provider"`
	Name              string `db:"name" json:"name"`
	Status            int    `db:"status" json:"status"`
	CreatedAt         string `db:"created_at" json:"created_at"`
	AccessKeyIDMasked string `json:"access_key_id_masked"`
}

func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.RequireUser(r.Context(), w, r); !ok {
		return
	}
	rows := make([]accountRow, 0)
	if err := h.db.SelectContext(r.Context(), &rows,
		`SELECT id,provider,name,status,created_at FROM cloud_accounts ORDER BY id`); err != nil {
		httpapi.Error(w, err)
		return
	}
	for i := range rows {
		rows[i].AccessKeyIDMasked = "****"
	}
	httpapi.OK(w, rows)
}

func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin")
	if !ok {
		return
	}
	in := httpapi.Input(r)
	providerName := str(in["provider"])
	if !contains([]string{"tencent", "aliyun", "volc", "spaceship"}, providerName) {
		httpapi.Fail(w, http.StatusBadRequest, "无效的云厂商")
		return
	}
	ak := str(in["access_key_id"])
	sk := str(in["access_key_secret"])
	if ak == "" || sk == "" {
		httpapi.Fail(w, http.StatusBadRequest, "AK/SK 不能为空")
		return
	}

	extra := map[string]string{}
	if em, ok := in["extra"].(map[string]any); ok {
		if v, ok := em["session_token"].(string); ok && v != "" {
			enc, err := crypto.Encrypt(h.cfg.App.SecretKey, v)
			if err != nil {
				httpapi.Error(w, err)
				return
			}
			extra["session_token"] = enc
		}
		if v, ok := em["region"].(string); ok {
			extra["region"] = v
		}
	}
	extraJSON, _ := json.Marshal(extra)

	encAK, err := crypto.Encrypt(h.cfg.App.SecretKey, ak)
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	encSK, err := crypto.Encrypt(h.cfg.App.SecretKey, sk)
	if err != nil {
		httpapi.Error(w, err)
		return
	}

	name := str(in["name"])
	if name == "" {
		name = providerName
	}
	_, err = h.db.ExecContext(ctx,
		`INSERT INTO cloud_accounts (provider,name,access_key_id,access_key_secret,extra) VALUES (?,?,?,?,?)`,
		providerName, name, encAK, encSK, string(extraJSON))
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	h.logger.Op(ctx, &u.ID, u.Username, "account.create", providerName, nil, httpapi.IP(r))
	httpapi.OK(w, nil)
}

func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := h.auth.RequireRole(ctx, w, r, "admin")
	if !ok {
		return
	}
	id := intFromPath(r, "id")
	err := h.db.InTx(func(tx *sqlx.Tx) error {
		for _, sql := range []string{
			`DELETE FROM sync_queue WHERE account_id=?`,
			`DELETE FROM record_provider_map WHERE account_id=?`,
			`DELETE FROM domain_providers WHERE account_id=?`,
			`DELETE FROM cloud_accounts WHERE id=?`,
		} {
			if _, err := tx.ExecContext(ctx, sql, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		httpapi.Error(w, err)
		return
	}
	h.logger.Op(ctx, &u.ID, u.Username, "account.delete", strconv.Itoa(id), nil, httpapi.IP(r))
	httpapi.OK(w, nil)
}

func (h *Handler) testAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, ok := h.auth.RequireRole(ctx, w, r, "admin", "operator"); !ok {
		return
	}
	id := intFromPath(r, "id")
	var providerName string
	err := h.db.GetContext(ctx, &providerName, `SELECT provider FROM cloud_accounts WHERE id=?`, id)
	if err != nil {
		httpapi.Fail(w, http.StatusNotFound, "账号不存在")
		return
	}
	prov, err := h.sync.MakeProvider(ctx, id)
	if err != nil {
		httpapi.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := prov.Ping(ctx)
	if err != nil {
		httpapi.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	httpapi.OK(w, res)
}
