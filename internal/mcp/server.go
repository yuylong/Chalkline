// Package mcp 是编码 Agent 接入平台的唯一入口：streamable HTTP + PAT 鉴权。
// 工具只做协议转换——参数校验在领域层，业务规则不得写在这里（AGENT.md 红线 3）。
// 错误统一为 {"error":{"code","message","details"}} 包络，作为工具结果文本返回
// （MCP 协议内业务错误经 CallToolResult.IsError 承载；传输层 401 由鉴权中间件负责）。
package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yuylong/Chalkline/internal/app"
	"github.com/yuylong/Chalkline/internal/auth"
	"github.com/yuylong/Chalkline/internal/domain/planning"
	"github.com/yuylong/Chalkline/internal/domain/product"
	"github.com/yuylong/Chalkline/internal/domain/workitem"
)

// Deps 是 MCP 网关依赖。
type Deps struct {
	Logger *slog.Logger
	Svc    func() app.API
}

// NewHandler 组装 MCP streamable HTTP handler（stateless：每请求独立会话，PAT 每次校验）。
func NewHandler(d Deps) http.Handler {
	s := mcp.NewServer(&mcp.Implementation{Name: "chalkline", Version: "m1"}, nil)
	registerTools(s, d)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s },
		&mcp.StreamableHTTPOptions{Stateless: true})
}

// ---- 公共小件 ----

func (d Deps) svc(ctx context.Context) (app.API, app.Actor, error) {
	actor, ok := auth.ActorFrom(ctx)
	if !ok {
		return nil, app.Actor{}, app.Unauthorized("MCP 请求未通过 PAT 鉴权")
	}
	svc := d.Svc()
	if svc == nil {
		return nil, app.Actor{}, &app.Error{Status: http.StatusServiceUnavailable,
			Code: "SERVICE_UNAVAILABLE", Message: "服务尚未就绪"}
	}
	return svc, actor, nil
}

// appErrResult 把 app 错误转为统一包络文本的工具错误结果（IsError=true）。
func appErrResult(err error) *mcp.CallToolResult {
	aerr := app.AsError(err)
	errBody := map[string]any{"code": aerr.Code, "message": aerr.Message}
	if details := aerr.GetDetails(); len(details) > 0 {
		errBody["details"] = details
	}
	body, _ := json.Marshal(map[string]any{"error": errBody})
	res := &mcp.CallToolResult{}
	res.Content = []mcp.Content{&mcp.TextContent{Text: string(body)}}
	res.SetError(err) // IsError=true；Content 已填充则保留
	return res
}

// fail 快捷返回鉴权/服务错误结果。
func fail(err error) (*mcp.CallToolResult, any, error) { return appErrResult(err), nil, nil }

// ---- 工具入参 DTO（json tag 即 MCP 输入 schema 属性名）----

type workItemRef struct {
	WorkItemType string `json:"workItemType"` // task | iteration
	WorkItemID   string `json:"workItemId"`
}

func (r workItemRef) scope() (workitem.Scope, error) {
	switch r.WorkItemType {
	case "task":
		return workitem.ScopeTask, nil
	case "iteration":
		return workitem.ScopeIteration, nil
	}
	return "", app.Invalid("workItemType %q 非法，应为 task / iteration", r.WorkItemType)
}

type artifactArg struct {
	Kind       string `json:"kind"`
	Provider   string `json:"provider"`
	URL        string `json:"url,omitempty"`
	ExternalID string `json:"externalId,omitempty"`
	Title      string `json:"title,omitempty"`
	Content    string `json:"content,omitempty"`
}

func (a artifactArg) toInput() app.ArtifactInput {
	return app.ArtifactInput{Kind: a.Kind, Provider: a.Provider, URL: a.URL,
		ExternalID: a.ExternalID, Title: a.Title, Content: a.Content}
}

// ---- 读工具 ----

func registerTools(s *mcp.Server, d Deps) {
	d.registerReadTools(s)
	d.registerWriteTools(s)
}

