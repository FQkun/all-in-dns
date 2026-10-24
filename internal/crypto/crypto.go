// Package crypto 提供 AES-256-GCM 加解密，密文格式与 PHP 版逐字节兼容。
//
// PHP 版格式（src/Core/Crypto.php）：
//
//	key     = sha256(secret_key) 二进制（32 字节）
//	密文     = base64( iv(12) + tag(16) + ciphertext )
//
// 因此历史密文（cloud_accounts.access_key_id 等）无需重加密即可直接解密。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// key 派生：sha256(secret_key) 二进制。
func key(secretKey string) []byte {
	sum := sha256.Sum256([]byte(secretKey))
	return sum[:]
}

// Encrypt 加密并返回 base64 字符串。
func Encrypt(secretKey, plain string) (string, error) {
	block, err := aes.NewCipher(key(secretKey))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize()) // 12
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	// Seal 返回 ciphertext||tag；PHP 格式为 iv||tag||ciphertext，需调整顺序。
	sealed := gcm.Seal(nil, nonce, []byte(plain), nil)
	ct := sealed[:len(sealed)-gcm.Overhead()]
	tag := sealed[len(sealed)-gcm.Overhead():]

	raw := make([]byte, 0, len(nonce)+len(tag)+len(ct))
	raw = append(raw, nonce...)
	raw = append(raw, tag...)
	raw = append(raw, ct...)
	return base64.StdEncoding.EncodeToString(raw), nil
}

// Decrypt 解密 base64 密文，失败返回错误。
func Decrypt(secretKey, enc string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key(secretKey))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns, oh := gcm.NonceSize(), gcm.Overhead()
	if len(raw) < ns+oh {
		return "", errors.New("密文长度不合法")
	}
	nonce := raw[:ns]
	tag := raw[ns : ns+oh]
	ct := raw[ns+oh:]

	// Open 需要 ciphertext||tag。
	sealed := make([]byte, 0, len(ct)+len(tag))
	sealed = append(sealed, ct...)
	sealed = append(sealed, tag...)

	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", errors.New("解密失败，请检查 secret_key")
	}
	return string(plain), nil
}
