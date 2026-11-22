package notify

// smtpstub_test.go：极简内存 SMTP 服务器，用于验证 SMTP 通道的完整投递路径
//（HELO/EHLO → 可选 AUTH → MAIL FROM → RCPT TO → DATA → QUIT）。
// 不校验协议细节，只保证 net/smtp 客户端能走完会话并落信体。

import (
	"bufio"
	"fmt"
	"net"
	"sync"
	"time"
)

// safeBuffer 是并发安全的字节缓冲（投递完成后再断言内容）。
type safeBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// newInmemorySMTP 监听一个回环端口。
func newInmemorySMTP(mail *safeBuffer) (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}

// serveSMTP 处理一个连接的最小 SMTP 会话。错误静默（测试桩）。
func serveSMTP(l net.Listener, mail *safeBuffer) {
	conn, err := l.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
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
			// DATA 模式：逐行收集直到独立点号结束。
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
			write("250 OK")
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

func starts(line, prefix string) bool {
	return len(line) >= len(prefix) && line[:len(prefix)] == prefix
}
