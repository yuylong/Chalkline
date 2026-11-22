package planning

import (
	"testing"
	"time"
)

func TestIterationNumber(t *testing.T) {
	n := FormatIterationNumber("CHK", 5)
	if n != "CHK-IT5" {
		t.Fatalf("编号 = %s, 期望 CHK-IT5", n)
	}
	key, seq, err := n.Parse()
	if err != nil || key != "CHK" || seq != 5 {
		t.Fatalf("解析异常: %s %d %v", key, seq, err)
	}
	for _, bad := range []IterationNumber{"chk-it5", "CHK-IT", "CHK-IT0", "CHK-IT05", "CHK-5", "", "CHK-IT5.1"} {
		if _, _, err := bad.Parse(); err == nil {
			t.Errorf("%q 应非法", string(bad))
		}
	}
}

func TestTaskNumber(t *testing.T) {
	n, err := FormatTaskNumber("CHK", 12)
	if err != nil || n != "CHK-12" {
		t.Fatalf("顶层编号 = %s, %v", n, err)
	}
	n, _ = FormatTaskNumber("CHK", 12, 3)
	if n != "CHK-12.3" {
		t.Fatalf("子编号 = %s", n)
	}
	if _, err := FormatTaskNumber("CHK", 1, 2, 3, 4); err == nil {
		t.Error("4 级编号应被拒绝")
	}
	if _, err := FormatTaskNumber("CHK", 0); err == nil {
		t.Error("0 序号应被拒绝")
	}

	if depth, _ := n.Depth(); depth != 2 {
		t.Errorf("深度 = %d, 期望 2", depth)
	}
	parent, ok, _ := n.Parent()
	if !ok || parent != "CHK-12" {
		t.Fatalf("父编号 = %s, %v", parent, ok)
	}
	_, ok, _ = TaskNumber("CHK-12").Parent()
	if ok {
		t.Error("顶层任务没有父级")
	}
	for _, bad := range []TaskNumber{"CHK-12.3.2.1", "chk-12", "CHK-12.", "CHK-.3", "CHK-0", ""} {
		if _, _, err := bad.Parse(); err == nil {
			t.Errorf("%q 应非法", string(bad))
		}
	}
}

func TestDecompositionRules(t *testing.T) {
	parent := Task{ID: "t1", ProductID: "p1", IterationID: "it1", Number: "CHK-12"}
	// 合法子任务：同产品、同迭代、编号延伸一级。
	child, err := NewTask("t2", "p1", "it1", &parent, "CHK-12.3", "子任务",
		KindDev, PriorityNormal, "wf-task-standard")
	if err != nil {
		t.Fatalf("合法拆解: %v", err)
	}
	if child.ParentID != "t1" {
		t.Errorf("parent_id 未回填: %s", child.ParentID)
	}

	// 跨产品。
	if _, err := NewTask("t3", "p2", "it1", &parent, "CHK-12.4", "跨产品",
		KindDev, PriorityNormal, "wf"); err == nil {
		t.Error("跨产品拆解应被拒绝")
	}
	// 跨迭代。
	if _, err := NewTask("t4", "p1", "it2", &parent, "CHK-12.4", "跨迭代",
		KindDev, PriorityNormal, "wf"); err == nil {
		t.Error("跨迭代拆解应被拒绝")
	}
	// 编号前缀不匹配。
	if _, err := NewTask("t5", "p1", "it1", &parent, "CHK-99.4", "编号断裂",
		KindDev, PriorityNormal, "wf"); err == nil {
		t.Error("编号非父级延伸应被拒绝")
	}
	// 深度上限：第 3 层不可再拆。
	grand, _ := NewTask("t6", "p1", "it1", &child, "CHK-12.3.2", "孙任务",
		KindDev, PriorityNormal, "wf")
	if _, err := NewTask("t7", "p1", "it1", &grand, "CHK-12.3.2.9", "超深",
		KindDev, PriorityNormal, "wf"); err == nil {
		t.Error("第 4 层拆解应被拒绝")
	}
}

func TestEnsureChildrenDone(t *testing.T) {
	children := []Task{{ID: "a", Number: "CHK-12.1", Status: TaskDone}, {ID: "b", Number: "CHK-12.2", Status: TaskOpen}}
	if err := EnsureChildrenDone(children, true); err == nil {
		t.Error("子任务未全 done 应阻塞")
	}
	if err := EnsureChildrenDone(children, false); err != nil {
		t.Errorf("模板关闭收口时不应阻塞: %v", err)
	}
	children[1].Status = TaskDone
	if err := EnsureChildrenDone(children, true); err != nil {
		t.Errorf("子任务全 done 不应阻塞: %v", err)
	}
}

func TestIterationClose(t *testing.T) {
	it, err := NewIteration("it1", "p1", "2026 Q4", "交付工作流引擎",
		"CHK-IT1", parseDate("2026-10-09"), parseDate("2026-11-20"), "wf-iteration-lifecycle")
	if err != nil {
		t.Fatalf("构造迭代: %v", err)
	}
	if err := it.Close(false); err == nil {
		t.Error("planned 迭代不可直接收口")
	}
	if err := it.Activate(); err != nil {
		t.Fatalf("激活迭代: %v", err)
	}
	if err := it.Close(true); err == nil {
		t.Error("存在未终结任务时不可收口")
	}
	if err := it.Close(false); err != nil {
		t.Errorf("收口: %v", err)
	}
	if !it.Status.Terminal() {
		t.Error("closed 是终态")
	}
	// 终态不可回退（不变量 11）。
	if err := it.Cancel(false); err == nil {
		t.Error("终态迭代不可取消")
	}
}

func parseDate(s string) time.Time {
	v, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return v
}
