package app

// api.go：API 接口——api（REST）与 mcp 两个入口共用的用例接缝。
// *Service 实现全部方法；协议层只做参数转换与错误码映射（docs/structure.md：
// mcp 与 api 不得互相调用、不得直接访问 store）。

import (
	"context"

	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/domain/planning"
	"github.com/yuylong/Chalkline/internal/domain/product"
	"github.com/yuylong/Chalkline/internal/domain/workitem"
	"github.com/yuylong/Chalkline/internal/workflow"
)

// API 是协议层可见的全部用例。
type API interface {
	// identity
	Login(ctx context.Context, username, password string) (LoginResult, error)
	Whoami(ctx context.Context, actor Actor) (WhoamiView, error)
	CreatePAT(ctx context.Context, actor Actor, name string, ttlDays int) (PATCreated, error)
	ListPATs(ctx context.Context, actor Actor) ([]PATView, error)
	RevokePAT(ctx context.Context, actor Actor, patID string) error
	CreateUser(ctx context.Context, actor Actor, in CreateUserInput) (UserView, error)
	ListUsers(ctx context.Context, actor Actor) ([]UserView, error)
	GetUser(ctx context.Context, actor Actor, id string) (UserView, error)
	UpdateUser(ctx context.Context, actor Actor, id string, roles identity.Roles, status identity.UserStatus) (UserView, error)
	SetPassword(ctx context.Context, actor Actor, id, newPassword string) error

	// catalog
	CreateProduct(ctx context.Context, actor Actor, in CreateProductInput) (ProductView, error)
	ListProducts(ctx context.Context, actor Actor, in ProductListQuery) (ProductListResult, error)
	GetProduct(ctx context.Context, actor Actor, id string) (ProductDetail, error)
	UpdateProduct(ctx context.Context, actor Actor, id string, in ProductUpdateInput) (ProductView, error)
	GetOverview(ctx context.Context, actor Actor) (OverviewView, error)
	ListVisibleProductIDs(ctx context.Context, actor Actor) ([]string, error)
	ListMembers(ctx context.Context, actor Actor, productID string) ([]MemberView, error)
	// ListMemberCandidates 添加成员时的候选用户（产品 owner / admin）。
	ListMemberCandidates(ctx context.Context, actor Actor, productID string) ([]UserView, error)
	UpsertMember(ctx context.Context, actor Actor, productID, userID string, role product.MemberRole) error
	RemoveMember(ctx context.Context, actor Actor, productID, userID string) error
	CreateModule(ctx context.Context, actor Actor, productID, parentID, name string, sortOrder int) (ModuleView, error)
	ListModules(ctx context.Context, actor Actor, productID string) ([]ModuleView, error)
	UpdateModule(ctx context.Context, actor Actor, productID, moduleID string, in UpdateModuleInput) (ModuleView, error)
	UpsertDocument(ctx context.Context, actor Actor, in UpsertDocumentInput) (DocumentView, error)
	GetDocument(ctx context.Context, actor Actor, ownerType, ownerID string) (DocumentView, error)

	// planning
	CreateIteration(ctx context.Context, actor Actor, productID string, in CreateIterationInput) (IterationView, error)
	ListIterations(ctx context.Context, actor Actor, productID string, status *planning.IterationStatus) ([]IterationView, error)
	GetIteration(ctx context.Context, actor Actor, id string) (IterationDetail, error)
	// 个人「当前迭代」偏好：首页直接展示该迭代看板。
	GetCurrentIteration(ctx context.Context, actor Actor) (CurrentIterationView, error)
	SetCurrentIteration(ctx context.Context, actor Actor, iterationID string) (IterationDetail, error)
	ClearCurrentIteration(ctx context.Context, actor Actor) error
	CreateTask(ctx context.Context, actor Actor, in CreateTaskInput) (TaskView, error)
	DecomposeTask(ctx context.Context, actor Actor, parentTaskID string, children []CreateTaskInput) ([]TaskView, error)
	GetTask(ctx context.Context, actor Actor, id string) (TaskDetail, error)
	ListTasks(ctx context.Context, actor Actor, f TaskFilter) ([]TaskView, error)
	UpdateTask(ctx context.Context, actor Actor, id string, in TaskUpdateInput) (TaskView, error)
	DeleteTask(ctx context.Context, actor Actor, id string) error
	LinkModule(ctx context.Context, actor Actor, taskID, moduleID string, add bool) error

	// workflow
	ListWorkflows(ctx context.Context, actor Actor, scope *workitem.Scope) ([]WorkflowView, error)
	GetWorkflow(ctx context.Context, actor Actor, id string) (WorkflowView, error)
	CreateWorkflow(ctx context.Context, actor Actor, yamlText string) (WorkflowView, error)
	UpdateWorkflow(ctx context.Context, actor Actor, id, yamlText string) (WorkflowView, error)
	ArchiveWorkflow(ctx context.Context, actor Actor, id string) error
	ExportWorkflow(ctx context.Context, actor Actor, id string) (WorkflowExport, error)
	ImportWorkflow(ctx context.Context, actor Actor, yamlText string) (WorkflowView, error)
	StartStep(ctx context.Context, actor Actor, wtype workitem.Scope, workItemID, nodeID string, expectedVersion int64) (NodeOpResult, error)
	SubmitStep(ctx context.Context, actor Actor, wtype workitem.Scope, workItemID, nodeID string, expectedVersion int64, artifacts []ArtifactInput, note string) (NodeOpResult, error)
	ReviewStep(ctx context.Context, actor Actor, wtype workitem.Scope, workItemID, nodeID string, decision workitem.Decision, comment string, expectedVersion int64) (NodeOpResult, error)
	AttachArtifact(ctx context.Context, actor Actor, wtype workitem.Scope, workItemID, nodeID string, in ArtifactInput) (NodeOpResult, error)
	GetNextAction(ctx context.Context, actor Actor, wtype workitem.Scope, workItemID string) (workflow.NextActionReport, error)
	ListPendingReviews(ctx context.Context, actor Actor) ([]PendingReviewView, error)
	GetInstanceViewByWorkItem(ctx context.Context, actor Actor, wtype workitem.Scope, workItemID string) (InstanceView, error)

	// audit（M3 审计留痕检索）
	SearchAuditLogs(ctx context.Context, actor Actor, f AuditQuery) (AuditListResult, error)

	// webhooks（M3 订阅管理，admin）
	CreateWebhook(ctx context.Context, actor Actor, in CreateWebhookInput) (WebhookView, error)
	ListWebhooks(ctx context.Context, actor Actor) ([]WebhookView, error)
	UpdateWebhook(ctx context.Context, actor Actor, id string, in UpdateWebhookInput) (WebhookView, error)
	DeleteWebhook(ctx context.Context, actor Actor, id string) error

	// integration（M3 provider 配置，admin；凭据仍只读环境变量）
	ListProviderConfigs(ctx context.Context, actor Actor) ([]ProviderConfigView, error)
	UpdateProviderConfig(ctx context.Context, actor Actor, kind string, in ProviderConfigInput) (ProviderConfigView, error)

	// 邮箱配置（M4 SMTP 发件配置，admin；密码加密落库，永不回显）
	GetSMTPConfig(ctx context.Context, actor Actor) (SMTPConfigView, error)
	UpdateSMTPConfig(ctx context.Context, actor Actor, in SMTPConfigInput) (SMTPConfigView, error)
}

// 编译期保证 *Service 实现 API。
var _ API = (*Service)(nil)
