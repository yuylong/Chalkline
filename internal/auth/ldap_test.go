package auth

// ldap_test.go：LDAP 原语纯函数测试——过滤器转义与配置校验。
// 拨号/绑定路径依赖真实目录服务，由管理台「测试连接」与联调环境覆盖。

import (
	"strings"
	"testing"
	"time"
)

func TestLDAPFilterEscapesUsername(t *testing.T) {
	got := ldapFilter("(uid=%s)", `evil*)(uid=*))(|(uid=*`)
	if strings.Contains(got, "evil*") {
		t.Fatalf("过滤器特殊字符应被转义: %s", got)
	}
	if !strings.HasPrefix(got, "(uid=") || !strings.HasSuffix(got, ")") {
		t.Fatalf("模板结构应保留: %s", got)
	}
	// 常规登录名不受影响。
	if got := ldapFilter("(uid=%s)", "zhangsan"); got != "(uid=zhangsan)" {
		t.Fatalf("常规登录名应原样代入: %s", got)
	}
}

func TestValidateLDAPSettings(t *testing.T) {
	base := LDAPSettings{
		Host: "ldap.example.com", Port: 389, TLSMode: "none",
		UserSearchBase: "ou=people,dc=example,dc=com", UserSearchFilter: "(uid=%s)",
		Timeout: 10 * time.Second,
	}
	if err := ValidateLDAPSettings(base); err != nil {
		t.Fatalf("齐备配置应通过: %v", err)
	}
	noHost := base
	noHost.Host = ""
	if err := ValidateLDAPSettings(noHost); err == nil {
		t.Fatal("缺 host 应报错")
	}
	noBase := base
	noBase.UserSearchBase = ""
	if err := ValidateLDAPSettings(noBase); err == nil {
		t.Fatal("缺检索 base 应报错")
	}
	noFilter := base
	noFilter.UserSearchFilter = "(uid=john)"
	if err := ValidateLDAPSettings(noFilter); err == nil {
		t.Fatal("过滤器缺少登录名占位符应报错")
	}
	badTLS := base
	badTLS.TLSMode = "ssl"
	if err := ValidateLDAPSettings(badTLS); err == nil {
		t.Fatal("非法 TLS 模式应报错")
	}
	for _, mode := range []string{"", "none", "starttls", "tls"} {
		m := base
		m.TLSMode = mode
		if err := ValidateLDAPSettings(m); err != nil {
			t.Fatalf("TLS 模式 %q 应通过: %v", mode, err)
		}
	}
}
