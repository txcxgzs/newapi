package common

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const merchantSecretPrefix = "enc:v1:"

func merchantSecretKey() []byte {
	secret := CryptoSecret
	if secret == "" {
		secret = SessionSecret
	}
	sum := sha256.Sum256([]byte("merchant-secret:" + secret))
	return sum[:]
}

// EncryptMerchantSecret uses AES-256-GCM to protect pending upstream API keys at rest.
func EncryptMerchantSecret(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if strings.HasPrefix(plaintext, merchantSecretPrefix) {
		return plaintext, nil
	}
	block, err := aes.NewCipher(merchantSecretKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return merchantSecretPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptMerchantSecret reverses EncryptMerchantSecret while transparently accepting legacy plaintext values.
func DecryptMerchantSecret(stored string) (string, error) {
	if stored == "" || !strings.HasPrefix(stored, merchantSecretPrefix) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, merchantSecretPrefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(merchantSecretKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted merchant secret payload")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func GenerateHMACWithKey(key []byte, data string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func GenerateHMAC(data string) string {
	h := hmac.New(sha256.New, []byte(CryptoSecret))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func Password2Hash(password string) (string, error) {
	passwordBytes := []byte(password)
	hashedPassword, err := bcrypt.GenerateFromPassword(passwordBytes, bcrypt.DefaultCost)
	return string(hashedPassword), err
}

func ValidatePasswordAndHash(password string, hash string) bool {
	if strings.HasPrefix(hash, "$argon2id$") {
		return validateArgon2AccountPassword(password, hash)
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
