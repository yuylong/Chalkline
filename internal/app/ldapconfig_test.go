package app

// ldapconfig_test.go：LDAP 认证的纯函数测试——登录分支（local/ldap/both）、
// 配置视图与入参校验。绑定密码永不回显由视图结构保证（无密码字段）。

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yuylong/Chalkline/internal/auth"
	"github.com/yuylong/Chalkline/internal/domain/identity"
	"github.com/yuylong/Chalkline/internal/store"
)

func ldapTestUser(source identity.AuthSource, password string) identity.User {
	hash, err := auth.HashPassword(password)
	if err != nil {
		panic(err)
	}
	return identity.User{ID: "usr_1", Username: "alice", DisplayName: "Alice",
		Email: "alice@example.com", PasswordHash: hash,
		Roles: identity.Roles{identity.RoleDev}, Status: identity.UserActive, AuthSource: source}
}

// ldapProbe 构造可编排的 LDAP 探针：返回值与是否被调用。
func ldapProbe(ok bool, callErr error) (func() (bool, error), *bool) {
	called := false
	return func() (bool, error) {
		called = true
		return ok, callErr
	}, &called
}

func TestCheckPasswordLocal(t *testing.T) {
	s := &Service{}
	u := ldapTestUser(identity.AuthLocal, "local-pass")
	probe, called := ldapProbe(true, nil)

	ok, err := s.checkPassword(context.Background(), u, "local-pass", probe)
	if err != nil || !ok {
		t.Fatalf("本地密码正确应通过: %v %v", ok, err)
	}
	if *called {
		t.Fatal("local 成员不应触发 LDAP 拨号")
	}
	ok, err = s.checkPassword(context.Background(), u, "wrong", probe)
	if err != nil || ok {
		t.Fatalf("本地密码错误应失败: %v %v", ok, err)
	}
	if *called {
		t.Fatal("local 成员密码错误也不应触发 LDAP")
	}
}

func TestCheckPasswordLDAPOnly(t *testing.T) {
	s := &Service{}
	u := ldapTestUser(identity.AuthLDAP, "local-pass") // 本地 hash 存在但不应被使用

	probe, called := ldapProbe(true, nil)
	ok, err := s.checkPassword(context.Background(), u, "ldap-pass", probe)
	if err != nil || !ok {
		t.Fatalf("LDAP 通过应登录: %v %v", ok, err)
	}
	if !*called {
		t.Fatal("ldap 成员必须走 LDAP 探针")
	}
	// LDAP 拒绝（密码不匹配）→ 登录失败，即使本地 hash 恰好匹配。
	probe, _ = ldapProbe(false, nil)
	ok, err = s.checkPassword(context.Background(), u, "local-pass", probe)
	if err != nil || ok {
		t.Fatalf("ldap 成员本地密码正确但 LDAP 拒绝应失败: %v %v", ok, err)
	}
	// LDAP 基础设施错误 → 按认证失败处理（不上抛 5xx）。
	probe, _ = ldapProbe(false, errors.New("dial timeout"))
	ok, err = s.checkPassword(context.Background(), u, "ldap-pass", probe)
	if err != nil || ok {
		t.Fatalf("LDAP 不可达应按认证失败处理: %v %v", ok, err)
	}
}

func TestCheckPasswordBoth(t *testing.T) {
	s := &Service{}
	u := ldapTestUser(identity.AuthBoth, "local-pass")

	// 本地密码正确：不触发 LDAP。
	probe, called := ldapProbe(false, nil)
	ok, err := s.checkPassword(context.Background(), u, "local-pass", probe)
	if err != nil || !ok {
		t.Fatalf("双认证本地密码正确应通过: %v %v", ok, err)
	}
	if *called {
		t.Fatal("本地通过后不应再拨 LDAP")
	}
	// 本地失败、LDAP 通过：任一通过即可登录。
	probe, called = ldapProbe(true, nil)
	ok, err = s.checkPassword(context.Background(), u, "ldap-pass", probe)
	if err != nil || !ok {
		t.Fatalf("双认证 LDAP 密码正确应通过: %v %v", ok, err)
	}
	if !*called {
		t.Fatal("本地失败后应尝试 LDAP")
	}
	// 双双失败 → 拒绝。
	probe, _ = ldapProbe(false, nil)
	ok, err = s.checkPassword(context.Background(), u, "wrong", probe)
	if err != nil || ok {
		t.Fatalf("双认证双双失败应拒绝: %v %v", ok, err)
	}
}

