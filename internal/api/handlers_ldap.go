package api

// handlers_ldap.go：LDAP 认证配置端点（admin，docs/modules/identity.md「LDAP 认证」）。
// 绑定密码只写不读：留空 = 不修改，空串 = 清除库内密码（回退匿名绑定）。

import (
	"net/http"

	"github.com/yuylong/Chalkline/internal/app"
)

// getLDAPConfig 读 LDAP 配置视图（admin；绑定密码永不回显，只报是否已配置）。
func (h *handlers) getLDAPConfig(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().GetLDAPConfig(r.Context(), actor)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type updateLDAPConfigReq struct {
	Enabled          *bool   `json:"enabled"`
	Host             *string `json:"host"`
	Port             *int    `json:"port"`
	TLSMode          *string `json:"tlsMode"`
	TLSSkipVerify    *bool   `json:"tlsSkipVerify"`
	BindDN           *string `json:"bindDn"`
	BindPassword     *string `json:"bindPassword"`
	UserSearchBase   *string `json:"userSearchBase"`
	UserSearchFilter *string `json:"userSearchFilter"`
	TimeoutSeconds   *int    `json:"timeoutSeconds"`
}

func (h *handlers) updateLDAPConfig(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req updateLDAPConfigReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().UpdateLDAPConfig(r.Context(), actor, app.LDAPConfigInput{
		Enabled: req.Enabled, Host: req.Host, Port: req.Port,
		TLSMode: req.TLSMode, TLSSkipVerify: req.TLSSkipVerify,
		BindDN: req.BindDN, BindPassword: req.BindPassword,
		UserSearchBase: req.UserSearchBase, UserSearchFilter: req.UserSearchFilter,
		TimeoutSeconds: req.TimeoutSeconds,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type testLDAPConfigReq struct {
	// Username/Password 可选：都提供时做完整登录验证（检索 + 用户绑定），
	// 否则只测连通性（拨号 + 服务账号绑定 + 试检索）。
	Username string `json:"username"`
	Password string `json:"password"`
}

// testLDAPConfig 测试 LDAP 连通性或完整登录验证（admin）。
// 失败返回可展示的错误信息（拨号/绑定/检索各阶段均有中文提示）。
func (h *handlers) testLDAPConfig(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req testLDAPConfigReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	result, err := h.svc().TestLDAPConfig(r.Context(), actor, req.Username, req.Password)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
