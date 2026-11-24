package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// PAT 前缀：明文令牌形如 chkpat_xxxx…，仅签发时返回一次；
// 库内只存 SHA-256 hash（docs/modules/identity.md：PAT 不可用于控制台登录）。
const PATPrefix = "chkpat_"

// NewPAT 生成明文令牌（43 位随机）。
func NewPAT() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: 生成 PAT 失败: %w", err)
	}
	return PATPrefix + hex.EncodeToString(buf), nil
}

// HashPAT 计算令牌的存储 hash（hex）。
func HashPAT(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// IsPAT 报告令牌是否为 PAT 形态（与 JWT 区分的入口判定）。
func IsPAT(token string) bool {
	return len(token) > len(PATPrefix) && token[:len(PATPrefix)] == PATPrefix
}