func (d Deps) registerReadTools(s *mcp.Server) {
	mcp.AddTool[struct{}, any](s, &mcp.Tool{
		Name:        "chalkline_whoami",
		Description: "当前身份、全局角色与可见产品。轮询起点之一。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		out, err := svc.Whoami(ctx, actor)
		if err != nil {
			return fail(err)
		}
		return nil, out, nil
	})

	mcp.AddTool[struct {
		Q      string `json:"q,omitempty"`
		Status string `json:"status,omitempty"`
		Limit  int    `json:"limit,omitempty"`
		Offset int    `json:"offset,omitempty"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_list_products",
		Description: "列出你有权访问的产品（成员关系过滤；admin 全量），支持搜索与分页。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		Q      string `json:"q,omitempty"`
		Status string `json:"status,omitempty"`
		Limit  int    `json:"limit,omitempty"`
		Offset int    `json:"offset,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		out, err := svc.ListProducts(ctx, actor, app.ProductListQuery{
			Q: in.Q, Status: in.Status, Limit: in.Limit, Offset: in.Offset,
		})
		if err != nil {
			return fail(err)
		}
		return nil, out, nil
	})

	mcp.AddTool[struct {
		ProductID string `json:"productId"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_get_product",
		Description: "产品详情：模块树、成员、产品文档、默认工作流。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ProductID string `json:"productId"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		out, err := svc.GetProduct(ctx, actor, in.ProductID)
		if err != nil {
			return fail(err)
		}
		return nil, out, nil
	})

	mcp.AddTool[struct {
		ProductID string `json:"productId"`
		Status    string `json:"status,omitempty"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_list_iterations",
		Description: "迭代列表，可按产品与状态过滤。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ProductID string `json:"productId"`
		Status    string `json:"status,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		var status *planning.IterationStatus
		if in.Status != "" {
			st := planning.IterationStatus(in.Status)
			if !st.Terminal() && st != planning.IterationPlanned && st != planning.IterationActive {
				return fail(app.Invalid("状态 %q 非法", in.Status))
			}
			status = &st
		}
		out, err := svc.ListIterations(ctx, actor, in.ProductID, status)
		if err != nil {
			return fail(err)
		}
		return nil, map[string]any{"items": out}, nil
	})

	mcp.AddTool[struct {
		ProductID   string `json:"productId,omitempty"`
		IterationID string `json:"iterationId,omitempty"`
		AssigneeID  string `json:"assigneeId,omitempty"`
		ModuleID    string `json:"moduleId,omitempty"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_list_tasks",
		Description: "任务列表，可按迭代、模块、负责人、产品过滤（至少一项）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ProductID   string `json:"productId,omitempty"`
		IterationID string `json:"iterationId,omitempty"`
		AssigneeID  string `json:"assigneeId,omitempty"`
		ModuleID    string `json:"moduleId,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		out, err := svc.ListTasks(ctx, actor, app.TaskFilter{
			ProductID: in.ProductID, IterationID: in.IterationID,
			AssigneeID: in.AssigneeID, ModuleID: in.ModuleID,
		})
		if err != nil {
			return fail(err)
		}
		return nil, map[string]any{"items": out}, nil
	})

	mcp.AddTool[struct {
		TaskID string `json:"taskId"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_get_task",
		Description: "任务详情：子树、关联模块、工作流节点状态、材料与审批。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		TaskID string `json:"taskId"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		out, err := svc.GetTask(ctx, actor, in.TaskID)
		if err != nil {
			return fail(err)
		}
		return nil, out, nil
	})

	mcp.AddTool[workItemRef, any](s, &mcp.Tool{
		Name:        "chalkline_get_next_action",
		Description: "当前工作项下一步与阻塞原因。并行分支返回多节点；blocked 时按 blockers 逐条处理，不得跳过。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in workItemRef) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		scope, err := in.scope()
		if err != nil {
			return fail(err)
		}
		out, err := svc.GetNextAction(ctx, actor, scope, in.WorkItemID)
		if err != nil {
			return fail(err)
		}
		return nil, out, nil
	})

	mcp.AddTool[struct {
		WorkflowID string `json:"workflowId"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_get_workflow",
		Description: "模板定义 + 说明。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		WorkflowID string `json:"workflowId"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		out, err := svc.GetWorkflow(ctx, actor, in.WorkflowID)
		if err != nil {
			return fail(err)
		}
		return nil, out, nil
	})

	mcp.AddTool[struct {
		Scope string `json:"scope,omitempty"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_list_workflows",
		Description: "可用模板列表（按 scope 过滤，含内置模板）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		Scope string `json:"scope,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		var scope *workitem.Scope
		if in.Scope != "" {
			sc := workitem.Scope(in.Scope)
			if !sc.Valid() {
				return fail(app.Invalid("scope %q 非法", in.Scope))
			}
			scope = &sc
		}
		out, err := svc.ListWorkflows(ctx, actor, scope)
		if err != nil {
			return fail(err)
		}
		return nil, map[string]any{"items": out}, nil
	})

	mcp.AddTool[struct{}, any](s, &mcp.Tool{
		Name:        "chalkline_get_pending_reviews",
		Description: "我是评审人、待我确认的门禁清单。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		out, err := svc.ListPendingReviews(ctx, actor)
		if err != nil {
			return fail(err)
		}
		return nil, map[string]any{"items": out}, nil
	})

	mcp.AddTool[struct {
		ProductID string `json:"productId"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_list_product_members",
		Description: "产品成员与各自的产品角色（owner / member / guest）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ProductID string `json:"productId"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		out, err := svc.ListMembers(ctx, actor, in.ProductID)
		if err != nil {
			return fail(err)
		}
		return nil, map[string]any{"items": out}, nil
	})
}

