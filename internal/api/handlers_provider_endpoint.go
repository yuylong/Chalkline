package api

// handlers_provider_endpoint.go：全局 Provider 连接目录与个人凭据端点。
// 只做参数转换与错误映射；权限与校验在 app 层（docs/structure.md）。
// 目录不存任何凭据；个人凭据只写不读（API 永不回显 Key）。

import (
	"net/http"

	"github.com/yuylong/Chalkline/internal/app"
)

// ---- 连接目录（admin 管理；成员可读已启用项）----

func (h *handlers) listProviderEndpoints(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	items, err := h.svc().ListProviderEndpoints(r.Context(), actor)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type createProviderEndpointReq struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	BaseURL string `json:"baseUrl"`
	Note    string `json:"note"`
}

func (h *handlers) createProviderEndpoint(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req createProviderEndpointReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().CreateProviderEndpoint(r.Context(), actor, app.ProviderEndpointInput{
		Kind: req.Kind, Label: req.Label, BaseURL: req.BaseURL, Note: req.Note,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

type updateProviderEndpointReq struct {
	Label   *string `json:"label"`
	BaseURL *string `json:"baseUrl"`
	Note    *string `json:"note"`
	Enabled *bool   `json:"enabled"`
}

func (h *handlers) updateProviderEndpoint(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req updateProviderEndpointReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().UpdateProviderEndpoint(r.Context(), actor, pathParam(r, "id"),
		app.ProviderEndpointUpdateInput{
			Label: req.Label, BaseURL: req.BaseURL, Note: req.Note, Enabled: req.Enabled,
		})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *handlers) deleteProviderEndpoint(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if err := h.svc().DeleteProviderEndpoint(r.Context(), actor, pathParam(r, "id")); err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// ---- 个人凭据（成员自助维护，只写不读）----

func (h *handlers) listMyCredentials(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	items, err := h.svc().ListMyCredentials(r.Context(), actor)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type setMyCredentialReq struct {
	Provider     string `json:"provider"`
	BaseURL      string `json:"baseUrl"`
	AuthType     string `json:"authType"`
	AuthUsername string `json:"authUsername"`
	// AuthSecret 只写不读：留空 = 保持库内已有密文。
	AuthSecret string `json:"authSecret"`
}

func (h *handlers) setMyCredential(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req setMyCredentialReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().SetMyCredential(r.Context(), actor, app.UserCredentialInput{
		Provider: req.Provider, BaseURL: req.BaseURL,
		AuthType: req.AuthType, AuthUsername: req.AuthUsername, AuthSecret: req.AuthSecret,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *handlers) deleteMyCredential(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	// baseURL 经查询参数传递（可含斜杠，避免路径转义歧义）；空 = 默认地址。
	if err := h.svc().DeleteMyCredential(r.Context(), actor,
		pathParam(r, "provider"), r.URL.Query().Get("baseUrl")); err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}
