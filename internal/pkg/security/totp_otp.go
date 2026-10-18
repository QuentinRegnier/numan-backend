package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	totpPeriod = 30
	totpSkew   = 1 // Tolérance ±1 pas de 30s contre la dérive d'horloge NTP
)

// GenerateTOTPSecret génère une clé secrète de 160 bits (20 octets) encodée en Base32 sans padding.
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// BuildTOTPURI construit l'URI otpauth:// conforme pour l'affichage du QR Code côté client.
func BuildTOTPURI(issuer, username, base32Secret string) string {
	v := url.Values{}
	v.Set("secret", base32Secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", "6")
	v.Set("period", "30")
	return fmt.Sprintf("otpauth://totp/%s:%s?%s", url.PathEscape(issuer), url.PathEscape(username), v.Encode())
}

// VerifyTOTP valide un code à 6 chiffres en temps constant selon la RFC 6238.
func VerifyTOTP(base32Secret, userCode string, now time.Time) bool {
	cleanSecret := strings.ToUpper(strings.TrimSpace(base32Secret))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(cleanSecret)
	if err != nil || len(key) == 0 {
		return false
	}

	baseCounter := uint64(now.UTC().Unix() / totpPeriod)
	for offset := -totpSkew; offset <= totpSkew; offset++ {
		counter := uint64(int64(baseCounter) + int64(offset))
		expected := computeHOTP(key, counter)
		if hmac.Equal([]byte(expected), []byte(strings.TrimSpace(userCode))) {
			return true
		}
	}
	return false
}

func computeHOTP(key []byte, counter uint64) string {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	binCode := (int(sum[offset])&0x7f)<<24 |
		(int(sum[offset+1])&0xff)<<16 |
		(int(sum[offset+2])&0xff)<<8 |
		(int(sum[offset+3]) & 0xff)

	return fmt.Sprintf("%06d", binCode%1000000)
}

// GenerateEmailOTP génère un code aléatoire à 6 chiffres et son empreinte HMAC-SHA256.
func GenerateEmailOTP() (rawCode string, codeHash string, err error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", "", err
	}
	rawCode = fmt.Sprintf("%06d", n.Int64())
	return rawCode, HashEmailOTP(rawCode), nil
}

// HashEmailOTP hache le code OTP avec le secret serveur pour ne jamais le stocker en clair en RAM.
func HashEmailOTP(rawCode string) string {
	secret := os.Getenv("JWT_SECRET")
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte("NUMAN_2FA_OTP:" + strings.TrimSpace(rawCode)))
	return hex.EncodeToString(h.Sum(nil))
}
