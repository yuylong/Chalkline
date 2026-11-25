package workitem

// provider_binding.go：迭代级 provider 绑定（docs/domain/integrations.md「迭代 Provider」）。
//
// 与全局连接配置（integration_provider_configs）的区别：
//   - 全局配置回答「这套部署能连哪些外部系统」（baseURL / 凭据 / 启停）；
//   - 迭代绑定回答「这个迭代的成果物、流程记录、代码分别落在哪里」，连接信息随迭代固化，
//     迭代创建或初始化阶段配置，首次提交成果物后冻结（不变量 22）。

import (
	"fmt"
	"net/url"
	"strings"
)

// ProviderCategory 是迭代 provider 的职责分类。
type ProviderCategory string

const (
	CategoryArtifact ProviderCategory = "artifact" // 成果物：必选且唯一
	CategoryProcess  ProviderCategory = "process"  // 流程记录：可选，至多一个
	CategoryCode     ProviderCategory = "code"     // 代码库：可选，可多个
)

func (c ProviderCategory) Valid() bool {
	switch c {
	case CategoryArtifact, CategoryProcess, CategoryCode:
		return true
	}
	return false
}

// Label 是控制台展示用的中文名。
func (c ProviderCategory) Label() string {
	switch c {
	case CategoryArtifact:
		return "成果物"
	case CategoryProcess:
		return "流程记录"
	case CategoryCode:
		return "代码库"
	}
	return string(c)
}

// categoryProviders 声明每类可绑定的 provider（不变量 22）。
var categoryProviders = map[ProviderCategory][]ArtifactProvider{
	CategoryArtifact: {ProviderInline, ProviderOutline, ProviderGit, ProviderSVN},
	CategoryProcess:  {ProviderRedmine},
	CategoryCode:     {ProviderGit, ProviderSVN},
}

// CategoryProviders 返回某类允许绑定的 provider（供控制台与校验复用）。
func CategoryProviders(c ProviderCategory) []ArtifactProvider {
	return categoryProviders[c]
}

// ProviderAllowedInCategory 报告 provider 是否属于该分类。
func ProviderAllowedInCategory(c ProviderCategory, p ArtifactProvider) bool {
	for _, allowed := range categoryProviders[c] {
		if allowed == p {
			return true
		}
	}
	return false
}

// DefaultArtifactDirTemplate 是成果物在外部 provider 中的默认目录结构：
// chalkline/<迭代编号>/<节点名>，文件名取材料类型。
const DefaultArtifactDirTemplate = "chalkline/{iteration}/{node}"

// ProviderBinding 是迭代绑定的一个 provider。连接信息（BaseURL）随迭代记录，
// 便于节点页面直接展示与跳转，也避免迭代中途全局配置变化导致成果物散落。
type ProviderBinding struct {
	Category    ProviderCategory `json:"category"`
	Provider    ArtifactProvider `json:"provider"`
	BaseURL     string           `json:"baseUrl"`     // 外部系统根地址；inline 为空
	Target      string           `json:"target"`      // outline collection / redmine project / 代码库路径
	DirTemplate string           `json:"dirTemplate"` // 成果物目录结构约定（artifact / code）
	Branch      string           `json:"branch"`      // 代码库分支（git 用，空即默认分支）
}

// IsExternal 报告是否指向平台外部系统（inline 为平台内置存储）。
func (b ProviderBinding) IsExternal() bool { return b.Provider != ProviderInline }

