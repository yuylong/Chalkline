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
