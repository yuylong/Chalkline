package workitem

import (
	"testing"

	"github.com/yuylong/Chalkline/internal/domain/identity"
)

func TestArtifactProviderKindMatrix(t *testing.T) {
	// 不变量 14：pr 只能来自 git / svn。
	if !ProviderAllowed(KindPR, ProviderGit) || !ProviderAllowed(KindPR, ProviderSVN) {
		t.Error("pr 应允许 git/svn")
	}
	if ProviderAllowed(KindPR, ProviderOutline) || ProviderAllowed(KindPR, ProviderInline) {
		t.Error("pr 不允许 outline/inline")
	}
	// pre_review 只承载平台内 Markdown。
	if !ProviderAllowed(KindPreReview, ProviderInline) {
		t.Error("pre_review 应允许 inline")
	}
	if ProviderAllowed(KindPreReview, ProviderURL) || ProviderAllowed(KindPreReview, ProviderGit) {
		t.Error("pre_review 不允许外部 provider")
	}
	for kind := range allowedProviders {
		if ProviderAllowed(kind, ProviderRedmine) {
			t.Errorf("材料 %s 不允许来自 redmine（redmine 仅用于外链）", kind)
		}
	}
}

func ref(kind ArtifactKind, provider ArtifactProvider, url string) ArtifactRef {
	return ArtifactRef{Kind: kind, Provider: provider, URL: url, NodeID: "n1", SubmittedBy: "usr"}
}

func TestValidateRef(t *testing.T) {
	// 合法：outline 规格。
	if err := ref(KindSpec, ProviderOutline, "https://outline.x.com/d/1").ValidateRef(); err != nil {
		t.Errorf("合法 outline 规格: %v", err)
	}
	// 不变量 14：svn 的 research_report。
	if err := ref(KindResearchReport, ProviderSVN, "https://svn.example.com/r/1").ValidateRef(); err == nil {
		t.Error("research_report 不允许来自 svn")
	}
	// URL 缺失 / 非法。
	if err := ref(KindPR, ProviderGit, "").ValidateRef(); err == nil {
		t.Error("git 材料必须有 URL")
	}
	if err := ref(KindPR, ProviderGit, "not-a-url").ValidateRef(); err == nil {
		t.Error("URL 必须是绝对地址")
	}
	if err := ref(KindPR, ProviderGit, "ftp://example.com/r/1").ValidateRef(); err == nil {
		t.Error("仅支持 http/https")
	}
	// inline 材料带 URL 非法；缺内容非法。
	badInline := ArtifactRef{Kind: KindPreReview, Provider: ProviderInline,
		URL: "https://x.com", NodeID: "n1", SubmittedBy: "usr"}
	if err := badInline.ValidateRef(); err == nil {
		t.Error("inline 材料不应携带 URL")
	}
	if err := (ArtifactRef{Kind: KindPreReview, Provider: ProviderInline, NodeID: "n1", SubmittedBy: "usr"}).ValidateRef(); err == nil {
		t.Error("inline 材料必须有内容")
	}
	// 未绑定节点 / 提交人。
	if err := (ArtifactRef{Kind: KindSpec, Provider: ProviderOutline, URL: "https://o.x/d/1"}).ValidateRef(); err == nil {
		t.Error("材料必须绑定节点与提交人")
	}
}

func TestGateValidate(t *testing.T) {
	valid := Gate{Approvals: 1, Roles: []identity.Role{identity.RoleReviewer}, Mode: GateModeAll,
		Artifacts: []ArtifactRequirement{{Kind: KindSpec, Required: true}}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("合法门禁: %v", err)
	}
	// any 模式多人审批应拒绝（语义含糊）。
	if err := (Gate{Approvals: 2, Roles: []identity.Role{identity.RoleReviewer}, Mode: GateModeAny}).Validate(); err == nil {
		t.Error("any + approvals>1 应拒绝")
	}
	// 空角色 / 零审批 / 重复材料。
	if err := (Gate{Approvals: 1}).Validate(); err == nil {
		t.Error("缺角色应拒绝")
	}
	if err := (Gate{Approvals: 0, Roles: []identity.Role{identity.RoleReviewer}}).Validate(); err == nil {
		t.Error("零审批应拒绝")
	}
	dup := Gate{Approvals: 1, Roles: []identity.Role{identity.RoleReviewer},
		Artifacts: []ArtifactRequirement{{Kind: KindSpec}, {Kind: KindSpec}}}
	if err := dup.Validate(); err == nil {
		t.Error("重复材料声明应拒绝")
	}
}

func TestReviewDecision(t *testing.T) {
	// 驳回必须写理由。
	r := ReviewDecision{NodeID: "n1", ReviewerID: "u1", Decision: DecisionReject}
	if err := r.Validate(); err == nil {
		t.Error("无理由驳回应拒绝")
	}
	r.Comment = "结论不成立"
	if err := r.Validate(); err != nil {
		t.Errorf("有理由驳回: %v", err)
	}
	if err := (ReviewDecision{NodeID: "n1", ReviewerID: "u1", Decision: Decision("maybe")}).Validate(); err == nil {
		t.Error("未知决策应拒绝")
	}
}

func TestRolesHasAny(t *testing.T) {
	rs := identity.Roles{identity.RoleDev}
	if rs.HasAny(identity.RoleReviewer) {
		t.Error("dev 不含 reviewer")
	}
	if rs.HasAny(identity.RoleAdmin) {
		t.Error("dev 不含 admin")
	}
	admin := identity.Roles{identity.RoleDev, identity.RoleAdmin}
	if !admin.HasAny(identity.RoleReviewer, identity.RolePM) {
		t.Error("admin 应恒有评审权")
	}
}
