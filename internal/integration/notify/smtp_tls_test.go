package notify

// smtp_tls_test.go：TLS/STARTTLS 投递路径测试。桩服务器用自签证书：
// - STARTTLS：明文连接后 EHLO 广播 STARTTLS，客户端协商升级再继续会话；
// - TLS（隐式）：连接即 TLS；
// - skipVerify=false 时自签证书应被拒绝（校验开关真实生效）。

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// selfSignedCert 生成 127.0.0.1 自签证书（仅测试用）。
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("签发证书失败: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// serveSMTPTLS 处理一条连接：advertiseStartTLS 时 EHLO 广播 STARTTLS 并在
// 收到该命令后升级为 TLS；implicitTLS 时连接即 TLS。会话逻辑同 serveSMTP。
func serveSMTPTLS(l net.Listener, cert tls.Certificate, mail *safeBuffer,
	advertiseStartTLS, implicitTLS bool) {
	conn, err := l.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	if implicitTLS {
		conn = tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	r := bufio.NewReader(conn)
	write := func(s string) { _, _ = fmt.Fprint(conn, s+"\r\n") }

	write("220 stub SMTP ready")
	inData := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		if inData {
			if line == ".\r\n" || line == ".\n" {
				inData = false
				write("250 OK: queued")
				continue
			}
			_, _ = mail.Write([]byte(line))
			continue
		}
		switch {
		case starts(line, "EHLO"), starts(line, "HELO"):
			write("250-stub")
			if advertiseStartTLS {
				write("250-STARTTLS")
			}
			write("250 OK")
		case starts(line, "STARTTLS"):
			write("220 Ready to start TLS")
			conn = tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
			r = bufio.NewReader(conn) // 升级后重置读取器
		case starts(line, "AUTH"):
			write("235 2.7.0 accepted")
		case starts(line, "MAIL FROM"):
			write("250 OK")
		case starts(line, "RCPT TO"):
			write("250 OK")
		case starts(line, "DATA"):
			inData = true
			write("354 End data with <CR><LF>.<CR><LF>")
		case starts(line, "QUIT"):
			write("221 Bye")
			return
		default:
			write("250 OK")
		}
	}
}

// TestSMTPStartTLS：STARTTLS 模式 + 跳过证书校验，自签桩上完整投递成功。
func TestSMTPStartTLS(t *testing.T) {
	cert := selfSignedCert(t)
	var mail safeBuffer
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("起桩失败: %v", err)
	}
	defer func() { _ = l.Close() }()
	go serveSMTPTLS(l, cert, &mail, true, false) //nolint:errcheck // 测试桩

	m := &SMTP{To: []string{"pm@example.com"}, Timeout: 2 * time.Second,
		Resolve: func(context.Context) (SMTPRuntime, bool) {
			return SMTPRuntime{Enabled: true, Host: "127.0.0.1",
				Port: l.Addr().(*net.TCPAddr).Port, From: "chalk@example.com",
				TLSMode: "starttls", TLSSkipVerify: true}, true
		}}
	if err := m.Send(context.Background(), ev(31, 0)); err != nil {
		t.Fatalf("STARTTLS 投递失败: %v", err)
	}
	if !strings.Contains(mail.String(), "From: chalk@example.com") {
		t.Fatalf("邮件应完整落盘: %q", mail.String())
	}
}

// TestSMTPStartTLSVerifyRejected：skipVerify=false 时自签证书应被拒绝。
func TestSMTPStartTLSVerifyRejected(t *testing.T) {
	cert := selfSignedCert(t)
	var mail safeBuffer
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("起桩失败: %v", err)
	}
	defer func() { _ = l.Close() }()
	go serveSMTPTLS(l, cert, &mail, true, false) //nolint:errcheck // 测试桩

	m := &SMTP{To: []string{"pm@example.com"}, Timeout: 2 * time.Second,
		Resolve: func(context.Context) (SMTPRuntime, bool) {
			return SMTPRuntime{Enabled: true, Host: "127.0.0.1",
				Port: l.Addr().(*net.TCPAddr).Port, From: "chalk@example.com",
				TLSMode: "starttls"}, true // 不跳过校验
		}}
	if err := m.Send(context.Background(), ev(32, 0)); err == nil {
		t.Fatal("自签证书 + 不跳过校验应失败")
	}
}

// TestSMTPImplicitTLS：隐式 TLS（tls 模式）+ 跳过证书校验，完整投递成功。
func TestSMTPImplicitTLS(t *testing.T) {
	cert := selfSignedCert(t)
	var mail safeBuffer
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("起桩失败: %v", err)
	}
	defer func() { _ = l.Close() }()
	go serveSMTPTLS(l, cert, &mail, false, true) //nolint:errcheck // 测试桩

	m := &SMTP{To: []string{"pm@example.com"}, Timeout: 2 * time.Second,
		Resolve: func(context.Context) (SMTPRuntime, bool) {
			return SMTPRuntime{Enabled: true, Host: "127.0.0.1",
				Port: l.Addr().(*net.TCPAddr).Port, From: "chalk@example.com",
				TLSMode: "tls", TLSSkipVerify: true}, true
		}}
	if err := m.Send(context.Background(), ev(33, 0)); err != nil {
		t.Fatalf("隐式 TLS 投递失败: %v", err)
	}
	if !strings.Contains(mail.String(), "From: chalk@example.com") {
		t.Fatalf("邮件应完整落盘: %q", mail.String())
	}
}
