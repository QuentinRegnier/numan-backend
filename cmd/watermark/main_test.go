package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/gen2brain/avif"
	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectBitInBlock_PreservesRGBChrominance(t *testing.T) {
	// Création d'une image 64x64 fortement colorée (rouge orangé vif : R=230, G=50, B=25)
	width, height := 64, 64
	src := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			src.Set(x, y, color.RGBA{R: 230, G: 50, B: 25, A: 255})
		}
	}

	watermarked, err := applyDCTWatermark(src, "A:101|P:202|R:303|T:1700000000")
	require.NoError(t, err)

	rgbaOut, ok := watermarked.(*image.RGBA)
	require.True(t, ok)

	// Vérifie sur les blocs tatoués (0..32, 0..32) que les pixels ne sont JAMAIS convertis en niveaux de gris (R=G=B)
	// et que la dominante rouge d'origine (R >> G > B) est préservée.
	nonGrayscaleCount := 0
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			c := rgbaOut.RGBAAt(x, y)
			assert.Equal(t, uint8(255), c.A)
			if c.R != c.G || c.G != c.B {
				nonGrayscaleCount++
			}
			assert.Greater(t, int(c.R), int(c.G)+120, "Red dominance must be preserved at (%d,%d): got R=%d G=%d B=%d", x, y, c.R, c.G, c.B)
			assert.Greater(t, int(c.G), int(c.B), "Green > Blue ratio must be preserved at (%d,%d): got R=%d G=%d B=%d", x, y, c.R, c.G, c.B)
		}
	}
	assert.Equal(t, 32*32, nonGrayscaleCount, "No pixel in watermarked colored blocks should collapse to grayscale")
}

func TestVerifyWatermarkHMAC_ConstantTime(t *testing.T) {
	secret := "super-secret-watermark-key"
	payload := "key=users/1/img.avif&author=1&post=2&reader=3&ts=1700000000"

	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	validSig := hex.EncodeToString(h.Sum(nil))

	assert.True(t, verifyWatermarkHMAC(payload, validSig, secret))
	assert.True(t, verifyWatermarkHMAC(payload, "  "+validSig+"  ", secret))
	assert.False(t, verifyWatermarkHMAC(payload, validSig+"00", secret))
	assert.False(t, verifyWatermarkHMAC(payload, "invalid-sig", secret))
	assert.False(t, verifyWatermarkHMAC(payload, "", secret))
	assert.False(t, verifyWatermarkHMAC(payload, validSig, ""))
}

func TestValidateWatermarkTimestamp_TTLAndClockSkew(t *testing.T) {
	now := time.Unix(1750000000, 0)

	// Default TTL = 3600s
	t.Setenv("WATERMARK_URL_TTL_SECONDS", "")
	assert.True(t, validateWatermarkTimestamp(strconv.FormatInt(now.Unix(), 10), now))
	assert.True(t, validateWatermarkTimestamp(strconv.FormatInt(now.Unix()-3599, 10), now))
	assert.False(t, validateWatermarkTimestamp(strconv.FormatInt(now.Unix()-3601, 10), now), "expired URL (>3600s) must be rejected")

	// Future clock skew check (<= 300s allowed, > 300s rejected)
	assert.True(t, validateWatermarkTimestamp(strconv.FormatInt(now.Unix()+120, 10), now))
	assert.False(t, validateWatermarkTimestamp(strconv.FormatInt(now.Unix()+305, 10), now), "far-future timestamp must be rejected")

	// Custom TTL via WATERMARK_URL_TTL_SECONDS
	t.Setenv("WATERMARK_URL_TTL_SECONDS", "600")
	assert.True(t, validateWatermarkTimestamp(strconv.FormatInt(now.Unix()-590, 10), now))
	assert.False(t, validateWatermarkTimestamp(strconv.FormatInt(now.Unix()-610, 10), now))

	// Invalid timestamps
	assert.False(t, validateWatermarkTimestamp("", now))
	assert.False(t, validateWatermarkTimestamp("not-a-number", now))
	assert.False(t, validateWatermarkTimestamp("-50", now))
}

func TestProcessHandler_EndToEnd(t *testing.T) {
	_, mc := test.SetupTestMinio(t)
	minioClient = mc
	bucketName = "numan-bucket"
	secretKey = "test-watermark-secret"
	t.Setenv("WATERMARK_URL_TTL_SECONDS", "3600")

	// Prépare une image AVIF colorée dans FakeMinioServer
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 180, B: 90, A: 255})
		}
	}
	var avifBuf bytes.Buffer
	require.NoError(t, avif.Encode(&avifBuf, img, avif.Options{Quality: 80, Speed: 8}))
	objectKey := "users/42/photo.avif"
	_, errPut := minioClient.PutObject(
		context.Background(),
		bucketName,
		objectKey,
		bytes.NewReader(avifBuf.Bytes()),
		int64(avifBuf.Len()),
		minio.PutObjectOptions{ContentType: "image/avif"},
	)
	require.NoError(t, errPut)

	signURL := func(key, author, post, reader, ts string) string {
		payload := fmt.Sprintf("key=%s&author=%s&post=%s&reader=%s&ts=%s", key, author, post, reader, ts)
		h := hmac.New(sha256.New, []byte(secretKey))
		h.Write([]byte(payload))
		sig := hex.EncodeToString(h.Sum(nil))
		return fmt.Sprintf("/process?%s&sig=%s", payload, sig)
	}

	// 1. Valid request -> 200 OK + colored AVIF output
	validTS := strconv.FormatInt(time.Now().Unix(), 10)
	reqValid := httptest.NewRequest(http.MethodGet, signURL(objectKey, "42", "100", "99", validTS), nil)
	wValid := httptest.NewRecorder()
	processHandler(wValid, reqValid)
	require.Equal(t, http.StatusOK, wValid.Code)
	assert.Equal(t, "image/avif", wValid.Header().Get("Content-Type"))

	decodedImg, err := avif.Decode(bytes.NewReader(wValid.Body.Bytes()))
	require.NoError(t, err)
	r, g, b, _ := decodedImg.At(4, 4).RGBA()
	assert.Greater(t, int(g>>8), int(r>>8)+60, "Decoded watermarked AVIF must retain green color dominance")
	assert.Greater(t, int(g>>8), int(b>>8)+30, "Decoded watermarked AVIF must retain green > blue dominance")

	// 2. Expired timestamp with valid HMAC -> 403 Forbidden
	expiredTS := strconv.FormatInt(time.Now().Unix()-7200, 10)
	reqExpired := httptest.NewRequest(http.MethodGet, signURL(objectKey, "42", "100", "99", expiredTS), nil)
	wExpired := httptest.NewRecorder()
	processHandler(wExpired, reqExpired)
	assert.Equal(t, http.StatusForbidden, wExpired.Code)

	// 3. Invalid HMAC signature -> 403 Forbidden
	reqBadSig := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/process?key=%s&author=42&post=100&reader=99&ts=%s&sig=deadbeef", objectKey, validTS), nil)
	wBadSig := httptest.NewRecorder()
	processHandler(wBadSig, reqBadSig)
	assert.Equal(t, http.StatusForbidden, wBadSig.Code)

	// 4. Missing parameters -> 400 Bad Request
	reqMissing := httptest.NewRequest(http.MethodGet, "/process?key=users/42/photo.avif", nil)
	wMissing := httptest.NewRecorder()
	processHandler(wMissing, reqMissing)
	assert.Equal(t, http.StatusBadRequest, wMissing.Code)
}
