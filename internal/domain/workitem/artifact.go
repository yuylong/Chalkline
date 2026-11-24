package workitem

import (
	"fmt"
	"net/url"
	"strings"
)

// ArtifactKind 是材料类型（docs/domain/model.md Artifact 表）。
type ArtifactKind string

const (
	KindResearchReport ArtifactKind = "research_report" // 竞品调研、技术调研
	KindTechSpike      ArtifactKind = "tech_spike"      // 关键技术验证
	KindSpec           ArtifactKind = "spec"            // 需求规格
	KindDesign         ArtifactKind = "design"          // 技术设计
	KindTestPlan       ArtifactKind = "test_plan"       // 测试设计
	KindTestCases      ArtifactKind = "test_cases"      // 用例集
	KindTestExecution  ArtifactKind = "test_execution"  // 测试执行记录
	KindTestReport     ArtifactKind = "test_report"     // 测试报告
	KindPR             ArtifactKind = "pr"              // 代码改动
	KindSummary        ArtifactKind = "summary"         // 迭代/任务总结
	KindPreReview      ArtifactKind = "pre_review"      // AI 预评审自查报告（inline）
	KindFile           ArtifactKind = "file"            // 其他证据附件
)

// Valid 报告材料类型是否已定义。
func (k ArtifactKind) Valid() bool {
	switch k {
	case KindResearchReport, KindTechSpike, KindSpec, KindDesign, KindTestPlan,
		KindTestCases, KindTestExecution, KindTestReport, KindPR, KindSummary,
		KindPreReview, KindFile:
		return true
	}
	return false
}

// ArtifactProvider 是材料引用的外部来源（docs/domain/model.md）。
type ArtifactProvider string

const (
	ProviderGit     ArtifactProvider = "git"
	ProviderSVN     ArtifactProvider = "svn"
	ProviderOutline ArtifactProvider = "outline"
	ProviderRedmine ArtifactProvider = "redmine"
	ProviderFile    ArtifactProvider = "file"
	ProviderURL     ArtifactProvider = "url"
	ProviderInline  ArtifactProvider = "inline"
)

func (p ArtifactProvider) Valid() bool {
	switch p {
	case ProviderGit, ProviderSVN, ProviderOutline, ProviderRedmine,
		ProviderFile, ProviderURL, ProviderInline:
		return true
	}
	return false
}

// allowedProviders 声明不变量 14：材料的 provider 必须与 kind 匹配
// （如 pr 只能来自 git / svn）。来源参考 docs/domain/model.md 与 workflow.md 的典型来源表。
var allowedProviders = map[ArtifactKind][]ArtifactProvider{
	KindResearchReport: {ProviderOutline, ProviderInline, ProviderURL, ProviderFile},
	KindTechSpike:      {ProviderOutline, ProviderGit, ProviderSVN, ProviderInline, ProviderURL, ProviderFile},
	KindSpec:           {ProviderOutline, ProviderInline, ProviderURL, ProviderFile},
	KindDesign:         {ProviderOutline, ProviderInline, ProviderURL, ProviderFile},
	KindTestPlan:       {ProviderOutline, ProviderInline, ProviderURL, ProviderFile},
	KindTestCases:      {ProviderOutline, ProviderFile, ProviderInline, ProviderURL},
	KindTestExecution:  {ProviderFile, ProviderURL, ProviderInline},
	KindTestReport:     {ProviderOutline, ProviderFile, ProviderInline, ProviderURL},
	KindPR:             {ProviderGit, ProviderSVN},
	KindSummary:        {ProviderOutline, ProviderInline, ProviderURL, ProviderFile},
	// pre_review 是提交人侧 Agent 的自查报告，只承载平台内 Markdown（docs/domain/skill.md）。
	KindPreReview: {ProviderInline},
	KindFile:      {ProviderFile, ProviderURL},
}

// ProviderAllowed 报告 kind 与 provider 是否匹配（不变量 14）。
func ProviderAllowed(kind ArtifactKind, provider ArtifactProvider) bool {
	for _, p := range allowedProviders[kind] {
		if p == provider {
			return true
		}
	}
	return false
}

// ArtifactMeta 是外部系统的引用快照（标题/版本/状态，docs/domain/model.md Artifact.snapshot）。
// 仅用于展示与审计；外部内容变化不回写（integrations.md「同步方向」）。
type ArtifactMeta struct {
	Title   string
	Version string
	Status  string
}

// ArtifactRef 是材料的引用——不是内容。材料与「提交人 + 提交节点」绑定，不可跨节点复用。
type ArtifactRef struct {
	Kind        ArtifactKind
	Provider    ArtifactProvider
	URL         string // 外部定位；inline 时为空
	ExternalID  string // 外部系统的稳定标识，用于去重与回查，不要求所有 provider 支持
	Title       string // 快照：便于展示与审计，外部内容变化不回写
	Content     string // 仅 provider=inline 时承载平台内 Markdown 说明
	NodeID      string // 绑定提交节点
	SubmittedBy string
}

// ValidateRef 校验材料引用存在且格式合法（不变量 14）。
// 门禁只校验引用存在与格式，不要求外部系统实时可访问——外围系统挂了不该阻断评审。
func (a ArtifactRef) ValidateRef() error {
	if !a.Kind.Valid() {
		return fmt.Errorf("未知材料类型 %q", a.Kind)
	}
	if !a.Provider.Valid() {
		return fmt.Errorf("未知 provider %q", a.Provider)
	}
	if !ProviderAllowed(a.Kind, a.Provider) {
		return fmt.Errorf("材料 %q 不允许来自 provider %q（不变量 14）", a.Kind, a.Provider)
	}
	if a.NodeID == "" {
		return fmt.Errorf("材料必须绑定提交节点（node_id 为空）")
	}
	if a.SubmittedBy == "" {
		return fmt.Errorf("材料必须绑定提交人（submitted_by 为空）")
	}
	if a.Provider == ProviderInline {
		if strings.TrimSpace(a.Content) == "" {
			return fmt.Errorf("inline 材料必须有内容")
		}
		if a.URL != "" {
			return fmt.Errorf("inline 材料不应携带外部 URL")
		}
		return nil
	}
	if a.URL == "" {
		return fmt.Errorf("provider %q 的材料必须有 URL", a.Provider)
	}
	u, err := url.Parse(a.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("材料 URL %q 非法，需为绝对地址", a.URL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("材料 URL %q 仅支持 http/https", a.URL)
	}
	return nil
}
