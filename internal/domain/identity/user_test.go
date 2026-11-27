package identity

import "testing"

func validUser() User {
	return User{ID: "usr_91c2", Username: "yuwen", DisplayName: "于文",
		Email: "yuwen@example.com", PasswordHash: "$argon2id$v=19$mock",
		Roles: Roles{RolePM, RoleDev}, Status: UserActive}
}

func TestUserValidate(t *testing.T) {
	if err := validUser().Validate(); err != nil {
		t.Fatalf("合法用户被拒: %v", err)
	}
	u := validUser()
	u.Username = "于文"
	if err := u.Validate(); err == nil {
		t.Error("中文 username 应拒绝")
	}
	u = validUser()
	u.Email = "not-an-email"
	if err := u.Validate(); err == nil {
		t.Error("非法 email 应拒绝")
	}
	u = validUser()
	u.Roles = nil
	if err := u.Validate(); err == nil {
		t.Error("无角色应拒绝")
	}
	u = validUser()
	u.Status = UserStatus("frozen")
	if err := u.Validate(); err == nil {
		t.Error("未知状态应拒绝")
	}
}

func TestUserAuthSource(t *testing.T) {
	if err := validUser().Validate(); err != nil {
		t.Fatalf("空认证来源应按 local 通过: %v", err)
	}
	for _, a := range []AuthSource{AuthLocal, AuthLDAP, AuthBoth} {
		u := validUser()
		u.AuthSource = a
		if err := u.Validate(); err != nil {
			t.Errorf("认证来源 %q 应通过: %v", a, err)
		}
	}
	u := validUser()
	u.AuthSource = AuthSource("oauth")
	if err := u.Validate(); err == nil {
		t.Error("未知认证来源应拒绝")
	}
	if AuthLDAP.UsesLDAP() != true || AuthBoth.UsesLDAP() != true || AuthLocal.UsesLDAP() != false {
		t.Error("UsesLDAP 判定不符")
	}
	// NewUser 应把空认证来源归一化为 local。
	nu, err := NewUser("usr_1", "alice", "Alice", "a@example.com", "$argon2id$v=19$mock",
		Roles{RoleDev}, UserActive)
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	if nu.AuthSource != AuthLocal {
		t.Fatalf("空认证来源应归一化为 local，实际 %q", nu.AuthSource)
	}
}

func TestParseRoles(t *testing.T) {
	rs, err := ParseRoles("pm, dev")
	if err != nil || len(rs) != 2 {
		t.Fatalf("解析: %v %v", rs, err)
	}
	if _, err := ParseRoles("pm, wizard"); err == nil {
		t.Error("未知角色应拒绝")
	}
	if _, err := ParseRoles("  "); err == nil {
		t.Error("空串应拒绝")
	}
}
