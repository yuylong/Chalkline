package app

// service.go：Service 组装与通用工具。权限判定全部在本层（docs/structure.md）：
// api/mcp 只做协议转换；越权写 403、非成员访问 404（不变量 16）。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/yuylong/Chalkline/internal/auth"
	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/domain/product"
	"github.com/yuylong/Chalkline/internal/domain/workitem"
	"github.com/yuylong/Chalkline/internal/store"
)

// Actor 复用 auth 的请求身份（actor_id 一路进事务与审计）。
type Actor = auth.Actor

// ArtifactGate 是材料引用的外部校验器（依赖倒置：integration 适配器实现，
// docs/modules/integration.md「对外接口」）。nil 时跳过外部校验（单测与无集成部署）。
// 实现只做引用可达性与格式校验 + 尽力快照，不得写领域状态。
type ArtifactGate interface {
	Validate(ctx context.Context, ref workitem.ArtifactRef) error
	Snapshot(ctx context.Context, ref workitem.ArtifactRef) (workitem.ArtifactMeta, error)
}

// Service 是全部用例的承载：事务、审计、权限、事件。
type Service struct {
	DB  *store.DB
	Log *slog.Logger
	JWT *auth.JWTManager
	Now func() time.Time
	// Gate 材料外部校验（M2 集成接入点，integration.md「材料挂接时」）。
	Gate ArtifactGate
	// ReviewSLA 评审超期阈值（M3：超过即提醒评审人；0 = 默认 24h）。
	ReviewSLA time.Duration
	// ReviewEscalation 升级阈值（M3：超过即升级给产品 owner/admin；0 = 默认 72h）。
	ReviewEscalation time.Duration
	// ProviderDefaults 是环境变量提供的集成 provider 默认配置（M3 管理台展示与环境提示）。
	ProviderDefaults []ProviderDefault
	// SMTPDefaults 是环境变量提供的 SMTP 发件默认值（M4 邮箱配置；Password 只进内存）。
	SMTPDefaults SMTPEnvDefaults
	// SecretKey 是静态凭据加密密钥（M4：SMTP 密码加密落库；来自 CHALKLINE_SECRET_KEY）。
	SecretKey string
	// SMTPTestSend 是测试邮件发送通道（依赖倒置：notify.SMTP 实现，server 组装层注入；
	// nil 时测试发送报内部错误，不影响其余用例）。
	SMTPTestSend func(ctx context.Context, rt SMTPRuntime, recipients []string) error
	// Distributor 是成果物分发通道（依赖倒置：integration.Dispatcher 实现；nil 时分发跳过）。
	Distributor ArtifactDistributor
	// Process 是流程记录通道（依赖倒置：integration.Dispatcher 实现；nil 时跳过）。
	Process ProcessRecorder
	// Async 决定事务后分发/流程记录的执行方式；nil 时同步执行（单测确定性强）。
	Async func(fn func())
}

// Options 是 NewService 的可选配置（M3 评审 SLA 与集成默认值；M4 邮箱配置）。
type Options struct {
	ReviewSLA        time.Duration
	ReviewEscalation time.Duration
	ProviderDefaults []ProviderDefault
	SMTPDefaults     SMTPEnvDefaults
	SecretKey        string
	// SMTPTestSend 注入测试邮件发送通道（server 组装层用 notify.SMTP 实现）。
	SMTPTestSend func(ctx context.Context, rt SMTPRuntime, recipients []string) error
	// Distributor / Process 注入成果物分发与流程记录通道（server 组装层用 integration.Dispatcher）。
	Distributor ArtifactDistributor
	Process     ProcessRecorder
	// Async 注入事务后任务的执行方式（server 用 goroutine；nil 即同步）。
	Async func(fn func())
}

