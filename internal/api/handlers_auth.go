package api

// handlers_auth.go：登录、PAT、用户管理端点（docs/modules/identity.md REST 表）。

import (
	"net/http"

	"github.com/yuylong/Chalkline/internal/app"
	"github.com/yuylong/Chalkline/internal/domain/identity"
)

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	result, err := h.svc().Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *handlers) me(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().Whoami(r.Context(), actor)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type createPATReq struct {
	Name    string `json:"name"`
	TTLDays int    `json:"ttlDays"`
}

func (h *handlers) createPAT(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req createPATReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	created, err := h.svc().CreatePAT(r.Context(), actor, req.Name, req.TTLDays)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *handlers) listPATs(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	pats, err := h.svc().ListPATs(r.Context(), actor)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": pats})
}

func (h *handlers) revokePAT(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if err := h.svc().RevokePAT(r.Context(), actor, pathParam(r, "id")); err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

type createUserReq struct {
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Email       string   `json:"email"`
	Password    string   `json:"password"`
	Roles       []string `json:"roles"`
}

func (h *handlers) createUser(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req createUserReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	roles, err := parseRoles(req.Roles)
	if err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().CreateUser(r.Context(), actor, app.CreateUserInput{
		Username: req.Username, DisplayName: req.DisplayName, Email: req.Email,
		Password: req.Password, Roles: roles,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (h *handlers) listUsers(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	users, err := h.svc().ListUsers(r.Context(), actor)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": users})
}

func (h *handlers) getUser(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().GetUser(r.Context(), actor, pathParam(r, "id"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type updateUserReq struct {
	Roles  []string `json:"roles"`
	Status string   `json:"status"`
}

func (h *handlers) updateUser(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req updateUserReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	roles, err := parseRoles(req.Roles)
	if err != nil {
		writeAppError(w, err)
		return
	}
	status := identity.UserStatus(req.Status)
	if !status.Valid() {
		writeAppError(w, app.Invalid("状态 %q 非法", req.Status))
		return
	}
	view, err := h.svc().UpdateUser(r.Context(), actor, pathParam(r, "id"), roles, status)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type setPasswordReq struct {
	Password string `json:"password"`
}

func (h *handlers) setUserPassword(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req setPasswordReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	if err := h.svc().SetPassword(r.Context(), actor, pathParam(r, "id"), req.Password); err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
