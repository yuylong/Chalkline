package api

// handlers_catalog.go：产品、成员、模块、文档端点（docs/modules/catalog.md REST 表）。

import (
	"net/http"

	"github.com/yuylong/Chalkline/internal/app"
	"github.com/yuylong/Chalkline/internal/domain/product"
)

type createProductReq struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (h *handlers) createProduct(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req createProductReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().CreateProduct(r.Context(), actor, app.CreateProductInput{
		Key: req.Key, Name: req.Name, Description: req.Description,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (h *handlers) listProducts(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	// M3 规模化：搜索 + 状态过滤 + 分页（mcp.md 列表分页约定）。
	in := app.ProductListQuery{
		Q:      r.URL.Query().Get("q"),
		Status: r.URL.Query().Get("status"),
		Limit:  queryInt(r, "limit", 0),
		Offset: queryInt(r, "offset", 0),
	}
	result, err := h.svc().ListProducts(r.Context(), actor, in)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *handlers) getOverview(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().GetOverview(r.Context(), actor)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *handlers) getProduct(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().GetProduct(r.Context(), actor, pathParam(r, "id"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type updateProductReq struct {
	Name                       *string           `json:"name"`
	Description                *string           `json:"description"`
	DefaultIterationWorkflowID *string           `json:"defaultIterationWorkflowId"`
	DefaultTaskWorkflowID      *string           `json:"defaultTaskWorkflowId"`
	// TaskWorkflowDefaults 按任务类型的默认任务流模板（kind → workflowId）；
	// 非 nil 即整组替换，空对象 = 清空（全部回退产品默认）。
	TaskWorkflowDefaults map[string]string `json:"taskWorkflowDefaults"`
	Archive              bool              `json:"archive"`
}

func (h *handlers) updateProduct(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req updateProductReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	var defaults *map[string]string
	if req.TaskWorkflowDefaults != nil {
		defaults = &req.TaskWorkflowDefaults
	}
	view, err := h.svc().UpdateProduct(r.Context(), actor, pathParam(r, "id"), app.ProductUpdateInput{
		Name: req.Name, Description: req.Description,
		DefaultIterationWorkflowID: req.DefaultIterationWorkflowID,
		DefaultTaskWorkflowID:      req.DefaultTaskWorkflowID,
		TaskWorkflowDefaults:       defaults,
		Archive:                    req.Archive,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *handlers) listMembers(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	items, err := h.svc().ListMembers(r.Context(), actor, pathParam(r, "id"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// listMemberCandidates 列可加入产品的成员（下拉候选）：产品 owner / admin。
func (h *handlers) listMemberCandidates(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	items, err := h.svc().ListMemberCandidates(r.Context(), actor, pathParam(r, "id"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type upsertMemberReq struct {
	UserID string `json:"userId"`
	Role   string `json:"role"`
}

func (h *handlers) upsertMember(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req upsertMemberReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	role := product.MemberRole(req.Role)
	if !role.Valid() {
		writeAppError(w, app.Invalid("产品角色 %q 非法", req.Role))
		return
	}
	if err := h.svc().UpsertMember(r.Context(), actor, pathParam(r, "id"), req.UserID, role); err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handlers) removeMember(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if err := h.svc().RemoveMember(r.Context(), actor, pathParam(r, "id"), pathParam(r, "uid")); err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handlers) listModules(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	items, err := h.svc().ListModules(r.Context(), actor, pathParam(r, "id"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type createModuleReq struct {
	ParentID  string `json:"parentId"`
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
}

func (h *handlers) createModule(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req createModuleReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().CreateModule(r.Context(), actor, pathParam(r, "id"), req.ParentID, req.Name, req.SortOrder)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

type updateModuleReq struct {
	Name     *string `json:"name"`
	ParentID *string `json:"parentId"`
}

func (h *handlers) updateModule(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req updateModuleReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().UpdateModule(r.Context(), actor, pathParam(r, "id"), pathParam(r, "mid"),
		app.UpdateModuleInput{Name: req.Name, ParentID: req.ParentID})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type upsertDocumentReq struct {
	Content string `json:"content"`
	Version int    `json:"version"`
}

func (h *handlers) upsertDocument(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req upsertDocumentReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().UpsertDocument(r.Context(), actor, app.UpsertDocumentInput{
		OwnerType: pathParam(r, "ownerType"), OwnerID: pathParam(r, "ownerID"),
		Content: req.Content, Version: req.Version,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *handlers) getDocument(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().GetDocument(r.Context(), actor, pathParam(r, "ownerType"), pathParam(r, "ownerID"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
