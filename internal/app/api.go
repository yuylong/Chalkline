package app

// api.go：API 接口——api（REST）与 mcp 两个入口共用的用例接缝。
// *Service 实现全部方法；协议层只做参数转换与错误码映射（docs/structure.md：
// mcp 与 api 不得互相调用、不得直接访问 store）。

import (
	"context"

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
	UpdateUser(ctx context.Context, actor Actor, id string, in UpdateUserInput) (UserView, error)
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
	CreateModule(ctx context.Context, actor Actor, productID, parentID, name string, sortOrder int, info ModuleInfoInput) (ModuleView, error)
	ListModules(ctx context.Context, actor Actor, productID string) ([]ModuleView, error)
	UpdateModule(ctx context.Context, actor Actor, productID, moduleID string, in UpdateModuleInput) (ModuleView, error)
	// DeleteModule 删除模块子树；有任务挂靠时须 force 确认（409 MODULE_HAS_TASKS）。
	DeleteModule(ctx context.Context, actor Actor, productID, moduleID string, force bool) error
	UpsertDocument(ctx context.Context, actor Actor, in UpsertDocumentInput) (DocumentView, error)
	GetDocument(ctx context.Context, actor Actor, ownerType, ownerID string) (DocumentView, error)

	// planning
	CreateIteration(ctx context.Context, actor Actor, productID string, in CreateIterationInput) (IterationView, error)
	ListIterations(ctx context.Context, actor Actor, productID string, status *planning.IterationStatus) ([]IterationView, error)
	GetIteration(ctx context.Context, actor Actor, id string) (IterationDetail, error)
	// 迭代级 provider 绑定（docs/domain/integrations.md「迭代 Provider」）。
	GetIterationProviders(ctx context.Context, actor Actor, iterationID string) (IterationProvidersView, error)
	SetIterationProviders(ctx context.Context, actor Actor, iterationID string, inputs []IterationProviderInput) (IterationProvidersView, error)
	// SetIterationProviderCredential 轮换写权限凭据（不受绑定冻结限制）。
	SetIterationProviderCredential(ctx context.Context, actor Actor, iterationID, providerID string, in ProviderCredentialInput) (IterationProvidersView, error)
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

	// 连接目录＝全局 provider 配置（admin 增删改查；成员可读已启用项用于选择处下拉建议）
	ListProviderEndpoints(ctx context.Context, actor Actor) ([]ProviderEndpointView, error)
	CreateProviderEndpoint(ctx context.Context, actor Actor, in ProviderEndpointInput) (ProviderEndpointView, error)
	UpdateProviderEndpoint(ctx context.Context, actor Actor, id string, in ProviderEndpointUpdateInput) (ProviderEndpointView, error)
	DeleteProviderEndpoint(ctx context.Context, actor Actor, id string) error

	// 个人 provider 凭据（成员自助维护；分发时优先以本人身份出站）
	ListMyCredentials(ctx context.Context, actor Actor) ([]UserCredentialView, error)
	SetMyCredential(ctx context.Context, actor Actor, in UserCredentialInput) (UserCredentialView, error)
	DeleteMyCredential(ctx context.Context, actor Actor, provider, baseURL string) error

	// 邮箱配置（M4 SMTP 发件配置，admin；密码加密落库，永不回显）
	GetSMTPConfig(ctx context.Context, actor Actor) (SMTPConfigView, error)
	UpdateSMTPConfig(ctx context.Context, actor Actor, in SMTPConfigInput) (SMTPConfigView, error)
	// TestSMTPConfig 用当前生效配置发送测试邮件（admin）。
	TestSMTPConfig(ctx context.Context, actor Actor) error

	// LDAP 认证配置（admin；绑定密码加密落库，永不回显）
	GetLDAPConfig(ctx context.Context, actor Actor) (LDAPConfigView, error)
	UpdateLDAPConfig(ctx context.Context, actor Actor, in LDAPConfigInput) (LDAPConfigView, error)
	// TestLDAPConfig 测试 LDAP 连通性；带用户名/密码时做完整登录验证（admin）。
	TestLDAPConfig(ctx context.Context, actor Actor, username, password string) (LDAPTestResult, error)
}

// 编译期保证 *Service 实现 API。
var _ API = (*Service)(nil)
