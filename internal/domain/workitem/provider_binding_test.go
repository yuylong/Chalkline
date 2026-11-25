package workitem

import (
	"strings"
	"testing"
)

func TestValidateBindings(t *testing.T) {
	inline := ProviderBinding{Category: CategoryArtifact, Provider: ProviderInline}
	outline := ProviderBinding{Category: CategoryArtifact, Provider: ProviderOutline,
		BaseURL: "https://outline.example.com", Target: "col-1"}
	process := ProviderBinding{Category: CategoryProcess, Provider: ProviderRedmine,
		BaseURL: "https://redmine.example.com", Target: "chalkline"}
	code := ProviderBinding{Category: CategoryCode, Provider: ProviderGit,
		BaseURL: "https://git.example.com", Target: "team/repo"}

	cases := []struct {
		name    string
		binding []ProviderBinding
		wantErr string
	}{
		{name: "inline 成果物即可", binding: []ProviderBinding{inline}},
		{name: "三类齐全", binding: []ProviderBinding{outline, process, code}},
		{name: "缺少成果物", binding: []ProviderBinding{process}, wantErr: "必须且只能绑定一个成果物"},
		{name: "成果物重复", binding: []ProviderBinding{inline, outline}, wantErr: "必须且只能绑定一个成果物"},
		{name: "流程记录重复", binding: []ProviderBinding{inline, process, {Category: CategoryProcess,
			Provider: ProviderRedmine, BaseURL: "https://redmine.example.com", Target: "other"}},
			wantErr: "流程记录 provider 至多绑定一个"},
		{name: "外部 provider 缺连接地址",
			binding: []ProviderBinding{{Category: CategoryArtifact, Provider: ProviderOutline, Target: "col"}},
			wantErr: "必须配置连接地址"},
		{name: "外部 provider 缺目标",
			binding: []ProviderBinding{{Category: CategoryArtifact, Provider: ProviderOutline,
				BaseURL: "https://outline.example.com"}},
			wantErr: "需要目标"},
		{name: "inline 不应带目标",
			binding: []ProviderBinding{{Category: CategoryArtifact, Provider: ProviderInline, Target: "x"}},
			wantErr: "不应配置 baseURL / 目标"},
		{name: "分类不允许的 provider",
			binding: []ProviderBinding{{Category: CategoryProcess, Provider: ProviderOutline,
				BaseURL: "https://outline.example.com", Target: "col"}},
			wantErr: "不允许使用"},
		{name: "baseURL 内嵌凭据",
			binding: []ProviderBinding{{Category: CategoryArtifact, Provider: ProviderOutline,
				BaseURL: "https://user:pass@outline.example.com", Target: "col"}},
			wantErr: "不得内嵌凭据"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateBindings(tc.binding)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("期望通过，实际 %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("期望错误包含 %q，实际 %v", tc.wantErr, err)
			}
		})
	}
}

func TestCheckArtifactProvider(t *testing.T) {
	bindings := []ProviderBinding{
		{Category: CategoryArtifact, Provider: ProviderOutline,
			BaseURL: "https://outline.example.com", Target: "col"},
		{Category: CategoryCode, Provider: ProviderGit,
			BaseURL: "https://git.example.com", Target: "team/repo"},
	}
	cases := []struct {
		name     string
		kind     ArtifactKind
		provider ArtifactProvider
		wantErr  bool
	}{
		{name: "成果物与绑定一致", kind: KindSpec, provider: ProviderOutline},
		{name: "inline 交给 Chalkline 分发", kind: KindSpec, provider: ProviderInline},
		{name: "外部来源与绑定不一致", kind: KindSpec, provider: ProviderSVN, wantErr: true},
		{name: "pr 来自绑定代码库", kind: KindPR, provider: ProviderGit},
		{name: "pr 来自未绑定代码库", kind: KindPR, provider: ProviderSVN, wantErr: true},
		{name: "附件不参与分发", kind: KindFile, provider: ProviderFile},
		{name: "pre_review 必须 inline", kind: KindPreReview, provider: ProviderInline},
		{name: "pre_review 不允许外部", kind: KindPreReview, provider: ProviderOutline, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckArtifactProvider(bindings, tc.kind, tc.provider)
			if tc.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v，实际 %v", tc.wantErr, err)
			}
		})
	}

	// 未配置绑定的历史迭代不改变既有行为。
	if err := CheckArtifactProvider(nil, KindSpec, ProviderInline); err != nil {
		t.Fatalf("无绑定时应放行：%v", err)
	}
}

func TestProviderBindingLinkURLAndDirPath(t *testing.T) {
	outline := ProviderBinding{Provider: ProviderOutline, BaseURL: "https://outline.example.com/", Target: "col-1"}
	if got, want := outline.LinkURL(), "https://outline.example.com/collection/col-1"; got != want {
		t.Fatalf("outline 链接 = %q，期望 %q", got, want)
	}
	redmine := ProviderBinding{Provider: ProviderRedmine, BaseURL: "https://redmine.example.com", Target: "chalk"}
	if got, want := redmine.LinkURL(), "https://redmine.example.com/projects/chalk"; got != want {
		t.Fatalf("redmine 链接 = %q，期望 %q", got, want)
	}
	inline := ProviderBinding{Provider: ProviderInline}
	if got := inline.LinkURL(); got != "" {
		t.Fatalf("inline 不应有链接，实际 %q", got)
	}
	git := ProviderBinding{Provider: ProviderGit, DirTemplate: "chalkline/{iteration}/{node}"}
	if got, want := git.DirPath(map[string]string{"iteration": "CHK-IT1", "node": "设计 评审"}),
		"chalkline/CHK-IT1/设计-评审"; got != want {
		t.Fatalf("DirPath = %q，期望 %q", got, want)
	}
}
