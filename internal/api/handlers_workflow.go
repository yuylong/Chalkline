package api

// handlers_workflow.go：工作流模板、实例查询与节点操作端点
//（docs/modules/workflow.md REST 表 + 与 MCP 工具同一套用例与错误码）。

import (
	"net/http"

	"github.com/yuylong/Chalkline/internal/app"
	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

func (h *handlers) listWorkflows(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var scope *workitem.Scope
	if s := r.URL.Query().Get("scope"); s != "" {
		sc := workitem.Scope(s)
		if !sc.Valid() {
			writeAppError(w, app.Invalid("scope %q 非法", s))
			return
		}
		scope = &sc
	}
	items, err := h.svc().ListWorkflows(r.Context(), actor, scope)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type createWorkflowReq struct {
	YAML string `json:"yaml"`
}

func (h *handlers) createWorkflow(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req createWorkflowReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().CreateWorkflow(r.Context(), actor, req.YAML)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (h *handlers) getWorkflow(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().GetWorkflow(r.Context(), actor, pathParam(r, "id"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *handlers) updateWorkflow(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req createWorkflowReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().UpdateWorkflow(r.Context(), actor, pathParam(r, "id"), req.YAML)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *handlers) archiveWorkflow(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if err := h.svc().ArchiveWorkflow(r.Context(), actor, pathParam(r, "id")); err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "archived"})
}

func (h *handlers) taskWorkflow(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().GetInstanceViewByWorkItem(r.Context(), actor, workitem.ScopeTask, pathParam(r, "id"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *handlers) iterationWorkflow(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().GetInstanceViewByWorkItem(r.Context(), actor, workitem.ScopeIteration, pathParam(r, "id"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *handlers) pendingReviews(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	items, err := h.svc().ListPendingReviews(r.Context(), actor)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ---- 节点操作（REST 等价入口；MCP 工具调同一用例）----

type startStepReq struct {
	NodeID          string `json:"nodeId"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

func (h *handlers) taskStartStep(w http.ResponseWriter, r *http.Request) {
	h.startStep(w, r, workitem.ScopeTask)
}

func (h *handlers) iterationStartStep(w http.ResponseWriter, r *http.Request) {
	h.startStep(w, r, workitem.ScopeIteration)
}

func (h *handlers) startStep(w http.ResponseWriter, r *http.Request, wtype workitem.Scope) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req startStepReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	res, err := h.svc().StartStep(r.Context(), actor, wtype, pathParam(r, "id"), req.NodeID, req.ExpectedVersion)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type artifactDTO struct {
	Kind       string `json:"kind"`
	Provider    string `json:"provider"`
	URL        string `json:"url,omitempty"`
	ExternalID string `json:"externalId,omitempty"`
	Title      string `json:"title,omitempty"`
	Content    string `json:"content,omitempty"`
}

type submitStepReq struct {
	NodeID          string        `json:"nodeId"`
	ExpectedVersion int64         `json:"expectedVersion"`
	Artifacts       []artifactDTO `json:"artifacts"`
	Note            string        `json:"note"`
}

func toArtifactInputs(in []artifactDTO) []app.ArtifactInput {
	out := make([]app.ArtifactInput, 0, len(in))
	for _, a := range in {
		out = append(out, app.ArtifactInput(a))
	}
	return out
}

func (h *handlers) taskSubmitStep(w http.ResponseWriter, r *http.Request) {
	h.submitStep(w, r, workitem.ScopeTask)
}

func (h *handlers) iterationSubmitStep(w http.ResponseWriter, r *http.Request) {
	h.submitStep(w, r, workitem.ScopeIteration)
}

func (h *handlers) submitStep(w http.ResponseWriter, r *http.Request, wtype workitem.Scope) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req submitStepReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	res, err := h.svc().SubmitStep(r.Context(), actor, wtype, pathParam(r, "id"), req.NodeID,
		req.ExpectedVersion, toArtifactInputs(req.Artifacts), req.Note)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type reviewStepReq struct {
	NodeID          string `json:"nodeId"`
	Decision        string `json:"decision"` // approve / reject
	Comment         string `json:"comment"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

func (h *handlers) taskReviewStep(w http.ResponseWriter, r *http.Request) {
	h.reviewStep(w, r, workitem.ScopeTask)
}

func (h *handlers) iterationReviewStep(w http.ResponseWriter, r *http.Request) {
	h.reviewStep(w, r, workitem.ScopeIteration)
}

func (h *handlers) reviewStep(w http.ResponseWriter, r *http.Request, wtype workitem.Scope) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req reviewStepReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	decision := workitem.Decision(req.Decision)
	if !decision.Valid() {
		writeAppError(w, app.Invalid("决策 %q 非法，应为 approve / reject", req.Decision))
		return
	}
	res, err := h.svc().ReviewStep(r.Context(), actor, wtype, pathParam(r, "id"), req.NodeID,
		decision, req.Comment, req.ExpectedVersion)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type attachArtifactReq struct {
	NodeID   string      `json:"nodeId"`
	Artifact artifactDTO `json:"artifact"`
}

func (h *handlers) taskAttachArtifact(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req attachArtifactReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	res, err := h.svc().AttachArtifact(r.Context(), actor, workitem.ScopeTask, pathParam(r, "id"),
		req.NodeID, app.ArtifactInput(req.Artifact))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *handlers) taskNextAction(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	report, err := h.svc().GetNextAction(r.Context(), actor, workitem.ScopeTask, pathParam(r, "id"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *handlers) iterationNextAction(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	report, err := h.svc().GetNextAction(r.Context(), actor, workitem.ScopeIteration, pathParam(r, "id"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