// ---- 写工具 ----

func (d Deps) registerWriteTools(s *mcp.Server) {
	mcp.AddTool[struct {
		ProductID  string `json:"productId"`
		Name       string `json:"name"`
		Goal       string `json:"goal,omitempty"`
		StartsOn   string `json:"startsOn"`
		EndsOn     string `json:"endsOn"`
		WorkflowID string `json:"workflowId,omitempty"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_create_iteration",
		Description: "建迭代（一条新生命周期），返回服务端分配的 <key>-IT<n> 编号并实例化生命周期工作流。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ProductID  string `json:"productId"`
		Name       string `json:"name"`
		Goal       string `json:"goal,omitempty"`
		StartsOn   string `json:"startsOn"`
		EndsOn     string `json:"endsOn"`
		WorkflowID string `json:"workflowId,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		starts, err := parseDate(in.StartsOn)
		if err != nil {
			return fail(err)
		}
		ends, err := parseDate(in.EndsOn)
		if err != nil {
			return fail(err)
		}
		out, err := svc.CreateIteration(ctx, actor, in.ProductID, app.CreateIterationInput{
			Name: in.Name, Goal: in.Goal, StartsOn: starts, EndsOn: ends, WorkflowID: in.WorkflowID,
		})
		if err != nil {
			return fail(err)
		}
		return nil, out, nil
	})

	mcp.AddTool[taskCreateArg, any](s, &mcp.Tool{
		Name:        "chalkline_create_task",
		Description: "建任务（parentTaskId 非空即拆解）：编号由服务端分配并返回；只能在 allows_task_creation 的迭代阶段调用。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in taskCreateArg) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		out, err := svc.CreateTask(ctx, actor, in.toInput())
		if err != nil {
			return fail(err)
		}
		return nil, out, nil
	})

	mcp.AddTool[struct {
		TaskID   string          `json:"taskId"`
		Children []taskCreateArg `json:"children"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_decompose_task",
		Description: "批量拆解为子任务，返回 <parent>.m 形式的编号；子任务继承父任务模板与模块。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		TaskID   string          `json:"taskId"`
		Children []taskCreateArg `json:"children"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		children := make([]app.CreateTaskInput, 0, len(in.Children))
		for _, c := range in.Children {
			ci := c.toInput()
			ci.ParentTaskID = in.TaskID
			children = append(children, ci)
		}
		out, err := svc.DecomposeTask(ctx, actor, in.TaskID, children)
		if err != nil {
			return fail(err)
		}
		return nil, map[string]any{"items": out}, nil
	})

	mcp.AddTool[struct {
		TaskID     string `json:"taskId"`
		AssigneeID string `json:"assigneeId"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_assign_task",
		Description: "指派负责人（唯一负责人；空串表示待认领）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		TaskID     string `json:"taskId"`
		AssigneeID string `json:"assigneeId"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		assignee := in.AssigneeID
		out, err := svc.UpdateTask(ctx, actor, in.TaskID, app.TaskUpdateInput{AssigneeID: &assignee})
		if err != nil {
			return fail(err)
		}
		return nil, out, nil
	})

	mcp.AddTool[struct {
		TaskID   string `json:"taskId"`
		ModuleID string `json:"moduleId"`
		Add      bool   `json:"add"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_link_module",
		Description: "增删任务与模块的关联（模块必须与任务同产品）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		TaskID   string `json:"taskId"`
		ModuleID string `json:"moduleId"`
		Add      bool   `json:"add"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		if err := svc.LinkModule(ctx, actor, in.TaskID, in.ModuleID, in.Add); err != nil {
			return fail(err)
		}
		return nil, map[string]string{"status": "ok"}, nil
	})

	mcp.AddTool[struct {
		workItemRef
		NodeID          string `json:"nodeId"`
		ExpectedVersion int64  `json:"expectedVersion,omitempty"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_start_step",
		Description: "开始当前可执行节点（ready → in_progress）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		workItemRef
		NodeID          string `json:"nodeId"`
		ExpectedVersion int64  `json:"expectedVersion,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		scope, err := in.scope()
		if err != nil {
			return fail(err)
		}
		out, err := svc.StartStep(ctx, actor, scope, in.WorkItemID, in.NodeID, in.ExpectedVersion)
		if err != nil {
			return fail(err)
		}
		return nil, out, nil
	})

	mcp.AddTool[struct {
		workItemRef
		NodeID          string        `json:"nodeId"`
		Artifacts       []artifactArg `json:"artifacts"`
		Note            string        `json:"note,omitempty"`
		ExpectedVersion int64         `json:"expectedVersion,omitempty"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_submit_step",
		Description: "提交节点，携带材料与说明；缺材料 422、门禁不足进入 awaiting_review。重复提交同节点返回当前状态（幂等）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		workItemRef
		NodeID          string        `json:"nodeId"`
		Artifacts       []artifactArg `json:"artifacts"`
		Note            string        `json:"note,omitempty"`
		ExpectedVersion int64         `json:"expectedVersion,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		scope, err := in.scope()
		if err != nil {
			return fail(err)
		}
		artifacts := make([]app.ArtifactInput, 0, len(in.Artifacts))
		for _, a := range in.Artifacts {
			artifacts = append(artifacts, a.toInput())
		}
		out, err := svc.SubmitStep(ctx, actor, scope, in.WorkItemID, in.NodeID, in.ExpectedVersion, artifacts, in.Note)
		if err != nil {
			return fail(err)
		}
		return nil, out, nil
	})

	mcp.AddTool[struct {
		workItemRef
		NodeID   string      `json:"nodeId"`
		Artifact artifactArg `json:"artifact"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_attach_artifact",
		Description: "挂接材料引用：provider + url/externalId，或 inline 内容。替换已进入评审的材料会重置该节点审批收集。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		workItemRef
		NodeID   string      `json:"nodeId"`
		Artifact artifactArg `json:"artifact"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		scope, err := in.scope()
		if err != nil {
			return fail(err)
		}
		out, err := svc.AttachArtifact(ctx, actor, scope, in.WorkItemID, in.NodeID, in.Artifact.toInput())
		if err != nil {
			return fail(err)
		}
		return nil, out, nil
	})

	mcp.AddTool[struct {
		workItemRef
		NodeID          string `json:"nodeId"`
		Decision        string `json:"decision"`
		Comment         string `json:"comment,omitempty"`
		ExpectedVersion int64  `json:"expectedVersion,omitempty"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_review_step",
		Description: "门禁确认或驳回（approve / reject；驳回必填理由）。需持门禁 roles 之一，admin 恒可；提交人本人为合格评审人时可自审。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		workItemRef
		NodeID          string `json:"nodeId"`
		Decision        string `json:"decision"`
		Comment         string `json:"comment,omitempty"`
		ExpectedVersion int64  `json:"expectedVersion,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		scope, err := in.scope()
		if err != nil {
			return fail(err)
		}
		decision := workitem.Decision(in.Decision)
		if !decision.Valid() {
			return fail(app.Invalid("决策 %q 非法，应为 approve / reject", in.Decision))
		}
		out, err := svc.ReviewStep(ctx, actor, scope, in.WorkItemID, in.NodeID, decision, in.Comment, in.ExpectedVersion)
		if err != nil {
			return fail(err)
		}
		return nil, out, nil
	})

	mcp.AddTool[struct {
		ProductID string `json:"productId"`
		UserID    string `json:"userId"`
		Role      string `json:"role"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_upsert_product_member",
		Description: "加入成员或调整产品角色（owner / member / guest）。需产品 owner 或 admin。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ProductID string `json:"productId"`
		UserID    string `json:"userId"`
		Role      string `json:"role"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		role := product.MemberRole(in.Role)
		if !role.Valid() {
			return fail(app.Invalid("产品角色 %q 非法", in.Role))
		}
		if err := svc.UpsertMember(ctx, actor, in.ProductID, in.UserID, role); err != nil {
			return fail(err)
		}
		return nil, map[string]string{"status": "ok"}, nil
	})

	mcp.AddTool[struct {
		ProductID string `json:"productId"`
		UserID    string `json:"userId"`
	}, any](s, &mcp.Tool{
		Name:        "chalkline_remove_product_member",
		Description: "移除成员；要求其未终结任务已转派，且产品至少保留一个 owner。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ProductID string `json:"productId"`
		UserID    string `json:"userId"`
	}) (*mcp.CallToolResult, any, error) {
		svc, actor, err := d.svc(ctx)
		if err != nil {
			return fail(err)
		}
		if err := svc.RemoveMember(ctx, actor, in.ProductID, in.UserID); err != nil {
			return fail(err)
		}
		return nil, map[string]string{"status": "ok"}, nil
	})
}