// Validate 校验单条绑定。
func (b ProviderBinding) Validate() error {
	if !b.Category.Valid() {
		return fmt.Errorf("未知 provider 分类 %q", b.Category)
	}
	if !b.Provider.Valid() {
		return fmt.Errorf("未知 provider %q", b.Provider)
	}
	if !ProviderAllowedInCategory(b.Category, b.Provider) {
		return fmt.Errorf("%s 类 provider 不允许使用 %q", b.Category.Label(), b.Provider)
	}
	if b.Provider == ProviderInline {
		if strings.TrimSpace(b.BaseURL) != "" || strings.TrimSpace(b.Target) != "" {
			return fmt.Errorf("inline 成果物 provider 不应配置 baseURL / 目标")
		}
		return nil
	}
	if err := validateBindingBaseURL(b.BaseURL); err != nil {
		return err
	}
	if strings.TrimSpace(b.Target) == "" {
		return fmt.Errorf("%s provider %q 需要目标（项目 / 集合 / 仓库路径）", b.Category.Label(), b.Provider)
	}
	return nil
}

// validateBindingBaseURL 要求 http(s) 绝对地址且不内嵌凭据（凭据只走环境变量）。
func validateBindingBaseURL(raw string) error {
	v := strings.TrimSpace(raw)
	if v == "" {
		return fmt.Errorf("外部 provider 必须配置连接地址 baseURL")
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		return fmt.Errorf("baseURL %q 不是合法 URL", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("baseURL 必须使用 http/https")
	}
	if u.User != nil {
		return fmt.Errorf("baseURL 不得内嵌凭据（凭据只经环境变量配置）")
	}
	return nil
}

// ValidateBindings 校验迭代整套绑定：
// 成果物必选且唯一、流程记录至多一个、代码库可多个；同一 (分类, provider, 目标) 不重复。
func ValidateBindings(bs []ProviderBinding) error {
	var artifacts, processes int
	seen := make(map[string]bool, len(bs))
	for i, b := range bs {
		if err := b.Validate(); err != nil {
			return fmt.Errorf("第 %d 个 provider 绑定无效：%w", i+1, err)
		}
		switch b.Category {
		case CategoryArtifact:
			artifacts++
		case CategoryProcess:
			processes++
		}
		key := string(b.Category) + "|" + string(b.Provider) + "|" + b.Target
		if seen[key] {
			return fmt.Errorf("provider 绑定重复：%s / %s / %s", b.Category.Label(), b.Provider, b.Target)
		}
		seen[key] = true
	}
	if artifacts != 1 {
		return fmt.Errorf("必须且只能绑定一个成果物 provider（当前 %d 个）", artifacts)
	}
	if processes > 1 {
		return fmt.Errorf("流程记录 provider 至多绑定一个（当前 %d 个）", processes)
	}
	return nil
}

// ArtifactBinding 取成果物绑定；ok=false 表示未配置（历史迭代）。
func ArtifactBinding(bs []ProviderBinding) (ProviderBinding, bool) {
	for _, b := range bs {
		if b.Category == CategoryArtifact {
			return b, true
		}
	}
	return ProviderBinding{}, false
}

// ProcessBinding 取流程记录绑定。
func ProcessBinding(bs []ProviderBinding) (ProviderBinding, bool) {
	for _, b := range bs {
		if b.Category == CategoryProcess {
			return b, true
		}
	}
	return ProviderBinding{}, false
}

// CodeBindings 取全部代码库绑定。
func CodeBindings(bs []ProviderBinding) []ProviderBinding {
	out := make([]ProviderBinding, 0, len(bs))
	for _, b := range bs {
		if b.Category == CategoryCode {
			out = append(out, b)
		}
	}
	return out
}

// CheckArtifactProvider 校验材料来源是否符合迭代绑定（不变量 22）。
//
// 语义：材料提交时的 provider 表示「内容从哪来 / 在哪」，迭代绑定决定「记录到哪」。
//   - 未配置绑定（历史迭代）不改变既有行为；
//   - 附件（file / url）不参与分发，始终允许；
//   - pre_review 必须是 inline（docs/domain/skill.md）；
//   - `inline` 表示内容交给 Chalkline，由绑定的成果物 provider 决定落点，始终允许；
//   - pr 必须来自绑定的代码库（未绑定代码库时保持原有 git/svn 校验）；
//   - 其它外部来源必须与绑定的成果物 provider 一致，或来自绑定的代码库（代码引用）。
func CheckArtifactProvider(bindings []ProviderBinding, kind ArtifactKind, provider ArtifactProvider) error {
	if len(bindings) == 0 || kind == KindFile {
		return nil
	}
	if provider == ProviderFile || provider == ProviderURL {
		return nil
	}
	if kind == KindPreReview {
		if provider != ProviderInline {
			return fmt.Errorf("pre_review 必须是 inline 材料")
		}
		return nil
	}
	if kind == KindPR {
		if len(CodeBindings(bindings)) == 0 {
			return nil
		}
		for _, b := range CodeBindings(bindings) {
			if b.Provider == provider {
				return nil
			}
		}
		return fmt.Errorf("代码改动（pr）必须来自迭代绑定的代码库 provider")
	}
	artifact, hasArtifact := ArtifactBinding(bindings)
	if !hasArtifact {
		return nil
	}
	if provider == ProviderInline || provider == artifact.Provider {
		return nil
	}
	if (provider == ProviderGit || provider == ProviderSVN) && ProviderAllowed(kind, provider) {
		for _, b := range CodeBindings(bindings) {
			if b.Provider == provider {
				return nil
			}
		}
	}
	return fmt.Errorf("材料来源 %q 与迭代绑定的成果物 provider %q 不一致", provider, artifact.Provider)
}

// LinkURL 返回可在控制台点击跳转的外部地址；inline 返回空串。
func (b ProviderBinding) LinkURL() string {
	base := strings.TrimRight(strings.TrimSpace(b.BaseURL), "/")
	target := strings.TrimSpace(b.Target)
	if b.Provider == ProviderInline || base == "" {
		return ""
	}
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return target
	}
	switch b.Provider {
	case ProviderOutline:
		if target == "" {
			return base
		}
		return base + "/collection/" + strings.TrimPrefix(target, "/")
	case ProviderRedmine:
		if target == "" {
			return base
		}
		return base + "/projects/" + strings.TrimPrefix(target, "/")
	default: // git / svn：目标是仓库路径
		if target == "" {
			return base
		}
		return base + "/" + strings.TrimPrefix(target, "/")
	}
}

