package app

// ldapconfig.go：LDAP 认证服务器配置用例（docs/modules/identity.md「LDAP 认证」）。
// 管理台维护服务器地址/TLS/绑定账号/用户检索；绑定密码经 platform.Seal 加密落库
// （与 SMTP 密码同属红线 8 的管理台例外），视图永不回显，只报「是否已配置」。
// 登录按成员 auth_source 分支：local 仅本地密码、ldap 仅 LDAP、both 双认证任一通过。

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/yuylong/Chalkline/internal/auth"
	"github.com/yuylong/Chalkline/internal/platform"
	"github.com/yuylong/Chalkline/internal/store"
)

// LDAPAuthenticator 是 LDAP 校验通道（依赖倒置：auth.LDAPClient 实现，
// server 组装层注入；nil 时 LDAP/both 成员登录只走本地密码并告警）。
type LDAPAuthenticator interface {
	Authenticate(ctx context.Context, s auth.LDAPSettings, username, password string) (bool, error)
	Ping(ctx context.Context, s auth.LDAPSettings) error
}

// LDAPConfigView 是管理台看到的 LDAP 配置（绑定密码只报布尔，永不回显）。
type LDAPConfigView struct {
	Enabled                bool   `json:"enabled"`
	Host                   string `json:"host"`
	Port                   int    `json:"port"`
	TLSMode                string `json:"tlsMode"`       // none / starttls / tls
	TLSSkipVerify          bool   `json:"tlsSkipVerify"` // 跳过证书校验（内网自签场景）
	BindDN                 string `json:"bindDn"`        // 服务账号 DN；空 = 匿名检索
	BindPasswordConfigured bool   `json:"bindPasswordConfigured"`
	UserSearchBase         string `json:"userSearchBase"`
	UserSearchFilter       string `json:"userSearchFilter"` // 含 %s 占位登录名
	TimeoutSeconds         int    `json:"timeoutSeconds"`
	UpdatedBy              string `json:"updatedBy,omitempty"`
	UpdatedAt              string `json:"updatedAt,omitempty"`
}

// LDAPConfigInput 是配置更新入参（nil 即不改；BindPassword 空串 = 清除库内密码）。
type LDAPConfigInput struct {
	Enabled          *bool
	Host             *string
	Port             *int
	TLSMode          *string
	TLSSkipVerify    *bool
	BindDN           *string
	BindPassword     *string
	UserSearchBase   *string
	UserSearchFilter *string
	TimeoutSeconds   *int
}

// LDAPTestResult 是「测试连接」的产物。
type LDAPTestResult struct {
	OK       bool   `json:"ok"`
	Mode     string `json:"mode"`     // connectivity（拨号+绑定+检索）| bind（完整用户登录验证）
	Username string `json:"username"` // bind 模式下被验证的登录名
}

// GetLDAPConfig 读配置视图（admin）。
func (s *Service) GetLDAPConfig(ctx context.Context, actor Actor) (LDAPConfigView, error) {
	if !actor.IsAdmin {
		return LDAPConfigView{}, RoleRequired("需要 admin 管理 LDAP 配置")
	}
	row, err := store.LDAPConfig(ctx, s.DB.Q())
	stored := true
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return LDAPConfigView{}, err
		}
		stored = false
	}
	return ldapConfigView(row, stored), nil
}