// taskCreateArg 是 create_task / decompose_task 共用的任务入参。
type taskCreateArg struct {
	IterationID  string   `json:"iterationId,omitempty"`
	ParentTaskID string   `json:"parentTaskId,omitempty"`
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	Priority     string   `json:"priority,omitempty"`
	AssigneeID   string   `json:"assigneeId,omitempty"`
	ModuleIDs    []string `json:"moduleIds,omitempty"`
	WorkflowID   string   `json:"workflowId,omitempty"`
}

func (a taskCreateArg) toInput() app.CreateTaskInput {
	kind := planning.TaskKind(a.Kind)
	if kind == "" {
		kind = planning.KindDev
	}
	priority := planning.Priority(a.Priority)
	if priority == "" {
		priority = planning.PriorityNormal
	}
	return app.CreateTaskInput{
		IterationID: a.IterationID, ParentTaskID: a.ParentTaskID,
		Title: a.Title, Description: a.Description,
		Kind: kind, Priority: priority, AssigneeID: a.AssigneeID,
		ModuleIDs: a.ModuleIDs, WorkflowID: a.WorkflowID,
	}
}

// parseDate 解析 YYYY-MM-DD。
func parseDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, app.Invalid("日期 %q 需为 YYYY-MM-DD", s)
	}
	return t, nil
}