func TestLDAPConfigView(t *testing.T) {
	v := ldapConfigView(store.LDAPConfigRow{}, false)
	if v.Enabled || v.Host != "" || v.Port != 389 {
		t.Fatalf("未保存过应展示默认值: %+v", v)
	}
	if v.TLSMode != "none" || v.UserSearchFilter != "(uid=%s)" || v.TimeoutSeconds != 10 {
		t.Fatalf("缺省 TLS/过滤器/超时应为默认值: %+v", v)
	}
	if v.BindPasswordConfigured {
		t.Fatal("未保存过不应报绑定密码已配置")
	}

	row := store.LDAPConfigRow{
		Enabled: true, Host: "ldap.example.com", Port: 636, TLSMode: "tls",
		BindDN: "cn=admin,dc=example,dc=com", BindPasswordEnc: "enc:v1:xxx",
		UserSearchBase: "ou=people,dc=example,dc=com", UserSearchFilter: "(sAMAccountName=%s)",
		TimeoutSeconds: 15, UpdatedBy: "usr_1",
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	v = ldapConfigView(row, true)
	if !v.Enabled || v.Host != "ldap.example.com" || v.Port != 636 || v.TLSMode != "tls" {
		t.Fatalf("库内字段应展示: %+v", v)
	}
	if !v.BindPasswordConfigured {
		t.Fatal("库内密文存在应报已配置")
	}
	if v.UserSearchFilter != "(sAMAccountName=%s)" || v.TimeoutSeconds != 15 {
		t.Fatalf("检索配置应展示: %+v", v)
	}
	if v.UpdatedAt == "" || v.UpdatedBy != "usr_1" {
		t.Fatalf("更新人/时间应展示: %+v", v)
	}
	// 端口/超时为 0 时回退默认。
	v = ldapConfigView(store.LDAPConfigRow{Enabled: true}, true)
	if v.Port != 389 || v.TimeoutSeconds != 10 {
		t.Fatalf("0 值应回退默认 389/10s: %+v", v)
	}
}

func TestValidateLDAPInput(t *testing.T) {
	if err := validateLDAPInput(LDAPConfigInput{}); err == nil {
		t.Fatal("空入参应报错")
	}
	str := func(v string) *string { return &v }
	num := func(v int) *int { return &v }
	bol := func(v bool) *bool { return &v }

	if err := validateLDAPInput(LDAPConfigInput{Host: str("ldap://x")}); err == nil {
		t.Fatal("带协议的 host 应报错")
	}
	if err := validateLDAPInput(LDAPConfigInput{Host: str("ldap.example.com")}); err != nil {
		t.Fatalf("纯主机名应通过: %v", err)
	}
	if err := validateLDAPInput(LDAPConfigInput{Port: num(65536)}); err == nil {
		t.Fatal("越界端口应报错")
	}
	if err := validateLDAPInput(LDAPConfigInput{Port: num(0)}); err != nil {
		t.Fatalf("0（默认 389）应通过: %v", err)
	}
	for _, mode := range []string{"none", "starttls", "tls", ""} {
		if err := validateLDAPInput(LDAPConfigInput{TLSMode: str(mode)}); err != nil {
			t.Fatalf("tlsMode %q 应通过: %v", mode, err)
		}
	}
	if err := validateLDAPInput(LDAPConfigInput{TLSMode: str("ssl")}); err == nil {
		t.Fatal("非法 tlsMode 应报错")
	}
	if err := validateLDAPInput(LDAPConfigInput{UserSearchFilter: str("(uid=john)")}); err == nil {
		t.Fatal("过滤器缺少登录名占位符应报错")
	}
	if err := validateLDAPInput(LDAPConfigInput{UserSearchFilter: str("(uid=%s)")}); err != nil {
		t.Fatalf("含占位的过滤器应通过: %v", err)
	}
	if err := validateLDAPInput(LDAPConfigInput{TimeoutSeconds: num(0)}); err == nil {
		t.Fatal("超时 0 应报错")
	}
	if err := validateLDAPInput(LDAPConfigInput{TimeoutSeconds: num(121)}); err == nil {
		t.Fatal("超时越界应报错")
	}
	if err := validateLDAPInput(LDAPConfigInput{Enabled: bol(true)}); err != nil {
		t.Fatalf("仅改开关应通过: %v", err)
	}
}