// DirPath 展开目录模板（支持 {iteration} / {node} / {task} / {kind} 占位符）。
// 空模板使用 DefaultArtifactDirTemplate；结果去掉首尾斜杠。
func (b ProviderBinding) DirPath(vars map[string]string) string {
	tpl := strings.TrimSpace(b.DirTemplate)
	if tpl == "" {
		tpl = DefaultArtifactDirTemplate
	}
	for k, v := range vars {
		tpl = strings.ReplaceAll(tpl, "{"+k+"}", sanitizePathSegment(v))
	}
	tpl = strings.ReplaceAll(tpl, "//", "/")
	return strings.Trim(tpl, "/")
}

// sanitizePathSegment 把目录名中的空白与路径分隔符归一化，避免越出模板根。
func sanitizePathSegment(s string) string {
	s = strings.TrimSpace(s)
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-", "..", "_")
	s = replacer.Replace(s)
	if s == "" {
		return "_"
	}
	return s
}

// PushedArtifact 是成果物分发到外部 provider 后的回执（只保留摘要，不复制内容）。
type PushedArtifact struct {
	ExternalID string
	URL        string
	Summary    string
}

// ProcessTicket 是流程记录 provider 上的票。
type ProcessTicket struct {
	ExternalID string
	URL        string
}

// ProcessNote 是要追加到票上的回复。Key 用于幂等去重（同一事实只推一次）。
type ProcessNote struct {
	Title string // 建票时的标题
	Body  string // 回复正文（Markdown）
	Key   string // 幂等键
}

// ProcessSubject 是流程记录的一个主体（节点或任务）。
type ProcessSubject struct {
	Type        string // node | task | iteration
	ID          string
	Title       string
	Description string
}
