package directory

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// App Secret 等凭据用服务端密钥 AES-256-GCM 加密后入库（ADR 0014：凭据不落明文）。
// 密文格式：nonce(12 字节) || ciphertext。

// SecretKeyEnv 是服务端密钥的环境变量名：32 字节，base64 编码。
const SecretKeyEnv = "AXIOMOS_SECRET_KEY"

// DevKey 是没有配置密钥时的开发用密钥：确定性派生，方便本地重启后仍能解密；生产必须配置真密钥。
func DevKey() []byte {
	sum := sha256.Sum256([]byte("axiomos-dev-secret-key:not-for-production"))
	return sum[:]
}

// ParseKey 解析 base64 的 32 字节密钥。
func ParseKey(b64 string) ([]byte, error) {
	if b64 == "" {
		return nil, errors.New("empty key")
	}
	k, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		if k2, err2 := base64.RawStdEncoding.DecodeString(b64); err2 == nil {
			k = k2
		} else {
			return nil, fmt.Errorf("key is not base64: %w", err)
		}
	}
	if len(k) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes, got %d", len(k))
	}
	return k, nil
}

// Encrypt 加密明文。
func Encrypt(key []byte, plaintext string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return append(nonce, gcm.Seal(nil, nonce, []byte(plaintext), nil)...), nil
}

// Decrypt 解密。
func Decrypt(key []byte, data []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	pt, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// EncryptSecrets 把全部保密凭据字段编成一个 JSON 对象再加密（一行密文，字段随提供方声明变化不用改表）。
func EncryptSecrets(key []byte, secrets map[string]string) ([]byte, error) {
	if len(secrets) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(secrets)
	if err != nil {
		return nil, err
	}
	return Encrypt(key, string(b))
}

// DecryptSecrets 解开 EncryptSecrets 的密文；空密文得到空表。
func DecryptSecrets(key []byte, data []byte) (map[string]string, error) {
	out := map[string]string{}
	if len(data) == 0 {
		return out, nil
	}
	pt, err := Decrypt(key, data)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(pt), &out); err != nil {
		return nil, err
	}
	return out, nil
}
