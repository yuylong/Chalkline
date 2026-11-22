package api

// handlers_planning.go：迭代与任务端点（docs/modules/planning.md REST 表）。

import (
	"net/http"

	"github.com/yuylong/Chalkline/internal/app"
	"github.com/yuylong/Chalkline/internal/domain/planning"
)

type createIterationReq struct {
	Name       string `json:"name"`
	Goal       string `json:"goal"`
	StartsOn   string `json:"startsOn"`
	EndsOn     string `json:"endsOn"`
	WorkflowID string `json:"workflowId"`
}

func (h *handlers) createIteration(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req createIterationReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	starts, err := parseDate(req.StartsOn)
	if err != nil {
		writeAppError(w, err)
		return
	}
	ends, err := parseDate(req.EndsOn)
	if err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().CreateIteration(r.Context(), actor, pathParam(r, "pid"), app.CreateIterationInput{
		Name: req.Name, Goal: req.Goal, StartsOn: starts, EndsOn: ends, WorkflowID: req.WorkflowID,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (h *handlers) listIterations(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var status *planning.IterationStatus
	if s := r.URL.Query().Get("status"); s != "" {
		st := planning.IterationStatus(s)
		if !st.Terminal() && st != planning.IterationPlanned && st != planning.IterationActive {
			writeAppError(w, app.Invalid("状态 %q 非法", s))
			return
		}
		status = &st
	}
	items, err := h.svc().ListIterations(r.Context(), actor, pathParam(r, "pid"), status)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *handlers) getIteration(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().GetIteration(r.Context(), actor, pathParam(r, "id"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type createTaskReq struct {
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Kind         string   `json:"kind"`
	Priority     string   `json:"priority"`
	AssigneeID   string   `json:"assigneeId"`
	ModuleIDs    []string `json:"moduleIds"`
	WorkflowID   string   `json:"workflowId"`
	ParentTaskID string   `json:"parentTaskId"`
}

func toTaskInput(req createTaskReq, iterationID string) (app.CreateTaskInput, error) {
	kind := planning.TaskKind(req.Kind)
	if kind == "" {
		kind = planning.KindDev
	}
	if !kind.Valid() {
		return app.CreateTaskInput{}, app.Invalid("任务类型 %q 非法", req.Kind)
	}
	priority := planning.Priority(req.Priority)
	if priority == "" {
		priority = planning.PriorityNormal
	}
	if !priority.Valid() {
		return app.CreateTaskInput{}, app.Invalid("优先级 %q 非法", req.Priority)
	}
	return app.CreateTaskInput{
		IterationID: iterationID, ParentTaskID: req.ParentTaskID,
		Title: req.Title, Description: req.Description,
		Kind: kind, Priority: priority, AssigneeID: req.AssigneeID,
		ModuleIDs: req.ModuleIDs, WorkflowID: req.WorkflowID,
	}, nil
}

// createTask 建任务（挂在迭代下；parentTaskId 非空即拆解）。
func (h *handlers) createTask(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req createTaskReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	in, err := toTaskInput(req, pathParam(r, "id"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().CreateTask(r.Context(), actor, in)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

type decomposeReq struct {
	Children []createTaskReq `json:"children"`
}

func (h *handlers) decomposeTask(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req decomposeReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	parentID := pathParam(r, "id")
	children := make([]app.CreateTaskInput, 0, len(req.Children))
	for _, c := range req.Children {
		in, err := toTaskInput(c, "")
		if err != nil {
			writeAppError(w, err)
			return
		}
		in.ParentTaskID = parentID
		children = append(children, in)
	}
	items, err := h.svc().DecomposeTask(r.Context(), actor, parentID, children)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"items": items})
}

func (h *handlers) getTask(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	view, err := h.svc().GetTask(r.Context(), actor, pathParam(r, "id"))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *handlers) listTasks(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	q := r.URL.Query()
	f := app.TaskFilter{
		ProductID:   q.Get("productId"),
		IterationID: q.Get("iterationId"),
		AssigneeID:  q.Get("assigneeId"),
		ModuleID:    q.Get("moduleId"),
	}
	items, err := h.svc().ListTasks(r.Context(), actor, f)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type updateTaskReq struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	AssigneeID  *string `json:"assigneeId"`
	Priority    *string `json:"priority"`
}

func (h *handlers) updateTask(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req updateTaskReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	in := app.TaskUpdateInput{Title: req.Title, Description: req.Description, AssigneeID: req.AssigneeID}
	if req.Priority != nil {
		p := planning.Priority(*req.Priority)
		if !p.Valid() {
			writeAppError(w, app.Invalid("优先级 %q 非法", *req.Priority))
			return
		}
		in.Priority = &p
	}
	view, err := h.svc().UpdateTask(r.Context(), actor, pathParam(r, "id"), in)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type linkModuleReq struct {
	ModuleID string `json:"moduleId"`
	Add      bool   `json:"add"`
}

func (h *handlers) linkModule(w http.ResponseWriter, r *http.Request) {
	actor, err := h.actor(r)
	if err != nil {
		writeAppError(w, err)
		return
	}
	var req linkModuleReq
	if err := decode(r, &req); err != nil {
		writeAppError(w, err)
		return
	}
	if req.ModuleID == "" {
		writeAppError(w, app.Invalid("moduleId 不可为空"))
		return
	}
	if err := h.svc().LinkModule(r.Context(), actor, pathParam(r, "id"), req.ModuleID, req.Add); err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
