package app

// smtpconfig.go：SMTP 发件邮箱配置用例（docs/modules/integration.md「M4 增量：邮箱配置」）。
// 管理台维护发件服务器与账号；密码经 platform.Seal 加密落库（红线 8 唯一例外），
// 视图永不回显密码，只报「是否已配置」。未保存过或字段留空 = 沿用环境变量默认值。

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/yuylong/Chalkline/internal/platform"
	"github.com/yuylong/Chalkline/internal/store"
)

// SMTPEnvDefaults 是环境变量提供的发件默认值（server 组装层注入）。
// Password 只进内存用于运行时回退，绝不序列化或写日志。
type SMTPEnvDefaults struct {
	Host       string
	Port       int
	Username   string
	Password   string
	From       string
	Recipients []string
}

// SMTPConfigView 是管理台看到的邮箱配置（合并环境变量默认值与库内覆盖）。
type SMTPConfigView struct {
	Enabled            bool     `json:"enabled"`
	Host               string   `json:"host"`
	Port               int      `json:"port"`
	Username           string   `json:"username"`
	From               string   `json:"from"`
	PasswordConfigured bool     `json:"passwordConfigured"` // 库内密文或环境变量任一已配置
	EnvHost            string   `json:"envHost"`
	EnvPort            int      `json:"envPort"`
	EnvFrom            string   `json:"envFrom"`
	EnvRecipients      []string `json:"envRecipients"` // 收件人只经 CHALKLINE_NOTIFY_EMAILS（只读提示）
	UpdatedBy          string   `json:"updatedBy,omitempty"`
	UpdatedAt          string   `json:"updatedAt,omitempty"`
}

// SMTPConfigInput 是配置更新入参（nil 即不改；Password 空串 = 清除库内密码）。
type SMTPConfigInput struct {
	Enabled  *bool
	Host     *string
	Port     *int
	Username *string
	From     *string
	Password *string
}

// SMTPRuntime 是投递器运行时解析出的生效配置（server 组装层消费）。
type SMTPRuntime struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// GetSMTPConfig 读配置视图（admin）。
func (s *Service) GetSMTPConfig(ctx context.Context, actor Actor) (SMTPConfigView, error) {
	if !actor.IsAdmin {
		return SMTPConfigView{}, RoleRequired("需要 admin 管理邮箱配置")
	}
	row, err := store.SMTPConfig(ctx, s.DB.Q())
	stored := true
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return SMTPConfigView{}, err
		}
		stored = false
	}
	return s.smtpConfigView(row, stored), nil
}

// UpdateSMTPConfig 保存发件配置（admin，写审计；密码只落密文，审计只记是否设置）。
func (s *Service) UpdateSMTPConfig(ctx context.Context, actor Actor, in SMTPConfigInput) (SMTPConfigView, error) {
	if !actor.IsAdmin {
		return SMTPConfigView{}, RoleRequired("需要 admin 管理邮箱配置")
	}
	if err := validateSMTPInput(in); err != nil {
		return SMTPConfigView{}, err
	}
	var passwordEnc string
	if in.Password != nil && strings.TrimSpace(*in.Password) != "" {
		enc, err := platform.Seal(s.SecretKey, *in.Password)
		if err != nil {
			return SMTPConfigView{}, Invalid("密码加密失败：请设置 CHALKLINE_SECRET_KEY（或 CHALKLINE_JWT_SECRET）后重试")
		}
		passwordEnc = enc
	}
	var row store.SMTPConfigRow
	err := s.DB.WithTx(ctx, func(q store.Querier) error {
		cur, err := store.SMTPConfig(ctx, q)
		switch {
		case err == nil:
			row = cur
		case errors.Is(err, store.ErrNotFound):
			row = store.SMTPConfigRow{Port: 25} // 首次保存：环境变量默认值打底
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
		if in.Username != nil {
			row.Username = strings.TrimSpace(*in.Username)
		}
		if in.From != nil {
			row.FromAddr = strings.TrimSpace(*in.From)
		}
		if in.Password != nil {
			if strings.TrimSpace(*in.Password) == "" {
				row.PasswordEnc = "" // 清除（含纯空白）：回退环境变量密码
			} else {
				row.PasswordEnc = passwordEnc
			}
		}
		row.UpdatedBy = actor.UserID
		if err := store.UpsertSMTPConfig(ctx, q, row); err != nil {
			return err
		}
		return audit(ctx, q, actor, "integration.smtp.update", "smtp", store.SMTPConfigKey, map[string]any{
			"enabled": row.Enabled, "host": row.Host, "port": row.Port,
			"username": row.Username, "from": row.FromAddr,
			"password_set": row.PasswordEnc != "",
		})
	})
	if err != nil {
		return SMTPConfigView{}, err
	}
	row.UpdatedAt = s.Now().UTC()
	return s.smtpConfigView(row, true), nil
}

// EffectiveSMTP 解析当前生效的发件配置（投递器每轮调用，管理台保存后立即生效）。
// 密钥不符导致解密失败时按「密码未配置」降级并告警，不阻断投递循环。
func (s *Service) EffectiveSMTP(ctx context.Context) (SMTPRuntime, error) {
	row, err := store.SMTPConfig(ctx, s.DB.Q())
	stored := true
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return SMTPRuntime{}, err
		}
		stored = false
	}
	env := s.SMTPDefaults
	rt := SMTPRuntime{
		Enabled:  envConfigured(env),
		Host:     env.Host,
		Port:     env.Port,
		Username: env.Username,
		Password: env.Password,
		From:     env.From,
	}
	if rt.Port == 0 {
		rt.Port = 25
	}
	if stored {
		rt.Enabled = row.Enabled
		if row.Host != "" {
			rt.Host = row.Host
		}
		if row.Port != 0 {
			rt.Port = row.Port
		}
		if row.Username != "" {
			rt.Username = row.Username
		}
		if row.FromAddr != "" {
			rt.From = row.FromAddr
		}
		if row.PasswordEnc != "" {
			pw, err := platform.Open(s.SecretKey, row.PasswordEnc)
			if err != nil {
				s.Log.Warn("SMTP 密码解密失败，按未配置降级（检查 CHALKLINE_SECRET_KEY 是否变更）")
			} else {
				rt.Password = pw
			}
		}
	}
	return rt, nil
}

