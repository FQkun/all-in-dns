package provider

import (
	"encoding/json"
	"fmt"

	"github.com/loeia/all-in-dns/internal/crypto"
)

// Account 云账号（密文形式），由上层从 cloud_accounts 表读取后传入。
type Account struct {
	Provider        string `db:"provider"`
	AccessKeyID     []byte `db:"access_key_id"`     // VARBINARY，base64 密文
	AccessKeySecret []byte `db:"access_key_secret"` // VARBINARY，base64 密文
	Extra           *string `db:"extra"`            // TEXT，JSON（session_token 可能已加密）
}

// New 解密 AK/SK 并构造 Provider（对齐 PHP ProviderFactory::make）。
func New(account Account, secretKey string) (Provider, error) {
	ak, err := crypto.Decrypt(secretKey, string(account.AccessKeyID))
	if err != nil {
		return nil, fmt.Errorf("解密 AccessKeyId 失败: %w", err)
	}
	sk, err := crypto.Decrypt(secretKey, string(account.AccessKeySecret))
	if err != nil {
		return nil, fmt.Errorf("解密 AccessKeySecret 失败: %w", err)
	}

	extra := map[string]string{}
	if account.Extra != nil && *account.Extra != "" {
		_ = json.Unmarshal([]byte(*account.Extra), &extra)
	}
	if tok, ok := extra["session_token"]; ok && tok != "" {
		if dec, err := crypto.Decrypt(secretKey, tok); err == nil {
			extra["session_token"] = dec
		}
	}

	switch account.Provider {
	case "tencent":
		return newTencent(ak, sk, extra), nil
	case "aliyun":
		return newAliyun(ak, sk, extra), nil
	case "volc":
		return newVolc(ak, sk, extra), nil
	default:
		return nil, providerErrorf("未知云厂商: %s", account.Provider)
	}
}
