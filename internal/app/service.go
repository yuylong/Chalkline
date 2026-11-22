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
}

// NewService 组装服务。gate 可为 nil：此时材料只做 domain 格式校验。
func NewService(db *store.DB, log *slog.Logger, jwt *auth.JWTManager, gate ArtifactGate) *Service {
	return &Service{DB: db, Log: log, JWT: jwt, Now: time.Now, Gate: gate}
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
//（docs/domain/model.md 对象级权限表）。
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