// UpdateLDAPConfig 保存 LDAP 配置（admin，写审计；绑定密码只落密文，审计只记是否设置）。
func (s *Service) UpdateLDAPConfig(ctx context.Context, actor Actor, in LDAPConfigInput) (LDAPConfigView, error) {
	if !actor.IsAdmin {
		return LDAPConfigView{}, RoleRequired("需要 admin 管理 LDAP 配置")
	}
	if err := validateLDAPInput(in); err != nil {
		return LDAPConfigView{}, err
	}
	var passwordEnc string
	if in.BindPassword != nil && strings.TrimSpace(*in.BindPassword) != "" {
		enc, err := platform.Seal(s.SecretKey, *in.BindPassword)
		if err != nil {
			return LDAPConfigView{}, Invalid("绑定密码加密失败：请设置 CHALKLINE_SECRET_KEY（或 CHALKLINE_JWT_SECRET）后重试")
		}
		passwordEnc = enc
	}
	var row store.LDAPConfigRow
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		cur, err := store.LDAPConfig(ctx, q)
		switch {
		case err == nil:
			row = cur
		case errors.Is(err, store.ErrNotFound):
			row = store.LDAPConfigRow{Port: 389, TLSMode: "none", UserSearchFilter: "(uid=%s)", TimeoutSeconds: 10}
		default:
			return err
		}
		if in.Enabled != nil {
			row.Enabled = *in.Enabled
		}
		if in.Host != nil {
			row.Host = strings.TrimSpace(*in.Host)
		}
		if in.Port != nil {
			row.Port = *in.Port
		}
		if in.TLSMode != nil {
			row.TLSMode = strings.ToLower(strings.TrimSpace(*in.TLSMode))
		}
		if in.TLSSkipVerify != nil {
			row.TLSSkipVerify = *in.TLSSkipVerify
		}
		if in.BindDN != nil {
			row.BindDN = strings.TrimSpace(*in.BindDN)
		}
		if in.BindPassword != nil {
			if strings.TrimSpace(*in.BindPassword) == "" {
				row.BindPasswordEnc = "" // 清除（含纯空白）：回退匿名绑定
			} else {
				row.BindPasswordEnc = passwordEnc
			}
		}
		if in.UserSearchBase != nil {
			row.UserSearchBase = strings.TrimSpace(*in.UserSearchBase)
		}
		if in.UserSearchFilter != nil {
			row.UserSearchFilter = strings.TrimSpace(*in.UserSearchFilter)
		}
		if in.TimeoutSeconds != nil {
			row.TimeoutSeconds = *in.TimeoutSeconds
		}
		row.UpdatedBy = actor.UserID
		if err := store.UpsertLDAPConfig(ctx, q, row); err != nil {
			return err
		}
		return audit(ctx, q, actor, "auth.ldap.update", "ldap", store.LDAPConfigKey, map[string]any{
			"enabled": row.Enabled, "host": row.Host, "port": row.Port,
			"tls_mode": row.TLSMode, "tls_skip_verify": row.TLSSkipVerify,
			"bind_dn": row.BindDN, "user_search_base": row.UserSearchBase,
			"user_search_filter": row.UserSearchFilter, "timeout_seconds": row.TimeoutSeconds,
			"bind_password_set": row.BindPasswordEnc != "",
		})
	})
	if err != nil {
		return LDAPConfigView{}, err
	}
	row.UpdatedAt = s.Now().UTC()
	return ldapConfigView(row, true), nil
}

// EffectiveLDAP 解析当前生效的 LDAP 配置（登录与测试连接共用）。
// ok=false 表示未启用或要素不齐（登录按本地密码处理）。
// 密钥不符导致解密失败时按「密码未配置」降级并告警，不阻断登录。
func (s *Service) EffectiveLDAP(ctx context.Context) (auth.LDAPSettings, bool, error) {
	row, err := store.LDAPConfig(ctx, s.DB.Q())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return auth.LDAPSettings{}, false, nil
		}
		return auth.LDAPSettings{}, false, err
	}
	rt := auth.LDAPSettings{
		Host: row.Host, Port: row.Port, TLSMode: row.TLSMode, TLSSkipVerify: row.TLSSkipVerify,
		BindDN: row.BindDN, UserSearchBase: row.UserSearchBase,
		UserSearchFilter: row.UserSearchFilter,
		Timeout:          time.Duration(row.TimeoutSeconds) * time.Second,
	}
	if row.BindPasswordEnc != "" {
		pw, err := platform.Open(s.SecretKey, row.BindPasswordEnc)
		if err != nil {
			s.Log.Warn("LDAP 绑定密码解密失败，按未配置降级（检查 CHALKLINE_SECRET_KEY 是否变更）")
		} else {
			rt.BindPassword = pw
		}
	}
	if !row.Enabled {
		return rt, false, nil
	}
	if err := auth.ValidateLDAPSettings(rt); err != nil {
		return rt, false, nil // 已启用但要素不齐：视为不可用，登录回退本地密码
	}
	return rt, true, nil
}

