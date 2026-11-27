package auth

// ldap.go：LDAP 认证原语（docs/modules/identity.md「LDAP 认证」）。
// 拨号 →（可选）服务账号绑定 → 按登录名检索用户 DN → 用该 DN + 密码绑定验证。
// 登录名经 EscapeFilter 转义后代入检索过滤器，杜绝 LDAP 注入。
// 本包不 import app/store：配置由 app 层解析后以值传入（依赖倒置同 SMTP）。

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// LDAPSettings 是一次 LDAP 交互所需的全部生效配置（app 层从库内解析后传入）。
type LDAPSettings struct {
	Host             string
	Port             int
	TLSMode          string // none / starttls / tls
	TLSSkipVerify    bool
	BindDN           string // 服务账号 DN；空 = 匿名检索
	BindPassword     string
	UserSearchBase   string
	UserSearchFilter string // 含 %s 占位登录名，如 (uid=%s)
	Timeout          time.Duration
}

// LDAPClient 是无状态的 LDAP 认证客户端（server 组装层注入 app）。
type LDAPClient struct{}

// NewLDAPClient 构造客户端。
func NewLDAPClient() *LDAPClient { return &LDAPClient{} }

// ValidateLDAPSettings 校验生效配置是否齐备（不拨号）。
// host / 检索 base / 过滤器为硬性要素；端口缺省 389。
func ValidateLDAPSettings(s LDAPSettings) error {
	if strings.TrimSpace(s.Host) == "" {
		return errors.New("LDAP 服务器地址未配置")
	}
	if strings.TrimSpace(s.UserSearchBase) == "" {
		return errors.New("用户检索 Base DN 未配置")
	}
	if strings.TrimSpace(s.UserSearchFilter) == "" {
		return errors.New("用户检索过滤器未配置")
	}
	if !strings.Contains(s.UserSearchFilter, "%s") {
		return errors.New("用户检索过滤器需包含 %s 占位登录名，如 (uid=%s)")
	}
	switch s.TLSMode {
	case "", "none", "starttls", "tls":
	default:
		return fmt.Errorf("TLS 模式 %q 非法，应为 none / starttls / tls", s.TLSMode)
	}
	return nil
}

// ldapFilter 把登录名转义后代入过滤器模板。
func ldapFilter(filter, username string) string {
	return fmt.Sprintf(filter, ldap.EscapeFilter(username))
}

// dial 按 TLS 模式建立连接；错误信息标注阶段，便于管理台直接展示。
func dial(ctx context.Context, s LDAPSettings) (*ldap.Conn, error) {
	if s.Port == 0 {
		s.Port = 389
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	scheme := "ldap"
	if s.TLSMode == "tls" {
		scheme = "ldaps"
	}
	addr := net.JoinHostPort(s.Host, fmt.Sprintf("%d", s.Port))
	tlsCfg := &tls.Config{InsecureSkipVerify: s.TLSSkipVerify, ServerName: s.Host} //nolint:gosec // 内网自签场景由管理台显式开启
	conn, err := ldap.DialURL(scheme+"://"+addr,
		ldap.DialWithDialer(&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}),
		ldap.DialWithTLSConfig(tlsCfg))
	if err != nil {
		return nil, fmt.Errorf("连接 %s 失败: %w", addr, err)
	}
	if s.TLSMode == "starttls" {
		if err := conn.StartTLS(tlsCfg); err != nil {
			conn.Close()
			return nil, fmt.Errorf("STARTTLS 升级失败（%s）: %w", addr, err)
		}
	}
	return conn, nil
}

// bindService 用服务账号（或匿名）绑定并返回已就绪的连接。
func bindService(conn *ldap.Conn, s LDAPSettings) error {
	if err := conn.Bind(s.BindDN, s.BindPassword); err != nil {
		return fmt.Errorf("服务账号绑定失败（DN %q）: %w", s.BindDN, err)
	}
	return nil
}

// searchUserDN 按登录名检索用户条目 DN；未命中返回 ErrNotFound 语义（ok=false）。
func searchUserDN(ctx context.Context, conn *ldap.Conn, s LDAPSettings, username string) (string, error) {
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetTimeout(time.Until(deadline)) //nolint:errcheck // 尽力收紧检索超时，拨号超时已由 Dialer 保证
	}
	req := ldap.NewSearchRequest(
		s.UserSearchBase, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 0, false,
		ldapFilter(s.UserSearchFilter, username), []string{}, nil)
	res, err := conn.Search(req)
	if err != nil {
		return "", fmt.Errorf("检索用户 %q 失败（Base %q）: %w", username, s.UserSearchBase, err)
	}
	if len(res.Entries) == 0 {
		return "", nil // 用户在 LDAP 中不存在：认证失败，非错误
	}
	if len(res.Entries) > 1 {
		return "", fmt.Errorf("检索用户 %q 命中 %d 条，过滤器需保证唯一", username, len(res.Entries))
	}
	return res.Entries[0].DN, nil
}

// Authenticate 校验用户名/密码：检索 DN → 以 DN + 密码绑定。
// 返回 (false, nil) 表示凭据不匹配或用户不存在；(false, err) 表示 LDAP 不可达等
// 基础设施错误（调用方决定如何呈现）。空密码直接拒绝（LDAP 空密码 = 匿名绑定）。
func (c *LDAPClient) Authenticate(ctx context.Context, s LDAPSettings, username, password string) (bool, error) {
	if username == "" || password == "" {
		return false, nil
	}
	if err := ValidateLDAPSettings(s); err != nil {
		return false, err
	}
	conn, err := dial(ctx, s)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	if err := bindService(conn, s); err != nil {
		return false, err
	}
	dn, err := searchUserDN(ctx, conn, s, username)
	if err != nil {
		return false, err
	}
	if dn == "" {
		return false, nil
	}
	if err := conn.Bind(dn, password); err != nil {
		return false, nil // 密码错误：凭据不匹配，不作为基础设施错误上抛
	}
	return true, nil
}

// Ping 测试连通性：拨号 → 服务账号（或匿名）绑定 → 用过滤器试检索一条。
// 供管理台「测试连接」按钮使用；错误信息按阶段标注、可直接展示。
func (c *LDAPClient) Ping(ctx context.Context, s LDAPSettings) error {
	if err := ValidateLDAPSettings(s); err != nil {
		return err
	}
	conn, err := dial(ctx, s)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := bindService(conn, s); err != nil {
		return err
	}
	// 用过滤器试检索（登录名用固定探针，只验证 base 可达与过滤器语法）。
	if _, err := searchUserDN(ctx, conn, s, "connectivity-probe"); err != nil {
		return err
	}
	return nil
}
