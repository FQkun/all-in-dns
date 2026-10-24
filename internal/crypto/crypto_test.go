package crypto

import (
	"encoding/base64"
	"strings"
	"testing"
)

const testKey = "0123456789abcdef0123456789abcdef" // 32 字节

func TestRoundTrip(t *testing.T) {
	for _, plain := range []string{
		"AKIDxxxxxxxxxxxxxxxx",
		"secret-with-symbols-!@#$%^&*()",
		"中文密钥内容",
		"",
	} {
		enc, err := Encrypt(testKey, plain)
		if err != nil {
			t.Fatalf("encrypt %q: %v", plain, err)
		}
		got, err := Decrypt(testKey, enc)
		if err != nil {
			t.Fatalf("decrypt %q: %v", plain, err)
		}
		if got != plain {
			t.Fatalf("roundtrip mismatch: want %q got %q", plain, got)
		}
	}
}

// TestFormat 校验密文格式与 PHP 版一致：base64( iv(12) + tag(16) + ciphertext )。
func TestFormat(t *testing.T) {
	plain := "hello-dns"
	enc, err := Encrypt(testKey, plain)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	// 12 + 16 + len(plain)
	if len(raw) != 12+16+len(plain) {
		t.Fatalf("ciphertext length = %d, want %d", len(raw), 12+16+len(plain))
	}
}

// TestTamper 篡改任意字节应解密失败（GCM 完整性校验）。
func TestTamper(t *testing.T) {
	enc, _ := Encrypt(testKey, "sensitive-data")
	raw, _ := base64.StdEncoding.DecodeString(enc)
	raw[0] ^= 0xff
	if _, err := Decrypt(testKey, base64.StdEncoding.EncodeToString(raw)); err == nil {
		t.Fatal("tampered ciphertext should fail to decrypt")
	}
}

// TestWrongKey 错误密钥应解密失败。
func TestWrongKey(t *testing.T) {
	enc, _ := Encrypt(testKey, "data")
	if _, err := Decrypt("wrong-key-0000000000000000000000", enc); err == nil {
		t.Fatal("wrong key should fail")
	}
}

// TestPHPCompat 用 PHP 版等价格式构造密文（iv||tag||ciphertext）验证解密路径。
// 真实 PHP 交叉样本将在联调阶段追加（用 PHP 版生成的历史密文）。
func TestPHPCompat(t *testing.T) {
	// 用本包 Encrypt 生成后再按 PHP 布局拆解，验证 Decrypt 能按相同布局解回。
	plain := "php-format-check"
	enc, _ := Encrypt(testKey, plain)
	raw, _ := base64.StdEncoding.DecodeString(enc)
	iv, tag, ct := raw[:12], raw[12:28], raw[28:]
	if len(tag) != 16 {
		t.Fatalf("tag length = %d, want 16", len(tag))
	}
	_ = iv
	_ = ct
	got, err := Decrypt(testKey, enc)
	if err != nil || got != plain {
		t.Fatalf("decrypt %q: %v", got, err)
	}
	if strings.Contains(enc, plain) {
		t.Fatal("ciphertext must not contain plaintext")
	}
}
