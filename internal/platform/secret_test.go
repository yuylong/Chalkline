package platform

// secret_test.go：AES-GCM 加解密往返、密钥不符与未配置场景。

import (
	"errors"
	"strings"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	const secret = "k1"
	enc, err := Seal(secret, "smtp-password-123")
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if !strings.HasPrefix(enc, "enc:v1:") {
		t.Fatalf("密文应带版本前缀: %q", enc)
	}
	if strings.Contains(enc, "smtp-password-123") {
		t.Fatal("密文不得包含明文")
	}
	plain, err := Open(secret, enc)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if plain != "smtp-password-123" {
		t.Fatalf("往返应还原明文，实际 %q", plain)
	}
}

func TestSealUniqueNonce(t *testing.T) {
	a, _ := Seal("k", "same")
	b, _ := Seal("k", "same")
	if a == b {
		t.Fatal("两次加密的密文应不同（随机 nonce）")
	}
}

func TestOpenWrongKeyFails(t *testing.T) {
	enc, err := Seal("k1", "secret")
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if _, err := Open("k2", enc); err == nil {
		t.Fatal("密钥不符应解密失败")
	}
}

func TestSealOpenMissingKey(t *testing.T) {
	if _, err := Seal("", "x"); !errors.Is(err, ErrSecretKeyMissing) {
		t.Fatalf("空密钥应报 ErrSecretKeyMissing，实际 %v", err)
	}
	enc, _ := Seal("k", "x")
	if _, err := Open("", enc); !errors.Is(err, ErrSecretKeyMissing) {
		t.Fatalf("空密钥解密应报 ErrSecretKeyMissing，实际 %v", err)
	}
}

func TestOpenBadCiphertext(t *testing.T) {
	if _, err := Open("k", "not-encrypted"); err == nil {
		t.Fatal("无版本前缀应报格式错误")
	}
	if _, err := Open("k", "enc:v1:%%%"); err == nil {
		t.Fatal("非法 base64 应报错")
	}
	if _, err := Open("k", "enc:v1:QUJD"); err == nil {
		t.Fatal("过短密文应报错")
	}
}
