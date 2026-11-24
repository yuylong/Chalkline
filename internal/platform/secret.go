package platform

// secret.go：静态凭据的对称加密（AES-256-GCM）。
// 用途：SMTP 发件密码经管理台保存时加密落库（AGENT 红线 8 的唯一例外），
// 密钥来自 CHALKLINE_SECRET_KEY（未设置回退 CHALKLINE_JWT_SECRET）。
// 密文带 enc:v1 版本前缀，便于将来换算法；密钥只进内存，不进日志。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const secretPrefix = "enc:v1:"

// ErrSecretKeyMissing 密钥未配置（调用方转为用户可读错误）。
var ErrSecretKeyMissing = errors.New("platform: 加密密钥未配置")

func newGCM(secret string) (cipher.AEAD, error) {
	if secret == "" {
		return nil, ErrSecretKeyMissing
	}
	// 任意长度的口令先收敛为 32 字节 AES-256 密钥。
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("platform: 构造 AES 失败: %w", err)
	}
	return cipher.NewGCM(block)
}

// Seal 加密明文，输出 enc:v1:<base64(nonce|ciphertext)>。
func Seal(secret, plaintext string) (string, error) {
	gcm, err := newGCM(secret)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("platform: 生成 nonce 失败: %w", err)
	}
	out := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return secretPrefix + base64.StdEncoding.EncodeToString(out), nil
}

// Open 解密 Seal 的产物；密钥不符或密文损坏返回错误（调用方按「未配置」降级）。
func Open(secret, ciphertext string) (string, error) {
	if !strings.HasPrefix(ciphertext, secretPrefix) {
		return "", errors.New("platform: 密文格式不符")
	}
	gcm, err := newGCM(secret)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, secretPrefix))
	if err != nil {
		return "", fmt.Errorf("platform: 密文解码失败: %w", err)
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("platform: 密文长度非法")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("platform: 解密失败（密钥不符或密文损坏）: %w", err)
	}
	return string(plain), nil
}
