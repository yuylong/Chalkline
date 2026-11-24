package api

// handlers_integration.go：集成 provider 配置端点（admin）。
// 凭据（token / API key）不经本接口读写——只管理开关与 baseURL（红线 8）。

import (
	"net/http"

	"github.com/yuylong/Chalkline/internal/app"
)

func (h *handlers) listProviderConfigs(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	items, err := h.svc().ListProviderConfigs(r.Context(), actor)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type updateProviderConfigReq struct {
	Enabled *bool   `json:"enabled"`
	BaseURL *string `json:"baseUrl"`
}

func (h *handlers) updateProviderConfig(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req updateProviderConfigReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().UpdateProviderConfig(r.Context(), actor, pathParam(r, "kind"),
		app.ProviderConfigInput{Enabled: req.Enabled, BaseURL: req.BaseURL})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// getSMTPConfig 读邮箱配置视图（admin；密码永不回显，只报是否已配置）。
func (h *handlers) getSMTPConfig(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().GetSMTPConfig(r.Context(), actor)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type updateSMTPConfigReq struct {
	Enabled  *bool   `json:"enabled"`
	Host     *string `json:"host"`
	Port     *int    `json:"port"`
	Username *string `json:"username"`
	From     *string `json:"from"`
	// Password 只写不读：留空 = 不修改，空串 = 清除库内密码（回退环境变量）。
	Password *string `json:"password"`
}

func (h *handlers) updateSMTPConfig(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req updateSMTPConfigReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().UpdateSMTPConfig(r.Context(), actor, app.SMTPConfigInput{
		Enabled: req.Enabled, Host: req.Host, Port: req.Port,
		Username: req.Username, From: req.From, Password: req.Password,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
