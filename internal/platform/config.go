// Package platform 提供横切基础设施：配置、日志、数据库迁移。
// 它可以被所有层使用，但不得反向 import 业务包（见 docs/structure.md 分层边界）。
package platform

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 是 chalklined 的全部启动配置。
// 只从环境变量读取（docker compose 注入），无配置文件（docs/tech.md：环境变量 + .env）。
type Config struct {
	// HTTP 监听地址，默认 ":8080"。
	Addr string
	// 数据库连接串，必填；例如 postgres://chalk:pass@localhost:5432/chalkline?sslmode=disable。
	DatabaseURL string
	// 日志级别：debug | info | warn | error，默认 info。
	LogLevel string
	// 数据库初始化失败后的重试间隔，默认 5s。
	DBRetryInterval time.Duration
	// HTTP 服务器读/写超时，默认 30s / 60s。
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	// JWT 签名密钥；未设置时进程启动生成临时密钥并告警（重启后会话失效，生产必须设置）。
	JWTSecret string
	// 首启 admin 引导凭据（可选）：users 表为空且二者非空时自动创建 admin。
	AdminUsername string
	AdminPassword string

	// ---- 集成（integration 模块，M2）：全部可选，凭据只经环境变量，不落库 ----

	// OutboundAllowlist 允许出站的 host 后缀（逗号分隔）；空 = 拒绝一切出站，"*" = 放行全部。
	OutboundAllowlist []string
	// Outline 知识库：baseURL 与 API token（材料快照用，尽力而为）。
	OutlineBaseURL string
	OutlineToken   string
	// Redmine：baseURL 与 API key（issue 标题快照，单向出站不回写）。
	RedmineBaseURL string
	RedmineToken   string
	// Webhook 订阅：URL 与 HMAC-SHA256 签名密钥。
	WebhookURL    string
	WebhookSecret string
	// SMTP 邮件通知：只发内网 SMTP；NotifyEmails 是收件人列表。
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	NotifyEmails []string
	// OutboxInterval 是通知投递器的轮询间隔，默认 5s。
	OutboxInterval time.Duration

	// ---- M3 规模化 ----

	// ReviewSLA 评审超期阈值：awaiting_review 超过该时长未裁决即提醒评审人，默认 24h。
	ReviewSLA time.Duration
	// ReviewEscalation 升级阈值：超过该时长升级给产品 owner / admin，默认 0 = 3×SLA。
	ReviewEscalation time.Duration
	// ReviewScanInterval SLA 监控器扫描间隔，默认 10m。
	ReviewScanInterval time.Duration
	// EventsInterval SSE 事件尾随轮询间隔，默认 3s。
	EventsInterval time.Duration
}

// Load 从环境变量构造配置并校验必填项。
func Load() (Config, error) {
	cfg := Config{
		Addr:            envString("CHALKLINE_ADDR", ":8080"),
		DatabaseURL:     os.Getenv("CHALKLINE_DATABASE_URL"),
		LogLevel:        strings.ToLower(envString("CHALKLINE_LOG_LEVEL", "info")),
		DBRetryInterval: envDuration("CHALKLINE_DB_RETRY_INTERVAL", 5*time.Second),
		ReadTimeout:     envDuration("CHALKLINE_READ_TIMEOUT", 30*time.Second),
		WriteTimeout:    envDuration("CHALKLINE_WRITE_TIMEOUT", 60*time.Second),
		JWTSecret:       os.Getenv("CHALKLINE_JWT_SECRET"),
		AdminUsername:   os.Getenv("CHALKLINE_ADMIN_USERNAME"),
		AdminPassword:   os.Getenv("CHALKLINE_ADMIN_PASSWORD"),

		OutboundAllowlist: envList("CHALKLINE_OUTBOUND_ALLOWLIST"),
		OutlineBaseURL:    os.Getenv("CHALKLINE_OUTLINE_BASE_URL"),
		OutlineToken:      os.Getenv("CHALKLINE_OUTLINE_TOKEN"),
		RedmineBaseURL:    os.Getenv("CHALKLINE_REDMINE_BASE_URL"),
		RedmineToken:      os.Getenv("CHALKLINE_REDMINE_TOKEN"),
		WebhookURL:        os.Getenv("CHALKLINE_WEBHOOK_URL"),
		WebhookSecret:     os.Getenv("CHALKLINE_WEBHOOK_SECRET"),
		SMTPHost:          os.Getenv("CHALKLINE_SMTP_HOST"),
		SMTPPort:          envInt("CHALKLINE_SMTP_PORT", 25),
		SMTPUsername:      os.Getenv("CHALKLINE_SMTP_USERNAME"),
		SMTPPassword:      os.Getenv("CHALKLINE_SMTP_PASSWORD"),
		SMTPFrom:          os.Getenv("CHALKLINE_SMTP_FROM"),
		NotifyEmails:      envList("CHALKLINE_NOTIFY_EMAILS"),
		OutboxInterval:    envDuration("CHALKLINE_OUTBOX_INTERVAL", 5*time.Second),

		ReviewSLA:         envDuration("CHALKLINE_REVIEW_SLA", 24*time.Hour),
		ReviewEscalation:  envDuration("CHALKLINE_REVIEW_ESCALATION", 0),
		ReviewScanInterval: envDuration("CHALKLINE_REVIEW_SCAN_INTERVAL", 10*time.Minute),
		EventsInterval:    envDuration("CHALKLINE_EVENTS_INTERVAL", 3*time.Second),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("CHALKLINE_DATABASE_URL 未设置")
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, fmt.Errorf("CHALKLINE_LOG_LEVEL 非法: %q", cfg.LogLevel)
	}
	return cfg, nil
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// envList 读逗号分隔列表环境变量（空白项忽略）。
func envList(key string) []string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return nil
	}
	out := []string{}
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