// NewService 组装服务。gate 可为 nil：此时材料只做 domain 格式校验。
func NewService(db *store.DB, log *slog.Logger, jwt *auth.JWTManager, gate ArtifactGate, opts Options) *Service {
	return &Service{DB: db, Log: log, JWT: jwt, Now: time.Now, Gate: gate,
		ReviewSLA: opts.ReviewSLA, ReviewEscalation: opts.ReviewEscalation,
		ProviderDefaults: opts.ProviderDefaults,
		SMTPDefaults:     opts.SMTPDefaults, SecretKey: opts.SecretKey,
		SMTPTestSend: opts.SMTPTestSend,
		Distributor:  opts.Distributor, Process: opts.Process, Async: opts.Async}
}

// newID 生成带类型前缀的短 id（mcp.md：所有 ID 带类型前缀）。
func (s *Service) newID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 失败属系统级异常，直接 panic 让进程退出更安全。
		panic("app: 生成 id 失败: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(buf)
}

// ---- 产品可见性与角色（不变量 16 的判定原语）----

// memberContext 汇总 actor 在产品内的身份。
type memberContext struct {
	Role    product.MemberRole // 产品角色（非成员为零值）
	IsAdmin bool
}

// memberCtx 查询 actor 与产品的关系；admin 恒可见（不变量 16）。
func (s *Service) memberCtx(ctx context.Context, q store.Querier, actor Actor, productID string) (memberContext, error) {
	if actor.IsAdmin {
		return memberContext{IsAdmin: true}, nil
	}
	role, ok, err := store.MemberRoleOf(ctx, q, productID, actor.UserID)
	if err != nil {
		return memberContext{}, err
	}
	if !ok {
		return memberContext{}, nil // 非成员：不可见
	}
	return memberContext{Role: role}, nil
}

// mustView 非成员不可见 → 404（不暴露存在性）。
func (s *Service) mustView(ctx context.Context, q store.Querier, actor Actor, productID string) (memberContext, error) {
	mc, err := s.memberCtx(ctx, q, actor, productID)
	if err != nil {
		return mc, err
	}
	if !mc.IsAdmin && mc.Role == "" {
		return mc, NotFound("对象不存在或不可见")
	}
	return mc, nil
}

// canManageProduct 管产品（改字段/管成员/管模块）：产品 owner 或 admin。
func canManageProduct(mc memberContext) error {
	if mc.IsAdmin || mc.Role == product.MemberOwner {
		return nil
	}
	return RoleRequired("需要产品 owner 或 admin")
}

// canCreateInProduct 建迭代/模块/任务：产品 owner/member 且持全局 pm，或 admin
// （docs/domain/model.md 对象级权限表）。
func canCreateInProduct(mc memberContext, actor Actor) error {
	if mc.IsAdmin {
		return nil
	}
	if (mc.Role == product.MemberOwner || mc.Role == product.MemberMember) && actor.Roles.HasAny(identity.RolePM) {
		return nil
	}
	return RoleRequired("需要产品成员身份并持 pm 角色，或 admin")
}

// canWorkTask 推进/提交/挂材料/改任务字段：任务负责人、产品 owner、pm、admin。
// guest 不可写（不变量 16）。
func canWorkTask(mc memberContext, actor Actor, assigneeID string) error {
	if mc.IsAdmin {
		return nil
	}
	if assigneeID != "" && assigneeID == actor.UserID {
		return nil
	}
	if mc.Role == product.MemberOwner {
		return nil
	}
	if mc.Role == product.MemberMember && actor.Roles.HasAny(identity.RolePM) {
		return nil
	}
	return RoleRequired("需要任务负责人、产品 owner、pm 或 admin")
}

// audit 是事务内写审计的快捷方式（不变量 13）。
func audit(ctx context.Context, q store.Querier, actor Actor, action, subjectType, subjectID string, payload map[string]any) error {
	return store.InsertAudit(ctx, q, store.AuditLog{
		ActorID: actor.UserID, Action: action, SubjectType: subjectType, SubjectID: subjectID, Payload: payload,
	})
}
