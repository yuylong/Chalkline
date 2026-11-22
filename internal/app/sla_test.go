package app

// sla_test.go：评审 SLA 提醒级别判定（纯函数，M3）。

import (
	"testing"
	"time"
)

func TestSLAReminderLevel(t *testing.T) {
	sla, esc := 24*time.Hour, 72*time.Hour
	cases := []struct {
		age  time.Duration
		want int
	}{
		{0, 0},
		{23 * time.Hour, 0},
		{24 * time.Hour, 1},
		{71 * time.Hour, 1},
		{72 * time.Hour, 2},
		{30 * 24 * time.Hour, 2},
	}
	for _, c := range cases {
		if got := SLAReminderLevel(c.age, sla, esc); got != c.want {
			t.Fatalf("age=%v 应为 level %d，实际 %d", c.age, c.want, got)
		}
	}
}

func TestReviewSLADefaults(t *testing.T) {
	s := &Service{}
	if s.reviewSLA() != 24*time.Hour {
		t.Fatalf("默认 SLA 应为 24h，实际 %v", s.reviewSLA())
	}
	if s.reviewEscalation() != 72*time.Hour {
		t.Fatalf("默认升级阈值应为 3×SLA=72h，实际 %v", s.reviewEscalation())
	}
	s.ReviewSLA = time.Hour
	s.ReviewEscalation = 2 * time.Hour
	if s.reviewSLA() != time.Hour || s.reviewEscalation() != 2*time.Hour {
		t.Fatal("显式配置应覆盖默认值")
	}
}
