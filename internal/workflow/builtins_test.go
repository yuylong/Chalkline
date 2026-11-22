package workflow

import "testing"

// 内置模板必须全部可解析可校验——它们是 M1 种子数据，坏一个就起不来。
func TestBuiltinsAllValid(t *testing.T) {
	builtins, err := LoadBuiltins()
	if err != nil {
		t.Fatalf("内置模板解析失败: %v", err)
	}
	want := 5 // 两个生命周期 + 三个任务模板
	if len(builtins) != want {
		t.Fatalf("内置模板数 = %d, 期望 %d", len(builtins), want)
	}
	scopes := map[string]int{}
	for name, def := range builtins {
		if def.ID == "" || def.Name == "" {
			t.Errorf("模板 %s 缺少 id 或 name", name)
		}
		scopes[string(def.Scope)]++
	}
	if scopes["iteration"] != 2 || scopes["task"] != 3 {
		t.Errorf("scope 分布异常: %+v", scopes)
	}
}

func TestLifecycleTemplateParallelStructure(t *testing.T) {
	def, err := LoadBuiltin("wf-iteration-lifecycle.yaml")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	// 分叉：竞品调研通过后，产品规划与关键技术调研同时就绪。
	succ := def.Successors("research_market")
	if len(succ) != 2 {
		t.Fatalf("research_market 后继数 = %d, 期望 2（并行分叉）", len(succ))
	}
	// 汇合：研发的前驱是任务拆解 + 关键技术调研（AND 语义）。
	pred := def.Predecessors("build")
	if len(pred) != 2 {
		t.Fatalf("build 前驱数 = %d, 期望 2（AND 汇合）", len(pred))
	}
	// 允许建任务的节点：任务拆解 / 研发 / 测试设计。
	allow := map[string]bool{}
	for _, n := range def.Nodes {
		if n.AllowsTaskCreation {
			allow[n.ID] = true
		}
	}
	for _, id := range []string{"breakdown", "build", "test_design"} {
		if !allow[id] {
			t.Errorf("节点 %s 应允许建任务", id)
		}
	}
	if len(allow) != 3 {
		t.Errorf("allows_task_creation 节点数 = %d, 期望 3", len(allow))
	}
	// 发布节点声明迭代收口。
	n, _ := def.Node("release")
	if !n.RequireIterationTasksDone {
		t.Error("release 节点应声明 require_iteration_tasks_done")
	}
}