// TestLDAPConfig 测试 LDAP 连通性（admin；写审计）。
// 传入 username+password 时做完整登录验证（检索 + 用户绑定），否则只做
// 拨号 + 服务账号绑定 + 试检索。失败返回 LDAP_TEST_FAILED（502），
// 错误信息按阶段标注、可直接展示给管理台。
func (s *Service) TestLDAPConfig(ctx context.Context, actor Actor, username, password string) (LDAPTestResult, error) {
	if !actor.IsAdmin {
		return LDAPTestResult{}, RoleRequired("需要 admin 测试 LDAP 配置")
	}
	if s.LDAP == nil {
		return LDAPTestResult{}, Internal(errors.New("LDAP 认证通道未装配"))
	}
	rt, ok, err := s.EffectiveLDAP(ctx)
	if err != nil {
		return LDAPTestResult{}, err
	}
	if !ok {
		return LDAPTestResult{}, Invalid("LDAP 未启用或配置不完整，请补全并保存后重试")
	}
	mode := "connectivity"
	if username != "" {
		mode = "bind"
	}
	if mode == "bind" {
		passed, aerr := s.LDAP.Authenticate(ctx, rt, username, password)
		if aerr != nil {
			return LDAPTestResult{}, ldapTestFailed(aerr)
		}
		if !passed {
			return LDAPTestResult{}, ldapTestFailed(errors.New("用户不存在或密码不匹配"))
		}
	} else if err := s.LDAP.Ping(ctx, rt); err != nil {
		return LDAPTestResult{}, ldapTestFailed(err)
	}
	if err := audit(ctx, s.DB.Q(), actor, "auth.ldap.test", "ldap", store.LDAPConfigKey, map[string]any{
		"host": rt.Host, "port": rt.Port, "tls_mode": rt.TLSMode, "mode": mode,
	}); err != nil {
		return LDAPTestResult{}, err
	}
	return LDAPTestResult{OK: true, Mode: mode, Username: username}, nil
}

// ldapTestFailed 把 LDAP 交互错误包装为可展示的对外错误（502，上游目录问题）。
func ldapTestFailed(err error) *Error {
	return &Error{Status: http.StatusBadGateway, Code: "LDAP_TEST_FAILED",
		Message: "LDAP 测试失败：" + err.Error()}
}

// ldapConfigView 把库内行转为对外视图（绑定密码只报布尔）。
func ldapConfigView(row store.LDAPConfigRow, stored bool) LDAPConfigView {
	v := LDAPConfigView{
		Host: "", Port: 389, TLSMode: "none",
		UserSearchFilter: "(uid=%s)", TimeoutSeconds: 10,
	}
	if stored {
		v.Enabled = row.Enabled
		v.Host = row.Host
		v.Port = row.Port
		v.TLSMode = row.TLSMode
		v.TLSSkipVerify = row.TLSSkipVerify
		v.BindDN = row.BindDN
		v.BindPasswordConfigured = row.BindPasswordEnc != ""
		v.UserSearchBase = row.UserSearchBase
		v.UserSearchFilter = row.UserSearchFilter
		v.TimeoutSeconds = row.TimeoutSeconds
		v.UpdatedBy = row.UpdatedBy
		if !row.UpdatedAt.IsZero() {
			v.UpdatedAt = row.UpdatedAt.UTC().Format(time.RFC3339)
		}
	}
	if v.Port == 0 {
		v.Port = 389
	}
	if v.TimeoutSeconds == 0 {
		v.TimeoutSeconds = 10
	}
	return v
}

// validateLDAPInput 校验更新入参：host 为纯主机名、port 0–65535（0 = 默认 389）、
// tlsMode ∈ {none, starttls, tls}、过滤器含 %s 占位、超时 1–120 秒。
func validateLDAPInput(in LDAPConfigInput) error {
	if in.Enabled == nil && in.Host == nil && in.Port == nil && in.TLSMode == nil &&
		in.TLSSkipVerify == nil && in.BindDN == nil && in.BindPassword == nil &&
		in.UserSearchBase == nil && in.UserSearchFilter == nil && in.TimeoutSeconds == nil {
		return Invalid("至少提供一个配置字段")
	}
	if in.Host != nil {
		h := strings.TrimSpace(*in.Host)
		if h != "" && (strings.ContainsAny(h, " /@:") || strings.Contains(h, "://")) {
			return Invalid("host %q 非法，应为纯主机名（不含协议与端口）", h)
		}
	}
	if in.Port != nil && (*in.Port < 0 || *in.Port > 65535) {
		return Invalid("port %d 非法，应在 0–65535（0 = 默认 389）", *in.Port)
	}
	if in.TLSMode != nil {
		m := strings.ToLower(strings.TrimSpace(*in.TLSMode))
		if m != "" && m != "none" && m != "starttls" && m != "tls" {
			return Invalid("tlsMode %q 非法，应为 none / starttls / tls", m)
		}
	}
	if in.UserSearchFilter != nil {
		f := strings.TrimSpace(*in.UserSearchFilter)
		if f != "" && !strings.Contains(f, "%s") {
			return Invalid("userSearchFilter 需包含 %%s 占位登录名，如 (uid=%%s)")
		}
	}
	if in.TimeoutSeconds != nil && (*in.TimeoutSeconds < 1 || *in.TimeoutSeconds > 120) {
		return Invalid("timeoutSeconds %d 非法，应在 1–120 秒", *in.TimeoutSeconds)
	}
	return nil
}
