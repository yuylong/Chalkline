package product

import "testing"

func TestProductKey(t *testing.T) {
	for _, good := range []Key{"AB", "CHK", "ABCDEFGH"} {
		if !good.Valid() {
			t.Errorf("%q 应合法", string(good))
		}
	}
	for _, bad := range []Key{"A", "A1", "chk", "CHECKLONGX", "AB-C", ""} {
		if bad.Valid() {
			t.Errorf("%q 应非法", string(bad))
		}
	}
}

func TestProductLifecycle(t *testing.T) {
	p, err := New("prd_1", "CHK", "墨线", "规格驱动协作平台", "usr_admin")
	if err != nil {
		t.Fatalf("构造产品: %v", err)
	}
	if p.Status != StatusActive {
		t.Error("新产品应 active")
	}
	if err := p.Archive(); err != nil {
		t.Fatalf("归档: %v", err)
	}
	if err := p.Archive(); err == nil {
		t.Error("重复归档应拒绝")
	}
}

func TestMemberOwnerRetained(t *testing.T) {
	members := []Member{
		{ProductID: "p1", UserID: "u1", MemberRole: MemberOwner},
		{ProductID: "p1", UserID: "u2", MemberRole: MemberMember},
	}
	if err := EnsureOwnerRetained(members, "u2"); err != nil {
		t.Errorf("移除普通成员不受影响: %v", err)
	}
	if err := EnsureOwnerRetained(members, "u1"); err == nil {
		t.Error("移除最后一个 owner 应拒绝（不变量 17）")
	}
}

func TestModuleTree(t *testing.T) {
	// 物化路径与深度。
	root, err := ChildPath("", "catalog")
	if err != nil || root != "/catalog" {
		t.Fatalf("根路径 = %s, %v", root, err)
	}
	child, err := ChildPath(root, "task-tree")
	if err != nil || child != "/catalog/task-tree" {
		t.Fatalf("子路径 = %s, %v", child, err)
	}
	// 同级唯一。
	siblings := []Module{{ID: "m1", ProductID: "p1", Name: "catalog", Path: "/catalog"}}
	if err := EnsureSiblingNameUnique(siblings, "catalog"); err == nil {
		t.Error("同级重名应拒绝（不变量 1）")
	}
	// 深度上限 5：/a/b/c/d/e 是第 5 层，其下不可再有子模块。
	if _, err := ChildPath("/a/b/c/d/e", "f"); err == nil {
		t.Error("第 6 层应拒绝")
	}
	if _, err := ChildPath("/catalog", ""); err == nil {
		t.Error("空模块名应拒绝")
	}
	if err := ValidatePath("catalog"); err == nil {
		t.Error("路径必须以 / 开头")
	}
}
