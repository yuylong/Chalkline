package store

// users_test.go：收件人合并前的成员 id 归一化（去空、去重、保序）。

import "testing"

func TestCompactIDs(t *testing.T) {
	got := compactIDs([]string{"usr_2", "", "usr_1", " usr_2 ", "usr_1"})
	want := []string{"usr_2", "usr_1"}
	if len(got) != len(want) {
		t.Fatalf("应去空去重，实际 %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("应保持首次出现顺序 %v，实际 %v", want, got)
		}
	}
	if got := compactIDs(nil); len(got) != 0 {
		t.Fatalf("空入参应返回空切片，实际 %v", got)
	}
}
