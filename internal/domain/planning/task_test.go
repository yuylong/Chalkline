package planning

import (
	"errors"
	"testing"
)

// bug 类型规则（docs/domain/model.md「任务拆解规则」）：
// bug 只能作为一级任务、不可拆解；修复是原子工作。
func TestBugKindRules(t *testing.T) {
	if !KindBug.Valid() {
		t.Fatal("bug 应是合法任务类型")
	}

	// 一级 bug 任务合法。
	num, err := FormatTaskNumber("CHK", 12)
	if err != nil {
		t.Fatalf("编号构造失败: %v", err)
	}
	bug, err := NewTask("tsk_1", "prd_1", "itr_1", nil, num, "登录页白屏", KindBug, PriorityHigh, "wf-task-bugfix")
	if err != nil {
		t.Fatalf("一级 bug 任务应可创建: %v", err)
	}

	// bug 不可拆解：父是 bug → 拒绝。
	childNum, err := FormatTaskNumber("CHK", 12, 1)
	if err != nil {
		t.Fatalf("子编号构造失败: %v", err)
	}
	parent := bug
	if _, err := NewTask("tsk_2", "prd_1", "itr_1", &parent, childNum, "修复切片", KindDev, PriorityNormal, ""); err == nil {
		t.Fatal("拆解 bug 父任务应被拒绝")
	} else if !errors.Is(err, ErrValidation) {
		t.Errorf("错误应包装 ErrValidation: %v", err)
	}

	// bug 不可作为子任务：子是 bug → 拒绝。
	devNum, _ := FormatTaskNumber("CHK", 13)
	dev, err := NewTask("tsk_3", "prd_1", "itr_1", nil, devNum, "支付流程", KindDev, PriorityNormal, "")
	if err != nil {
		t.Fatalf("一级 dev 任务构造失败: %v", err)
	}
	if _, err := NewTask("tsk_4", "prd_1", "itr_1", &dev, childNum, "子级缺陷", KindBug, PriorityHigh, ""); err == nil {
		t.Fatal("bug 挂为子任务应被拒绝")
	}
}