// smtpConfigView 合并环境变量默认值与库内覆盖（密码只报布尔，永不回显）。
func (s *Service) smtpConfigView(row store.SMTPConfigRow, stored bool) SMTPConfigView {
	env := s.SMTPDefaults
	v := SMTPConfigView{
		Enabled:       envConfigured(env),
		Host:          env.Host,
		Port:          env.Port,
		Username:      env.Username,
		From:          env.From,
		EnvHost:       env.Host,
		EnvPort:       env.Port,
		EnvFrom:       env.From,
		EnvRecipients: env.Recipients,
	}
	if v.Port == 0 {
		v.Port = 25
	}
	if v.EnvRecipients == nil {
		v.EnvRecipients = []string{}
	}
	v.PasswordConfigured = env.Password != ""
	if stored {
		v.Enabled = row.Enabled
		if row.Host != "" {
			v.Host = row.Host
		}
		if row.Port != 0 {
			v.Port = row.Port
		}
		if row.Username != "" {
			v.Username = row.Username
		}
		if row.FromAddr != "" {
			v.From = row.FromAddr
		}
		v.PasswordConfigured = row.PasswordEnc != "" || env.Password != ""
		v.UpdatedBy = row.UpdatedBy
		if !row.UpdatedAt.IsZero() {
			v.UpdatedAt = row.UpdatedAt.UTC().Format(time.RFC3339)
		}
	}
	return v
}

// envConfigured 环境变量三要素齐全（host/from/收件人）即视为默认启用。
func envConfigured(env SMTPEnvDefaults) bool {
	return env.Host != "" && env.From != "" && len(env.Recipients) > 0
}

// validateSMTPInput 校验更新入参：host 为纯主机名、port 0–65535、from 合法邮箱。
func validateSMTPInput(in SMTPConfigInput) error {
	if in.Enabled == nil && in.Host == nil && in.Port == nil &&
		in.Username == nil && in.From == nil && in.Password == nil {
		return Invalid("至少提供一个配置字段")
	}
	if in.Host != nil {
		h := strings.TrimSpace(*in.Host)
		if h != "" && (strings.ContainsAny(h, " /@:") || strings.Contains(h, "://")) {
			return Invalid("host %q 非法，应为纯主机名（不含协议与端口）", h)
		}
	}
	if in.Port != nil && (*in.Port < 0 || *in.Port > 65535) {
		return Invalid("port %d 非法，应在 0–65535（0 = 默认 25）", *in.Port)
	}
	if in.From != nil {
		f := strings.TrimSpace(*in.From)
		if f != "" {
			if _, err := mail.ParseAddress(f); err != nil {
				return Invalid("from %q 不是合法邮箱地址", f)
			}
		}
	}
	return nil
}
