package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// JWTClaims 是 chalkline 会话声明：user id、用户名与全局角色。
// 短时效无状态（docs/modules/identity.md：不建会话表，吊销走 PAT 与用户禁用）。
type JWTClaims struct {
	Sub      string   `json:"sub"`
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
	Iat      int64    `json:"iat"`
	Exp      int64    `json:"exp"`
}

// JWTManager 签发与校验 HS256 JWT。密钥从环境变量读取；
// 未配置时进程启动即生成临时随机密钥并告警（重启后既有会话失效，生产必须配置）。
type JWTManager struct {
	secret []byte
	now    func() time.Time
}

// NewJWTManager 构造 JWT 管理器；secret 为空时生成临时密钥（调用方负责告警）。
func NewJWTManager(secret string) *JWTManager {
	m := &JWTManager{now: time.Now}
	if secret != "" {
		m.secret = []byte(secret)
	} else {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			panic("auth: 生成临时 JWT 密钥失败")
		}
		m.secret = buf
	}
	return m
}

// EphemeralKey 报告密钥是否为进程内临时生成（调用方据此打告警日志）。
func (m *JWTManager) EphemeralKey() bool { return len(m.secret) == 0 }

// Issue 签发 JWT，ttl 为有效期。
func (m *JWTManager) Issue(userID, username string, roles []string, ttl time.Duration) (string, error) {
	now := m.now()
	claims := JWTClaims{
		Sub: userID, Username: username, Roles: roles,
		Iat: now.Unix(), Exp: now.Add(ttl).Unix(),
	}
	header := base64URL([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("auth: 序列化声明失败: %w", err)
	}
	payload := header + "." + base64URL(body)
	return payload + "." + base64URL(sign(m.secret, payload)), nil
}

// Verify 校验签名与有效期，返回声明。
func (m *JWTManager) Verify(token string) (JWTClaims, error) {
	var claims JWTClaims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, errors.New("auth: JWT 格式非法")
	}
	payload := parts[0] + "." + parts[1]
	want := sign(m.secret, payload)
	if !hmac.Equal([]byte(parts[2]), []byte(base64URL(want))) {
		return claims, errors.New("auth: JWT 签名不匹配")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, errors.New("auth: JWT 载荷非法")
	}
	var header struct {
		Alg string `json:"alg"`
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(hb, &header) != nil || header.Alg != "HS256" {
		return claims, errors.New("auth: JWT 头非法")
	}
	if err := json.Unmarshal(body, &claims); err != nil {
		return claims, errors.New("auth: JWT 声明非法")
	}
	if m.now().Unix() >= claims.Exp {
		return claims, errors.New("auth: JWT 已过期")
	}
	return claims, nil
}

func sign(secret []byte, payload string) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func base64URL(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
