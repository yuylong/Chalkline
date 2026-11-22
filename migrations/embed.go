// Package migrations 嵌入 goose SQL 迁移脚本。
// 策略：只前进，不自动回滚（docs/tech.md）。chalklined 启动时统一应用。
package migrations

import "embed"

// FS 是全部迁移脚本；由 internal/store 的迁移器消费。
//
//go:embed *.sql
var FS embed.FS
