package directory

import (
	"encoding/base64"
	"testing"
)

func TestSecretRoundTrip(t *testing.T) {
	key := DevKey()
	enc, err := Encrypt(key, "s3cret-app-secret")
	if err != nil {
		t.Fatal(err)
	}
	if string(enc) == "s3cret-app-secret" || len(enc) <= 12 {
		t.Fatal("密文不应等于明文")
	}
	got, err := Decrypt(key, enc)
	if err != nil || got != "s3cret-app-secret" {
		t.Fatalf("解密不符: %q %v", got, err)
	}
	// 换密钥解不开
	other := make([]byte, 32)
	copy(other, key)
	other[0] ^= 0xff
	if _, err := Decrypt(other, enc); err == nil {
		t.Fatal("错误密钥不应能解密")
	}
	// 同一明文两次密文不同（随机 nonce）
	enc2, _ := Encrypt(key, "s3cret-app-secret")
	if string(enc2) == string(enc) {
		t.Fatal("两次加密的密文不应相同")
	}
}

func TestParseKey(t *testing.T) {
	if _, err := ParseKey(""); err == nil {
		t.Fatal("空密钥应报错")
	}
	if _, err := ParseKey(base64.StdEncoding.EncodeToString(make([]byte, 16))); err == nil {
		t.Fatal("16 字节密钥应报错")
	}
	if k, err := ParseKey(base64.StdEncoding.EncodeToString(make([]byte, 32))); err != nil || len(k) != 32 {
		t.Fatalf("32 字节密钥应通过: %v", err)
	}
}
