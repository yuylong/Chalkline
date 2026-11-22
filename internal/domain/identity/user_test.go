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
