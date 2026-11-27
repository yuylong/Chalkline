package app

// provider_endpoint_test.go：连接目录与个人凭据的纯函数测试——
// 入参校验、视图组装、个人凭据解析优先级（不触库的部分）。

import (
	"testing"

	"github.com/yuylong/Chalkline/internal/domain/workitem"
	"github.com/yuylong/Chalkline/internal/store"
)

func endpointTestService() *Service {
	return &Service{ProviderDefaults: []ProviderDefault{
		{Kind: "git", Label: "Git / GitLab / Gitea"},
		{Kind: "svn", Label: "SVN"},
		{Kind: "outline", Label: "Outline", TokenRequired: true},
		{Kind: "redmine", Label: "Redmine", TokenRequired: true},
	}}
}

func TestValidateProviderEndpointKind(t *testing.T) {
	s := endpointTestService()
	if err := s.validateProviderEndpoint("git", "https://git.example.com"); err != nil {
		t.Fatalf("已知 provider 应通过: %v", err)
	}
	if err := s.validateProviderEndpoint("inline", "https://x.example.com"); err == nil {
		t.Fatal("未知 provider 应被拒绝")
	}
}

func TestValidateProviderEndpointBaseURL(t *testing.T) {
	s := endpointTestService()
	for _, bad := range []string{"", "git.example.com", "ftp://git.example.com", "https://user:pass@git.example.com"} {
		if err := s.validateProviderEndpoint("git", bad); err == nil {
			t.Fatalf("非法 baseURL %q 应被拒绝", bad)
		}
	}
}

func TestProviderEndpointView(t *testing.T) {
	s := endpointTestService()
	v := s.providerEndpointView(store.ProviderEndpointRow{
		ID: "pep_1", Kind: "redmine", Label: "研发一部 Redmine",
		BaseURL: "https://rm1.example.com", Note: "流程记录", Enabled: true,
	})
	if v.KindLabel != "Redmine" {
		t.Fatalf("kindLabel 应取 ProviderDefaults 的展示名: %+v", v)
	}
	if v.ID != "pep_1" || v.BaseURL != "https://rm1.example.com" || !v.Enabled {
		t.Fatalf("视图字段应原样透出: %+v", v)
	}
}

func TestUserCredentialViewNeverLeaksSecret(t *testing.T) {
	s := endpointTestService()
	v := s.userCredentialView(store.UserProviderCredentialRow{
		ID: "upc_1", Provider: "outline", BaseURL: "https://outline.example.com",
		AuthType: "token", SecretEnc: "enc:v1:xxx",
	})
	if !v.SecretConfigured {
		t.Fatal("库内密文存在应报已配置")
	}
	if v.ProviderLabel != "Outline" {
		t.Fatalf("providerLabel 应取 ProviderDefaults 的展示名: %+v", v)
	}
	// 视图结构无 Secret 字段：编译期保证永不回显（本用例即结构断言）。
}

func TestPersonalCredentialConfiguredInline(t *testing.T) {
	s := endpointTestService()
	// inline 绑定不适用个人凭据。
	ok, err := s.personalCredentialConfigured(nil, nil, "usr_1", workitem.ProviderBinding{
		Category: workitem.CategoryArtifact, Provider: workitem.ProviderInline,
	})
	if err != nil {
		t.Fatalf("inline 绑定不应报错: %v", err)
	}
	if ok {
		t.Fatal("inline 绑定应恒报未配置")
	}
}
