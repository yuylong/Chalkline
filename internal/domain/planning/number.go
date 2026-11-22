// Package planning 承载迭代与任务：编号规则、多级拆解、指派与归属不变量。
// 对应 docs/domain/model.md 的 Iteration / Task / 任务编号规则 / 任务拆解规则。
package planning

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MaxTaskDepth 是任务树深度上限（不变量 1：任务深度 ≤ 3）。
const MaxTaskDepth = 3

var (
	iterationNumberRe = regexp.MustCompile(`^[A-Z]{2,8}-IT([1-9][0-9]*)$`)
	taskNumberRe      = regexp.MustCompile(`^[A-Z]{2,8}-([1-9][0-9]*)(\.[1-9][0-9]*){0,2}$`)
)

// IterationNumber 是迭代编号，形如 CHK-IT5，产品内递增。
type IterationNumber string

// FormatIterationNumber 由产品 key 与递增序号合成编号。
func FormatIterationNumber(productKey string, seq int) IterationNumber {
	return IterationNumber(fmt.Sprintf("%s-IT%d", productKey, seq))
}

// Parse 校验迭代编号格式。
func (n IterationNumber) Parse() (productKey string, seq int, err error) {
	m := iterationNumberRe.FindStringSubmatch(string(n))
	if m == nil {
		return "", 0, fmt.Errorf("迭代编号 %q 非法，应为 <产品key>-IT<n>（如 CHK-IT5）", string(n))
	}
	seq, err = strconv.Atoi(m[1])
	if err != nil {
		return "", 0, fmt.Errorf("迭代编号 %q 序号非法: %w", string(n), err)
	}
	key := strings.TrimSuffix(strings.TrimSuffix(string(n), m[1]), "-IT")
	return key, seq, nil
}

// TaskNumber 是多级任务编号：顶层 CHK-12，子任务 CHK-12.3，三级 CHK-12.3.2。
// 编号在创建时分配，永不复用、永不重排；任务取消后其编号作废（不变量 3）。
type TaskNumber string

// FormatTaskNumber 由产品 key 与各级序号合成编号。
func FormatTaskNumber(productKey string, seqs ...int) (TaskNumber, error) {
	if len(seqs) == 0 || len(seqs) > MaxTaskDepth {
		return "", fmt.Errorf("任务编号层级 %d 非法，支持 1-%d 级", len(seqs), MaxTaskDepth)
	}
	parts := make([]string, len(seqs))
	for i, s := range seqs {
		if s < 1 {
			return "", fmt.Errorf("序号 %d 非法，需从 1 开始", s)
		}
		parts[i] = strconv.Itoa(s)
	}
	return TaskNumber(productKey + "-" + strings.Join(parts, ".")), nil
}

// Parse 校验并解析任务编号，返回产品 key 与各级序号。
func (n TaskNumber) Parse() (productKey string, seqs []int, err error) {
	m := taskNumberRe.FindStringSubmatch(string(n))
	if m == nil {
		return "", nil, fmt.Errorf("任务编号 %q 非法，应为 <key>-<n>[.<m>][.<k>]（如 CHK-12.3）", string(n))
	}
	body := m[0][strings.Index(m[0], "-")+1:]
	for _, p := range strings.Split(body, ".") {
		s, err := strconv.Atoi(p)
		if err != nil {
			return "", nil, fmt.Errorf("任务编号 %q 序号非法: %w", string(n), err)
		}
		seqs = append(seqs, s)
	}
	return string(n[:strings.Index(string(n), "-")]), seqs, nil
}

// Depth 返回编号隐含的任务层级（1–3）。
func (n TaskNumber) Depth() (int, error) {
	_, seqs, err := n.Parse()
	if err != nil {
		return 0, err
	}
	return len(seqs), nil
}

// Parent 返回父任务编号；顶层任务没有父级（返回空串与 false）。
func (n TaskNumber) Parent() (TaskNumber, bool, error) {
	_, seqs, err := n.Parse()
	if err != nil {
		return "", false, err
	}
	if len(seqs) == 1 {
		return "", false, nil
	}
	parent, err := FormatTaskNumber(string(n[:strings.Index(string(n), "-")]), seqs[:len(seqs)-1]...)
	if err != nil {
		return "", false, err
	}
	return parent, true, nil
}

// Validate 检查编号格式与深度上限。
func (n TaskNumber) Validate() error {
	d, err := n.Depth()
	if err != nil {
		return err
	}
	if d > MaxTaskDepth {
		return fmt.Errorf("任务编号 %q 层级 %d 超过上限 %d", string(n), d, MaxTaskDepth)
	}
	return nil
}
