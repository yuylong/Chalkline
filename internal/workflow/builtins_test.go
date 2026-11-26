package workflow

import "testing"

// 内置模板必须全部可解析可校验——它们是 M1 种子数据，坏一个就起不来。
func TestBuiltinsAllValid(t *testing.T) {
	builtins, err := LoadBuiltins()
	if err != nil {
		t.Fatalf("内置模板解析失败: %v", err)
	}
	want := 7 // 两个生命周期 + 五个任务模板
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
	if scopes["iteration"] != 2 || scopes["task"] != 5 {
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
	// 允许建任务的节点（按类型白名单）：任务拆解（全类型）/ 研发 [dev, bug] /
	// 测试设计 [test] / 测评 [bug]。
	allow := map[string][]string{}
	for _, n := range def.Nodes {
		if n.AllowsTaskCreation || len(n.AllowsTaskKinds) > 0 {
			allow[n.ID] = n.AllowsTaskKinds
		}
	}
	if _, ok := allow["breakdown"]; !ok {
		t.Error("节点 breakdown 应允许建任务")
	}
	if kinds := allow["build"]; len(kinds) != 2 || kinds[0] != "dev" || kinds[1] != "bug" {
		t.Errorf("build 白名单异常: %v", kinds)
	}
	if kinds := allow["test_design"]; len(kinds) != 1 || kinds[0] != "test" {
		t.Errorf("test_design 白名单异常: %v", kinds)
	}
	if kinds := allow["assess"]; len(kinds) != 1 || kinds[0] != "bug" {
		t.Errorf("assess 应只允许建 bug 任务: %v", kinds)
	}
	if len(allow) != 4 {
		t.Errorf("允许建任务的节点数 = %d, 期望 4", len(allow))
	}
	// 白名单判定：测评允许 bug、拒绝 dev。
	assess, _ := def.Node("assess")
	if !assess.AllowsTaskKind("bug") || assess.AllowsTaskKind("dev") {
		t.Error("assess 应只允许 bug 类型")
	}
	// 发布节点声明迭代收口。
	n, _ := def.Node("release")
	if !n.RequireIterationTasksDone {
		t.Error("release 节点应声明 require_iteration_tasks_done")
	}
}
