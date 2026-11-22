// Package auth 承载鉴权原语：argon2id 密码哈希、HS256 JWT、个人访问令牌（PAT）、
// HTTP 鉴权中间件。人用 JWT、MCP 用 PAT，二者共用 identity 的权限模型（docs/tech.md）。
// 本包不 import api/mcp，HTTP 包络在中间件内自行输出，避免反向依赖（docs/structure.md）。
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id 参数（OWASP 推荐档：m=32MB, t=3, p=2）。
const (
	argonTime    = 3
	argonMemory  = 32 * 1024 // KiB
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// HashPassword 生成 argon2id 编码串：$argon2id$v=19$m=32768,t=3,p=2$salt$hash。
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: 生成盐值失败: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword 校验密码与编码 hash 是否匹配（恒定时间比较）。
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, fmt.Errorf("auth: 密码 hash 格式非法")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, fmt.Errorf("auth: 密码 hash 版本非法")
	}
	var m, t, p int
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, fmt.Errorf("auth: 密码 hash 参数非法")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("auth: 密码 hash 盐值非法")
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("auth: 密码 hash 摘要非法")
	}
	got := argon2.IDKey([]byte(password), salt, uint32(t), uint32(m), uint8(p), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
