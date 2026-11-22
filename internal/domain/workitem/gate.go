package workitem

import (
	"fmt"
	"time"

	"github.com/yuylong/Chalkline/internal/domain/identity"
)

// GateMode 是会签语义：all = 需来自 N 个不同评审人；any = 任一人即可。
// 当前只做这两种，配置结构预留扩展位（docs/domain/workflow.md）。
type GateMode string

const (
	// GateModeAll 需 approvals 数量的不同评审人各投一票有效 approve。
	GateModeAll GateMode = "all"
	// GateModeAny 任一名合格评审人 approve 即可。
	GateModeAny GateMode = "any"
)

func (m GateMode) Valid() bool { return m == GateModeAll || m == GateModeAny }

// ArtifactRequirement 声明门禁要求的材料类型。
type ArtifactRequirement struct {
	Kind     ArtifactKind `yaml:"kind" json:"kind"`
	Required bool         `yaml:"required" json:"required"` // false 时可挂可不挂（如 pre_review）
}

// Gate 是节点的准出条件。判定完全在服务端（AGENT.md 红线 2）。
type Gate struct {
	// Approvals 需要的有效 approve 数量，≥ 1。
	Approvals int `yaml:"approvals" json:"approvals"`
	// Roles 审批人必须持有其中任一角色（admin 恒可）。
	Roles []identity.Role `yaml:"roles" json:"roles"`
	// Mode 会签语义，默认 all。
	Mode GateMode `yaml:"mode" json:"mode"`
	// Artifacts 门禁要求的材料清单。
	Artifacts []ArtifactRequirement `yaml:"artifacts" json:"artifacts"`
}

// Normalize 归一化默认值：mode 省略即 all。
func (g *Gate) Normalize() {
	if g.Mode == "" {
		g.Mode = GateModeAll
	}
}

// Validate 校验门禁配置自身。
func (g Gate) Validate() error {
	if g.Approvals < 1 {
		return fmt.Errorf("approvals 需 ≥ 1，当前 %d", g.Approvals)
	}
	if len(g.Roles) == 0 {
		return fmt.Errorf("roles 不可为空")
	}
	for _, r := range g.Roles {
		if !r.Valid() {
			return fmt.Errorf("未知角色 %q", r)
		}
	}
	mode := g.Mode
	if mode == "" {
		mode = GateModeAll
	}
	if !mode.Valid() {
		return fmt.Errorf("mode 非法 %q", g.Mode)
	}
	if g.Mode == GateModeAny && g.Approvals > 1 {
		return fmt.Errorf("mode=any 时 approvals 应为 1，当前 %d", g.Approvals)
	}
	seen := map[ArtifactKind]bool{}
	for _, a := range g.Artifacts {
		if !a.Kind.Valid() {
			return fmt.Errorf("未知材料类型 %q", a.Kind)
		}
		if seen[a.Kind] {
			return fmt.Errorf("材料类型 %q 重复声明", a.Kind)
		}
		seen[a.Kind] = true
	}
	return nil
}

// RequiresArtifact 报告门禁是否要求给定材料类型，及是否必填。
func (g Gate) RequiresArtifact(kind ArtifactKind) (required bool, declared bool) {
	for _, a := range g.Artifacts {
		if a.Kind == kind {
			return a.Required, true
		}
	}
	return false, false
}

// ReviewDecision 是一条门禁审批记录（GateApproval 的领域形态）。
// 同一评审人在同一节点只计一次有效意见，以最近一次决策为准。
type ReviewDecision struct {
	NodeID     string
	ReviewerID string
	// Decision 取值 approve / reject。
	Decision Decision
	Comment  string        // 驳回必填
	Roles    identity.Roles // 评审人当时的全局角色（判定资格用，由 app 层在写入时固化）
	CreatedAt time.Time
}

// Decision 是评审结论。
type Decision string

const (
	DecisionApprove Decision = "approve"
	DecisionReject  Decision = "reject"
)

func (d Decision) Valid() bool { return d == DecisionApprove || d == DecisionReject }

// Validate 检查决策记录；驳回必须携带理由（docs/domain/model.md GateApproval）。
func (r ReviewDecision) Validate() error {
	if r.NodeID == "" || r.ReviewerID == "" {
		return fmt.Errorf("审批记录缺少节点或评审人")
	}
	if !r.Decision.Valid() {
		return fmt.Errorf("未知决策 %q", r.Decision)
	}
	if r.Decision == DecisionReject && r.Comment == "" {
		return fmt.Errorf("驳回必须填写理由")
	}
	return nil
}
